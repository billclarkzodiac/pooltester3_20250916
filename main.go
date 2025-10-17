// NgaSim Pool Controller - Main application entry point
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Current version of the NgaSim application
const NgaSimVersion = "2.2.0-clean"

// Placeholder types for missing components
type CommandRegistry struct {
	categories map[string][]CommandInfo
}
type ProtobufReflectionEngine struct {
	messages map[string]MessageDescriptor
}

// MessageDescriptor represents a protobuf message descriptor
type MessageDescriptor struct {
    Name        string
    Package     string
    Fields      []FieldDescriptor
    Category    string
    Description string
    IsRequest   bool
}

// FieldDescriptor represents a protobuf field descriptor
type FieldDescriptor struct {
    Name         string
    Type         string
    Number       int32
    Description  string
    Unit         string
    Required     bool
    Label        string
    Min          interface{}
    Max          interface{}
    DefaultValue interface{}
    EnumValues   []string
}

// NewCommandRegistry creates a new command registry (placeholder)
func NewCommandRegistry() *CommandRegistry {
    return &CommandRegistry{
        categories: map[string][]CommandInfo{
            "sanitizerGen2": {
                {
                    Name:        "SetSanitizerTargetPercentageRequestPayload",
                    DisplayName: "Set Target Percentage",
                    Description: "Set sanitizer target percentage",
                    Category:    "sanitizerGen2",
                    Fields:      []CommandField{},
                    IsQuery:     false,
                },
                {
                    Name:        "GetSanitizerStatusRequestPayload",
                    DisplayName: "Get Status",
                    Description: "Get sanitizer status",
                    Category:    "sanitizerGen2",
                    Fields:      []CommandField{},
                    IsQuery:     true,
                },
            },
            "lights": {
                {
                    Name:        "SetBrightness",
                    DisplayName: "Set Brightness",
                    Description: "Set light brightness",
                    Category:    "lights",
                    Fields:      []CommandField{},
                    IsQuery:     false,
                },
            },
        },
    }
}

// NgaSim is the main application structure
type NgaSim struct {
	devices             map[string]*Device
	mutex               sync.RWMutex
	mqtt                mqtt.Client
	server              *http.Server
	pollerCmd           *exec.Cmd
	logger              *DeviceLogger
	terminalLogger      *TerminalLogger
	deviceCommands      map[string][]string
	commandRegistry     *CommandRegistry
	reflectionEngine    *ProtobufReflectionEngine
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
	TopicTelemetry = "async/+/+/dt"
	TopicStatus    = "async/+/+/sts"
	TopicError     = "async/+/+/error"
)

// NewNgaSim creates a new NgaSim instance (FIXED VERSION)
func NewNgaSim() *NgaSim {
	terminalLogger, err := NewTerminalLogger("ngasim_terminal.log", 1000)
	if err != nil {
		log.Printf("Warning: Terminal logger creation failed: %v", err)
		terminalLogger = nil
	}

	nga := &NgaSim{
		devices:        make(map[string]*Device),
		logger:         NewDeviceLogger(1000),
		terminalLogger: terminalLogger,
		deviceCommands: make(map[string][]string),
	}

	// Initialize missing components
	nga.initializeComponents()

	return nga
}

// connectMQTT connects to the MQTT broker
func (sim *NgaSim) connectMQTT() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(MQTTBroker)
	opts.SetClientID(MQTTClientID)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetKeepAlive(30 * time.Second)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
	})

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

// subscribeToTopics subscribes to device topics
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
	payload := msg.Payload()

	log.Printf("Received MQTT message on topic: %s", topic)

	// Parse topic to extract device info
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
		sim.handleDeviceAnnounce(category, deviceSerial, payload)
	case "dt":
		sim.handleDeviceTelemetry(category, deviceSerial, payload)
	case "sts":
		sim.handleDeviceStatus(category, deviceSerial, payload)
	case "error":
		sim.handleDeviceError(category, deviceSerial, payload)
	default:
		log.Printf("Unknown message type: %s", messageType)
	}
}

// handleDeviceAnnounce processes device announcement messages
func (sim *NgaSim) handleDeviceAnnounce(category, deviceSerial string, payload []byte) {
	log.Printf("Device announce from %s (category: %s): %d bytes", deviceSerial, category, len(payload))

	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	device, exists := sim.devices[deviceSerial]
	if !exists {
		device = &Device{
			ID:       deviceSerial,
			Serial:   deviceSerial,
			Name:     fmt.Sprintf("Device-%s", deviceSerial),
			Type:     category,
			Category: category,
			Status:   "DISCOVERED",
			LastSeen: time.Now(),
		}
		sim.devices[deviceSerial] = device
		log.Printf("New device discovered: %s", deviceSerial)
	}

	device.Status = "ONLINE"
	device.LastSeen = time.Now()

	// Add to device terminal
	sim.addDeviceTerminalEntry(deviceSerial, "ANNOUNCE",
		fmt.Sprintf("Device announced: %s", device.Name), payload)
}

// handleDeviceTelemetry processes device telemetry messages
func (sim *NgaSim) handleDeviceTelemetry(category, deviceSerial string, payload []byte) {
	log.Printf("Device telemetry from %s (category: %s): %d bytes", deviceSerial, category, len(payload))

	sim.mutex.Lock()
	device, exists := sim.devices[deviceSerial]
	if exists {
		device.Status = "ONLINE"
		device.LastSeen = time.Now()
	}
	sim.mutex.Unlock()

	// Add to device terminal
	sim.addDeviceTerminalEntry(deviceSerial, "TELEMETRY",
		"Telemetry received", payload)
}

// handleDeviceStatus processes device status messages
func (sim *NgaSim) handleDeviceStatus(category, deviceSerial string, payload []byte) {
	log.Printf("Device status from %s (category: %s): %d bytes", deviceSerial, category, len(payload))
}

// handleDeviceError processes device error messages
func (sim *NgaSim) handleDeviceError(category, deviceSerial string, payload []byte) {
	log.Printf("Device error from %s (category: %s): %d bytes", deviceSerial, category, len(payload))
}

// addDeviceTerminalEntry adds an entry to a device's terminal
func (sim *NgaSim) addDeviceTerminalEntry(deviceSerial, entryType, message string, rawData []byte) {
	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	device, exists := sim.devices[deviceSerial]
	if !exists {
		return
	}

	entry := TerminalEntry{
		Timestamp: time.Now(),
		Type:      entryType,
		Message:   message,
		Data:      string(rawData),
	}

	// Try to parse protobuf data
	if parsed, err := ParseProtobufMessage(rawData, entryType, device.Type); err == nil {
		entry.ParsedProtobuf = parsed
		// Update message with parsed info
		if len(parsed.Fields) > 0 {
			entry.Message = fmt.Sprintf("%s (%d fields parsed)", message, len(parsed.Fields))
		}
	}

	// Add to device's live terminal
	device.LiveTerminal = append(device.LiveTerminal, entry)

	// Keep only last 50 entries per device
	if len(device.LiveTerminal) > 50 {
		device.LiveTerminal = device.LiveTerminal[len(device.LiveTerminal)-50:]
	}
}

// startPoller starts the C poller subprocess
func (sim *NgaSim) startPoller() error {
	log.Println("Starting C poller subprocess...")

	sim.pollerCmd = exec.Command("sudo", "./poller")

	if err := sim.pollerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start poller: %v", err)
	}

	log.Printf("Started C poller with PID: %d", sim.pollerCmd.Process.Pid)

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

		if err := sim.pollerCmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("Failed to send SIGTERM to poller: %v", err)
		} else {
			log.Println("Sent SIGTERM to poller")
		}

		sim.pollerCmd.Wait()
		sim.pollerCmd = nil
	}
}

// createDemoDevices creates demo devices for testing
func (sim *NgaSim) createDemoDevices() {
	log.Println("Creating demo devices...")

	demoDevices := []*Device{
		{
			ID:               "1234567890ABCDEF00",
			Serial:           "1234567890ABCDEF00",
			Name:             "Demo Salt Chlorinator",
			Type:             "sanitizerGen2",
			Category:         "sanitizerGen2",
			Status:           "ONLINE",
			LastSeen:         time.Now(),
			ProductName:      "AquaRite Pro",
			PercentageOutput: 45,
			ActualPercentage: 45,
			PPMSalt:          3200,
			LineInputVoltage: 240,
			RSSI:             -45,
		},
		{
			ID:               "1234567890ABCDEF01",
			Serial:           "1234567890ABCDEF01",
			Name:             "Demo Salt Chlorinator 2",
			Type:             "sanitizerGen2",
			Category:         "sanitizerGen2",
			Status:           "ONLINE",
			LastSeen:         time.Now(),
			ProductName:      "AquaRite Pro 2",
			PercentageOutput: 60,
			ActualPercentage: 60,
			PPMSalt:          3100,
			LineInputVoltage: 238,
			RSSI:             -50,
		},
	}

	sim.mutex.Lock()
	defer sim.mutex.Unlock()

	for _, device := range demoDevices {
		sim.devices[device.Serial] = device
		log.Printf("Created demo device: %s (%s)", device.Name, device.Serial)
	}

	log.Printf("Created %d demo devices", len(demoDevices))
}

// getSortedDevices returns devices sorted by serial number
func (sim *NgaSim) getSortedDevices() []*Device {
	sim.mutex.RLock()
	devices := make([]*Device, 0, len(sim.devices))
	for _, device := range sim.devices {
		devices = append(devices, device)
	}
	sim.mutex.RUnlock()

	// Sort by serial number
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Serial < devices[j].Serial
	})

	return devices
}

// cleanup performs application cleanup
func (sim *NgaSim) cleanup() {
	log.Println("Performing cleanup...")

	sim.stopPoller()

	if sim.mqtt != nil && sim.mqtt.IsConnected() {
		log.Println("Disconnecting from MQTT...")
		sim.mqtt.Disconnect(1000)
	}

	if sim.logger != nil {
		log.Println("Closing device logger...")
		sim.logger.Close()
	}

	log.Println("Cleanup completed")
}

// Start starts the NgaSim application
func (sim *NgaSim) Start() error {
	log.Println("Starting NgaSim v" + NgaSimVersion)

	// Connect to MQTT broker
	log.Println("Connecting to MQTT broker...")
	if err := sim.connectMQTT(); err != nil {
		log.Printf("MQTT connection failed: %v", err)
		log.Println("Falling back to demo mode...")
		sim.createDemoDevices()
	} else {
		log.Println("MQTT connected successfully")

		if err := sim.startPoller(); err != nil {
			log.Printf("Failed to start poller: %v", err)
		}
	}

	// Start web server
	mux := http.NewServeMux()

	// Main routes
	mux.HandleFunc("/", sim.handleRoot)
	mux.HandleFunc("/api/devices", sim.handleDevices)
	mux.HandleFunc("/api/sanitizer/command", sim.handleSanitizerCommand)
	mux.HandleFunc("/goodbye", sim.handleGoodbye)
	mux.HandleFunc("/api/exit", sim.handleExit)

	sim.server = &http.Server{Addr: ":8082", Handler: mux}

	go func() {
		log.Println("Web server starting on :8082")
		if err := sim.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	return nil
}

// main is the application entry point
func main() {
	log.Println("=== NgaSim Pool Controller Simulator ===")
	log.Printf("Version: %s", NgaSimVersion)

	nga := NewNgaSim()
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

	if err := nga.Start(); err != nil {
		log.Fatalf("❌ Failed to start NgaSim: %v", err)
	}

	log.Println("🚀 NgaSim started successfully!")
	log.Println("📍 Main Interface: http://localhost:8082")
	log.Println("Press Ctrl+C to exit")

	select {}
}

// initializeComponents initializes missing components
func (sim *NgaSim) initializeComponents() {
	// Initialize command registry
	sim.commandRegistry = NewCommandRegistry()

	// Initialize reflection engine (placeholder)
	sim.reflectionEngine = &ProtobufReflectionEngine{
		messages: make(map[string]MessageDescriptor),
	}

	// Initialize sanitizer controller
	sim.sanitizerController = NewSanitizerController(sim)
}

// sendSanitizerCommand sends a command to a sanitizer device (UPDATED VERSION)
func (sim *NgaSim) sendSanitizerCommand(deviceSerial, commandType string, value interface{}) error {
	if !sim.mqtt.IsConnected() {
		return fmt.Errorf("MQTT not connected")
	}

	// Create command structure
	command := map[string]interface{}{
		"type":  commandType,
		"value": value,
	}

	// Convert command to JSON
	data, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %v", err)
	}

	topic := fmt.Sprintf("async/sanitizerGen2/%s/cmd", deviceSerial)

	if token := sim.mqtt.Publish(topic, 1, false, data); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish command: %v", token.Error())
	}

	log.Printf("Sent sanitizer command to %s: %s = %v", deviceSerial, commandType, value)
	return nil
}

// GetAllCategories returns all available command categories
func (cr *CommandRegistry) GetAllCategories() []string {
	categories := make([]string, 0, len(cr.categories))
	for category := range cr.categories {
		categories = append(categories, category)
	}
	return categories
}

// GetCommandsForCategory returns commands for a specific category
func (cr *CommandRegistry) GetCommandsForCategory(category string) ([]CommandInfo, bool) {
    if commands, exists := cr.categories[category]; exists {
        return commands, true
    }
    return []CommandInfo{}, false
}

// GetAllMessages returns all available message descriptors
func (pre *ProtobufReflectionEngine) GetAllMessages() map[string]MessageDescriptor {
	if pre.messages == nil {
		// Return some default message descriptors as a map
		return map[string]MessageDescriptor{
			"SetSanitizerTargetPercentageRequestPayload": {
				Name:        "SetSanitizerTargetPercentageRequestPayload",
                IsRequest:   true,
				Package:     "sanitizer",
				Category:    "sanitizerGen2",
				Description: "Set sanitizer target percentage",
				Fields: []FieldDescriptor{
					{
                        Name:         "targetPercentage",
                        Type:         "int32",
                        Number:       1,
                        Description:  "Target percentage (0-100)",
                        Unit:         "%",
                        Required:     true,
                        Label:        "Target Percentage",
                        Min:          0,
                        Max:          100,
                        DefaultValue: 50,
                        EnumValues:   []string{},
					},
				},
			},
			"GetSanitizerStatusRequestPayload": {
				Name:        "GetSanitizerStatusRequestPayload",
                IsRequest:   true,
				Package:     "sanitizer",
				Category:    "sanitizerGen2",
				Description: "Get sanitizer status",
				Fields:      []FieldDescriptor{},
			},
		}
	}

	return pre.messages
}

// CreateMessage creates a new protobuf message (placeholder)
func (pre *ProtobufReflectionEngine) CreateMessage(messageType string) (interface{}, error) {
    log.Printf("Creating message of type: %s", messageType)
    
    // Return a generic struct that can be marshaled
    return struct {
        MessageType string                 `json:"messageType"`
        Fields      map[string]interface{} `json:"fields"`
    }{
        MessageType: messageType,
        Fields:      make(map[string]interface{}),
    }, nil
}

// PopulateMessage populates a protobuf message with field values (placeholder)
func (pre *ProtobufReflectionEngine) PopulateMessage(message interface{}, fieldValues map[string]interface{}) error {
    log.Printf("Populating message with %d field values", len(fieldValues))
    
    // For now, just log the operation
    if msgMap, ok := message.(map[string]interface{}); ok {
        if fields, exists := msgMap["fields"]; exists {
            if fieldMap, ok := fields.(map[string]interface{}); ok {
                for key, value := range fieldValues {
                    fieldMap[key] = value
                }
            }
        }
    }
    
    return nil
}



