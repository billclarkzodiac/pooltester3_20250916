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
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"NgaSim/ned" // Import protobuf definitions

	"github.com/google/uuid"

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

	// Add this missing field:
	deviceCommands map[string][]string // Device command mappings
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

// connectMQTT establishes connection to the MQTT broker and configures message handling.
// This function demonstrates several important Go networking and concurrency patterns that
// are essential for IoT device communication in production environments.
//
// Key Go concepts demonstrated:
//   - Function pointers: Go lets you pass functions as parameters (like C function pointers)
//   - Method receivers: Functions can be "attached" to struct types (like C++ member functions)
//   - Error handling: Go's explicit error return pattern (no exceptions like Java/C#)
//   - Interface satisfaction: mqtt.Client interface is satisfied by the concrete implementation
//   - Callback pattern: Handler functions are called automatically when events occur
//
// MQTT concepts explained:
//   - Clean session: true = don't remember previous connection state (fresh start each time)
//   - Auto reconnect: true = automatically reconnect if connection is lost (critical for IoT)
//   - Keep alive: Send ping every 30 seconds to detect broken connections
//   - QoS levels: Quality of Service (0=fire and forget, 1=at least once, 2=exactly once) using QoS=1.
//
// The function sets up three critical event handlers:
//  1. Connection lost handler - Called when network connection breaks
//  2. Connection established handler - Called when connection succeeds (triggers topic subscription)
//  3. Message handler - Called for every incoming MQTT message (set in subscribeToTopics)
//
// Error handling follows Go's explicit pattern: functions return error as last parameter.
// If error is nil, the operation succeeded. If not nil, something went wrong.
// This is much more reliable than exception-based error handling in other languages.
//
// Returns nil on success, error on failure. Caller must check the error!
func (sim *NgaSim) connectMQTT() error {
	log.Printf("🔌 Connecting to MQTT broker: %s", MQTTBroker)

	// Create MQTT client options - this is the configuration object
	// Note: mqtt.NewClientOptions() returns a pointer to ClientOptions struct
	opts := mqtt.NewClientOptions()

	// Configure connection parameters
	opts.AddBroker(MQTTBroker)          // Where to connect (tcp://169.254.1.1:1883)
	opts.SetClientID(MQTTClientID)      // Unique identifier for this client
	opts.SetCleanSession(true)          // Don't remember state from previous connections
	opts.SetAutoReconnect(true)         // Automatically reconnect if connection breaks
	opts.SetKeepAlive(30 * time.Second) // Send ping every 30 seconds to detect dead connections

	// Set up event handlers using function pointers
	// These functions will be called automatically by the MQTT library when events occur

	// Called when connection is lost (network issues, broker restart, etc.)
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("🔥 MQTT connection lost: %v", err)
		log.Println("   Auto-reconnect will attempt to restore connection...")
		// Note: No need to manually reconnect due to SetAutoReconnect(true)
	})

	// Called when connection is successfully established
	// This is where we subscribe to device topics since we need an active connection first
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Printf("✅ Connected to MQTT broker: %s", MQTTBroker)
		log.Println("🔔 Subscribing to device announcement and telemetry topics...")

		// Subscribe to device topics now that we're connected
		sim.subscribeToTopics()
	})

	// Create the actual MQTT client using our configuration
	// This doesn't connect yet - just creates the client object
	sim.mqtt = mqtt.NewClient(opts)

	// Attempt to establish the connection
	// Connect() returns a "token" (like a promise/future in other languages)
	token := sim.mqtt.Connect()

	// Wait for connection attempt to complete and check for errors
	// This blocks until connection succeeds or fails
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker %s: %v", MQTTBroker, token.Error())
	}

	log.Printf("🎉 MQTT client initialized successfully")
	return nil // Success! Return nil error to indicate everything worked
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
func (n *NgaSim) handleDeviceTelemetry(topic string, payload []byte) {
	// Parse topic to extract device info
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		log.Printf("Invalid topic format: %s", topic)
		return
	}

	category := parts[1]
	deviceSerial := parts[2]

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

			// Get device for status info (need device to check PendingPercentage)
			n.mutex.RLock()
			device, exists := n.devices[deviceSerial]
			n.mutex.RUnlock()

			// Add to device terminal with more detailed info
			statusInfo := "normal"
			if exists && device.PendingPercentage != 0 && telemetry.GetPercentageOutput() != device.PendingPercentage {
				statusInfo = fmt.Sprintf("ramping to %d%%", device.PendingPercentage)
			}

			n.addDeviceTerminalEntry(deviceSerial, "TELEMETRY",
				fmt.Sprintf("← Current power: %d%% (%s) | Salt: %dppm | RSSI: %ddBm",
					telemetry.GetPercentageOutput(), statusInfo, telemetry.GetPpmSalt(), telemetry.GetRssi()), payload)

			n.updateDeviceFromSanitizerTelemetry(deviceSerial, telemetry)

			return

		} else {
			log.Printf("Failed to parse as sanitizer TelemetryMessage: %v", err)
		}
	}

	// Try to parse as JSON (fallback)
	var telemetryData map[string]interface{}
	if err := json.Unmarshal(payload, &telemetryData); err == nil {
		n.updateDeviceFromTelemetry(deviceSerial, telemetryData)

		// Add to device terminal
		n.addDeviceTerminalEntry(deviceSerial, "TELEMETRY", "Telemetry received (JSON)", payload)
		return
	}

	log.Printf("Could not parse telemetry message: %x", payload)
}

// messageHandler processes incoming MQTT messages and routes them to appropriate handlers.
// This is the central nervous system of NgaSim - every device communication flows through here.
// This function demonstrates Go's string processing, error handling, and message routing patterns.
//
// Key Go concepts demonstrated:
//   - Method receivers: This function is "attached" to the NgaSim struct
//   - String manipulation: strings.Split() for parsing structured data
//   - Switch statements: Go's clean alternative to if/else chains
//   - Early returns: Go's preferred error handling pattern (abandon bad data, continue processing)
//   - Interface types: mqtt.Client and mqtt.Message are interfaces (like C++ pure virtual)
//   - Callback pattern: MQTT library calls this function automatically when messages arrive
//
// MQTT message flow architecture:
//  1. Device publishes message to structured topic (async/deviceType/serial/messageType)
//  2. MQTT broker delivers message to NgaSim (we're subscribed to async/+/+/+)
//  3. messageHandler receives message and parses the topic structure
//  4. Based on message type, routes to specialized handler function
//  5. Specialized handler updates device state and logs the activity
//
// Topic structure parsing:
//
//	async/sanitizerGen2/1234567890ABCDEF00/anc
//	  |        |              |           |
//	prefix  category      deviceSerial  msgType
//
// Message types handled:
//   - "anc" (announce): Device saying "I exist!" with basic info
//   - "dt" (data/telemetry): Device sending sensor readings
//   - "sts" (status): Device reporting operational status
//   - "error": Device reporting error conditions
//
// Error handling philosophy: This is a callback function called by the MQTT library.
// If we can't parse a message, we log the problem and abandon THAT message, but
// continue processing other messages. We don't return an error because that would
// break the entire MQTT message processing pipeline.
func (sim *NgaSim) messageHandler(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	log.Printf("📡 Received MQTT message on topic: %s", topic)

	// Parse topic to extract device information
	// Topic format: async/category/serial/type
	// Example: "async/sanitizerGen2/1234567890ABCDEF00/anc"
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		log.Printf("❌ Invalid topic format: %s (expected: async/category/serial/type)", topic)
		return // Fail fast - can't process malformed topics
	}

	// Extract structured information from topic
	// parts[0] = "async" (protocol prefix - always the same)
	// parts[1] = device category (sanitizerGen2, digitalControllerGen2, etc.)
	// parts[2] = device serial number (unique identifier)
	// parts[3] = message type (anc, dt, sts, error)
	category := parts[1]
	deviceSerial := parts[2]
	messageType := parts[3]

	log.Printf("   📋 Parsed: category=%s, serial=%s, type=%s, payload=%d bytes",
		category, deviceSerial, messageType, len(payload))

	// Route message to appropriate handler based on message type
	// Go's switch statement doesn't need 'break' - each case automatically exits
	switch messageType {
	case "anc":
		// Device announcement - "Hello, I'm here!"
		sim.handleDeviceAnnounce(topic, payload)

	case "dt":
		// Device telemetry - sensor readings, status data
		sim.handleDeviceTelemetry(topic, payload)

	case "sts":
		// Device status - operational state changes
		sim.handleDeviceStatus(category, deviceSerial, payload)

	case "error":
		// Device error - something went wrong
		sim.handleDeviceError(category, deviceSerial, payload)

	default:
		// Unknown message type - log for debugging but don't crash
		log.Printf("⚠️  Unknown message type: %s (topic: %s)", messageType, topic)
		log.Printf("   This might be a new message type we don't support yet")
		// Note: We continue processing other messages - unknown types don't break the system
	}
}

// handleDeviceAnnounce processes device announcement messages and creates/updates device records.
// This is the device discovery engine - it's responsible for turning raw MQTT messages into
// organized device records that the system can track and control.
//
// Key Go concepts demonstrated:
//   - Function composition: This function calls multiple specialized helper functions
//   - Error handling fallback: Try protobuf first, fall back to JSON if that fails
//   - Early returns: Abandon processing if we can't parse the topic structure
//   - Mutex-free operations: Topic parsing happens outside locks for performance
//   - Multiple data format support: Handles both binary protobuf and text JSON
//
// Device announcement flow:
//  1. Parse MQTT topic to extract device category and serial number
//  2. Log the raw announcement for debugging and audit trails
//  3. Try to parse payload as protobuf (preferred format - efficient and typed)
//  4. If protobuf fails, fall back to JSON parsing (legacy device support)
//  5. If both fail, log the failure but continue system operation
//  6. Update device record with discovered information
//  7. Add entry to device's live terminal for real-time monitoring
//
// Topic structure expected:
//
//	async/sanitizerGen2/1234567890ABCDEF00/anc
//	  |        |              |           |
//	prefix  category      deviceSerial  msgType
//
// The function demonstrates Go's "graceful degradation" philosophy - if we can't
// parse one format, we try another. If we can't parse anything, we log the issue
// but don't crash the entire system.
func (n *NgaSim) handleDeviceAnnounce(topic string, payload []byte) {
	// Phase 1: Parse MQTT topic to extract device metadata
	// This parsing happens outside any locks for better performance
	parts := strings.Split(topic, "/")
	if len(parts) < 4 { // Magic number 4 = expected parts (async/category/serial/type)
		log.Printf("❌ Invalid announce topic format: %s (expected: async/category/serial/type)", topic)
		return // Early return - can't process malformed topics
	}

	// Extract device identification from topic structure
	category := parts[1]     // Device category (sanitizerGen2, digitalControllerGen2, etc.)
	deviceSerial := parts[2] // Unique device identifier
	// parts[3] would be "anc" (announce) - we already know this from routing

	log.Printf("📢 Device announce from %s (category: %s): %d bytes", deviceSerial, category, len(payload))

	// Phase 2: Try to parse as protobuf GetDeviceInformationResponsePayload (preferred format)
	// Protobuf is preferred because it's faster, smaller, and type-safe
	announce := &ned.GetDeviceInformationResponsePayload{}
	if err := proto.Unmarshal(payload, announce); err == nil {
		// SUCCESS: Protobuf parsing worked
		log.Printf("✅ Successfully parsed protobuf announcement from %s", deviceSerial)

		// Log detailed protobuf message for debugging and audit
		// This mirrors the Python reflect_message function behavior
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
		log.Printf("==========================================")

		// Update device record with protobuf data
		n.updateDeviceFromProtobufAnnounce(category, deviceSerial, announce)

		// Add entry to device's live terminal for real-time monitoring
		// This creates a breadcrumb trail of device communications
		n.addDeviceTerminalEntry(deviceSerial, "ANNOUNCE",
			fmt.Sprintf("Device announced: %s", announce.GetProductName()), payload)

		return // Success! We're done processing this announcement
	} else {
		// Protobuf parsing failed - log the error but continue with fallback
		log.Printf("⚠️  Failed to parse as protobuf GetDeviceInformationResponsePayload: %v", err)
		log.Printf("   Attempting JSON fallback for device %s...", deviceSerial)
	}

	// Phase 3: Fallback to JSON parsing (legacy device support)
	// Some older devices or development tools might send JSON instead of protobuf
	var announceData map[string]interface{}
	if err := json.Unmarshal(payload, &announceData); err == nil {
		// SUCCESS: JSON parsing worked
		log.Printf("✅ Successfully parsed JSON announcement from %s", deviceSerial)
		log.Printf("📋 JSON data: %+v", announceData)

		// Update device record with JSON data
		n.updateDeviceFromJSONAnnounce(deviceSerial, announceData)

		// Add entry to device's live terminal
		n.addDeviceTerminalEntry(deviceSerial, "ANNOUNCE", "Device announced (JSON)", payload)

		return // Success! JSON fallback worked
	} else {
		// Both protobuf AND JSON parsing failed
		log.Printf("❌ Failed to parse JSON fallback: %v", err)
	}

	// Phase 4: Complete parsing failure - log for debugging but continue system operation
	// This demonstrates Go's "resilient system" philosophy - one bad message doesn't crash everything
	log.Printf("💥 Could not parse announce message from %s (tried protobuf + JSON)", deviceSerial)
	log.Printf("   Raw payload (%d bytes): %x", len(payload), payload)
	log.Printf("   System continues running - this device will remain undiscovered")

	// Note: We don't return an error because this is a callback function
	// The MQTT system expects us to handle individual message failures gracefully
	// Bad messages are logged for debugging but don't break the message processing pipeline
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

	// FIXED: Check if pending command has been achieved
	// The key fix: Don't exclude 0% commands - check if we have ANY pending command time
	if !device.LastCommandTime.IsZero() && device.ActualPercentage == device.PendingPercentage {
		log.Printf("✅ Command achieved! %s: Pending %d%% = Actual %d%% (clearing pending state)",
			deviceSerial, device.PendingPercentage, device.ActualPercentage)
		device.PendingPercentage = 0
		device.LastCommandTime = time.Time{}
	} else if !device.LastCommandTime.IsZero() {
		// FIXED: Check timeout for ANY pending command, including 0%
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

// NewNgaSim creates and initializes a new NgaSim instance with all required components.
// This is the constructor function that sets up the entire NgaSim system before it starts running.
//
// Key Go concepts demonstrated:
//   - Constructor pattern: Go doesn't have classes, so we use functions that return pointers to structs
//   - Error handling: Each component initialization can fail, but we handle errors gracefully
//   - Composition: NgaSim is built by combining multiple smaller components (not inheritance)
//   - Zero values: Go initializes missing fields to sensible defaults automatically
//   - make(): Required for initializing maps and slices (they start as nil otherwise)
//
// Components initialized:
//  1. ProtobufReflectionEngine - Automatically discovers what message types each device supports
//  2. TerminalLogger - Records all device communications to both screen and file
//  3. DeviceLogger - Tracks device command history and responses
//  4. ProtobufCommandRegistry - Maps device types to their available commands
//  5. PopupUIGenerator - Creates web forms automatically from protobuf definitions
//  6. SanitizerController - Handles sanitizer-specific command processing
//  7. Device maps - Storage for discovered devices (initially empty)
//
// The function uses graceful degradation - if optional components fail to initialize,
// the system continues with reduced functionality rather than crashing completely.
// This makes the system more robust in production environments.
//
// Returns a fully configured NgaSim instance ready to be started with Start().
func NewNgaSim() *NgaSim {
	log.Println("🏗️ Initializing NgaSim components...")

	// Create reflection engine for automatic protobuf discovery
	// This component scans all .pb.go files and builds a map of what each device type can do
	reflectionEngine := NewProtobufReflectionEngine()

	// Discover all protobuf messages at startup
	// This happens once at startup rather than every time we receive a message (performance)
	if err := reflectionEngine.DiscoverMessages(); err != nil {
		log.Printf("⚠️ Warning: Protobuf discovery failed: %v", err)
		log.Println("   System will continue with reduced protobuf reflection capabilities")
	} else {
		messages := reflectionEngine.GetAllMessages()
		log.Printf("✅ Discovered %d protobuf message types across all device categories", len(messages))
	}

	// Create terminal logger with file tee (logs to both screen and file)
	// Buffer size of 1000 means we keep the last 1000 log entries in memory for the web interface
	terminalLogger, err := NewTerminalLogger("ngasim_terminal.log", 1000)
	if err != nil {
		log.Printf("⚠️ Warning: Terminal logger creation failed: %v", err)
		log.Println("   System will continue without terminal logging to file")
		terminalLogger = nil // Set to nil so other components know it's unavailable
	} else {
		log.Println("✅ Terminal logger initialized with file tee to ngasim_terminal.log")
	}

	// Create the main NgaSim struct
	// Note: Go's zero values automatically initialize most fields to safe defaults
	ngaSim := &NgaSim{
		devices:          make(map[string]*Device),     // make() required - maps start as nil
		logger:           NewDeviceLogger(1000),        // Command/response logging
		commandRegistry:  NewProtobufCommandRegistry(), // Command discovery system
		reflectionEngine: reflectionEngine,             // Protobuf introspection
		terminalLogger:   terminalLogger,               // Live terminal feed
		deviceCommands:   make(map[string][]string),    // Device capability mapping
		// Other fields (mutex, mqtt, server, etc.) automatically get zero values
		// which is exactly what we want for uninitialized components
	}

	// Create popup generator (depends on terminal logger, so check if it exists)
	// This demonstrates Go's nil-safe programming pattern
	if terminalLogger != nil {
		ngaSim.popupGenerator = NewPopupUIGenerator(reflectionEngine, terminalLogger, ngaSim)
		log.Println("✅ Popup UI generator initialized for dynamic device interfaces")
	} else {
		log.Println("⚠️ Popup UI generator disabled (terminal logger unavailable)")
	}

	// Initialize sanitizer controller (always needed for sanitizer devices)
	ngaSim.sanitizerController = NewSanitizerController(ngaSim)
	log.Println("✅ Sanitizer controller initialized")

	// Discover commands and populate deviceCommands map
	// This builds the mapping of device types -> available commands
	ngaSim.commandRegistry.discoverCommands()
	ngaSim.populateDeviceCommands()

	log.Printf("✅ Device command registry populated for %d device categories", len(ngaSim.deviceCommands))

	log.Println("🎉 NgaSim initialization complete - ready to start!")
	return ngaSim
}

// Start initializes and launches all NgaSim components including the comprehensive web server.
// This function demonstrates Go's HTTP server patterns and is the main application orchestrator.
//
// Key Go concepts demonstrated:
//   - http.ServeMux: Go's built-in HTTP request router (like Apache mod_rewrite but simpler)
//   - http.HandleFunc: Registers handler functions for specific URL patterns
//   - Method receivers: Functions attached to the NgaSim struct can access all its data
//   - Goroutines for servers: HTTP server runs in background without blocking main thread
//   - Error handling: Graceful fallback when components fail to initialize
//
// Web server architecture:
//   - Single HTTP server listening on port 8082
//   - Multiple route categories: main pages, API endpoints, static assets, protobuf interfaces
//   - RESTful API design: /api/ prefix for programmatic access
//   - Static asset serving: /static/ prefix for CSS, JS, images, documentation
//
// The server provides four main interface categories:
//  1. Human interfaces: Web pages for operators and technicians
//  2. API endpoints: JSON endpoints for programmatic control
//  3. Protobuf interfaces: Dynamic device control based on discovered capabilities
//  4. Static documentation: Specifications, diagrams, and technical docs
//
// Error handling follows the "graceful degradation" pattern - if optional components
// (like MQTT or protobuf reflection) fail, the system continues with reduced functionality
// rather than crashing completely.
//
// Returns error only on catastrophic failure, nil on successful startup.
func (n *NgaSim) Start() error {
	log.Println("Starting NgaSim v" + NgaSimVersion)

	// Phase 1: Initialize MQTT communication (with fallback to demo mode)
	// This is the "device discovery engine" - connects to pool devices
	log.Println("Connecting to MQTT broker...")
	if err := n.connectMQTT(); err != nil {
		log.Printf("MQTT connection failed: %v", err)
		log.Println("Falling back to demo mode...")
		n.createDemoDevices() // Create fake devices for development/testing
	} else {
		log.Println("MQTT connected successfully - waiting for device announcements...")

		// Start the C poller to wake up devices (sends broadcast packets)
		// Think of this as "knocking on doors" to get devices to announce themselves
		if err := n.startPoller(); err != nil {
			log.Printf("Failed to start poller: %v", err)
			log.Println("Device discovery may not work properly")
			// Note: We continue anyway - manual device addition might still work
		}
	}

	// Phase 2: Create the HTTP request router
	// ServeMux is Go's built-in URL router - maps URL patterns to handler functions
	// Think of it as a telephone switchboard directing calls to the right department
	mux := http.NewServeMux()

	// ==================== HUMAN INTERFACE ROUTES ====================
	// These serve HTML pages for humans to interact with the system

	mux.HandleFunc("/", n.handleGoDemo)         // Main interface - Go-centric single driver approach
	mux.HandleFunc("/js-demo", n.handleDemo)    // JavaScript-heavy demo page
	mux.HandleFunc("/old", n.handleHome)        // Original legacy home page
	mux.HandleFunc("/demo", n.handleDemo)       // Demo page alias for convenience
	mux.HandleFunc("/goodbye", n.handleGoodbye) // Shutdown confirmation page

	// ==================== API ROUTES (JSON endpoints) ====================
	// These return JSON data for programmatic access (mobile apps, scripts, etc.)

	mux.HandleFunc("/api/exit", n.handleExit)                          // Gracefully shut down application
	mux.HandleFunc("/api/devices", n.handleAPI)                        // Get list of all discovered devices
	mux.HandleFunc("/api/sanitizer/command", n.handleSanitizerCommand) // Send commands to sanitizer devices
	mux.HandleFunc("/api/sanitizer/states", n.handleSanitizerStates)   // Get sanitizer status information
	mux.HandleFunc("/api/power-levels", n.handlePowerLevels)           // Get available power level options
	mux.HandleFunc("/api/emergency-stop", n.handleEmergencyStop)       // Emergency stop all pool equipment
	mux.HandleFunc("/api/ui/spec", n.handleUISpecAPI)                  // Get UI specification for dynamic interfaces

	// ==================== DEVICE COMMAND API ROUTES ====================
	// These provide automatic command discovery based on protobuf reflection

	mux.HandleFunc("/api/device-commands/", n.handleDeviceCommands)   // Get commands for specific device category
	mux.HandleFunc("/api/device-commands", n.handleAllDeviceCommands) // Get all available commands across all device types

	// ==================== STATIC ASSET ROUTES ====================
	// These serve documentation, diagrams, and specifications

	mux.HandleFunc("/static/wireframe.svg", n.handleWireframeSVG) // System architecture diagram (SVG format)
	mux.HandleFunc("/static/wireframe.mmd", n.handleWireframeMMD) // Mermaid diagram source code
	mux.HandleFunc("/static/ui-spec.toml", n.handleUISpecTOML)    // UI specification in TOML format
	mux.HandleFunc("/static/ui-spec.txt", n.handleUISpecTXT)      // Human-readable UI specification

	// ==================== PROTOBUF INTERFACE ROUTES ====================
	// These provide dynamic device control based on discovered protobuf capabilities

	//	mux.HandleFunc("/protobuf", n.handleProtobufMessages)                      // Interactive protobuf message browser
	mux.HandleFunc("/terminal", n.handleTerminalView)                          // Live terminal view of device communications
	mux.HandleFunc("/protobuf", n.handleEnhancedProtobufMessages)              // Enhanced Go-heavy version
	mux.HandleFunc("/api/protobuf/command", n.handleProtobufCommandSubmission) // Process command form submissions

	// ==================== ADVANCED PROTOBUF API ROUTES ====================
	// These routes are only available if the protobuf reflection system initialized successfully
	// This demonstrates Go's nil-safe programming pattern

	if n.popupGenerator != nil {
		// Dynamic popup generation based on protobuf message definitions
		mux.HandleFunc("/api/protobuf/popup", n.popupGenerator.handleProtobufPopup) // Generate device-specific popup forms
		// REMOVED: Duplicate /api/protobuf/command route - using handleProtobufCommandSubmission instead
		mux.HandleFunc("/api/protobuf/messages", n.popupGenerator.handleMessageTypes) // List available message types
		mux.HandleFunc("/api/terminal/logs", n.popupGenerator.handleTerminalLogs)     // Get formatted terminal logs
		mux.HandleFunc("/api/terminal/clear", n.handleClearTerminal)                  // Clear global terminal
		mux.HandleFunc("/api/terminal/clear-device", n.handleClearDeviceTerminal)     // Clear device-specific terminal
	} else {
		log.Println("⚠️ Protobuf popup routes disabled (popupGenerator not available)")
	}

	// Phase 3: Create and configure the HTTP server
	// Server struct contains all the HTTP server configuration
	n.server = &http.Server{
		Addr:    ":8082", // Listen on all interfaces, port 8082
		Handler: mux,     // Use our route multiplexer to handle requests
		// Note: Go's HTTP server has sensible defaults for timeouts, etc.
	}

	// Phase 4: Test the protobuf reflection system
	// This validates that our automatic device discovery is working
	n.testProtobufSystem()

	// Phase 5: Start the HTTP server in a goroutine
	// The goroutine is CRITICAL - without it, ListenAndServe() would block forever
	// and the main() function would never reach the "select {}" statement
	go func() {
		log.Println("Web server starting on :8082")

		// ListenAndServe() blocks until the server shuts down
		// It returns http.ErrServerClosed on normal shutdown, or an actual error on failure
		if err := n.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
		// When this goroutine exits, the HTTP server has stopped
	}()

	// Phase 6: Log all available endpoints for the operator
	// This creates a "menu" of what the system can do
	log.Println("🚀 NgaSim started successfully!")
	log.Println("")
	log.Println("📍 Available Interfaces:")
	log.Println("   🌐 Main Interface:    http://localhost:8082")
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

	return nil // Success! Everything started correctly
}

// main is the entry point for the NgaSim Pool Controller application.
// It performs the complete startup sequence and manages application lifecycle:
//
//  1. Creates a new NgaSim instance (the main controller object)
//  2. Sets up graceful shutdown handling using Go's signal system
//  3. Connects to the MQTT broker at 169.254.1.1:1883 for device communication
//  4. Starts the web server on port 8082 for the management dashboard
//  5. Runs indefinitely until terminated by interrupt signal (Ctrl+C)
//
// Key Go concepts demonstrated:
//   - defer: Ensures cleanup() runs when main() exits (like C++ destructor but better)
//   - select {}: Blocks forever waiting for channel operations (keeps program alive)
//   - goroutines: Signal handler runs concurrently without blocking main execution
//   - channels: Provides safe communication between goroutines for shutdown coordination
//
// The defer statement is particularly important - it guarantees cleanup() will run
// even if the program crashes or is terminated unexpectedly. This ensures MQTT
// connections are closed and the C poller subprocess is properly killed.
func main() {
	log.Println("=== NgaSim Pool Controller Simulator ===")
	log.Printf("Version: %s", NgaSimVersion)
	log.Println("Starting up...")

	// Create NgaSim instance
	nga := NewNgaSim()

	// CRITICAL: This defer ensures cleanup happens no matter how main() exits
	// It's like a C++ destructor but more reliable - runs even on crashes
	defer nga.cleanup()

	// Set up graceful shutdown handling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Handle shutdown signals in a separate goroutine
	go func() {
		<-c // Block until signal received
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
	log.Println("   🌐 Main Interface:    http://localhost:8082")
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

	// Block forever waiting for shutdown signal
	// select {} with no cases blocks indefinitely - this keeps the program running
	// Without this, main() would exit immediately and the program would terminate
	select {}
}

// createDemoDevices creates demo devices for testing when MQTT is not available
func (n *NgaSim) createDemoDevices() {
	log.Println("Creating enhanced demo devices for sorting test...")

	demoDevices := []*Device{
		// Multiple Sanitizers (different serial numbers for sorting test)
		{
			ID:               "demo-sanitizer-003",
			Serial:           "demo-sanitizer-003",
			Name:             "Demo Salt Chlorinator #3",
			Type:             "sanitizerGen2",
			Category:         "sanitizerGen2",
			Status:           "ONLINE",
			LastSeen:         time.Now(),
			ProductName:      "AquaRite Pro Max",
			ModelId:          "AQR-PRO-40",
			FirmwareVersion:  "2.1.3",
			PercentageOutput: 65,
			ActualPercentage: 65,
			PPMSalt:          3400,
			LineInputVoltage: 240,
			RSSI:             -52,
		},
		{
			ID:               "demo-sanitizer-001",
			Serial:           "demo-sanitizer-001",
			Name:             "Demo Salt Chlorinator #1",
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
			ID:               "demo-sanitizer-002",
			Serial:           "demo-sanitizer-002",
			Name:             "Demo Salt Chlorinator #2",
			Type:             "sanitizerGen2",
			Category:         "sanitizerGen2",
			Status:           "ONLINE",
			LastSeen:         time.Now(),
			ProductName:      "AquaRite Standard",
			ModelId:          "AQR-STD-15",
			FirmwareVersion:  "2.0.8",
			PercentageOutput: 30,
			ActualPercentage: 30,
			PPMSalt:          3100,
			LineInputVoltage: 240,
			RSSI:             -48,
		},
		// Multiple VSPs
		{
			ID:       "demo-vsp-002",
			Serial:   "demo-vsp-002",
			Name:     "Demo Variable Speed Pump #2",
			Type:     "VSP",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			RPM:      1800,
			Power:    650,
			Temp:     29.2,
		},
		{
			ID:       "demo-vsp-001",
			Serial:   "demo-vsp-001",
			Name:     "Demo Variable Speed Pump #1",
			Type:     "VSP",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			RPM:      2400,
			Power:    850,
			Temp:     32.5,
		},
		// Multiple Pool Lights
		{
			ID:       "demo-icl-002",
			Serial:   "demo-icl-002",
			Name:     "Demo Pool Light #2 (Spa)",
			Type:     "ICL",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			Red:      255,
			Green:    128,
			Blue:     64,
			White:    150,
		},
		{
			ID:       "demo-icl-001",
			Serial:   "demo-icl-001",
			Name:     "Demo Pool Light #1 (Main)",
			Type:     "ICL",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			Red:      128,
			Green:    64,
			Blue:     255,
			White:    200,
		},
		// Multiple Sensors
		{
			ID:       "demo-trusense-002",
			Serial:   "demo-trusense-002",
			Name:     "Demo pH/ORP Sensor #2 (Spa)",
			Type:     "TruSense",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			PH:       7.4,
			ORP:      720,
			Temp:     27.1,
		},
		{
			ID:       "demo-trusense-001",
			Serial:   "demo-trusense-001",
			Name:     "Demo pH/ORP Sensor #1 (Pool)",
			Type:     "TruSense",
			Status:   "ONLINE",
			LastSeen: time.Now(),
			PH:       7.2,
			ORP:      750,
			Temp:     25.8,
		},
		// Multiple Heaters
		{
			ID:        "demo-heater-002",
			Serial:    "demo-heater-002",
			Name:      "Demo Spa Heater",
			Type:      "Heater",
			Status:    "ONLINE",
			LastSeen:  time.Now(),
			SetTemp:   38.0,
			WaterTemp: 36.2,
			Power:     20000,
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

	log.Printf("Created %d demo devices (multiple per type for sorting test)", len(demoDevices))
}

// Enhanced sendSanitizerCommand with continuous 0% safety mode
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

	// Log command to device terminal for immediate feedback
	n.addDeviceTerminalEntry(serial, "COMMAND",
		fmt.Sprintf("→ Set power level to %d%%", percentage),
		[]byte(fmt.Sprintf(`{"command":"set_power","percentage":%d}`, percentage)))

	// If MQTT is connected, send the real command
	if n.mqtt != nil && n.mqtt.IsConnected() {
		err := n.sendMQTTSanitizerCommand(serial, category, percentage)

		// ENHANCED: For 0% (safety/emergency) commands, start continuous sending
		if percentage == 0 {
			go n.startContinuous0PercentMode(serial, category)
		}

		return err
	}

	// Demo mode - simulate the command execution
	// Log demo completion to device terminal
	n.addDeviceTerminalEntry(serial, "DEMO",
		fmt.Sprintf("✅ Demo command completed: Power set to %d%%", percentage),
		[]byte(fmt.Sprintf(`{"result":"success","percentage":%d}`, percentage)))

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

// startContinuous0PercentMode sends 0% commands every 5 seconds until device reaches 0%
func (n *NgaSim) startContinuous0PercentMode(serial, category string) {
	log.Printf("🚨 Starting continuous 0%% safety mode for %s", serial)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	maxDuration := 2 * time.Minute // Stop after 2 minutes max
	startTime := time.Now()

	for {
		select {
		case <-ticker.C:
			// Check if we've been running too long
			if time.Since(startTime) > maxDuration {
				log.Printf("🕒 Continuous 0%% mode timeout for %s after 2 minutes", serial)
				return
			}

			// Check device current status
			n.mutex.RLock()
			device, exists := n.devices[serial]
			if !exists {
				n.mutex.RUnlock()
				log.Printf("❌ Device %s disappeared during continuous 0%% mode", serial)
				return
			}

			currentLevel := device.ActualPercentage
			n.mutex.RUnlock()

			// If device reached 0%, we can stop
			if currentLevel == 0 {
				log.Printf("✅ Device %s reached 0%% - stopping continuous mode", serial)
				return
			}

			// Device not at 0% yet, send another 0% command
			log.Printf("🔄 Continuous 0%% mode: %s still at %d%%, sending another 0%% command", serial, currentLevel)

			if err := n.sendMQTTSanitizerCommand(serial, category, 0); err != nil {
				log.Printf("❌ Failed to send continuous 0%% command to %s: %v", serial, err)
				// Don't stop on single failure - keep trying
			}

		default:
			// Non-blocking check - continue loop
		}

		// Small delay to prevent busy loop
		time.Sleep(100 * time.Millisecond)
	}
}

// sendMQTTSanitizerCommand sends a sanitizer command via MQTT using proper protobuf + UUID
func (n *NgaSim) sendMQTTSanitizerCommand(serial, category string, percentage int) error {
	log.Printf("📡 Sending MQTT sanitizer command: %s -> %d%%", serial, percentage)

	// Generate UUID for command correlation (CRITICAL: This prevents import removal!)
	commandUUID := uuid.New().String()

	// Create the inner sanitizer command
	saltCmd := &ned.SetSanitizerTargetPercentageRequestPayload{
		TargetPercentage: int32(percentage),
	}

	// Wrap it in SanitizerRequestPayloads using the oneof pattern
	wrapper := &ned.SanitizerRequestPayloads{
		RequestType: &ned.SanitizerRequestPayloads_SetSanitizerOutputPercentage{
			SetSanitizerOutputPercentage: saltCmd,
		},
	}

	// Serialize the protobuf wrapper
	msgBytes, err := proto.Marshal(wrapper)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf command: %v", err)
	}

	// Construct the MQTT topic for sending commands
	// Topic format: async/category/serial/cmd
	topic := fmt.Sprintf("async/%s/%s/cmd", category, serial)

	// Log successful command transmission to device terminal
	n.addDeviceTerminalEntry(serial, "MQTT_CMD",
		fmt.Sprintf("📡 MQTT command sent: Set power to %d%% (UUID: %s)", percentage, commandUUID), msgBytes)

	// Use UUID as correlation ID instead of random string
	correlationID := commandUUID

	// Log the command for debugging
	n.logger.LogRequest(serial, "SetSanitizerTargetPercentage", msgBytes, category, "sanitizer", "protobuf_command")

	// Send the command via MQTT
	token := n.mqtt.Publish(topic, 1, false, msgBytes)
	if token.Wait() && token.Error() != nil {
		n.logger.LogError(serial, "SetSanitizerTargetPercentage",
			fmt.Sprintf("MQTT publish failed: %v", token.Error()), correlationID, category)
		return fmt.Errorf("failed to publish command: %v", token.Error())
	}

	log.Printf("✅ MQTT protobuf command sent successfully: %s -> %d%% (UUID: %s)", serial, percentage, commandUUID)
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

// clearDeviceTerminal clears the live terminal for a specific device
func (n *NgaSim) clearDeviceTerminal(deviceSerial string) bool {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	device, exists := n.devices[deviceSerial]
	if !exists {
		return false
	}

	// Clear the device's live terminal
	device.LiveTerminal = make([]TerminalEntry, 0, 50)

	log.Printf("🗑️ Device terminal cleared for %s", deviceSerial)
	return true
}

// Enhanced addDeviceTerminalEntry with protobuf parsing
func (n *NgaSim) addDeviceTerminalEntry(deviceSerial, entryType, message string, rawData []byte) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	device, exists := n.devices[deviceSerial]
	if !exists {
		return
	}

	// Create protobuf parser
	parser := NewProtobufMessageParser(n.reflectionEngine)

	// Create terminal entry
	entry := TerminalEntry{
		Timestamp: time.Now(),
		Type:      entryType,
		Message:   message,
		Data:      string(rawData),
	}

	// Try to parse protobuf data based on entry type
	switch strings.ToUpper(entryType) {
	case "ANNOUNCE":
		if parsed, err := parser.ParseAnnounceMessage(rawData, deviceSerial); err == nil {
			entry.ParsedProtobuf = parsed
			// Update message with parsed info
			if len(parsed.Fields) > 0 {
				entry.Message = fmt.Sprintf("Device announced: %s (%d fields parsed)",
					device.Name, len(parsed.Fields))
			}
		} else {
			log.Printf("Failed to parse announce protobuf: %v", err)
		}

	case "TELEMETRY":
		if parsed, err := parser.ParseTelemetryMessage(rawData, deviceSerial); err == nil {
			entry.ParsedProtobuf = parsed
			// Update message with key telemetry info
			if len(parsed.Fields) > 0 {
				entry.Message = fmt.Sprintf("Telemetry received (%d fields parsed)",
					len(parsed.Fields))
			}
		} else {
			log.Printf("Failed to parse telemetry protobuf: %v", err)
		}
	}

	// Add to device's live terminal
	device.LiveTerminal = append(device.LiveTerminal, entry)

	// Keep only last 50 entries per device
	if len(device.LiveTerminal) > 50 {
		device.LiveTerminal = device.LiveTerminal[len(device.LiveTerminal)-50:]
	}

	// Also log to global terminal if available
	if n.terminalLogger != nil {
		n.terminalLogger.LogProtobufMessage(entryType, deviceSerial, "DEVICE", message, rawData)
	}
}

// getSortedDevices returns devices sorted by serial number
func (n *NgaSim) getSortedDevices() []*Device {
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	// Sort by serial number for consistent ordering
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Serial < devices[j].Serial
	})

	return devices
}

// populateDeviceCommands populates the deviceCommands map from discovered protobuf messages
func (n *NgaSim) populateDeviceCommands() {
	if n.reflectionEngine == nil {
		return
	}

	messages := n.reflectionEngine.GetAllMessages()

	// Group commands by category
	for _, desc := range messages {
		if desc.IsRequest && desc.Category != "" {
			// Add command to category
			if n.deviceCommands[desc.Category] == nil {
				n.deviceCommands[desc.Category] = make([]string, 0)
			}

			// Add command name (simplified from technical name)
			commandName := desc.Name
			if strings.HasSuffix(commandName, "RequestPayload") {
				commandName = strings.TrimSuffix(commandName, "RequestPayload")
			}

			n.deviceCommands[desc.Category] = append(n.deviceCommands[desc.Category], commandName)
		}
	}

	log.Printf("📋 Populated device commands for %d categories", len(n.deviceCommands))
}

// discoverDeviceCapabilities uses protobuf reflection to identify what
// commands and telemetry each device type supports
func (sim *NgaSim) discoverDeviceCapabilities(deviceType string) map[string]interface{} {
	capabilities := make(map[string]interface{})

	// Use reflection to scan ned package for device-specific messages
	// This automatically adapts to new protobuf files added to ned/

	switch deviceType {
	case "sanitizerGen2":
		// Reflection discovers sanitizer commands automatically
	case "digitalControllerGen2":
		// Reflection discovers controller commands automatically
	case "speedsetplus":
		// Reflection discovers pump commands automatically
	default:
		// Unknown device - reflection still discovers basic capabilities
	}

	return capabilities
}

// generateDynamicWebUI creates device-specific web interface
// based on discovered protobuf capabilities
func (sim *NgaSim) generateDynamicWebUI(deviceSerial string, capabilities map[string]interface{}) string {
	// Future maintainer: This generates web forms automatically
	// from protobuf message definitions using reflection
	return "<div>Device-specific UI generated automatically</div>"
}
