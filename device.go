package main

import (
	"log"
	"sync"
	"time"
)

// Device represents a pool device with all its telemetry and state information
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

	// NEW FIELDS for Phase 1 human-friendly interface
	LiveTerminal   []TerminalEntry `json:"live_terminal"`   // Device-specific terminal
	LastCommand    string          `json:"last_command"`    // Last command sent
	CommandStatus  string          `json:"command_status"`  // SUCCESS/FAILED/PENDING
	HumanName      string          `json:"human_name"`      // Friendly display name
	ConnectionTime time.Time       `json:"connection_time"` // When device first connected
}

// TerminalEntry represents a single terminal log entry for a device
type TerminalEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"` // ANNOUNCE, TELEMETRY, COMMAND, RESPONSE, ERROR
	Message   string    `json:"message"`
	Data      string    `json:"data"` // Raw data for debugging
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

	// Add more commands...
	sanitizerCommands = append(sanitizerCommands, CommandInfo{
		Name:        "get_status",
		DisplayName: "Get Device Status",
		Description: "Retrieve current device status and telemetry",
		Category:    "sanitizerGen2",
		IsQuery:     true,
		Fields:      []CommandField{},
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
