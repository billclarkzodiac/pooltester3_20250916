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
	"github.com/google/uuid"
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
	PercentageOutput   int32 `json:"percentage_output"`               // Current output percentage (actual device state)
	AccelerometerX     int32 `json:"accelerometer_x,omitempty"`       // X-axis tilt
	AccelerometerY     int32 `json:"accelerometer_y,omitempty"`       // Y-axis tilt
	AccelerometerZ     int32 `json:"accelerometer_z,omitempty"`       // Z-axis tilt
	LineInputVoltage   int32 `json:"line_input_voltage,omitempty"`    // Input voltage
	IsCellFlowReversed bool  `json:"is_cell_flow_reversed,omitempty"` // Flow direction

	// Command state tracking fields
	PendingPercentage int32     `json:"pending_percentage"`          // What we asked device to do
	LastCommandTime   time.Time `json:"last_command_time,omitempty"` // When we sent the last command
	ActualPercentage  int32     `json:"actual_percentage"`           // Alias for PercentageOutput (for clarity)
}

type NgaSim struct {
	devices             map[string]*Device
	mutex               sync.RWMutex
	mqtt                mqtt.Client
	server              *http.Server
	pollerCmd           *exec.Cmd
	logger              *DeviceLogger
	registry            *ProtobufRegistry
	sanitizerController *SanitizerController
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
		// Wait for process to actually exit
		sim.pollerCmd.Wait()
		sim.pollerCmd = nil
	}
}

// cleanup performs comprehensive cleanup of all resources
func (sim *NgaSim) cleanup() {
	log.Println("Performing cleanup...")

	// Stop poller first
	sim.stopPoller()

	// Kill any orphaned poller processes
	sim.killOrphanedPollers()

	// Disconnect MQTT
	if sim.mqtt != nil && sim.mqtt.IsConnected() {
		log.Println("Disconnecting from MQTT...")
		sim.mqtt.Disconnect(1000)
	}

	// Close device logger
	if sim.logger != nil {
		log.Println("Closing device logger...")
		if err := sim.logger.Close(); err != nil {
			log.Printf("Error closing device logger: %v", err)
		}
	}

	log.Println("Cleanup completed")
}

// killOrphanedPollers kills any remaining poller processes
func (sim *NgaSim) killOrphanedPollers() {
	log.Println("Cleaning up any orphaned poller processes...")

	// Use pkill to kill any remaining poller processes
	cmd := exec.Command("sudo", "pkill", "-f", "./poller")
	if err := cmd.Run(); err != nil {
		// Don't log error if no processes found (expected case)
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() != 1 { // Exit code 1 means no processes found
				log.Printf("Warning: Failed to kill orphaned pollers: %v", err)
			}
		}
	} else {
		log.Println("Cleaned up orphaned poller processes")
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
		// Auto-create device from telemetry if it doesn't exist
		device = &Device{
			ID:       deviceSerial,
			Serial:   deviceSerial,
			Name:     fmt.Sprintf("Sanitizer-%s", deviceSerial),
			Type:     "sanitizerGen2",
			Category: "sanitizerGen2",
			Status:   "ONLINE",
			LastSeen: time.Now(),
		}
		sim.devices[deviceSerial] = device
		log.Printf("✅ Auto-created sanitizer device from telemetry: %s", deviceSerial)
	}

	// Update sanitizer-specific telemetry fields
	device.RSSI = telemetry.GetRssi()
	device.PPMSalt = telemetry.GetPpmSalt()
	device.PercentageOutput = telemetry.GetPercentageOutput()
	device.ActualPercentage = telemetry.GetPercentageOutput() // Keep both for clarity
	device.AccelerometerX = telemetry.GetAccelerometerX()
	device.AccelerometerY = telemetry.GetAccelerometerY()
	device.AccelerometerZ = telemetry.GetAccelerometerZ()
	device.LineInputVoltage = telemetry.GetLineInputVoltage()
	device.IsCellFlowReversed = telemetry.GetIsCellFlowReversed()

	// Check if pending command has been achieved
	if device.PendingPercentage != 0 && device.ActualPercentage == device.PendingPercentage {
		log.Printf("✅ Command achieved! %s: Pending %d%% = Actual %d%% (clearing pending state)",
			deviceSerial, device.PendingPercentage, device.ActualPercentage)
		device.PendingPercentage = 0
		device.LastCommandTime = time.Time{}
	} else if device.PendingPercentage != 0 {
		timeSinceCommand := time.Since(device.LastCommandTime)
		if timeSinceCommand > 30*time.Second {
			log.Printf("⏰ Command timeout: %s: Pending %d%% != Actual %d%% after %v (clearing pending)",
				deviceSerial, device.PendingPercentage, device.ActualPercentage, timeSinceCommand)
			device.PendingPercentage = 0
			device.LastCommandTime = time.Time{}
		} else {
			log.Printf("🔄 Command in progress: %s: Pending %d%% -> Actual %d%% (%.1fs ago)",
				deviceSerial, device.PendingPercentage, device.ActualPercentage, timeSinceCommand.Seconds())
		}
	}

	// Update legacy fields for compatibility
	device.Salinity = int(telemetry.GetPpmSalt())
	device.PowerLevel = int(telemetry.GetPercentageOutput())

	device.LastSeen = time.Now()
	log.Printf("Updated sanitizer telemetry for device %s: Salt=%dppm, Output=%d%%, RSSI=%ddBm",
		deviceSerial, device.PPMSalt, device.PercentageOutput, device.RSSI)

	// Update sanitizer controller state
	sim.sanitizerController.RegisterSanitizer(deviceSerial)
	sim.sanitizerController.UpdateFromTelemetry(deviceSerial, telemetry.GetPercentageOutput())
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
	// Create protobuf registry for message parsing
	registry := NewProtobufRegistry()

	// Create device logger for structured command logging
	logger, err := NewDeviceLogger(1000, "device_commands.log", registry)
	if err != nil {
		log.Printf("Warning: Failed to create device logger: %v", err)
		logger = nil
	} else {
		log.Println("Device logger initialized - commands will be logged to device_commands.log")
	}

	ngaSim := &NgaSim{
		devices:  make(map[string]*Device),
		logger:   logger,
		registry: registry,
	}

	// Initialize sanitizer controller
	ngaSim.sanitizerController = NewSanitizerController(ngaSim)

	return ngaSim
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

// sendSanitizerCommand sends a power level command to a sanitizer device
// Matches the Python script send_salt_command() functionality
func (sim *NgaSim) sendSanitizerCommand(deviceSerial, category string, targetPercentage int) error {
	// Create SetSanitizerTargetPercentageRequestPayload
	saltCmd := &ned.SetSanitizerTargetPercentageRequestPayload{
		TargetPercentage: int32(targetPercentage),
	}

	// Create SanitizerRequestPayloads wrapper
	wrapper := &ned.SanitizerRequestPayloads{
		RequestType: &ned.SanitizerRequestPayloads_SetSanitizerOutputPercentage{
			SetSanitizerOutputPercentage: saltCmd,
		},
	}

	// Update device pending state BEFORE sending command
	sim.mutex.Lock()
	device, exists := sim.devices[deviceSerial]
	if exists {
		device.PendingPercentage = int32(targetPercentage)
		device.LastCommandTime = time.Now()
		log.Printf("📝 Set pending state: %s -> %d%% (was %d%%)", deviceSerial, targetPercentage, device.PercentageOutput)
	}
	sim.mutex.Unlock()

	// Create command UUID for tracking
	cmdUuid := uuid.New().String()
	log.Printf("🔑 Command UUID: %s for %s -> %d%%", cmdUuid, deviceSerial, targetPercentage)

	// Debug: Verify the oneof field is properly set in sanitizer payload
	if setCmd, ok := wrapper.RequestType.(*ned.SanitizerRequestPayloads_SetSanitizerOutputPercentage); ok {
		log.Printf("✅ Sanitizer oneof field properly set - target: %d%%", setCmd.SetSanitizerOutputPercentage.TargetPercentage)
	} else {
		log.Printf("❌ ERROR: Sanitizer RequestType not properly set: %T", wrapper.RequestType)
		return fmt.Errorf("sanitizer RequestType not set properly")
	}

	// CRITICAL FIX: Create CommandRequestMessage wrapper to match Python structure
	// Python: msg = sanitizer_pb2.CommandRequestMessage()
	//         msg.command_uuid = uuid
	//         msg.sanitizer.CopyFrom(wrapper)
	// But Go ned.CommandRequestMessage doesn't have sanitizer field!

	// Create a custom message structure that matches what device expects
	// Since Go protobuf doesn't match Python, we need to create the bytes manually

	// Serialize the sanitizer payload first
	sanitizerBytes, err := proto.Marshal(wrapper)
	if err != nil {
		log.Printf("❌ ERROR: Failed to marshal sanitizer payload: %v", err)
		return fmt.Errorf("failed to marshal sanitizer payload: %v", err)
	}

	// Create a manual protobuf message with:
	// Field 1 (string): command_uuid
	// Field 3 (bytes): sanitizer payload (field 3 based on Python structure)
	// This mimics: CommandRequestMessage { command_uuid=..., sanitizer=... }

	var msgBuf []byte

	// Add field 1: command_uuid (string, wire type 2)
	// Tag = (1 << 3) | 2 = 10 (0x0A)
	uuidBytes := []byte(cmdUuid)
	msgBuf = append(msgBuf, 0x0A) // field 1, wire type 2 (length-delimited)
	msgBuf = append(msgBuf, byte(len(uuidBytes)))
	msgBuf = append(msgBuf, uuidBytes...)

	// Add field 3: sanitizer (message, wire type 2)
	// Tag = (3 << 3) | 2 = 26 (0x1A)
	msgBuf = append(msgBuf, 0x1A) // field 3, wire type 2 (length-delimited)
	msgBuf = append(msgBuf, byte(len(sanitizerBytes)))
	msgBuf = append(msgBuf, sanitizerBytes...)

	data := msgBuf
	log.Printf("🔧 Created manual CommandRequestMessage: UUID=%s, sanitizer_size=%d bytes", cmdUuid, len(sanitizerBytes))

	// Log the outgoing request with structured logging
	correlationID := ""
	if sim.logger != nil {
		correlationID = sim.logger.LogRequest(
			deviceSerial,
			"SetSanitizerTargetPercentage",
			data,
			"chlorination",
			fmt.Sprintf("target_%d_percent", targetPercentage),
			fmt.Sprintf("category_%s", category),
		)
	}

	log.Printf("Sending sanitizer command: %s -> %d%% (Correlation: %s)",
		deviceSerial, targetPercentage, correlationID)

	// Build topic following Python script: cmd/<category>/<serial>/req
	topic := fmt.Sprintf("cmd/%s/%s/req", category, deviceSerial)
	log.Printf("Publishing to MQTT topic: %s", topic)

	// Publish to MQTT
	token := sim.mqtt.Publish(topic, 1, false, data)
	token.Wait()

	if token.Error() != nil {
		// Log the MQTT error
		if sim.logger != nil {
			sim.logger.LogError(deviceSerial, "SetSanitizerTargetPercentage",
				fmt.Sprintf("MQTT publish failed: %v", token.Error()), correlationID,
				"chlorination", "mqtt_error")
		}
		return fmt.Errorf("failed to publish sanitizer command: %v", token.Error())
	}

	log.Printf("✅ Sanitizer command sent successfully: %s -> %d%% (Correlation: %s)",
		deviceSerial, targetPercentage, correlationID)

	log.Printf("📝 Structured logging: Check device_commands.log for detailed protobuf data")

	return nil
}

func (n *NgaSim) startWebServer() error {
	// Start web server
	mux := http.NewServeMux()
	mux.HandleFunc("/", n.handleHome)
	mux.HandleFunc("/api/exit", n.handleExit)
	mux.HandleFunc("/api/devices", n.handleAPI)
	mux.HandleFunc("/api/sanitizer/command", n.handleSanitizerCommand)
	mux.HandleFunc("/api/sanitizer/states", n.handleSanitizerStates)
	mux.HandleFunc("/api/power-levels", n.handlePowerLevels)
	mux.HandleFunc("/api/emergency-stop", n.handleEmergencyStop)

	n.server = &http.Server{Addr: ":8082", Handler: mux}

	go func() {
		log.Println("Web server starting on :8082")
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
  <meta charset="utf-8" />
  <title>NgaSim Dashboard</title>
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <style>
	:root{--bg:#f3f4f6;--card:#ffffff;--muted:#6b7280;--accent:#10b981;--accent-2:#3b82f6}
	body{margin:0;font-family:Arial,Helvetica,sans-serif;background:var(--bg);color:#111}
	.page{max-width:1200px;margin:18px auto;padding:0 20px}
	.header{background:#111827;color:#fff;padding:14px 20px;border-radius:6px;display:flex;align-items:center;justify-content:space-between}
	.hdr-title{font-size:18px;font-weight:600}
	.hdr-right{font-size:14px;color:#d1d5db}

	/* Grid */
	.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-top:18px}
	.card{background:var(--card);border:1px solid #e5e7eb;border-radius:8px;padding:14px;height:140px;box-shadow:0 1px 2px rgba(0,0,0,0.04);cursor:pointer}
	.card h4{margin:0 0 6px 0;font-size:14px}
	.card .meta{font-size:12px;color:var(--muted);margin-bottom:6px}
	.card .state{position:absolute;right:20px;top:22px;font-weight:700;color:var(--accent)}
	.card-wrapper{position:relative}

	/* Popup overlay */
	.overlay{position:fixed;left:0;top:0;width:100%;height:100%;display:none;align-items:center;justify-content:center;background:rgba(17,24,39,0.45);z-index:30}
	.popup{width:760px;background:#fff;border-radius:10px;padding:16px;box-shadow:0 8px 24px rgba(0,0,0,0.25);}
	.popup-head{display:flex;justify-content:space-between;align-items:center}
	.popup-head h3{margin:0;font-size:16px}
	.popup-body{display:flex;margin-top:12px;gap:16px}
	.left-panel{width:360px;background:#f9fafb;border-radius:6px;padding:12px}
	.gauge{width:100%;height:220px;display:flex;align-items:center;justify-content:center}
	.gauge .value{font-size:32px;font-weight:700;color:#111}
	.ppm-box{margin-top:12px;background:#fff;border-radius:6px;padding:8px;border:1px solid #e5e7eb;text-align:center}
	.right-panel{flex:1;display:flex;flex-direction:column;gap:10px}
	.controls{background:#f9fafb;border-radius:6px;padding:10px;border:1px solid #e5e7eb}
	.controls .btn-row{display:flex;gap:10px;margin-top:8px}
	.btn{padding:10px 12px;border-radius:6px;border:none;cursor:pointer;font-weight:600;color:#fff}
	.btn.off{background:#10b981}
	.btn.p10{background:#f59e0b}
	.btn.p50{background:#3b82f6}
	.btn.p100{background:#16a34a}
	.btn.boost{background:#ef4444}
	.btn.close{background:#ef4444;padding:6px 10px}
	.device-info{background:#fff;border-radius:6px;padding:10px;border:1px solid #e5e7eb}
	.small{font-size:13px;color:var(--muted)}

	@media(max-width:1000px){.grid{grid-template-columns:repeat(2,1fr)}.popup{width:92%}}
	@media(max-width:640px){.grid{grid-template-columns:1fr}.popup{width:95%}}
  </style>
  <script>
	// Minimal client: open popup for a device card and send commands
	let devices = [];
	function fetchDevices(){
	  fetch('/api/devices').then(r=>r.json()).then(d=>{devices=d;renderGrid(d)})
	}
	function renderGrid(devs){
	  const grid = document.getElementById('grid');
	  grid.innerHTML = '';
	  for(const dev of devs){
		const wrap = document.createElement('div'); wrap.className='card-wrapper';
		const card = document.createElement('div'); card.className='card';
		const title = document.createElement('h4'); title.textContent = dev.name || dev.id || dev.serial || 'Device';
		const meta = document.createElement('div'); meta.className='meta'; meta.textContent = (dev.type||dev.category||'').toUpperCase() + ' • Serial: ' + (dev.serial||dev.id||'')
		const state = document.createElement('div'); state.className='state'; state.textContent = dev.status||''
		card.appendChild(title); card.appendChild(meta); wrap.appendChild(card); wrap.appendChild(state);
		wrap.onclick = ()=>openPopup(dev);
		grid.appendChild(wrap);
	  }
	  document.getElementById('hdr-count').textContent = devs.length + ' devices';
	}

	function openPopup(dev){
	  const overlay = document.getElementById('overlay'); overlay.style.display='flex';
	  document.getElementById('popup-title').textContent = (dev.name||dev.serial||dev.id);
	  document.getElementById('gauge-value').textContent = (dev.percentage_output||dev.actual_percentage||0) + '%';
	  document.getElementById('ppm-value').textContent = (dev.ppm_salt||dev.ppmsalt||dev.salinity||'---');
	  document.getElementById('device-model').textContent = dev.model_id || dev.product_name || '';
	  document.getElementById('device-fw').textContent = dev.firmware_version || '';
	  document.getElementById('device-last').textContent = dev.last_seen?dev.last_seen:'-';
	  // store active serial on overlay for command buttons
	  overlay.dataset.serial = dev.serial||dev.id||'';
	}
	function closePopup(){document.getElementById('overlay').style.display='none'}

	function sendCmd(percent){
	  const overlay = document.getElementById('overlay'); const serial = overlay.dataset.serial || '';
	  if(!serial){alert('No device serial');return}
	  fetch('/api/sanitizer/command',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({serial:serial,percentage:percent})})
		.then(r=>r.json()).then(j=>{ if(j.success){ document.getElementById('gauge-value').textContent = percent + '%'; fetchDevices(); } else alert('Command failed:'+JSON.stringify(j)) })
		.catch(e=>alert('Network error:'+e.message))
	}

	window.addEventListener('load', ()=>{ fetchDevices(); setInterval(fetchDevices,10000) })

			function confirmExit(){
				if(!confirm('Really exit pool-controller and poller?')) return;
				fetch('/api/exit',{method:'POST'}).then(r=>r.json()).then(j=>{alert(j.message);}).catch(e=>alert('Exit request failed: '+e.message));
			}
  </script>
</head>
<body>
  <div class="page">
	<div class="header">
				<div class="hdr-title">NgaSim Dashboard v{{.Version}}</div>
				<div style="display:flex;align-items:center;gap:12px">
					<div class="hdr-right" id="hdr-count">{{len .Devices}} devices</div>
					<button id="exitBtn" style="background:#ef4444;color:#fff;border:none;padding:8px 12px;border-radius:6px;font-weight:700;cursor:pointer" onclick="confirmExit()">EXIT</button>
				</div>
	</div>

	<div id="grid" class="grid">
	  {{range .Devices}}
	  <div class="card-wrapper">
		<div class="card" onclick="openPopup({{printf "%#v" .}})">
		  <h4>{{.Name}}</h4>
		  <div class="meta">{{.Type}} • Serial: {{.Serial}}</div>
		</div>
		<div class="state">{{.Status}}</div>
	  </div>
	  {{end}}
	</div>
  </div>

  <!-- Overlay popup matching the SVG wireframe -->
  <div id="overlay" class="overlay">
	<div class="popup">
	  <div class="popup-head">
		<h3 id="popup-title">Sanitizer</h3>
		<button class="btn close" onclick="closePopup()">X</button>
	  </div>
	  <div class="popup-body">
		<div class="left-panel">
		  <div class="gauge"><div class="value" id="gauge-value">0%</div></div>
		  <div class="ppm-box"><div class="small">PPM</div><div style="font-size:22px;font-weight:700" id="ppm-value">---</div></div>
		</div>
		<div class="right-panel">
		  <div class="controls">
			<div class="small">Power Controls</div>
			<div class="btn-row">
			  <button class="btn off" onclick="sendCmd(0)">OFF</button>
			  <button class="btn p10" onclick="sendCmd(10)">10%</button>
			  <button class="btn p50" onclick="sendCmd(50)">50%</button>
			  <button class="btn p100" onclick="sendCmd(100)">100%</button>
			</div>
			<div style="margin-top:8px"><button class="btn boost" style="width:100%" onclick="sendCmd(101)">BOOST</button></div>
		  </div>
		  <div class="device-info">
			<div class="small">Device Info</div>
			<div>Model: <span id="device-model"></span></div>
			<div>FW: <span id="device-fw"></span></div>
			<div>Last Seen: <span id="device-last"></span></div>
		  </div>
		</div>
	  </div>
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

	// Sort devices by serial number for consistent display order
	for i := 0; i < len(devices)-1; i++ {
		for j := i + 1; j < len(devices); j++ {
			if devices[i].Serial > devices[j].Serial {
				devices[i], devices[j] = devices[j], devices[i]
			}
		}
	}

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

	// Sort devices by serial number for consistent API order
	for i := 0; i < len(devices)-1; i++ {
		for j := i + 1; j < len(devices); j++ {
			if devices[i].Serial > devices[j].Serial {
				devices[i], devices[j] = devices[j], devices[i]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// handleSanitizerCommand provides a web API to test sanitizer commands
func (n *NgaSim) handleSanitizerCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// Parse JSON request
	var req struct {
		Serial     string `json:"serial"`
		Percentage int    `json:"percentage"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate parameters
	if req.Serial == "" {
		http.Error(w, "Serial number required", http.StatusBadRequest)
		return
	}

	if req.Percentage < 0 || req.Percentage > 101 {
		http.Error(w, "Percentage must be 0-101", http.StatusBadRequest)
		return
	}

	// Find the device to get its category
	n.mutex.RLock()
	device := n.devices[req.Serial]
	n.mutex.RUnlock()

	if device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	// Send the command
	if err := n.sendSanitizerCommand(req.Serial, device.Category, req.Percentage); err != nil {
		http.Error(w, fmt.Sprintf("Command failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Return success response
	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Sanitizer command sent: %s -> %d%%", req.Serial, req.Percentage),
		"device":  device.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleExit performs a graceful cleanup and then exits the process.
func (n *NgaSim) handleExit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// Respond quickly to the client
	resp := map[string]interface{}{"success": true, "message": "Shutting down - cleaning up processes"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	// Run cleanup in a goroutine then exit after a short delay so response can complete
	go func() {
		log.Println("Exit requested via /api/exit - starting cleanup")
		n.cleanup()
		time.Sleep(500 * time.Millisecond)
		log.Println("Exiting process now")
		os.Exit(0)
	}()
}

// handleSanitizerStates returns all sanitizer states from the controller
func (n *NgaSim) handleSanitizerStates(w http.ResponseWriter, r *http.Request) {
	states := n.sanitizerController.GetAllStates()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(states)
}

// handlePowerLevels returns valid power level definitions
func (n *NgaSim) handlePowerLevels(w http.ResponseWriter, r *http.Request) {
	levels := GetValidPowerLevels()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(levels)
}

// handleEmergencyStop stops all sanitizers immediately
func (n *NgaSim) handleEmergencyStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	states := n.sanitizerController.GetAllStates()
	count := 0
	for serial := range states {
		cmd := SanitizerCommand{
			Serial:    serial,
			Action:    "emergency_stop",
			ClientID:  "emergency",
			Timestamp: time.Now(),
		}
		if err := n.sanitizerController.QueueCommand(cmd); err == nil {
			count++
		}
	}

	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Emergency stop sent to %d sanitizers", count),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	log.Println("=== NgaSim Pool Controller Simulator ===")

	nga := NewNgaSim()

	// Ensure cleanup happens on any exit
	defer func() {
		log.Println("Shutting down NgaSim...")
		nga.cleanup()
	}()

	if err := nga.Start(); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	log.Println("NgaSim started successfully!")
	log.Println("Visit http://localhost:8082 to view the web interface")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	<-sigChan

	// The defer function will handle cleanup
}
