package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

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
	DeviceID      string                 `json:"device_id"` // Keep this field for ui_generator.go compatibility
	DeviceSerial  string                 `json:"device_serial"`
	Command       string                 `json:"command"`
	Direction     string                 `json:"direction"` // "REQUEST" or "RESPONSE"
	Data          []byte                 `json:"data,omitempty"`
	DataHex       string                 `json:"data_hex,omitempty"`
	Level         LogLevel               `json:"level"`
	Message       string                 `json:"message"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Category      string                 `json:"category,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Success       bool                   `json:"success"`      // Add for ui_generator.go compatibility
	MessageType   string                 `json:"message_type"` // Add for ui_generator.go compatibility
}

// DeviceLogger handles comprehensive logging of device communications
type DeviceLogger struct {
	entries    []*DeviceLogEntry // Change to pointer slice for ui_generator.go compatibility
	maxEntries int
	mutex      sync.RWMutex
	filename   string
	registry   *ProtobufRegistry // For command introspection
}

// LogFilter represents criteria for filtering log entries
type LogFilter struct {
	DeviceID      string    `json:"device_id,omitempty"` // Keep this field
	DeviceSerial  string    `json:"device_serial,omitempty"`
	Command       string    `json:"command,omitempty"`
	Level         *LogLevel `json:"level,omitempty"`
	Since         time.Time `json:"since,omitempty"`
	Until         time.Time `json:"until,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	Category      string    `json:"category,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
}

// NewDeviceLogger creates a new device logger
func NewDeviceLogger(maxSize int) *DeviceLogger {
	return &DeviceLogger{
		entries:    make([]*DeviceLogEntry, 0),
		maxEntries: maxSize,
	}
}

// LogRequest logs an outgoing device command request
func (dl *DeviceLogger) LogRequest(deviceSerial, command string, data []byte, category string, tags ...string) string {
	correlationID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	entry := &DeviceLogEntry{
		ID:            fmt.Sprintf("log_%d", time.Now().UnixNano()),
		Timestamp:     time.Now(),
		DeviceID:      deviceSerial, // Set both for compatibility
		DeviceSerial:  deviceSerial,
		Command:       command,
		Direction:     "REQUEST",
		Data:          data,
		DataHex:       fmt.Sprintf("%x", data),
		Level:         LogLevelInfo,
		Message:       fmt.Sprintf("Sent %s command to %s", command, deviceSerial),
		CorrelationID: correlationID,
		Category:      category,
		Tags:          tags,
		Success:       true,
		MessageType:   "REQUEST",
	}

	dl.addEntry(entry)
	return correlationID
}

// LogResponse logs an incoming device response
func (dl *DeviceLogger) LogResponse(deviceSerial, command string, data []byte, correlationID, category string, tags ...string) {
	entry := &DeviceLogEntry{
		ID:            fmt.Sprintf("log_%d", time.Now().UnixNano()),
		Timestamp:     time.Now(),
		DeviceID:      deviceSerial, // Set both for compatibility
		DeviceSerial:  deviceSerial,
		Command:       command,
		Direction:     "RESPONSE",
		Data:          data,
		DataHex:       fmt.Sprintf("%x", data),
		Level:         LogLevelInfo,
		Message:       fmt.Sprintf("Received %s response from %s", command, deviceSerial),
		CorrelationID: correlationID,
		Category:      category,
		Tags:          tags,
		Success:       true,
		MessageType:   "RESPONSE",
	}

	dl.addEntry(entry)
}

// LogError logs an error during device communication
func (dl *DeviceLogger) LogError(deviceSerial, command, message, correlationID, category string, tags ...string) {
	entry := &DeviceLogEntry{
		ID:            fmt.Sprintf("log_%d", time.Now().UnixNano()),
		Timestamp:     time.Now(),
		DeviceID:      deviceSerial, // Set both for compatibility
		DeviceSerial:  deviceSerial,
		Command:       command,
		Direction:     "ERROR",
		Level:         LogLevelError,
		Message:       message,
		CorrelationID: correlationID,
		Category:      category,
		Tags:          tags,
		Success:       false,
		MessageType:   "ERROR",
	}

	dl.addEntry(entry)
}

// addEntry adds a log entry to the logger (with size management)
func (dl *DeviceLogger) addEntry(entry *DeviceLogEntry) {
	dl.mutex.Lock()
	defer dl.mutex.Unlock()

	dl.entries = append(dl.entries, entry)

	// Keep only the most recent entries
	if len(dl.entries) > dl.maxEntries {
		dl.entries = dl.entries[len(dl.entries)-dl.maxEntries:]
	}
}

// GetEntries returns filtered log entries
func (dl *DeviceLogger) GetEntries(filter LogFilter) []*DeviceLogEntry {
	dl.mutex.RLock()
	defer dl.mutex.RUnlock()

	var filtered []*DeviceLogEntry
	for _, entry := range dl.entries {
		if dl.matchesFilter(entry, filter) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// matchesFilter checks if an entry matches the given filter
func (dl *DeviceLogger) matchesFilter(entry *DeviceLogEntry, filter LogFilter) bool {
	if filter.DeviceID != "" && entry.DeviceID != filter.DeviceID {
		return false
	}
	if filter.DeviceSerial != "" && entry.DeviceSerial != filter.DeviceSerial {
		return false
	}
	if filter.Command != "" && entry.Command != filter.Command {
		return false
	}
	if filter.Level != nil && entry.Level != *filter.Level {
		return false
	}
	if !filter.Since.IsZero() && entry.Timestamp.Before(filter.Since) {
		return false
	}
	if filter.CorrelationID != "" && entry.CorrelationID != filter.CorrelationID {
		return false
	}
	return true
}

// Close cleans up the logger - return error for compatibility
func (dl *DeviceLogger) Close() error {
	dl.mutex.Lock()
	defer dl.mutex.Unlock()

	log.Println("DeviceLogger: Cleanup complete")
	return nil // Return error for compatibility
}
