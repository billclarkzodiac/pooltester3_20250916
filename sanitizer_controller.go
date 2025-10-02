package main

import (
	"fmt"
	"sync"
	"time"
)

// SanitizerController handles all sanitizer business logic on the server side
type SanitizerController struct {
	devices      map[string]*SanitizerState
	mutex        sync.RWMutex
	commandQueue chan SanitizerCommand
	ngaSim       *NgaSim
}

// SanitizerState represents the complete state of a sanitizer
type SanitizerState struct {
	Serial          string    `json:"serial"`
	CurrentOutput   int32     `json:"current_output"`
	TargetOutput    int32     `json:"target_output"`
	IsBoostMode     bool      `json:"is_boost_mode"`
	BoostStartTime  time.Time `json:"boost_start_time,omitempty"`
	BoostDuration   int       `json:"boost_duration_minutes"`
	LastCommandTime time.Time `json:"last_command_time"`
	CommandInFlight bool      `json:"command_in_flight"`
	SafetyLocked    bool      `json:"safety_locked"`
	ErrorCount      int       `json:"error_count"`
	Status          string    `json:"status"`
}

// SanitizerCommand represents a command to be processed
type SanitizerCommand struct {
	Serial    string    `json:"serial"`
	Action    string    `json:"action"`    // "set_power", "boost", "stop", "emergency_stop"
	Value     int32     `json:"value"`     // Target percentage for set_power
	Duration  int       `json:"duration"`  // Minutes for boost mode
	ClientID  string    `json:"client_id"` // Which browser sent this
	Timestamp time.Time `json:"timestamp"`
}

// PowerLevel represents valid power settings
type PowerLevel struct {
	Percentage int32  `json:"percentage"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	IsSpecial  bool   `json:"is_special"`
}

// GetValidPowerLevels returns the standard power levels
func GetValidPowerLevels() []PowerLevel {
	return []PowerLevel{
		{0, "OFF", "danger", false},
		{10, "LOW", "warning", false},
		{50, "MEDIUM", "primary", false},
		{100, "HIGH", "success", false},
		{101, "BOOST", "success", true},
	}
}

// NewSanitizerController creates a new controller instance
func NewSanitizerController(ngaSim *NgaSim) *SanitizerController {
	sc := &SanitizerController{
		devices:      make(map[string]*SanitizerState),
		commandQueue: make(chan SanitizerCommand, 100),
		ngaSim:       ngaSim,
	}

	// Start command processor
	go sc.processCommands()
	return sc
}

// RegisterSanitizer adds a new sanitizer to the controller
func (sc *SanitizerController) RegisterSanitizer(serial string) *SanitizerState {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	if _, exists := sc.devices[serial]; !exists {
		sc.devices[serial] = &SanitizerState{
			Serial:        serial,
			CurrentOutput: 0,
			TargetOutput:  0,
			BoostDuration: 60, // Default 1 hour
			Status:        "ONLINE",
		}
	}
	return sc.devices[serial]
}

// ValidateCommand checks if a command is safe and valid
func (sc *SanitizerController) ValidateCommand(cmd SanitizerCommand) error {
	sc.mutex.RLock()
	device, exists := sc.devices[cmd.Serial]
	sc.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("sanitizer %s not found", cmd.Serial)
	}

	if device.SafetyLocked {
		return fmt.Errorf("sanitizer %s is safety locked", cmd.Serial)
	}

	switch cmd.Action {
	case "set_power":
		if cmd.Value < 0 || cmd.Value > 101 {
			return fmt.Errorf("invalid power level: %d (must be 0-101)", cmd.Value)
		}
	case "boost":
		if cmd.Duration < 1 || cmd.Duration > 1440 { // Max 24 hours
			return fmt.Errorf("invalid boost duration: %d minutes (must be 1-1440)", cmd.Duration)
		}
	case "emergency_stop":
		// Always allowed
	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}

	// Rate limiting - max 1 command per 3 seconds per device
	if time.Since(device.LastCommandTime) < 3*time.Second {
		return fmt.Errorf("rate limit exceeded for %s", cmd.Serial)
	}

	return nil
}

// QueueCommand adds a command to the processing queue
func (sc *SanitizerController) QueueCommand(cmd SanitizerCommand) error {
	cmd.Timestamp = time.Now()

	if err := sc.ValidateCommand(cmd); err != nil {
		return err
	}

	select {
	case sc.commandQueue <- cmd:
		return nil
	default:
		return fmt.Errorf("command queue full")
	}
}

// processCommands runs the command processor loop
func (sc *SanitizerController) processCommands() {
	for cmd := range sc.commandQueue {
		sc.executeCommand(cmd)
	}
}

// executeCommand processes a single command
func (sc *SanitizerController) executeCommand(cmd SanitizerCommand) {
	sc.mutex.Lock()
	device := sc.devices[cmd.Serial]
	sc.mutex.Unlock()

	switch cmd.Action {
	case "set_power":
		sc.setPowerLevel(device, cmd.Value)
	case "boost":
		sc.activateBoost(device, cmd.Duration)
	case "stop":
		sc.setPowerLevel(device, 0)
	case "emergency_stop":
		sc.emergencyStop(device)
	}

	device.LastCommandTime = time.Now()
}

// setPowerLevel sets the target power level
func (sc *SanitizerController) setPowerLevel(device *SanitizerState, percentage int32) {
	device.TargetOutput = percentage
	device.IsBoostMode = (percentage == 101)
	device.CommandInFlight = true

	if device.IsBoostMode {
		device.BoostStartTime = time.Now()
	}

	// Send actual MQTT command
	category := "sanitizerGen2" // Default, should come from device registry
	err := sc.ngaSim.sendSanitizerCommand(device.Serial, category, int(percentage))
	if err != nil {
		device.ErrorCount++
		device.Status = "ERROR"
	}
}

// activateBoost activates boost mode with duration
func (sc *SanitizerController) activateBoost(device *SanitizerState, durationMinutes int) {
	device.BoostDuration = durationMinutes
	sc.setPowerLevel(device, 101)
}

// emergencyStop immediately stops the sanitizer
func (sc *SanitizerController) emergencyStop(device *SanitizerState) {
	device.TargetOutput = 0
	device.IsBoostMode = false
	device.SafetyLocked = true // Requires manual unlock
	device.CommandInFlight = true

	// Send immediate stop command
	category := "sanitizerGen2"
	sc.ngaSim.sendSanitizerCommand(device.Serial, category, 0)
}

// GetAllStates returns all sanitizer states
func (sc *SanitizerController) GetAllStates() map[string]*SanitizerState {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()

	// Return copy to avoid race conditions
	states := make(map[string]*SanitizerState)
	for k, v := range sc.devices {
		// Create copy of state
		stateCopy := *v
		states[k] = &stateCopy
	}
	return states
}

// UpdateFromTelemetry updates state from incoming device telemetry
func (sc *SanitizerController) UpdateFromTelemetry(serial string, actualOutput int32) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	device, exists := sc.devices[serial]
	if !exists {
		return
	}

	device.CurrentOutput = actualOutput

	// Check if command achieved
	if device.CommandInFlight && device.CurrentOutput == device.TargetOutput {
		device.CommandInFlight = false
		device.ErrorCount = 0
		device.Status = "ONLINE"
	}

	// Check boost timeout
	if device.IsBoostMode && time.Since(device.BoostStartTime) > time.Duration(device.BoostDuration)*time.Minute {
		// Auto-return to 100% after boost duration
		sc.setPowerLevel(device, 100)
	}
}
