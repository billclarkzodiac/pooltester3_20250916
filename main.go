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
    <meta charset="UTF-8">
    <title>NgaSim Pool Controller</title>
    <meta http-equiv="refresh" content="10">
    <style>
        body { font-family: Arial; background: #667eea; margin: 0; padding: 20px; }
        .container { max-width: 1400px; margin: 0 auto; }
        .header { background: white; padding: 20px; border-radius: 10px; margin-bottom: 20px; text-align: center; }
        .control-panel { background: white; padding: 20px; border-radius: 10px; margin-bottom: 20px; }
        .controls-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
        .control-group { padding: 15px; border: 1px solid #e0e0e0; border-radius: 8px; }
        .control-group h3 { margin-top: 0; color: #333; }
        .slider-container { margin: 15px 0; }
        .slider { width: 100%; height: 8px; border-radius: 5px; background: #ddd; outline: none; }
        .slider::-webkit-slider-thumb { appearance: none; width: 20px; height: 20px; border-radius: 50%; background: #3B82F6; cursor: pointer; }
        .slider::-moz-range-thumb { width: 20px; height: 20px; border-radius: 50%; background: #3B82F6; cursor: pointer; border: none; }
        .slider-value { font-weight: bold; color: #3B82F6; font-size: 1.2em; }
        .button-group { display: flex; gap: 10px; margin-top: 10px; }
        .btn { padding: 8px 16px; border: none; border-radius: 5px; cursor: pointer; font-weight: bold; transition: all 0.3s; }
        .btn-primary { background: #3B82F6; color: white; }
        .btn-success { background: #10B981; color: white; }
        .btn-warning { background: #F59E0B; color: white; }
        .btn-danger { background: #EF4444; color: white; }
        .btn:hover { transform: translateY(-2px); box-shadow: 0 4px 8px rgba(0,0,0,0.2); }
        .status-indicator { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 8px; }
        .status-online { background: #10B981; }
        .status-offline { background: #EF4444; }
        .devices { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
        .device { background: white; padding: 20px; border-radius: 10px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        .device-header { display: flex; justify-content: space-between; margin-bottom: 15px; }
        .device-type { color: white; padding: 5px 10px; border-radius: 5px; font-size: 0.9em; }
        .device-type.VSP { background: #3B82F6; }
        .device-type.Sanitizer { background: #10B981; }
        .device-type.sanitizerGen2 { background: #10B981; }
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
        .device-controls { margin-top: 15px; padding: 10px; background: #f8f9fa; border-radius: 5px; }
        .hidden { display: none; }
        .command-pending { animation: pulse 2s infinite; }
        @keyframes pulse {
            0% { opacity: 1; }
            50% { opacity: 0.7; }
            100% { opacity: 1; }
        }
    </style>
    <script>
        let autoRefresh = true;
        let commandRepeatRate = 30; // seconds
        let topologyRate = 60; // seconds
        
        function toggleAutoRefresh() {
            autoRefresh = !autoRefresh;
            const btn = document.getElementById('refreshBtn');
            if (autoRefresh) {
                btn.textContent = 'Disable Auto-Refresh';
                btn.className = 'btn btn-warning';
                setTimeout(() => { if (autoRefresh) window.location.reload(); }, 10000);
            } else {
                btn.textContent = 'Enable Auto-Refresh';
                btn.className = 'btn btn-success';
            }
        }
        
        function updateSliderValue(sliderId, valueId) {
            const slider = document.getElementById(sliderId);
            const valueDisplay = document.getElementById(valueId);
            valueDisplay.textContent = slider.value + (sliderId.includes('Rate') ? 's' : '%');
            
            if (sliderId === 'commandRate') {
                commandRepeatRate = slider.value;
            } else if (sliderId === 'topologyRate') {
                topologyRate = slider.value;
            }
        }
        
        async function sendSanitizerCommand(serial, percentage) {
            try {
                const response = await fetch('/api/sanitizer/command', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ serial: serial, percentage: parseInt(percentage) })
                });
                const result = await response.json();
                
                if (result.success) {
                    showStatus('✅ Command sent: ' + result.message + ' - GUI will stay at ' + percentage + '%', 'success');
                    // Store successful command value to keep GUI consistent
                    localStorage.setItem('lastSuccessfulCommand', percentage);
                    
                    // Only refresh if user hasn't made another command recently (prevents override)
                    setTimeout(() => { 
                        const lastCommand = localStorage.getItem('lastCommandTime');
                        const timeDiff = Date.now() - parseInt(lastCommand || '0');
                        if (timeDiff > 2500) { // Only refresh if no recent commands
                            window.location.reload(); 
                        }
                    }, 3000);
                } else {
                    showStatus('❌ Command failed: ' + result.message, 'error');
                }
            } catch (error) {
                showStatus('❌ Network error: ' + error.message, 'error');
            }
        }
        
        function showStatus(message, type) {
            // Create or update status div
            let statusDiv = document.getElementById('statusMessage');
            if (!statusDiv) {
                statusDiv = document.createElement('div');
                statusDiv.id = 'statusMessage';
                document.querySelector('.header').appendChild(statusDiv);
            }
            
            statusDiv.textContent = message;
            statusDiv.style.background = type == 'success' ? '#10B981' : '#EF4444';
            statusDiv.style.color = 'white';
            statusDiv.style.padding = '10px';
            statusDiv.style.borderRadius = '5px';
            statusDiv.style.margin = '10px 0';
            statusDiv.style.display = 'block';
            
            // Auto-hide after 3 seconds
            setTimeout(() => { 
                statusDiv.style.display = 'none'; 
            }, 3000);
        }
        
        function highlightActiveButton(percentage) {
            // Reset all button styles
            const buttons = ['btn-0', 'btn-10', 'btn-50', 'btn-100', 'btn-101'];
            buttons.forEach(btnId => {
                const btn = document.getElementById(btnId);
                if (btn) {
                    btn.style.border = '';
                    btn.style.boxShadow = '';
                }
            });
            
            // Highlight the closest matching button
            let activeId = 'btn-50'; // Default
            if (percentage === 0) activeId = 'btn-0';
            else if (percentage <= 10) activeId = 'btn-10';
            else if (percentage <= 50) activeId = 'btn-50';
            else if (percentage <= 100) activeId = 'btn-100';
            else activeId = 'btn-101'; // BOOST for 101%+
            
            const activeBtn = document.getElementById(activeId);
            if (activeBtn) {
                activeBtn.style.border = '3px solid #FFD700';
                activeBtn.style.boxShadow = '0 0 10px #FFD700';
            }
        }
        
        function highlightActiveButtonGroup(buttonId, groupPrefix) {
            // Reset all buttons in the group
            const buttons = document.querySelectorAll('[id^="' + groupPrefix + '"]');
            buttons.forEach(btn => {
                btn.style.border = '';
                btn.style.boxShadow = '';
            });
            
            // Highlight the active button
            const activeBtn = document.getElementById(buttonId);
            if (activeBtn) {
                activeBtn.style.border = '3px solid #FFD700';
                activeBtn.style.boxShadow = '0 0 10px #FFD700';
            }
        }
        
        function setCommandRate(value, buttonId) {
            // Update slider and display
            document.getElementById('commandRate').value = value;
            updateSliderValue('commandRate', 'commandRateValue');
            
            // Highlight the pressed button
            highlightActiveButtonGroup(buttonId, 'cmd-');
            
            // Store for persistence
            localStorage.setItem('lastCommandRate', value);
            localStorage.setItem('lastCommandRateButton', buttonId);
            
            // Show confirmation
            showStatus('✅ Background command rate set to ' + value + 's', 'success');
        }
        
        function setTopologyRate(value, buttonId) {
            // Update slider and display  
            document.getElementById('topologyRate').value = value;
            updateSliderValue('topologyRate', 'topologyRateValue');
            
            // Highlight the pressed button
            highlightActiveButtonGroup(buttonId, 'topo-');
            
            // Store for persistence
            localStorage.setItem('lastTopologyRate', value);
            localStorage.setItem('lastTopologyRateButton', buttonId);
            
            // Show confirmation
            showStatus('✅ Topology reporting rate set to ' + value + 's', 'success');
        }
        
        function setSanitizerPower(percentage) {
            // Get the active sanitizer device serial dynamically
            getActiveSanitizerSerial().then(serial => {
                // Update the main slider to reflect the command being sent
                document.getElementById('chlorinationSlider').value = percentage;
                updateSliderValue('chlorinationSlider', 'chlorinationValue');
                
                // Highlight the pressed button
                highlightActiveButton(percentage);
                
                // Store the last command value so it persists across page refreshes
                localStorage.setItem('lastChlorinationValue', percentage);
                localStorage.setItem('lastCommandTime', Date.now());
                
                // Send the command
                sendSanitizerCommand(serial, percentage);
            });
        }
        
        function syncSliderWithDeviceState() {
            // Use the API to get current device state instead of template rendering
            fetch('/api/devices')
                .then(response => response.json())
                .then(devices => {
                    let activeSanitizer = null;
                    
                    for (let device of devices) {
                        if (device.type === 'Sanitizer' || device.category === 'sanitizerGen2' || device.type === 'sanitizerGen2') {
                            activeSanitizer = device;
                            break;
                        }
                    }
                    
                    if (activeSanitizer) {
                        // Use device's actual percentage, or pending if command is in progress
                        let currentPercentage = activeSanitizer.percentage_output || activeSanitizer.actual_percentage || 0;
                        
                        // If there's a pending command, show the pending value instead (gives immediate UI feedback)
                        if (activeSanitizer.pending_percentage && activeSanitizer.pending_percentage !== 0) {
                            currentPercentage = activeSanitizer.pending_percentage;
                        }
                        
                        // Update slider and button highlights to match device state
                        document.getElementById('chlorinationSlider').value = currentPercentage;
                        updateSliderValue('chlorinationSlider', 'chlorinationValue');
                        highlightActiveButton(currentPercentage);
                        
                        console.log('Synced slider to device state: ' + currentPercentage + '%');
                        return currentPercentage;
                    }
                })
                .catch(error => {
                    console.log('Failed to fetch device state for sync: ' + error.message);
                });
            
            return null;
        }
        
        function getActiveSanitizerSerial() {
            // Use the API to get current device serial
            return fetch('/api/devices')
                .then(response => response.json())
                .then(devices => {
                    for (let device of devices) {
                        if (device.type === 'Sanitizer' || device.category === 'sanitizerGen2' || device.type === 'sanitizerGen2') {
                            return device.serial || device.id;
                        }
                    }
                    return '1234567890ABCDEF00'; // Fallback to hardcoded value
                })
                .catch(error => {
                    console.log('Failed to fetch device serial: ' + error.message);
                    return '1234567890ABCDEF00'; // Fallback on error
                });
        }
        
        window.onload = function() {
            // PRIORITY 1: Sync with actual device state (overrides localStorage)
            const devicePercentage = syncSliderWithDeviceState();
            
            // PRIORITY 2: Only use localStorage if no device state available
            if (devicePercentage === null) {
                const lastChlorinationValue = localStorage.getItem('lastChlorinationValue');
                if (lastChlorinationValue) {
                    document.getElementById('chlorinationSlider').value = lastChlorinationValue;
                    highlightActiveButton(parseInt(lastChlorinationValue));
                }
            }
            
            // Restore command rate state
            const lastCommandRate = localStorage.getItem('lastCommandRate');
            const lastCommandRateButton = localStorage.getItem('lastCommandRateButton');
            if (lastCommandRate) {
                document.getElementById('commandRate').value = lastCommandRate;
                if (lastCommandRateButton) {
                    highlightActiveButtonGroup(lastCommandRateButton, 'cmd-');
                }
            }
            
            // Restore topology rate state
            const lastTopologyRate = localStorage.getItem('lastTopologyRate');
            const lastTopologyRateButton = localStorage.getItem('lastTopologyRateButton');
            if (lastTopologyRate) {
                document.getElementById('topologyRate').value = lastTopologyRate;
                if (lastTopologyRateButton) {
                    highlightActiveButtonGroup(lastTopologyRateButton, 'topo-');
                }
            }
            
            // Initialize slider display values
            updateSliderValue('chlorinationSlider', 'chlorinationValue');
            updateSliderValue('commandRate', 'commandRateValue');
            updateSliderValue('topologyRate', 'topologyRateValue');
            
            // Set up auto-refresh (but respect recent commands)
            if (autoRefresh) {
                setTimeout(() => { 
                    const lastCommand = localStorage.getItem('lastCommandTime');
                    const timeDiff = Date.now() - parseInt(lastCommand || '0');
                    if (autoRefresh && timeDiff > 12000) { // Don't auto-refresh if recent commands
                        window.location.reload(); 
                    }
                }, 15000);
            }
        }
    </script>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>NgaSim Pool Controller v{{.Version}}</h1>
            <p>
                <span class="status-indicator status-online"></span>
                {{len .Devices}} devices discovered
                <button id="refreshBtn" class="btn btn-warning" onclick="toggleAutoRefresh()">Disable Auto-Refresh</button>
            </p>
            <div id="statusMessage"></div>
        </div>
        
        <div class="control-panel">
            <h2 style="margin-top: 0; color: #333;">System Controls</h2>
            <div class="controls-grid">
                <!-- Chlorination Power Control -->
                <div class="control-group">
                    <h3>🧪 Chlorination Power</h3>
                    <div class="slider-container">
                        <input type="range" min="0" max="101" value="50" class="slider" id="chlorinationSlider" 
                               oninput="updateSliderValue('chlorinationSlider', 'chlorinationValue')">
                        <div style="text-align: center; margin-top: 10px;">
                            <span class="slider-value" id="chlorinationValue">50%</span>
                        </div>
                        <div class="button-group" style="justify-content: center;">
                            <button class="btn btn-danger" id="btn-0" onclick="setSanitizerPower(0)">OFF</button>
                            <button class="btn btn-warning" id="btn-10" onclick="setSanitizerPower(10)">10%</button>
                            <button class="btn btn-primary" id="btn-50" onclick="setSanitizerPower(50)">50%</button>
                            <button class="btn btn-success" id="btn-100" onclick="setSanitizerPower(100)">100%</button>
                            <button class="btn btn-success" id="btn-101" onclick="setSanitizerPower(101)">BOOST</button>
                        </div>
                        <div style="text-align: center; margin-top: 10px;">
                            <button class="btn btn-primary" onclick="getActiveSanitizerSerial().then(serial => sendSanitizerCommand(serial, document.getElementById('chlorinationSlider').value))">
                                Send Command
                            </button>
                        </div>
                    </div>
                </div>
                
                <!-- Command Repeat Rate -->
                <div class="control-group">
                    <h3>⏱️ Background Command Rate</h3>
                    <div class="slider-container">
                        <input type="range" min="2" max="60" value="4" class="slider" id="commandRate" 
                               oninput="updateSliderValue('commandRate', 'commandRateValue')">
                        <div style="text-align: center; margin-top: 10px;">
                            <span class="slider-value" id="commandRateValue">4s</span>
                        </div>
                        <div class="button-group" style="justify-content: center;">
                            <button class="btn btn-primary" id="cmd-fast" onclick="setCommandRate(2, 'cmd-fast')">Fast</button>
                            <button class="btn btn-primary" id="cmd-normal" onclick="setCommandRate(4, 'cmd-normal')">Normal</button>
                            <button class="btn btn-primary" id="cmd-slow" onclick="setCommandRate(15, 'cmd-slow')">Slow</button>
                        </div>
                        <p style="font-size: 0.85em; color: #666; margin: 10px 0 0 0;">
                            Controls how often background commands are sent (Normal=4s avoids timeouts)
                        </p>
                    </div>
                </div>
                
                <!-- Topology Reporting Rate -->
                <div class="control-group">
                    <h3>📡 Topology Reporting Rate</h3>
                    <div class="slider-container">
                        <input type="range" min="5" max="300" value="10" class="slider" id="topologyRate" 
                               oninput="updateSliderValue('topologyRate', 'topologyRateValue')">
                        <div style="text-align: center; margin-top: 10px;">
                            <span class="slider-value" id="topologyRateValue">10s</span>
                        </div>
                        <div class="button-group" style="justify-content: center;">
                            <button class="btn btn-primary" id="topo-frequent" onclick="setTopologyRate(5, 'topo-frequent')">Frequent</button>
                            <button class="btn btn-primary" id="topo-normal" onclick="setTopologyRate(10, 'topo-normal')">Normal</button>
                            <button class="btn btn-primary" id="topo-infrequent" onclick="setTopologyRate(120, 'topo-infrequent')">Infrequent</button>
                        </div>
                        <p style="font-size: 0.85em; color: #666; margin: 10px 0 0 0;">
                            Device topology reporting rate (Normal=10s is preferred optimal rate)
                        </p>
                    </div>
                </div>
                
                <!-- System Status -->
                <div class="control-group">
                    <h3>🔧 System Status</h3>
                    <div style="font-size: 0.9em; color: #333;">
                        <div style="margin: 8px 0;">
                            <strong>MQTT Broker:</strong> 169.254.1.1:1883
                        </div>
                        <div style="margin: 8px 0;">
                            <strong>Web Server:</strong> localhost:8082
                        </div>
                        <div style="margin: 8px 0;">
                            <strong>Active Devices:</strong> {{len .Devices}}
                        </div>
                        <div style="margin: 8px 0;">
                            <strong>System Mode:</strong> 
                            {{if gt (len .Devices) 0}}
                                <span style="color: #10B981;">Live Hardware</span>
                            {{else}}
                                <span style="color: #F59E0B;">Demo Mode</span>
                            {{end}}
                        </div>
                    </div>
                </div>
            </div>
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
                            {{if and (ne .PendingPercentage 0) (ne .PendingPercentage .PercentageOutput)}}
                                <div class="metric-value current-output" data-serial="{{.Serial}}" style="color: #F59E0B;">
                                    {{.PendingPercentage}}% → {{.PercentageOutput}}%
                                </div>
                                <div class="metric-label" style="color: #F59E0B;">Command → Actual</div>
                            {{else}}
                                <div class="metric-value current-output" data-serial="{{.Serial}}">{{.PercentageOutput}}%</div>
                                <div class="metric-label">Current Output</div>
                            {{end}}
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
                
                {{if or (eq .Type "Sanitizer") (eq .Category "sanitizerGen2") (eq .Type "sanitizerGen2")}}
                <div class="device-controls">
                    <h4 style="margin: 0 0 10px 0; color: #333;">Quick Commands</h4>
                    <div class="button-group">
                        <button class="btn btn-danger" onclick="sendSanitizerCommand('{{.Serial}}', 0)" title="Turn off sanitizer">
                            OFF
                        </button>
                        <button class="btn btn-warning" onclick="sendSanitizerCommand('{{.Serial}}', 10)" title="Set to 10% power">
                            10%
                        </button>
                        <button class="btn btn-primary" onclick="sendSanitizerCommand('{{.Serial}}', 50)" title="Set to 50% power">
                            50%
                        </button>
                        <button class="btn btn-success" onclick="sendSanitizerCommand('{{.Serial}}', 100)" title="Set to 100% power">
                            100%
                        </button>
                        <button class="btn btn-success" onclick="sendSanitizerCommand('{{.Serial}}', 101)" title="Set to boost mode (101%)">
                            BOOST
                        </button>
                    </div>
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
