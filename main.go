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

	"NgaSim/ned" // Import protobuf definitions

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"google.golang.org/protobuf/proto"
)

const NgaSimVersion = "2.1.1"

type Device struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	Serial   string    `json:"serial"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`

	// Additional protobuf fields for detailed device info
	ProductName     string `json:"product_name,omitempty"`
	Category        string `json:"category,omitempty"`
	ModelId         string `json:"model_id,omitempty"`
	ModelVersion    string `json:"model_version,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	OtaVersion      string `json:"ota_version,omitempty"`

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

	// Sanitizer-specific telemetry fields
	RSSI               int32 `json:"rssi,omitempty"`                  // Signal strength
	PPMSalt            int32 `json:"ppm_salt,omitempty"`              // Salt concentration in PPM
	PercentageOutput   int32 `json:"percentage_output,omitempty"`     // Current output percentage
	AccelerometerX     int32 `json:"accelerometer_x,omitempty"`       // X-axis tilt
	AccelerometerY     int32 `json:"accelerometer_y,omitempty"`       // Y-axis tilt
	AccelerometerZ     int32 `json:"accelerometer_z,omitempty"`       // Z-axis tilt
	LineInputVoltage   int32 `json:"line_input_voltage,omitempty"`    // Input voltage
	IsCellFlowReversed bool  `json:"is_cell_flow_reversed,omitempty"` // Flow direction
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
	TopicAnnounce  = "async/+/+/anc"
	TopicInfo      = "async/+/+/info"
	TopicTelemetry = "async/+/+/dt"
	TopicError     = "async/+/+/error" // Match Python example
	TopicStatus    = "async/+/+/sts"
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
	topics := []string{TopicAnnounce, TopicTelemetry, TopicStatus, TopicError}

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

	// Parse topic to extract device info
	// Topic format: async/category/serial/type
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		log.Printf("Invalid topic format: %s", topic)
		return
	}

	category := parts[1]
	deviceSerial := parts[2]
	messageType := parts[3]

	switch messageType {
	case "anc":
		sim.handleDeviceAnnounce(category, deviceSerial, msg.Payload())
	case "dt":
		sim.handleDeviceTelemetry(category, deviceSerial, msg.Payload())
	case "sts":
		sim.handleDeviceStatus(category, deviceSerial, msg.Payload())
	case "error":
		sim.handleDeviceError(category, deviceSerial, msg.Payload())
	default:
		log.Printf("Unknown message type: %s", messageType)
	}
}

// handleDeviceAnnounce processes device announcement messages
func (sim *NgaSim) handleDeviceAnnounce(category, deviceSerial string, payload []byte) {
	log.Printf("Device announce from %s (category: %s): %d bytes", deviceSerial, category, len(payload))

	// Try to parse as protobuf GetDeviceInformationResponsePayload
	announce := &ned.GetDeviceInformationResponsePayload{}
	if err := proto.Unmarshal(payload, announce); err == nil {
		// Log detailed protobuf message like Python reflect_message function
		log.Printf("=== Device Announcement (Protobuf) ===")
		log.Printf("product_name: %s", announce.GetProductName())
		log.Printf("serial_number: %s", announce.GetSerialNumber())
		log.Printf("category: %s", announce.GetCategory())
		log.Printf("model_id: %s", announce.GetModelId())
		log.Printf("model_version: %s", announce.GetModelVersion())
		log.Printf("firmware_version: %s", announce.GetFirmwareVersion())
		log.Printf("ota_version: %s", announce.GetOtaVersion())
		log.Printf("available_bus_types: %v", announce.GetAvailableBusTypes())
		if activeBus := announce.GetActiveBus(); activeBus != nil {
			log.Printf("active_bus: %+v", activeBus)
		}
		log.Printf("===========================================")

		sim.updateDeviceFromProtobufAnnounce(category, deviceSerial, announce)
		return
	} else {
		log.Printf("Failed to parse as protobuf GetDeviceInformationResponsePayload: %v", err)
	}

	// Fallback: try to parse as JSON
	var announceData map[string]interface{}
	if err := json.Unmarshal(payload, &announceData); err == nil {
		sim.updateDeviceFromJSONAnnounce(deviceSerial, announceData)
		return
	}

	log.Printf("Could not parse announce message from %s: %x", deviceSerial, payload)
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

// handleDeviceStatus processes device status messages
func (sim *NgaSim) handleDeviceStatus(category, deviceSerial string, payload []byte) {
	log.Printf("Device status from %s (category: %s): %d bytes", deviceSerial, category, len(payload))
	// TODO: Implement status message parsing
}

// handleDeviceError processes device error messages
func (sim *NgaSim) handleDeviceError(category, deviceSerial string, payload []byte) {
	log.Printf("Device error from %s (category: %s): %d bytes - %s", deviceSerial, category, len(payload), string(payload))
	// TODO: Implement error message parsing
}

// updateDeviceFromSanitizerTelemetry updates device with sanitizer telemetry data
func (sim *NgaSim) updateDeviceFromSanitizerTelemetry(deviceSerial string, telemetry *ned.TelemetryMessage) {
	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	device, exists := sim.devices[deviceSerial]
	if !exists {
		log.Printf("Received telemetry for unknown device: %s", deviceSerial)
		return
	}

	// Update sanitizer-specific telemetry fields
	device.RSSI = telemetry.GetRssi()
	device.PPMSalt = telemetry.GetPpmSalt()
	device.PercentageOutput = telemetry.GetPercentageOutput()
	device.AccelerometerX = telemetry.GetAccelerometerX()
	device.AccelerometerY = telemetry.GetAccelerometerY()
	device.AccelerometerZ = telemetry.GetAccelerometerZ()
	device.LineInputVoltage = telemetry.GetLineInputVoltage()
	device.IsCellFlowReversed = telemetry.GetIsCellFlowReversed()

	// Update legacy fields for compatibility
	device.Salinity = int(telemetry.GetPpmSalt())
	device.PowerLevel = int(telemetry.GetPercentageOutput())

	device.LastSeen = time.Now()
	log.Printf("Updated sanitizer telemetry for device %s: Salt=%dppm, Output=%d%%, RSSI=%ddBm",
		deviceSerial, device.PPMSalt, device.PercentageOutput, device.RSSI)
}

// handleDeviceTelemetry processes device telemetry messages
func (sim *NgaSim) handleDeviceTelemetry(category, deviceSerial string, payload []byte) {
	log.Printf("Device telemetry from %s (category: %s): %d bytes", deviceSerial, category, len(payload))

	// Try sanitizer-specific protobuf parsing for sanitizer devices
	if strings.Contains(strings.ToLower(category), "sanitizer") {
		telemetry := &ned.TelemetryMessage{}
		if err := proto.Unmarshal(payload, telemetry); err == nil {
			log.Printf("=== Sanitizer Telemetry (Protobuf) ===")
			log.Printf("rssi: %d dBm", telemetry.GetRssi())
			log.Printf("ppm_salt: %d ppm", telemetry.GetPpmSalt())
			log.Printf("percentage_output: %d%%", telemetry.GetPercentageOutput())
			log.Printf("accelerometer: x=%d, y=%d, z=%d", telemetry.GetAccelerometerX(), telemetry.GetAccelerometerY(), telemetry.GetAccelerometerZ())
			log.Printf("line_input_voltage: %d V", telemetry.GetLineInputVoltage())
			log.Printf("is_cell_flow_reversed: %t", telemetry.GetIsCellFlowReversed())
			log.Printf("========================================")

			sim.updateDeviceFromSanitizerTelemetry(deviceSerial, telemetry)
			return
		} else {
			log.Printf("Failed to parse as sanitizer TelemetryMessage: %v", err)
		}
	}

	// Try to parse as JSON (fallback)
	var telemetryData map[string]interface{}
	if err := json.Unmarshal(payload, &telemetryData); err == nil {
		sim.updateDeviceFromTelemetry(deviceSerial, telemetryData)
		return
	}

	log.Printf("Could not parse telemetry message: %x", payload)
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

// updateDeviceFromProtobufAnnounce updates device from protobuf announcement message
func (sim *NgaSim) updateDeviceFromProtobufAnnounce(category, deviceSerial string, announce *ned.GetDeviceInformationResponsePayload) {
	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	// Use the serial number from the protobuf message as the authoritative source
	serialFromMsg := announce.GetSerialNumber()
	if serialFromMsg != "" {
		deviceSerial = serialFromMsg
	}

	device, exists := sim.devices[deviceSerial]
	if !exists {
		device = &Device{
			ID:       deviceSerial,
			Serial:   deviceSerial,
			Name:     fmt.Sprintf("Device-%s", deviceSerial),
			Status:   "DISCOVERED",
			LastSeen: time.Now(),
		}
		sim.devices[deviceSerial] = device
		log.Printf("New device discovered via protobuf: %s", deviceSerial)
	}

	// Extract information from the protobuf message
	device.ProductName = announce.GetProductName()
	device.Category = announce.GetCategory()
	device.ModelId = announce.GetModelId()
	device.ModelVersion = announce.GetModelVersion()
	device.FirmwareVersion = announce.GetFirmwareVersion()
	device.OtaVersion = announce.GetOtaVersion()

	// Set display fields
	if device.ProductName != "" {
		device.Name = device.ProductName
	}
	if device.Category != "" {
		device.Type = device.Category
	} else {
		device.Type = category // Fallback to MQTT topic category
	}
	device.Serial = deviceSerial

	device.Status = "ONLINE"
	device.LastSeen = time.Now()

	log.Printf("Device %s fully updated: ProductName='%s', Category='%s', Model='%s', FirmwareVer='%s'",
		deviceSerial, device.ProductName, device.Category, device.ModelId, device.FirmwareVersion)
}

// updateDeviceFromJSONAnnounce updates device from JSON announcement (fallback)
func (sim *NgaSim) updateDeviceFromJSONAnnounce(deviceSerial string, data map[string]interface{}) {
	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	device, exists := sim.devices[deviceSerial]
	if !exists {
		device = &Device{
			ID:       deviceSerial,
			Serial:   deviceSerial,
			Name:     fmt.Sprintf("Device-%s", deviceSerial),
			Status:   "DISCOVERED",
			LastSeen: time.Now(),
		}
		sim.devices[deviceSerial] = device
		log.Printf("New device discovered via JSON: %s", deviceSerial)
	}

	// Update device fields from JSON announce data
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

	log.Printf("Updated device %s from JSON: type=%s, name=%s", deviceSerial, device.Type, device.Name)
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
                                                                {{else if or (eq .Type "Sanitizer") (eq .Category "sanitizerGen2") (eq .Type "sanitizerGen2")}}
                        <div class="metric">
                            <div class="metric-value">{{.PPMSalt}}ppm</div>
                            <div class="metric-label">Salt Level</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.PercentageOutput}}%</div>
                            <div class="metric-label">Output</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{{.RSSI}}dBm</div>
                            <div class="metric-label">Signal</div>
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
                {{if .ProductName}}
                <div style="margin-top: 15px; padding: 10px; background: #f8f9fa; border-radius: 5px; font-size: 0.85em;">
                    <div><strong>Product:</strong> {{.ProductName}}</div>
                    {{if .ModelId}}<div><strong>Model:</strong> {{.ModelId}} {{.ModelVersion}}</div>{{end}}
                    {{if .FirmwareVersion}}<div><strong>Firmware:</strong> {{.FirmwareVersion}}</div>{{end}}
                    {{if .OtaVersion}}<div><strong>OTA:</strong> {{.OtaVersion}}</div>{{end}}
                    {{if ne .LineInputVoltage 0}}<div><strong>Voltage:</strong> {{.LineInputVoltage}}V</div>{{end}}
                    {{if .IsCellFlowReversed}}<div><strong>Flow:</strong> <span style="color: orange;">Reversed</span></div>{{end}}
                </div>
                {{end}}
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
