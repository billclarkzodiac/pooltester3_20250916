package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const NgaSimVersion = "2.1.0"

type Device struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	Serial   string    `json:"serial"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`

	// VSP fields
	RPM   int     `json:"rpm,omitempty"`
	Temp  float64 `json:"temperature,omitempty"`
	Power int     `json:"power,omitempty"`

	// Sanitizer fields
	PowerLevel  int     `json:"power_level,omitempty"`  // 0-101%
	Salinity    int     `json:"salinity,omitempty"`     // ppm
	CellTemp    float64 `json:"cell_temp,omitempty"`    // Celsius
	CellVoltage float64 `json:"cell_voltage,omitempty"` // Volts
	CellCurrent float64 `json:"cell_current,omitempty"` // Amps

	// ICL fields
	Red   int `json:"red,omitempty"`   // 0-255
	Green int `json:"green,omitempty"` // 0-255
	Blue  int `json:"blue,omitempty"`  // 0-255
	White int `json:"white,omitempty"` // 0-255

	// TruSense fields
	PH  float64 `json:"ph,omitempty"`  // pH level
	ORP int     `json:"orp,omitempty"` // mV

	// Heater/HeatPump fields
	SetTemp     float64 `json:"set_temp,omitempty"`     // Target temperature
	WaterTemp   float64 `json:"water_temp,omitempty"`   // Current water temp
	HeatingMode string  `json:"heating_mode,omitempty"` // OFF/HEAT/COOL
}

type NgaSim struct {
	devices   map[string]*Device
	mutex     sync.RWMutex
	mqtt      mqtt.Client
	server    *http.Server
	pollerCmd *exec.Cmd
}

// MQTT connection parameters
const (
	MQTTBroker   = "tcp://169.254.1.1:1883"
	MQTTClientID = "NgaSim-WebUI"
)

// MQTT Topics for device discovery
const (
	TopicAnnounce  = "devices/+/announce"
	TopicTelemetry = "devices/+/telemetry"
	TopicStatus    = "devices/+/status"
)

// connectMQTT initializes MQTT client and connects to broker
func (sim *NgaSim) connectMQTT() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(MQTTBroker)
	opts.SetClientID(MQTTClientID)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetKeepAlive(30 * time.Second)

	// Set connection lost handler
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
	})

	// Set connection handler
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("Connected to MQTT broker at", MQTTBroker)
		sim.subscribeToTopics()
	})

	sim.mqtt = mqtt.NewClient(opts)
	if token := sim.mqtt.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %v", token.Error())
	}

	return nil
}

// startPoller starts the C poller as a subprocess to wake up devices
func (sim *NgaSim) startPoller() error {
	log.Println("Starting C poller subprocess...")

	sim.pollerCmd = exec.Command("sudo", "./poller")

	// Start the poller in the background
	if err := sim.pollerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start poller: %v", err)
	}

	log.Printf("Started C poller with PID: %d", sim.pollerCmd.Process.Pid)

	// Monitor the poller process in a separate goroutine
	go func() {
		if err := sim.pollerCmd.Wait(); err != nil {
			log.Printf("Poller process exited with error: %v", err)
		} else {
			log.Println("Poller process exited cleanly")
		}
	}()

	return nil
}

// stopPoller stops the C poller subprocess
func (sim *NgaSim) stopPoller() {
	if sim.pollerCmd != nil && sim.pollerCmd.Process != nil {
		log.Printf("Stopping poller process (PID: %d)...", sim.pollerCmd.Process.Pid)
		if err := sim.pollerCmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill poller process: %v", err)
		}
	}
}

// subscribeToTopics subscribes to device announcement and telemetry topics
func (sim *NgaSim) subscribeToTopics() {
	topics := []string{TopicAnnounce, TopicTelemetry, TopicStatus}

	for _, topic := range topics {
		if token := sim.mqtt.Subscribe(topic, 1, sim.messageHandler); token.Wait() && token.Error() != nil {
			log.Printf("Failed to subscribe to topic %s: %v", topic, token.Error())
		} else {
			log.Printf("Subscribed to topic: %s", topic)
		}
	}
}

// messageHandler processes incoming MQTT messages
func (sim *NgaSim) messageHandler(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())

	log.Printf("Received MQTT message on topic: %s", topic)
	log.Printf("Payload: %s", payload)

	// Parse topic to extract device ID
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		log.Printf("Invalid topic format: %s", topic)
		return
	}

	deviceID := parts[1]
	messageType := parts[2]

	switch messageType {
	case "announce":
		sim.handleDeviceAnnounce(deviceID, payload)
	case "telemetry":
		sim.handleDeviceTelemetry(deviceID, payload)
	case "status":
		sim.handleDeviceStatus(deviceID, payload)
	default:
		log.Printf("Unknown message type: %s", messageType)
	}
}

// handleDeviceAnnounce processes device announcement messages
func (sim *NgaSim) handleDeviceAnnounce(deviceID, payload string) {
	log.Printf("Device announce from %s: %s", deviceID, payload)

	// Try to parse as JSON (fallback for non-protobuf devices)
	var announceData map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &announceData); err == nil {
		sim.updateDeviceFromAnnounce(deviceID, announceData)
		return
	}

	// TODO: Add protobuf parsing here
	log.Printf("Could not parse announce message as JSON, may be protobuf: %s", payload)
}

// updateDeviceFromAnnounce updates device from announcement data
func (sim *NgaSim) updateDeviceFromAnnounce(deviceID string, data map[string]interface{}) {
	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	device, exists := sim.devices[deviceID]
	if !exists {
		device = &Device{
			ID:       deviceID,
			Name:     fmt.Sprintf("Device-%s", deviceID),
			Status:   "DISCOVERED",
			LastSeen: time.Now(),
		}
		sim.devices[deviceID] = device
		log.Printf("New device discovered: %s", deviceID)
	}

	// Update device fields from announce data
	if deviceType, ok := data["type"].(string); ok {
		device.Type = deviceType
	}
	if name, ok := data["name"].(string); ok {
		device.Name = name
	}
	if serial, ok := data["serial"].(string); ok {
		device.Serial = serial
	}

	device.Status = "ONLINE"
	device.LastSeen = time.Now()

	log.Printf("Updated device %s: type=%s, name=%s", deviceID, device.Type, device.Name)
}

// handleDeviceTelemetry processes device telemetry messages
func (sim *NgaSim) handleDeviceTelemetry(deviceID, payload string) {
	log.Printf("Device telemetry from %s: %s", deviceID, payload)

	// Try to parse as JSON
	var telemetryData map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &telemetryData); err == nil {
		sim.updateDeviceFromTelemetry(deviceID, telemetryData)
		return
	}

	// TODO: Add protobuf parsing here
	log.Printf("Could not parse telemetry message as JSON, may be protobuf: %s", payload)
}

// updateDeviceFromTelemetry updates device with telemetry data
func (sim *NgaSim) updateDeviceFromTelemetry(deviceID string, data map[string]interface{}) {
	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	device, exists := sim.devices[deviceID]
	if !exists {
		log.Printf("Received telemetry for unknown device: %s", deviceID)
		return
	}

	// Update common fields
	if temp, ok := data["temperature"].(float64); ok {
		device.Temp = temp
	}
	if power, ok := data["power"].(float64); ok {
		device.Power = int(power)
	}

	// Update device-specific fields based on type
	switch device.Type {
	case "VSP":
		if rpm, ok := data["rpm"].(float64); ok {
			device.RPM = int(rpm)
		}
	case "Sanitizer":
		if salinity, ok := data["salinity"].(float64); ok {
			device.Salinity = int(salinity)
		}
		if output, ok := data["output"].(float64); ok {
			device.PowerLevel = int(output)
		}
	case "TruSense":
		if ph, ok := data["ph"].(float64); ok {
			device.PH = ph
		}
		if orp, ok := data["orp"].(float64); ok {
			device.ORP = int(orp)
		}
	case "ICL":
		if red, ok := data["red"].(float64); ok {
			device.Red = int(red)
		}
		if green, ok := data["green"].(float64); ok {
			device.Green = int(green)
		}
		if blue, ok := data["blue"].(float64); ok {
			device.Blue = int(blue)
		}
		if white, ok := data["white"].(float64); ok {
			device.White = int(white)
		}
	}

	device.LastSeen = time.Now()
	log.Printf("Updated telemetry for device %s", deviceID)
}

// handleDeviceStatus processes device status messages
func (sim *NgaSim) handleDeviceStatus(deviceID, payload string) {
	log.Printf("Device status from %s: %s", deviceID, payload)

	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	if device, exists := sim.devices[deviceID]; exists {
		// Simple status parsing - could be enhanced
		if strings.Contains(strings.ToUpper(payload), "OFFLINE") {
			device.Status = "OFFLINE"
		} else if strings.Contains(strings.ToUpper(payload), "READY") {
			device.Status = "ONLINE"
		}
		device.LastSeen = time.Now()
	}
}

func NewNgaSim() *NgaSim {
	return &NgaSim{
		devices: make(map[string]*Device),
	}
}

func (n *NgaSim) Start() error {
	log.Println("Starting NgaSim v" + NgaSimVersion)

	// Connect to MQTT broker
	log.Println("Connecting to MQTT broker...")
	if err := n.connectMQTT(); err != nil {
		log.Printf("MQTT connection failed: %v", err)
		log.Println("Falling back to demo mode...")
		n.createDemoDevices()
	} else {
		log.Println("MQTT connected successfully - waiting for device announcements...")

		// Start the C poller to wake up devices
		if err := n.startPoller(); err != nil {
			log.Printf("Failed to start poller: %v", err)
			log.Println("Device discovery may not work properly")
		}
	}

	// Start web server
	return n.startWebServer()
}

func (n *NgaSim) createDemoDevices() {
	n.mutex.Lock()

	// VSP - Variable Speed Pump
	n.devices["VSP001"] = &Device{
		ID:       "VSP001",
		Type:     "VSP",
		Name:     "Pool Pump",
		Serial:   "VSP001",
		Status:   "ONLINE",
		LastSeen: time.Now(),
		RPM:      1800,
		Temp:     22.5,
		Power:    1200,
	}

	// Sanitizer - Salt chlorine generator
	n.devices["SALT001"] = &Device{
		ID:          "SALT001",
		Type:        "Sanitizer",
		Name:        "Salt Chlorinator",
		Serial:      "SALT001",
		Status:      "ONLINE",
		LastSeen:    time.Now(),
		PowerLevel:  75,   // 75%
		Salinity:    3200, // ppm
		CellTemp:    28.5, // Celsius
		CellVoltage: 4.2,  // Volts
		CellCurrent: 8.5,  // Amps
		Temp:        28.5, // PIB temperature
	}

	// ICL - Infinite Color Light
	n.devices["ICL001"] = &Device{
		ID:       "ICL001",
		Type:     "ICL",
		Name:     "Pool Lights",
		Serial:   "ICL001",
		Status:   "ONLINE",
		LastSeen: time.Now(),
		Red:      128,
		Green:    200,
		Blue:     255,
		White:    100,
		Temp:     24.0, // Controller temperature
	}

	// TruSense - pH and ORP sensors
	n.devices["TRUS001"] = &Device{
		ID:       "TRUS001",
		Type:     "TruSense",
		Name:     "Water Sensors",
		Serial:   "TRUS001",
		Status:   "ONLINE",
		LastSeen: time.Now(),
		PH:       7.2,  // pH level
		ORP:      650,  // mV
		Temp:     25.8, // Water temperature
	}

	// Heater - Gas heater
	n.devices["HEAT001"] = &Device{
		ID:          "HEAT001",
		Type:        "Heater",
		Name:        "Gas Heater",
		Serial:      "HEAT001",
		Status:      "ONLINE",
		LastSeen:    time.Now(),
		SetTemp:     28.0, // Target temperature
		WaterTemp:   25.8, // Current water temp
		HeatingMode: "HEAT",
		Temp:        45.2, // Heater internal temp
	}

	// HeatPump - Heat pump heater/chiller
	n.devices["HP001"] = &Device{
		ID:          "HP001",
		Type:        "HeatPump",
		Name:        "Heat Pump",
		Serial:      "HP001",
		Status:      "ONLINE",
		LastSeen:    time.Now(),
		SetTemp:     26.0, // Target temperature
		WaterTemp:   25.8, // Current water temp
		HeatingMode: "OFF",
		Temp:        22.1, // Ambient air temp
		Power:       2400, // Watts
	}

	// ORION - Another sanitation controller
	n.devices["ORION001"] = &Device{
		ID:       "ORION001",
		Type:     "ORION",
		Name:     "ORION Sanitizer",
		Serial:   "ORION001",
		Status:   "ONLINE",
		LastSeen: time.Now(),
		Temp:     26.5, // Controller temperature
	}

	n.mutex.Unlock()
}

func (n *NgaSim) startWebServer() error {
	// Start web server
	mux := http.NewServeMux()
	mux.HandleFunc("/", n.handleHome)
	mux.HandleFunc("/api/devices", n.handleAPI)

	n.server = &http.Server{Addr: ":8080", Handler: mux}

	go func() {
		log.Println("Web server starting on :8080")
		if err := n.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	return nil
}

var tmpl = template.Must(template.New("home").Parse(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>NgaSim Pool Controller</title>
    <meta http-equiv="refresh" content="10">
    <style>
        body { font-family: Arial; background: #667eea; margin: 0; padding: 20px; }
        .container { max-width: 1200px; margin: 0 auto; }
        .header { background: white; padding: 20px; border-radius: 10px; margin-bottom: 20px; text-align: center; }
        .devices { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
        .device { background: white; padding: 20px; border-radius: 10px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        .device-header { display: flex; justify-content: space-between; margin-bottom: 15px; }
        .device-type { color: white; padding: 5px 10px; border-radius: 5px; font-size: 0.9em; }
        .device-type.VSP { background: #3B82F6; }
        .device-type.Sanitizer { background: #10B981; }
        .device-type.ICL { background: #8B5CF6; }
        .device-type.TruSense { background: #F59E0B; }
        .device-type.Heater { background: #EF4444; }
        .device-type.HeatPump { background: #06B6D4; }
        .device-type.ORION { background: #6B7280; }
        .device-status { background: #10B981; color: white; padding: 3px 8px; border-radius: 3px; font-size: 0.8em; }
        .metrics { display: grid; grid-template-columns: repeat(3, 1fr); gap: 15px; margin-top: 15px; }
        .metric { text-align: center; }
        .metric-value { font-size: 1.3em; font-weight: bold; color: #3B82F6; }
        .metric-label { font-size: 0.8em; color: #666; margin-top: 5px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>NgaSim Pool Controller v{{.Version}}</h1>
            <p>{{len .Devices}} devices discovered</p>
        </div>
        <div class="devices">
            {{range .Devices}}
            <div class="device">
                <div class="device-header">
                    <div class="device-type {{.Type}}">{{.Type}}</div>
                    <div class="device-status">{{.Status}}</div>
                </div>
                <h3>{{.Name}}</h3>
                <p>Serial: {{.Serial}}</p>
                <div class="metrics">
                    {{if eq .Type "VSP"}}
                        <div class="metric">
                            <div class="metric-value">{{.RPM}}</div>
                            <div class="metric-label">RPM</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .Temp}}</div>
                            <div class="metric-label">Temperature</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.Power}}W</div>
                            <div class="metric-label">Power</div>
                        </div>
                                        {{else if eq .Type "Sanitizer"}}
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .Temp}}</div>
                            <div class="metric-label">Temperature</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.Salinity}}ppm</div>
                            <div class="metric-label">Salinity</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.Output}}%</div>
                            <div class="metric-label">Output</div>
                        </div>
                    {{else if eq .Type "ICL"}}
                        <div class="metric">
                            <div class="metric-value" style="background: rgb({{.Red}},{{.Green}},{{.Blue}}); color: white; padding: 5px; border-radius: 3px;">RGB</div>
                            <div class="metric-label">Color</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.White}}</div>
                            <div class="metric-label">White Level</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .Temp}}</div>
                            <div class="metric-label">Controller Temp</div>
                        </div>
                    {{else if eq .Type "TruSense"}}
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1f" .PH}}</div>
                            <div class="metric-label">pH</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.ORP}}</div>
                            <div class="metric-label">ORP (mV)</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .Temp}}</div>
                            <div class="metric-label">Water Temp</div>
                        </div>
                    {{else if or (eq .Type "Heater") (eq .Type "HeatPump")}}
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .SetTemp}}</div>
                            <div class="metric-label">Set Temp</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .WaterTemp}}</div>
                            <div class="metric-label">Water Temp</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.HeatingMode}}</div>
                            <div class="metric-label">Mode</div>
                        </div>
                    {{else}}
                        <div class="metric">
                            <div class="metric-value">{{printf "%.1fC" .Temp}}</div>
                            <div class="metric-label">Temperature</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.Serial}}</div>
                            <div class="metric-label">Serial</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.Status}}</div>
                            <div class="metric-label">Status</div>
                        </div>
                    {{end}}
                </div>
                <p style="text-align: center; margin-top: 15px; font-size: 0.9em; color: #666;">
                    Last seen: {{.LastSeen.Format "15:04:05"}}
                </p>
            </div>
            {{end}}
        </div>
    </div>
</body>
</html>
`))

func (n *NgaSim) handleHome(w http.ResponseWriter, r *http.Request) {
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	data := struct {
		Devices []*Device
		Version string
	}{Devices: devices, Version: NgaSimVersion}

	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, data)
}

func (n *NgaSim) handleAPI(w http.ResponseWriter, r *http.Request) {
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

func main() {
	log.Println("=== NgaSim Pool Controller Simulator ===")

	nga := NewNgaSim()

	if err := nga.Start(); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	log.Println("NgaSim started successfully!")
	log.Println("Visit http://localhost:8080 to view the web interface")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down NgaSim...")

	// Stop the poller subprocess
	nga.stopPoller()
}
