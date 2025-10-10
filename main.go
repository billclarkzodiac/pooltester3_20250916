package main

import (
	"context"
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

	"github.com/BurntSushi/toml"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const NgaSimVersion = "2.1.2"

// TOML UI Specification structs
type UISpec struct {
	Meta             MetaInfo          `toml:"meta"`
	Dashboard        Dashboard         `toml:"dashboard"`
	DeviceTypes      []DeviceType      `toml:"device_type"`
	TelemetryConfig  TelemetryConfig   `toml:"telemetry_config"`
	DeviceInfoFields []DeviceInfoField `toml:"device_info_fields"`
}

type MetaInfo struct {
	Title   string `toml:"title"`
	Version string `toml:"version"`
	Author  string `toml:"author"`
}

type Dashboard struct {
	Name            string          `toml:"name"`
	ShowOnline      bool            `toml:"show_online"`
	ShowOffline     bool            `toml:"show_offline"`
	DashboardFields []string        `toml:"dashboard_fields"`
	Visual          DashboardVisual `toml:"visual"`
}

type DashboardVisual struct {
	View            string `toml:"view"`
	OnlineColor     string `toml:"online_color"`
	OfflineColor    string `toml:"offline_color"`
	ErrorFlashColor string `toml:"error_flash_color"`
}

type DeviceType struct {
	Name     string   `toml:"name"`
	Short    string   `toml:"short"`
	Category string   `toml:"category"`
	Widgets  []Widget `toml:"widget"`
}

type Widget struct {
	ID          string                 `toml:"id"`
	Type        string                 `toml:"type"`
	Label       string                 `toml:"label"`
	Description string                 `toml:"description,omitempty"`
	Properties  map[string]interface{} `toml:"properties,omitempty"`
	Digits      int                    `toml:"digits,omitempty"`
	States      []string               `toml:"states,omitempty"`
	Visual      map[string]interface{} `toml:"visual,omitempty"`
	Channels    []string               `toml:"channels,omitempty"`
	Range       []int                  `toml:"range,omitempty"`
	// Additional fields for various widget types
	FallbackWhenWaiting string   `toml:"fallback_when_waiting,omitempty"`
	FallbackWhenOff     string   `toml:"fallback_when_off,omitempty"`
	FallbackWhenMissing string   `toml:"fallback_when_missing,omitempty"`
	FlashingWhenRamping bool     `toml:"flashing_when_ramping,omitempty"`
	PossibleValues      []string `toml:"possible_values,omitempty"`
}

type TelemetryConfig struct {
	Label  string `toml:"label"`
	Widget string `toml:"widget"`
	Notes  string `toml:"notes"`
}

type DeviceInfoField struct {
	Name     string `toml:"name"`
	Type     string `toml:"type"`
	ItemType string `toml:"item_type,omitempty"`
}

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

// Command Discovery Structures for Protobuf Reflection
type CommandField struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
	EnumValues  []string    `json:"enum_values,omitempty"`
	Min         interface{} `json:"min,omitempty"`
	Max         interface{} `json:"max,omitempty"`
}

type CommandInfo struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Description string         `json:"description"`
	Category    string         `json:"category"`
	Fields      []CommandField `json:"fields"`
	IsQuery     bool           `json:"is_query"` // GET vs SET command
}

type DeviceCommands struct {
	Category string        `json:"category"`
	Commands []CommandInfo `json:"commands"`
}

// Protobuf Command Registry
type ProtobufCommandRegistry struct {
	commandsByCategory map[string][]CommandInfo
	mutex              sync.RWMutex
}

func NewProtobufCommandRegistry() *ProtobufCommandRegistry {
	return &ProtobufCommandRegistry{
		commandsByCategory: make(map[string][]CommandInfo),
	}
}

// discoverCommands uses reflection to analyze protobuf messages and extract command information
func (pcr *ProtobufCommandRegistry) discoverCommands() {
	pcr.mutex.Lock()
	defer pcr.mutex.Unlock()

	log.Println("Discovering device commands via protobuf reflection...")

	// Discover sanitizer commands
	pcr.discoverSanitizerCommands()

	// TODO: Add other device types (pumps, heaters, etc.) as needed

	log.Printf("Command discovery complete. Found commands for %d device categories", len(pcr.commandsByCategory))
}

// discoverSanitizerCommands analyzes sanitizer protobuf structures
func (pcr *ProtobufCommandRegistry) discoverSanitizerCommands() {
	sanitizerCommands := []CommandInfo{}

	// SetSanitizerTargetPercentage command
	sanitizerCommands = append(sanitizerCommands, CommandInfo{
		Name:        "set_sanitizer_output_percentage",
		DisplayName: "Set Output Percentage",
		Description: "Set the sanitizer output percentage (0-100%)",
		Category:    "sanitizerGen2",
		IsQuery:     false,
		Fields: []CommandField{
			{
				Name:        "target_percentage",
				Type:        "int32",
				Description: "Target output percentage (0-100)",
				Required:    true,
				Min:         0,
				Max:         100,
			},
		},
	})

	// GetSanitizerStatus command
	sanitizerCommands = append(sanitizerCommands, CommandInfo{
		Name:        "get_status",
		DisplayName: "Get Device Status",
		Description: "Retrieve current device status and telemetry",
		Category:    "sanitizerGen2",
		IsQuery:     true,
		Fields:      []CommandField{}, // No parameters needed
	})

	// GetSanitizerConfiguration command
	sanitizerCommands = append(sanitizerCommands, CommandInfo{
		Name:        "get_configuration",
		DisplayName: "Get Configuration",
		Description: "Retrieve current device configuration",
		Category:    "sanitizerGen2",
		IsQuery:     true,
		Fields:      []CommandField{}, // No parameters needed
	})

	// OverrideFlowSensorType command
	sanitizerCommands = append(sanitizerCommands, CommandInfo{
		Name:        "override_flow_sensor_type",
		DisplayName: "Override Flow Sensor Type",
		Description: "Override the detected flow sensor type",
		Category:    "sanitizerGen2",
		IsQuery:     false,
		Fields: []CommandField{
			{
				Name:        "flow_sensor_type",
				Type:        "enum",
				Description: "Flow sensor type to use",
				Required:    true,
				EnumValues:  []string{"SENSOR_TYPE_UNKNOWN", "GAS", "SWITCH"},
			},
		},
	})

	// GetActiveErrors command
	sanitizerCommands = append(sanitizerCommands, CommandInfo{
		Name:        "get_active_errors",
		DisplayName: "Get Active Errors",
		Description: "Retrieve list of currently active errors",
		Category:    "sanitizerGen2",
		IsQuery:     true,
		Fields:      []CommandField{}, // No parameters needed
	})

	pcr.commandsByCategory["sanitizerGen2"] = sanitizerCommands
	log.Printf("Discovered %d commands for sanitizerGen2 devices", len(sanitizerCommands))
}

// GetCommandsForCategory returns available commands for a device category
func (pcr *ProtobufCommandRegistry) GetCommandsForCategory(category string) ([]CommandInfo, bool) {
	pcr.mutex.RLock()
	defer pcr.mutex.RUnlock()

	commands, exists := pcr.commandsByCategory[category]
	return commands, exists
}

// GetAllCategories returns all discovered device categories
func (pcr *ProtobufCommandRegistry) GetAllCategories() []string {
	pcr.mutex.RLock()
	defer pcr.mutex.RUnlock()

	categories := make([]string, 0, len(pcr.commandsByCategory))
	for category := range pcr.commandsByCategory {
		categories = append(categories, category)
	}
	return categories
}

// LogLevel represents the severity of a log entry
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// DeviceLogEntry represents a single device communication log entry
type DeviceLogEntry struct {
	ID            string                 `json:"id"`
	Timestamp     time.Time              `json:"timestamp"`
	DeviceID      string                 `json:"device_id"`
	Direction     string                 `json:"direction"` // "REQUEST" or "RESPONSE"
	MessageType   string                 `json:"message_type"`
	RawData       []byte                 `json:"raw_data"`
	ParsedData    map[string]interface{} `json:"parsed_data"`
	Success       bool                   `json:"success"`
	Error         string                 `json:"error,omitempty"`
	Duration      time.Duration          `json:"duration,omitempty"`
	Level         LogLevel               `json:"level"`
	Tags          []string               `json:"tags,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
}

// DeviceLogger handles comprehensive logging of device communications
type DeviceLogger struct {
	entries    []*DeviceLogEntry
	mutex      sync.RWMutex
	maxEntries int
	logFile    *os.File
	registry   *ProtobufRegistry
}

// LogFilter represents criteria for filtering log entries
type LogFilter struct {
	DeviceID      string    `json:"device_id,omitempty"`
	MessageType   string    `json:"message_type,omitempty"`
	Direction     string    `json:"direction,omitempty"`
	Level         LogLevel  `json:"level,omitempty"`
	StartTime     time.Time `json:"start_time,omitempty"`
	EndTime       time.Time `json:"end_time,omitempty"`
	Success       *bool     `json:"success,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
}

type NgaSim struct {
	devices             map[string]*Device
	mutex               sync.RWMutex
	mqtt                mqtt.Client
	server              *http.Server
	pollerCmd           *exec.Cmd
	logger              *DeviceLogger
	commandRegistry     *ProtobufCommandRegistry
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

func NewNgaSim() *NgaSim {
	// Create protobuf command registry for automatic UI generation
	commandRegistry := NewProtobufCommandRegistry()

	// Initialize command registry with known device categories
	commandRegistry.discoverCommands()

	// Create a minimal protobuf registry for the logger (compatibility)
	legacyRegistry := &ProtobufRegistry{} // Placeholder for existing logger compatibility

	// Create device logger for structured command logging
	logger, err := NewDeviceLogger(1000, "device_commands.log", legacyRegistry)
	if err != nil {
		log.Printf("Warning: Failed to create device logger: %v", err)
		logger = nil
	} else {
		log.Println("Device logger initialized - commands will be logged to device_commands.log")
	}

	ngaSim := &NgaSim{
		devices:         make(map[string]*Device),
		logger:          logger,
		commandRegistry: commandRegistry,
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
	mux := http.NewServeMux()
	mux.HandleFunc("/", n.handleGoDemo)      // Go-centric single driver approach
	mux.HandleFunc("/js-demo", n.handleDemo) // Keep JS version for comparison
	mux.HandleFunc("/old", n.handleHome)     // Keep old interface accessible
	mux.HandleFunc("/goodbye", n.handleGoodbye)
	mux.HandleFunc("/api/exit", n.handleExit)
	mux.HandleFunc("/api/devices", n.handleAPI)
	mux.HandleFunc("/api/sanitizer/command", n.handleSanitizerCommand)
	mux.HandleFunc("/api/sanitizer/states", n.handleSanitizerStates)
	mux.HandleFunc("/api/power-levels", n.handlePowerLevels)
	mux.HandleFunc("/api/emergency-stop", n.handleEmergencyStop)

	// UI Specification API - parsed TOML as JSON
	mux.HandleFunc("/api/ui/spec", n.handleUISpecAPI)

	// Frontend demo (also available at /demo for compatibility)
	mux.HandleFunc("/demo", n.handleDemo)

	// Serve design assets for web developers
	mux.HandleFunc("/static/wireframe.svg", n.handleWireframeSVG)
	mux.HandleFunc("/static/wireframe.mmd", n.handleWireframeMMD)
	mux.HandleFunc("/static/ui-spec.toml", n.handleUISpecTOML)
	mux.HandleFunc("/static/ui-spec.txt", n.handleUISpecTXT)

	n.server = &http.Server{Addr: ":8082", Handler: mux}

	go func() {
		log.Println("Web server starting on :8082")
		if err := n.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	return nil
}

func (n *NgaSim) createDemoDevices() {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	// Create two sanitizer devices with realistic serial numbers and telemetry
	n.devices["1234567890ABCDEF00"] = &Device{
		ID:               "1234567890ABCDEF00",
		Type:             "sanitizerGen2",
		Name:             "Pool Sanitizer A",
		Serial:           "1234567890ABCDEF00",
		Status:           "ONLINE",
		LastSeen:         time.Now(),
		Category:         "sanitizerGen2",
		ProductName:      "AquaRite Pro",
		ModelId:          "AQR-PRO-15",
		FirmwareVersion:  "2.1.4",
		PercentageOutput: 50,
		PPMSalt:          3200,
		LineInputVoltage: 240,
		RSSI:             -45,
	}

	n.devices["1234567890ABCDEF01"] = &Device{
		ID:               "1234567890ABCDEF01",
		Type:             "sanitizerGen2",
		Name:             "Pool Sanitizer B",
		Serial:           "1234567890ABCDEF01",
		Status:           "ONLINE",
		LastSeen:         time.Now(),
		Category:         "sanitizerGen2",
		ProductName:      "AquaRite Pro",
		ModelId:          "AQR-PRO-15",
		FirmwareVersion:  "2.1.4",
		PercentageOutput: 0,
		PPMSalt:          2800,
		LineInputVoltage: 238,
		RSSI:             -52,
	}

	// Keep all your existing demo devices (VSP, ICL, etc.)
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

	log.Println("Created demo devices with working sanitizer protobuf reflection")
}

// sendSanitizerCommand sends a power level command to a sanitizer device
// Matches the Python script send_salt_command() functionality
func (sim *NgaSim) sendSanitizerCommand(deviceSerial, category string, targetPercentage int) error {
	// Check if MQTT is connected
	if sim.mqtt == nil || !sim.mqtt.IsConnected() {
		log.Printf("⚠️  MQTT not connected - simulating command for demo mode")
		// In demo mode, simulate the command by updating the device state
		sim.mutex.Lock()
		if device, exists := sim.devices[deviceSerial]; exists {
			device.PercentageOutput = int32(targetPercentage)
			device.ActualPercentage = int32(targetPercentage)
			device.PendingPercentage = 0
		}
		sim.mutex.Unlock()
		log.Printf("✅ Demo command completed: %s -> %d%%", deviceSerial, targetPercentage)
		return nil
	}

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
				fmt.Sprintf("MQTT publish failed: %v", token.Error()),
				correlationID,
				"mqtt_error",
				"publish_failed")
		}
		return fmt.Errorf("MQTT publish failed: %v", token.Error())
	}

	log.Printf("✅ Command published successfully to %s", topic)
	return nil
}

// handleAllDeviceCommands returns all available device commands
func (n *NgaSim) handleAllDeviceCommands(w http.ResponseWriter, r *http.Request) {
	categories := n.commandRegistry.GetAllCategories()
	result := make(map[string]DeviceCommands)

	for _, category := range categories {
		commands, _ := n.commandRegistry.GetCommandsForCategory(category)
		result[category] = DeviceCommands{
			Category: category,
			Commands: commands,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}
}

func (n *NgaSim) startWebServer() error {
	// Start web server
	mux := http.NewServeMux()
	mux.HandleFunc("/", n.handleGoDemo)      // Go-centric single driver approach
	mux.HandleFunc("/js-demo", n.handleDemo) // Keep JS version for comparison
	mux.HandleFunc("/old", n.handleHome)     // Keep old interface accessible
	mux.HandleFunc("/goodbye", n.handleGoodbye)
	mux.HandleFunc("/api/exit", n.handleExit)
	mux.HandleFunc("/api/devices", n.handleAPI)
	mux.HandleFunc("/api/sanitizer/command", n.handleSanitizerCommand)
	mux.HandleFunc("/api/sanitizer/states", n.handleSanitizerStates)
	mux.HandleFunc("/api/power-levels", n.handlePowerLevels)
	mux.HandleFunc("/api/emergency-stop", n.handleEmergencyStop)

	// UI Specification API - parsed TOML as JSON
	mux.HandleFunc("/api/ui/spec", n.handleUISpecAPI)

	// Device Commands API - discovered via protobuf reflection
	mux.HandleFunc("/api/device-commands/", n.handleDeviceCommands)
	mux.HandleFunc("/api/device-commands", n.handleAllDeviceCommands)

	// Frontend demo (also available at /demo for compatibility)
	mux.HandleFunc("/demo", n.handleDemo)

	// Serve design assets for web developers
	mux.HandleFunc("/static/wireframe.svg", n.handleWireframeSVG)
	mux.HandleFunc("/static/wireframe.mmd", n.handleWireframeMMD)
	mux.HandleFunc("/static/ui-spec.toml", n.handleUISpecTOML)
	mux.HandleFunc("/static/ui-spec.txt", n.handleUISpecTXT)

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

var goDemoTemplate = template.Must(template.New("goDemoTemplate").Parse(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>{{.Title}}</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: Arial, sans-serif; margin: 0; background: #f5f5f5; }
        .header { background: #2563eb; color: white; padding: 1rem; text-align: center; }
        .container { max-width: 1200px; margin: 2rem auto; padding: 0 1rem; }
        .devices-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 1rem; }
        .device-card { background: white; border-radius: 8px; padding: 1rem; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .device-title { font-size: 1.2rem; font-weight: bold; margin-bottom: 0.5rem; }
        .device-meta { color: #666; font-size: 0.9rem; margin-bottom: 1rem; }
        .device-status { padding: 0.25rem 0.5rem; border-radius: 4px; font-size: 0.8rem; }
        .status-online { background: #10b981; color: white; }
        .status-offline { background: #ef4444; color: white; }
        .device-controls { margin-top: 1rem; }
        .control-group { margin-bottom: 1rem; }
        .control-label { font-weight: bold; margin-bottom: 0.5rem; }
        .button-row { display: flex; gap: 0.5rem; flex-wrap: wrap; }
        .btn { padding: 0.5rem 1rem; border: none; border-radius: 4px; cursor: pointer; font-weight: bold; }
        .btn-off { background: #ef4444; color: white; }
        .btn-low { background: #f59e0b; color: white; }
        .btn-med { background: #3b82f6; color: white; }
        .btn-high { background: #10b981; color: white; }
        .btn-boost { background: #7c3aed; color: white; }
        .telemetry { background: #f8fafc; padding: 0.5rem; border-radius: 4px; margin-top: 0.5rem; }
        .exit-btn { position: fixed; top: 1rem; right: 1rem; background: #ef4444; color: white; 
                   border: none; padding: 0.5rem 1rem; border-radius: 4px; font-weight: bold; cursor: pointer; }
    </style>
</head>
<body>
    <button class="exit-btn" onclick="if(confirm('Exit?')) fetch('/api/exit', {method:'POST'})">EXIT</button>
    
    <div class="header">
        <h1>{{.Title}}</h1>
        <p>{{len .Devices}} devices • Go-centric single driver architecture</p>
    </div>

    <div class="container">
        <div class="devices-grid">
        {{range .Devices}}
            <div class="device-card">
                <div class="device-title">{{.Name}}</div>
                <div class="device-meta">
                    {{.Type}} • Serial: {{.Serial}}<br>
                    <span class="device-status {{if eq .Status "ONLINE"}}status-online{{else}}status-offline{{end}}">
                        {{.Status}}
                    </span>
                </div>
                
                {{if eq .Type "sanitizerGen2"}}
                <div class="telemetry">
                    <strong>Power:</strong> {{.PercentageOutput}}% 
                    {{if .PPMSalt}}<strong>PPM:</strong> {{.PPMSalt}}{{end}}
                    {{if .LineInputVoltage}}<strong>Voltage:</strong> {{.LineInputVoltage}}V{{end}}
                </div>
                
                <div class="device-controls">
                    <div class="control-group">
                        <div class="control-label">Power Controls</div>
                        <div class="button-row">
                            <button class="btn btn-off" onclick="sendCommand('{{.Serial}}', 0)">OFF</button>
                            <button class="btn btn-low" onclick="sendCommand('{{.Serial}}', 10)">10%</button>
                            <button class="btn btn-med" onclick="sendCommand('{{.Serial}}', 50)">50%</button>
                            <button class="btn btn-high" onclick="sendCommand('{{.Serial}}', 100)">100%</button>
                            <button class="btn btn-boost" onclick="sendCommand('{{.Serial}}', 101)">BOOST</button>
                        </div>
                    </div>
                    
                    {{if index $.DeviceCommands .Type}}
                    <div class="control-group">
                        <div class="control-label">Protobuf Commands ({{len (index $.DeviceCommands .Type).Commands}} available)</div>
                        {{range (index $.DeviceCommands .Type).Commands}}
                        <div style="margin: 0.25rem 0; padding: 0.25rem; background: #e5e7eb; border-radius: 4px; font-size: 0.8rem;">
                            <strong>{{.DisplayName}}</strong>: {{.Description}}
                        </div>
                        {{end}}
                    </div>
                    {{end}}
                </div>
                {{end}}
                
                {{if ne .Type "sanitizerGen2"}}
                <div class="telemetry">
                    {{if .RPM}}<strong>RPM:</strong> {{.RPM}} {{end}}
                    {{if .Temp}}<strong>Temp:</strong> {{printf "%.1f" .Temp}}°C {{end}}
                    {{if .Power}}<strong>Power:</strong> {{.Power}}W {{end}}
                    {{if .PH}}<strong>pH:</strong> {{printf "%.1f" .PH}} {{end}}
                    {{if .ORP}}<strong>ORP:</strong> {{.ORP}}mV {{end}}
                </div>
                {{end}}
            </div>
        {{end}}
        </div>
    </div>

    <script>
        function sendCommand(serial, percentage) {
            fetch('/api/sanitizer/command', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({serial: serial, percentage: percentage})
            })
            .then(r => r.json())
            .then(result => {
                if(result.success) {
                    alert('Command sent: ' + percentage + '%');
                    location.reload(); // Refresh to show updated state
                } else {
                    alert('Command failed: ' + JSON.stringify(result));
                }
            })
            .catch(e => alert('Network error: ' + e.message));
        }
        
        // Auto-refresh every 10 seconds to show live updates
        setInterval(() => location.reload(), 10000);
    </script>
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
	// Redirect client to a goodbye page which will attempt to close the tab
	http.Redirect(w, r, "/goodbye", http.StatusSeeOther)

	// Perform graceful shutdown in background so the redirect can complete
	go func() {
		log.Println("Exit requested via /api/exit - initiating graceful shutdown")

		// First perform application cleanup
		n.cleanup()

		// Then attempt to gracefully shutdown the HTTP server
		if n.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := n.server.Shutdown(ctx); err != nil {
				log.Printf("Server shutdown error: %v", err)
			} else {
				log.Println("HTTP server shut down cleanly")
			}
		}

		// Small pause then exit to ensure process terminates if needed
		time.Sleep(300 * time.Millisecond)
		log.Println("Exiting process now")
		os.Exit(0)
	}()
}

// handleGoodbye serves a small page that attempts to auto-close the browser tab
func (n *NgaSim) handleGoodbye(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!doctype html>
<html><head><meta charset="utf-8"><title>Goodbye</title></head>
<body style="font-family:Arial;background:#f8fafc;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
  <div style="text-align:center;padding:20px;background:#fff;border-radius:8px;box-shadow:0 6px 18px rgba(0,0,0,0.08)">
	<h2 style="margin:0 0 8px 0">Shutting down...</h2>
	<p style="color:#6b7280;margin:0 0 12px 0">The controller is stopping. This page will try to close automatically.</p>
	<button onclick="tryClose()" style="background:#ef4444;color:#fff;border:none;padding:8px 12px;border-radius:6px;cursor:pointer">Close tab</button>
  </div>
  <script>
	function tryClose(){
	  try{window.open('','_self'); window.close();}catch(e){}
	  document.body.innerHTML = '<div style="text-align:center;margin-top:40px">If the tab did not close, please close it manually.</div>'
	}
	// Try automatically after a short delay
	setTimeout(tryClose, 800);
  </script>
</body></html>`)
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

// handleWireframeSVG serves the SVG wireframe for web developers
func (n *NgaSim) handleWireframeSVG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	http.ServeFile(w, r, "Device_Window_wireframe.svg")
}

// handleWireframeMMD serves the Mermaid diagram for web developers
func (n *NgaSim) handleWireframeMMD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	http.ServeFile(w, r, "Device_Window_diagram.mmd")
}

// handleUISpecTOML serves the TOML UI specification for web developers
func (n *NgaSim) handleUISpecTOML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	http.ServeFile(w, r, "Device_Window_spec-20251002bc.toml")
}

// handleUISpecTXT serves the text UI specification for web developers
func (n *NgaSim) handleUISpecTXT(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	http.ServeFile(w, r, "Device_Window_spec-20251002bc.txt")
}

// handleUISpecAPI parses TOML UI spec and serves as JSON for dynamic frontends
func (n *NgaSim) handleUISpecAPI(w http.ResponseWriter, r *http.Request) {
	// Read the TOML file
	data, err := os.ReadFile("Device_Window_spec-20251002bc.toml")
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading TOML file: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse TOML into our struct
	var spec UISpec
	if err := toml.Unmarshal(data, &spec); err != nil {
		http.Error(w, fmt.Sprintf("Error parsing TOML: %v", err), http.StatusInternalServerError)
		return
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // Allow CORS for frontend development
	if err := json.NewEncoder(w).Encode(spec); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleDemo serves the frontend demo HTML page
func (n *NgaSim) handleDemo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, "demo.html")
}

// handleDeviceCommands returns available commands for a specific device category
func (n *NgaSim) handleDeviceCommands(w http.ResponseWriter, r *http.Request) {
	// Extract category from URL path
	path := r.URL.Path
	category := strings.TrimPrefix(path, "/api/device-commands/")

	if category == "" {
		http.Error(w, "Device category required", http.StatusBadRequest)
		return
	}

	// Get commands for this category
	commands, exists := n.commandRegistry.GetCommandsForCategory(category)
	if !exists {
		http.Error(w, fmt.Sprintf("No commands found for device category: %s", category), http.StatusNotFound)
		return
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	result := DeviceCommands{
		Category: category,
		Commands: commands,
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleGoDemo serves a Go-generated HTML page with embedded device data and controls
func (n *NgaSim) handleGoDemo(w http.ResponseWriter, r *http.Request) {
	// Get current devices and commands
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	// Sort devices by serial number for consistent display
	for i := 0; i < len(devices)-1; i++ {
		for j := i + 1; j < len(devices); j++ {
			if devices[i].Serial > devices[j].Serial {
				devices[i], devices[j] = devices[j], devices[i]
			}
		}
	}

	// Get available commands for each device category
	deviceCommands := make(map[string]DeviceCommands)
	categories := n.commandRegistry.GetAllCategories()
	for _, category := range categories {
		commands, _ := n.commandRegistry.GetCommandsForCategory(category)
		deviceCommands[category] = DeviceCommands{
			Category: category,
			Commands: commands,
		}
	}

	// Prepare template data
	data := struct {
		Title          string
		Devices        []*Device
		DeviceCommands map[string]DeviceCommands
	}{
		Title:          "NgaSim Pool Controller - Go-Centric Dashboard",
		Devices:        devices,
		DeviceCommands: deviceCommands,
	}

	w.Header().Set("Content-Type", "text/html")
	goDemoTemplate.Execute(w, data)
}

// NewDeviceLogger creates a new device logger
func NewDeviceLogger(maxEntries int, logFilePath string, registry *ProtobufRegistry) (*DeviceLogger, error) {
	logger := &DeviceLogger{
		entries:    make([]*DeviceLogEntry, 0),
		maxEntries: maxEntries,
		registry:   registry,
	}

	// Open log file for persistent storage
	if logFilePath != "" {
		file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %v", err)
		}
		logger.logFile = file
	}

	return logger, nil
}

// LogRequest logs an outgoing device request
func (dl *DeviceLogger) LogRequest(deviceID, messageType string, data []byte, tags ...string) string {
	correlationID := dl.generateCorrelationID()

	entry := &DeviceLogEntry{
		ID:            dl.generateEntryID(),
		Timestamp:     time.Now(),
		DeviceID:      deviceID,
		Direction:     "REQUEST",
		MessageType:   messageType,
		RawData:       data,
		Success:       true,
		Level:         LogLevelInfo,
		Tags:          tags,
		CorrelationID: correlationID,
	}

	// Parse protobuf data if possible
	if parsedData, err := dl.parseProtobufData(messageType, data); err == nil {
		entry.ParsedData = parsedData
	} else {
		entry.Error = fmt.Sprintf("Failed to parse protobuf: %v", err)
		entry.Success = false
		entry.Level = LogLevelWarn
	}

	dl.addEntry(entry)
	return correlationID
}

// LogResponse logs an incoming device response
func (dl *DeviceLogger) LogResponse(deviceID, messageType string, data []byte, correlationID string, duration time.Duration, tags ...string) {
	entry := &DeviceLogEntry{
		ID:            dl.generateEntryID(),
		Timestamp:     time.Now(),
		DeviceID:      deviceID,
		Direction:     "RESPONSE",
		MessageType:   messageType,
		RawData:       data,
		Success:       true,
		Level:         LogLevelInfo,
		Duration:      duration,
		Tags:          tags,
		CorrelationID: correlationID,
	}

	// Parse protobuf data if possible
	if parsedData, err := dl.parseProtobufData(messageType, data); err == nil {
		entry.ParsedData = parsedData
	} else {
		entry.Error = fmt.Sprintf("Failed to parse protobuf: %v", err)
		entry.Success = false
		entry.Level = LogLevelWarn
	}

	dl.addEntry(entry)
}

// LogError logs an error that occurred during device communication
func (dl *DeviceLogger) LogError(deviceID, messageType, errorMsg string, correlationID string, tags ...string) {
	entry := &DeviceLogEntry{
		ID:            dl.generateEntryID(),
		Timestamp:     time.Now(),
		DeviceID:      deviceID,
		Direction:     "ERROR",
		MessageType:   messageType,
		Success:       false,
		Error:         errorMsg,
		Level:         LogLevelError,
		Tags:          tags,
		CorrelationID: correlationID,
	}

	dl.addEntry(entry)
}

// parseProtobufData attempts to parse protobuf data into a readable format
func (dl *DeviceLogger) parseProtobufData(messageType string, data []byte) (map[string]interface{}, error) {
	if dl.registry == nil {
		return nil, fmt.Errorf("no protobuf registry available")
	}

	// Create message instance
	msg, err := dl.registry.CreateMessage(messageType)
	if err != nil {
		return nil, err
	}

	// Unmarshal the data
	if err := proto.Unmarshal(data, msg); err != nil {
		return nil, err
	}

	// Convert to map using reflection
	return dl.protoMessageToMap(msg.ProtoReflect()), nil
}

// protoMessageToMap converts a protobuf message to a map using reflection
func (dl *DeviceLogger) protoMessageToMap(msg protoreflect.Message) map[string]interface{} {
	result := make(map[string]interface{})

	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		fieldName := string(fd.Name())

		switch {
		case fd.IsList():
			list := v.List()
			items := make([]interface{}, list.Len())
			for i := 0; i < list.Len(); i++ {
				items[i] = dl.convertValue(fd, list.Get(i))
			}
			result[fieldName] = items
		case fd.IsMap():
			mapVal := v.Map()
			mapResult := make(map[string]interface{})
			mapVal.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
				mapResult[k.String()] = dl.convertValue(fd.MapValue(), v)
				return true
			})
			result[fieldName] = mapResult
		default:
			result[fieldName] = dl.convertValue(fd, v)
		}

		return true
	})

	return result
}

// convertValue converts a protobuf value to a Go interface{}
func (dl *DeviceLogger) convertValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) interface{} {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return v.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int32(v.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return uint32(v.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return v.Uint()
	case protoreflect.FloatKind:
		return float32(v.Float())
	case protoreflect.DoubleKind:
		return v.Float()
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.BytesKind:
		return v.Bytes()
	case protoreflect.EnumKind:
		return fd.Enum().Values().ByNumber(v.Enum()).Name()
	case protoreflect.MessageKind:
		return dl.protoMessageToMap(v.Message())
	default:
		return v.Interface()
	}
}

// addEntry adds a new log entry and manages rotation
func (dl *DeviceLogger) addEntry(entry *DeviceLogEntry) {
	dl.mutex.Lock()
	defer dl.mutex.Unlock()

	// Add to memory
	dl.entries = append(dl.entries, entry)

	// Rotate if necessary
	if len(dl.entries) > dl.maxEntries {
		dl.entries = dl.entries[1:]
	}

	// Write to file if configured
	if dl.logFile != nil {
		if jsonData, err := json.Marshal(entry); err == nil {
			dl.logFile.WriteString(string(jsonData) + "\n")
			dl.logFile.Sync()
		}
	}

	// Log to standard logger as well
	log.Printf("[%s] %s %s -> %s: %s",
		entry.Level.String(),
		entry.Direction,
		entry.DeviceID,
		entry.MessageType,
		dl.formatLogMessage(entry))
}

// formatLogMessage formats a log entry for display
func (dl *DeviceLogger) formatLogMessage(entry *DeviceLogEntry) string {
	if entry.Success {
		if entry.Duration > 0 {
			return fmt.Sprintf("Success (%v)", entry.Duration)
		}
		return "Success"
	} else {
		return fmt.Sprintf("Error: %s", entry.Error)
	}
}

// GetEntries returns log entries with optional filtering
func (dl *DeviceLogger) GetEntries(filter LogFilter) []*DeviceLogEntry {
	dl.mutex.RLock()
	defer dl.mutex.RUnlock()

	var filtered []*DeviceLogEntry

	for _, entry := range dl.entries {
		if filter.matches(entry) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// matches checks if a log entry matches the filter criteria
func (lf *LogFilter) matches(entry *DeviceLogEntry) bool {
	if lf.DeviceID != "" && entry.DeviceID != lf.DeviceID {
		return false
	}

	if lf.MessageType != "" && entry.MessageType != lf.MessageType {
		return false
	}

	if lf.Direction != "" && entry.Direction != lf.Direction {
		return false
	}

	if lf.Level != 0 && entry.Level < lf.Level {
		return false
	}

	if !lf.StartTime.IsZero() && entry.Timestamp.Before(lf.StartTime) {
		return false
	}

	if !lf.EndTime.IsZero() && entry.Timestamp.After(lf.EndTime) {
		return false
	}

	if lf.Success != nil && entry.Success != *lf.Success {
		return false
	}

	if lf.CorrelationID != "" && entry.CorrelationID != lf.CorrelationID {
		return false
	}

	// Check tags
	if len(lf.Tags) > 0 {
		tagMatch := false
		for _, filterTag := range lf.Tags {
			for _, entryTag := range entry.Tags {
				if filterTag == entryTag {
					tagMatch = true
					break
				}
			}
			if tagMatch {
				break
			}
		}
		if !tagMatch {
			return false
		}
	}

	return true
}

// generateEntryID generates a unique ID for a log entry
func (dl *DeviceLogger) generateEntryID() string {
	return fmt.Sprintf("log_%d", time.Now().UnixNano())
}

// generateCorrelationID generates a unique correlation ID for request/response pairs
func (dl *DeviceLogger) generateCorrelationID() string {
	return fmt.Sprintf("corr_%d", time.Now().UnixNano())
}

// GetStats returns statistics about logged communications
func (dl *DeviceLogger) GetStats() map[string]interface{} {
	dl.mutex.RLock()
	defer dl.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_entries": len(dl.entries),
		"by_device":     make(map[string]int),
		"by_message":    make(map[string]int),
		"by_level":      make(map[string]int),
		"success_rate":  0.0,
	}

	successCount := 0
	for _, entry := range dl.entries {
		// Count by device
		stats["by_device"].(map[string]int)[entry.DeviceID]++

		// Count by message type
		stats["by_message"].(map[string]int)[entry.MessageType]++

		// Count by level
		stats["by_level"].(map[string]int)[entry.Level.String()]++

		// Count successes
		if entry.Success {
			successCount++
		}
	}

	// Calculate success rate
	if len(dl.entries) > 0 {
		stats["success_rate"] = float64(successCount) / float64(len(dl.entries)) * 100
	}

	return stats
}

// Close closes the logger and any open files
func (dl *DeviceLogger) Close() error {
	if dl.logFile != nil {
		return dl.logFile.Close()
	}
	return nil
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
