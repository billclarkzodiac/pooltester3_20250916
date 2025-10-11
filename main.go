// NgaSim Pool Controller - Main application entry point
// This file contains the main NgaSim pool controller simulator
// that provides:
// - MQTT communication with pool devices
// - Web-based device management interface
// - Protobuf-based command processing
// - Real-time device telemetry handling
// - Demo mode for testing without hardware

package main

import (
	"encoding/json"
	"fmt"
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

// Current version of the NgaSim application
// This version string is displayed in the web interface and logs
const NgaSimVersion = "2.1.3"

// UISpec represents TOML UI Specification structure for dynamic interface generation.
// It contains metadata and configuration for generating device-specific user interfaces.
type UISpec struct {
	Meta             MetaInfo          `toml:"meta"`               // Application metadata
	Dashboard        Dashboard         `toml:"dashboard"`          // Dashboard configuration
	DeviceTypes      []DeviceType      `toml:"device_type"`        // Supported device types
	TelemetryConfig  TelemetryConfig   `toml:"telemetry_config"`   // Telemetry settings
	DeviceInfoFields []DeviceInfoField `toml:"device_info_fields"` // Device information fields
}

// MetaInfo holds application metadata information
type MetaInfo struct {
	Title   string `toml:"title"`   // Application title
	Version string `toml:"version"` // Application version
	Author  string `toml:"author"`  // Application author
}

// Dashboard holds configuration for dashboard display
type Dashboard struct {
	Name            string          `toml:"name"`             // Dashboard name
	ShowOnline      bool            `toml:"show_online"`      // Show online devices
	ShowOffline     bool            `toml:"show_offline"`     // Show offline devices
	DashboardFields []string        `toml:"dashboard_fields"` // Fields to display
	Visual          DashboardVisual `toml:"visual"`           // Visual styling options
}

// DashboardVisual holds visual styling configuration for the dashboard
type DashboardVisual struct {
	View            string `toml:"view"`              // View type (grid, list, etc.)
	OnlineColor     string `toml:"online_color"`      // Color for online devices
	OfflineColor    string `toml:"offline_color"`     // Color for offline devices
	ErrorFlashColor string `toml:"error_flash_color"` // Color for error states
}

// DeviceType holds device type configuration with associated widgets
type DeviceType struct {
	Name     string   `toml:"name"`     // Device type name
	Short    string   `toml:"short"`    // Short display name
	Category string   `toml:"category"` // Device category
	Widgets  []Widget `toml:"widget"`   // UI widgets for this device type
}

// Widget holds UI widget configuration for device controls
type Widget struct {
	ID          string                 `toml:"id"`                    // Widget unique identifier
	Type        string                 `toml:"type"`                  // Widget type (slider, button, etc.)
	Label       string                 `toml:"label"`                 // Display label
	Description string                 `toml:"description,omitempty"` // Optional description
	Properties  map[string]interface{} `toml:"properties,omitempty"`  // Widget-specific properties
	Digits      int                    `toml:"digits,omitempty"`      // Number of decimal digits
	States      []string               `toml:"states,omitempty"`      // Possible states for state widgets
	Visual      map[string]interface{} `toml:"visual,omitempty"`      // Visual styling options
	Channels    []string               `toml:"channels,omitempty"`    // Color channels for color widgets
	Range       []int                  `toml:"range,omitempty"`       // Value range [min, max]
	// Additional fields for various widget types
	FallbackWhenWaiting string   `toml:"fallback_when_waiting,omitempty"` // Fallback value when waiting
	FallbackWhenOff     string   `toml:"fallback_when_off,omitempty"`     // Fallback value when off
	FallbackWhenMissing string   `toml:"fallback_when_missing,omitempty"` // Fallback value when missing
	FlashingWhenRamping bool     `toml:"flashing_when_ramping,omitempty"` // Flash during value changes
	PossibleValues      []string `toml:"possible_values,omitempty"`       // List of possible values
}

// TelemetryConfig holds configuration for telemetry data handling
type TelemetryConfig struct {
	Label  string `toml:"label"`  // Telemetry label
	Widget string `toml:"widget"` // Associated widget type
	Notes  string `toml:"notes"`  // Configuration notes
}

// DeviceInfoField holds device information field configuration
type DeviceInfoField struct {
	Name     string `toml:"name"`                // Field name
	Type     string `toml:"type"`                // Field data type
	ItemType string `toml:"item_type,omitempty"` // Item type for arrays
}

// NgaSim is the main NgaSim application structure
// It is the central coordinator for MQTT communication, web server, and device management
type NgaSim struct {
	devices             map[string]*Device
	mutex               sync.RWMutex
	mqtt                mqtt.Client
	server              *http.Server
	pollerCmd           *exec.Cmd
	logger              *DeviceLogger
	commandRegistry     *ProtobufCommandRegistry
	sanitizerController *SanitizerController

	// New fields for dynamic protobuf system
	reflectionEngine *ProtobufReflectionEngine // Dynamic protobuf discovery
	terminalLogger   *TerminalLogger           // Terminal display with file tee
	popupGenerator   *PopupUIGenerator         // Dynamic popup UI generator
}

// MQTT connection parameters
const (
	MQTTBroker   = "tcp://169.254.1.1:1883" ///< MQTT broker address
	MQTTClientID = "NgaSim-WebUI"           ///< MQTT client identifier
)

// MQTT Topics for device discovery
const (
	TopicAnnounce  = "async/+/+/anc"   ///< Device announcement topic pattern
	TopicInfo      = "async/+/+/info"  ///< Device information topic pattern
	TopicTelemetry = "async/+/+/dt"    ///< Device telemetry topic pattern
	TopicError     = "async/+/+/error" ///< Device error topic pattern
	TopicStatus    = "async/+/+/sts"   ///< Device status topic pattern
)

// Connects to the MQTT broker and sets up message handlers
// - Configures MQTT client options (broker, client ID, timeouts)
// - Sets up connection lost and reconnection handlers
// - Establishes connection to the MQTT broker
// - Automatically subscribes to device topics on successful connection
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

// Starts the C poller subprocess to wake up pool devices
// The C poller sends broadcast packets to wake up pool devices,
// enabling them to announce themselves and send telemetry data.
// The poller runs as a separate subprocess with sudo privileges.
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

// Gracefully stops the C poller subprocess
// Attempts graceful shutdown with SIGTERM first, then
// forces termination with SIGKILL if necessary. Also cleans up
// any orphaned poller processes.
func (sim *NgaSim) stopPoller() {
	if sim.pollerCmd != nil && sim.pollerCmd.Process != nil {
		log.Printf("Stopping poller process (PID: %d)...", sim.pollerCmd.Process.Pid)

		// Try graceful termination first
		if err := sim.pollerCmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("Failed to send SIGTERM to poller: %v", err)
		} else {
			log.Println("Sent SIGTERM to poller, waiting 2 seconds...")

			// Wait up to 2 seconds for graceful shutdown
			done := make(chan error, 1)
			go func() {
				done <- sim.pollerCmd.Wait()
			}()

			select {
			case err := <-done:
				if err != nil {
					log.Printf("Poller exited with error: %v", err)
				} else {
					log.Println("Poller exited gracefully")
				}
				sim.pollerCmd = nil
				return
			case <-time.After(2 * time.Second):
				log.Println("Poller didn't respond to SIGTERM, using SIGKILL...")
			}
		}

		// Force kill if graceful didn't work
		if err := sim.pollerCmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill poller process: %v", err)
		} else {
			log.Println("Force killed poller process")
		}

		// Wait for process to actually exit
		sim.pollerCmd.Wait()
		sim.pollerCmd = nil
	}

	// Additional cleanup - kill any remaining poller processes by name
	sim.killOrphanedPollers()
}

// Performs comprehensive cleanup of all NgaSim resources
// Called during application shutdown to ensure:
// - Poller subprocess is terminated
// - MQTT connection is properly closed
// - Device logger is cleaned up
// - All orphaned processes are killed
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
		} else {
			log.Printf("Device logger closed successfully")
		}
	}

	log.Println("Cleanup completed")
}

// Kills any orphaned poller processes that may be running
// Uses multiple kill strategies (pkill, killall) with escalating
// force levels to ensure all poller processes are terminated.
func (sim *NgaSim) killOrphanedPollers() {
	log.Println("Cleaning up any orphaned poller processes...")

	// First try to kill by process name (more aggressive)
	commands := [][]string{
		{"sudo", "pkill", "-f", "poller"},
		{"sudo", "pkill", "-9", "-f", "poller"},
		{"sudo", "killall", "poller"},
		{"sudo", "killall", "-9", "poller"},
	}

	for _, cmd := range commands {
		log.Printf("Running: %s", strings.Join(cmd, " "))
		execCmd := exec.Command(cmd[0], cmd[1:]...)
		if err := execCmd.Run(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				if exitError.ExitCode() == 1 {
					// Exit code 1 means no processes found - this is good
					log.Printf("No %s processes found (good)", cmd[len(cmd)-1])
					break // If no processes found, we're done
				}
			}
			log.Printf("Command failed: %v", err)
		} else {
			log.Printf("Successfully killed poller processes with: %s", strings.Join(cmd, " "))
			break // Success, no need to try more aggressive methods
		}

		// Small delay between attempts
		time.Sleep(500 * time.Millisecond)
	}

	// Verify cleanup by checking for remaining processes
	checkCmd := exec.Command("pgrep", "-f", "poller")
	if output, err := checkCmd.Output(); err != nil {
		// pgrep returns error if no processes found - this is what we want
		log.Println("✅ No poller processes remain")
	} else {
		log.Printf("⚠️ Warning: Some poller processes may still be running: %s", string(output))
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

// handleDeviceTelemetry processes device telemetry messages
func (sim *NgaSim) handleDeviceTelemetry(category, deviceSerial string, payload []byte) {
	log.Printf("Device telemetry from %s (category: %s): %d bytes", deviceSerial, category, len(payload))

	// Try to parse as sanitizer telemetry first
	if category == "sanitizerGen2" {
		telemetry := &ned.TelemetryMessage{}
		if err := proto.Unmarshal(payload, telemetry); err == nil {
			log.Printf("======== Sanitizer Telemetry ========")
			log.Printf("serial: %s", deviceSerial)
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
		}
	}

	device.Status = "ONLINE"
	device.LastSeen = time.Now()
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

// NewNgaSim creates a new NgaSim instance with all components
func NewNgaSim() *NgaSim {
	// Create reflection engine
	reflectionEngine := NewProtobufReflectionEngine()

	// Discover all protobuf messages at startup
	if err := reflectionEngine.DiscoverMessages(); err != nil {
		log.Printf("Warning: Protobuf discovery failed: %v", err)
	}

	// Create terminal logger with file tee
	terminalLogger, err := NewTerminalLogger("ngasim_terminal.log", 1000)
	if err != nil {
		log.Printf("Warning: Terminal logger creation failed: %v", err)
		terminalLogger = nil
	}

	ngaSim := &NgaSim{
		devices:          make(map[string]*Device),
		logger:           NewDeviceLogger(1000),
		commandRegistry:  NewProtobufCommandRegistry(),
		reflectionEngine: reflectionEngine,
		terminalLogger:   terminalLogger,
	}

	// Create popup generator
	if terminalLogger != nil {
		ngaSim.popupGenerator = NewPopupUIGenerator(reflectionEngine, terminalLogger, ngaSim)
	}

	// Initialize sanitizer controller
	ngaSim.sanitizerController = NewSanitizerController(ngaSim)

	// Discover commands
	ngaSim.commandRegistry.discoverCommands()

	return ngaSim
}

// Starts the NgaSim application and all its components
// Initializes:
// - MQTT connection (with fallback to demo mode)
// - C poller subprocess for device discovery
// - HTTP web server with all route handlers
// - Device command registry and logging
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

	// Start web server with ALL route handlers
	mux := http.NewServeMux()

	// Main interface routes
	mux.HandleFunc("/", n.handleGoDemo)         // Go-centric single driver approach
	mux.HandleFunc("/js-demo", n.handleDemo)    // JS-centric demo page
	mux.HandleFunc("/old", n.handleHome)        // Original home page
	mux.HandleFunc("/demo", n.handleDemo)       // Demo page alias
	mux.HandleFunc("/goodbye", n.handleGoodbye) // Goodbye page

	// API routes
	mux.HandleFunc("/api/exit", n.handleExit)                          // Application exit
	mux.HandleFunc("/api/devices", n.handleAPI)                        // Device list API
	mux.HandleFunc("/api/sanitizer/command", n.handleSanitizerCommand) // Sanitizer commands
	mux.HandleFunc("/api/sanitizer/states", n.handleSanitizerStates)   // Sanitizer states
	mux.HandleFunc("/api/power-levels", n.handlePowerLevels)           // Power level options
	mux.HandleFunc("/api/emergency-stop", n.handleEmergencyStop)       // Emergency stop all
	mux.HandleFunc("/api/ui/spec", n.handleUISpecAPI)                  // UI specification

	// Device command API routes
	mux.HandleFunc("/api/device-commands/", n.handleDeviceCommands)   // Commands by category
	mux.HandleFunc("/api/device-commands", n.handleAllDeviceCommands) // All commands

	// Static asset routes
	mux.HandleFunc("/static/wireframe.svg", n.handleWireframeSVG) // SVG wireframe
	mux.HandleFunc("/static/wireframe.mmd", n.handleWireframeMMD) // Mermaid diagram
	mux.HandleFunc("/static/ui-spec.toml", n.handleUISpecTOML)    // TOML specification
	mux.HandleFunc("/static/ui-spec.txt", n.handleUISpecTXT)      // Text specification

	// Protobuf interface routes
	mux.HandleFunc("/protobuf", n.handleProtobufMessages) // Protobuf message interface
	mux.HandleFunc("/terminal", n.handleTerminalView)     // Live terminal view

	// Protobuf API routes
	if n.popupGenerator != nil {
		mux.HandleFunc("/api/protobuf/popup", n.popupGenerator.handleProtobufPopup)     // Generate popup
		mux.HandleFunc("/api/protobuf/command", n.popupGenerator.handleProtobufCommand) // Execute command
		mux.HandleFunc("/api/protobuf/messages", n.popupGenerator.handleMessageTypes)   // List messages
		mux.HandleFunc("/api/terminal/logs", n.popupGenerator.handleTerminalLogs)       // Get logs
	}

	n.server = &http.Server{Addr: ":8082", Handler: mux}

	// Test protobuf system
	n.testProtobufSystem()

	go func() {
		log.Println("Web server starting on :8082")
		if err := n.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	return nil
}

/**
 * @brief Application main entry point
 *
 * @details Initializes NgaSim, sets up signal handlers for graceful shutdown,
 * and starts all application components. Provides helpful startup information
 * including available URLs and API endpoints.
 */
func main() {
	log.Println("=== NgaSim Pool Controller Simulator ===")
	log.Printf("Version: %s", NgaSimVersion)
	log.Println("Starting up...")

	// Create NgaSim instance
	nga := NewNgaSim()

	// Set up cleanup on interrupt
	defer nga.cleanup()

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("\n🛑 Interrupt received, shutting down gracefully...")
		nga.cleanup()
		os.Exit(0)
	}()

	// Start the NgaSim system
	if err := nga.Start(); err != nil {
		log.Fatalf("❌ Failed to start NgaSim: %v", err)
	}

	log.Println("🚀 NgaSim started successfully!")
	log.Println("")
	log.Println("📍 Available Interfaces:")
	log.Println("   🌐 Main Interface:     http://localhost:8082")
	log.Println("   🎮 JS Demo:           http://localhost:8082/js-demo")
	log.Println("   🏠 Original Home:     http://localhost:8082/old")
	log.Println("   📊 Demo Page:         http://localhost:8082/demo")
	log.Println("")
	log.Println("🔗 API Endpoints:")
	log.Println("   📊 Device List:       http://localhost:8082/api/devices")
	log.Println("   🧪 Sanitizer Cmd:     http://localhost:8082/api/sanitizer/command")
	log.Println("   ⚡ Power Levels:      http://localhost:8082/api/power-levels")
	log.Println("   🛑 Emergency Stop:    http://localhost:8082/api/emergency-stop")
	log.Println("   🔧 Exit App:          http://localhost:8082/api/exit")
	log.Println("")
	log.Println("📋 Documentation:")
	log.Println("   📄 UI Spec (TOML):    http://localhost:8082/static/ui-spec.toml")
	log.Println("   🎨 Wireframe (SVG):   http://localhost:8082/static/wireframe.svg")
	log.Println("")
	log.Println("Press Ctrl+C to exit")

	// Keep the program running
	select {}
}

// createDemoDevices creates demo devices for testing when MQTT is not available
func (n *NgaSim) createDemoDevices() {
	log.Println("Creating demo devices...")

	demoDevices := []*Device{
		{
			ID:               "demo-sanitizer-001",
			Serial:           "demo-sanitizer-001",
			Name:             "Demo Salt Chlorinator",
			Type:             "sanitizerGen2",
			Category:         "sanitizerGen2",
			Status:           "ONLINE",
			LastSeen:         time.Now(),
			ProductName:      "AquaRite Pro",
			ModelId:          "AQR-PRO-25",
			FirmwareVersion:  "2.1.3",
			PercentageOutput: 45,
			ActualPercentage: 45,
			PPMSalt:          3200,
			LineInputVoltage: 240,
			RSSI:             -45,
		},
		{
			ID:       "demo-vsp-001",
			Serial:   "demo-vsp-001",
			Name:     "Demo Variable Speed Pump",
			Type:     "VSP",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			RPM:      2400,
			Power:    850,
			Temp:     32.5,
		},
		{
			ID:       "demo-icl-001",
			Serial:   "demo-icl-001",
			Name:     "Demo Pool Light",
			Type:     "ICL",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			Red:      128,
			Green:    64,
			Blue:     255,
			White:    200,
		},
		{
			ID:       "demo-trusense-001",
			Serial:   "demo-trusense-001",
			Name:     "Demo pH/ORP Sensor",
			Type:     "TruSense",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			PH:       7.2,
			ORP:      750,
			Temp:     25.8,
		},
		{
			ID:        "demo-heater-001",
			Serial:    "demo-heater-001",
			Name:      "Demo Pool Heater",
			Type:      "Heater",
			Status:    "ONLINE",
			LastSeen:  time.Now(),
			SetTemp:   28.0,
			WaterTemp: 26.5,
			Power:     15000,
		},
	}

	n.mutex.Lock()
	defer n.mutex.Unlock()

	for _, device := range demoDevices {
		n.devices[device.Serial] = device
		log.Printf("Created demo device: %s (%s)", device.Name, device.Serial)
	}

	log.Printf("Created %d demo devices", len(demoDevices))
}

// sendSanitizerCommand sends a command to a sanitizer device
func (n *NgaSim) sendSanitizerCommand(serial, category string, percentage int) error {
	log.Printf("🧪 Sending sanitizer command: %s -> %d%%", serial, percentage)

	// Validate percentage range
	if percentage < 0 || percentage > 101 {
		return fmt.Errorf("invalid percentage: %d (must be 0-101)", percentage)
	}

	// Find the device
	n.mutex.RLock()
	device, exists := n.devices[serial]
	n.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("device not found: %s", serial)
	}

	// Update device state to show pending command
	n.mutex.Lock()
	device.PendingPercentage = int32(percentage)
	device.LastCommandTime = time.Now()
	n.mutex.Unlock()

	// If MQTT is connected, send the real command
	if n.mqtt != nil && n.mqtt.IsConnected() {
		return n.sendMQTTSanitizerCommand(serial, category, percentage)
	}

	// Demo mode - simulate the command execution
	log.Printf("🔧 Demo mode: Simulating command execution for %s", serial)

	// Simulate command processing in a goroutine
	go func() {
		time.Sleep(2 * time.Second) // Simulate command processing delay

		n.mutex.Lock()
		if device, exists := n.devices[serial]; exists {
			device.PercentageOutput = int32(percentage)
			device.ActualPercentage = int32(percentage)
			device.PendingPercentage = 0 // Clear pending state
			device.LastCommandTime = time.Time{}
			device.LastSeen = time.Now()
			log.Printf("✅ Demo command completed: %s -> %d%%", serial, percentage)
		}
		n.mutex.Unlock()
	}()

	return nil
}

// sendMQTTSanitizerCommand sends a sanitizer command via MQTT
func (n *NgaSim) sendMQTTSanitizerCommand(serial, category string, percentage int) error {
	log.Printf("📡 Sending MQTT sanitizer command: %s -> %d%%", serial, percentage)

	// Create the protobuf command message
	//	command := &ned.SetSanitizerTargetPercentage{
	//		TargetPercentage: int32(percentage),
	//	}

	// Create a simple command payload (JSON format as fallback)
	commandPayload := map[string]interface{}{
		"command":           "set_percentage",
		"target_percentage": percentage,
		"timestamp":         time.Now().Unix(),
	}

	// Serialize as JSON instead of protobuf
	commandBytes, err := json.Marshal(commandPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %v", err)
	}

	// Construct the MQTT topic for sending commands
	// Topic format: async/category/serial/cmd
	topic := fmt.Sprintf("async/%s/%s/cmd", category, serial)

	// Log the command for debugging
	correlationID := n.logger.LogRequest(serial, "SetSanitizerTargetPercentage", commandBytes, category, "sanitizer", "percentage_command")

	// Send the command via MQTT
	token := n.mqtt.Publish(topic, 1, false, commandBytes)
	if token.Wait() && token.Error() != nil {
		n.logger.LogError(serial, "SetSanitizerTargetPercentage",
			fmt.Sprintf("MQTT publish failed: %v", token.Error()), correlationID, category)
		return fmt.Errorf("failed to publish command: %v", token.Error())
	}

	log.Printf("✅ MQTT command sent successfully: %s -> %d%% (correlation: %s)", serial, percentage, correlationID)
	return nil
}

// handleAllDeviceCommands returns all available device commands
func (n *NgaSim) handleAllDeviceCommands(w http.ResponseWriter, r *http.Request) {
	log.Println("🔧 All device commands request received")

	categories := n.commandRegistry.GetAllCategories()
	result := make(map[string]DeviceCommands)

	for _, category := range categories {
		commands, exists := n.commandRegistry.GetCommandsForCategory(category)
		if exists {
			result[category] = DeviceCommands{
				Category: category,
				Commands: commands,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("📤 Sent commands for %d categories", len(result))
}

// testProtobufSystem tests the protobuf reflection system
func (n *NgaSim) testProtobufSystem() {
	log.Println("🧪 Testing protobuf reflection system...")

	if n.reflectionEngine == nil {
		log.Println("❌ Reflection engine not initialized")
		return
	}

	messages := n.reflectionEngine.GetAllMessages()
	log.Printf("📋 Found %d protobuf message types:", len(messages))

	for _, desc := range messages { // Remove 'fullName' variable since it's not used
		log.Printf("   📄 %s (%s) - %d fields", desc.Name, desc.Package, len(desc.Fields))
		if desc.IsRequest {
			log.Printf("      🚀 REQUEST message for category: %s", desc.Category)
		}
		if desc.IsResponse {
			log.Printf("      📨 RESPONSE message")
		}
		if desc.IsTelemetry {
			log.Printf("      📊 TELEMETRY message")
		}
	}

	log.Println("✅ Protobuf system test complete")
}
