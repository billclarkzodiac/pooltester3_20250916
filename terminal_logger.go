package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// TerminalLogger provides live terminal display and file logging with tee functionality
type TerminalLogger struct {
	logFile    *os.File
	entries    []LogEntry
	mutex      sync.RWMutex
	maxEntries int
	teeWriter  io.Writer
}

// LogEntry represents a terminal/file log entry
type LogEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	Type        string                 `json:"type"` // "REQUEST", "RESPONSE", "TELEMETRY", "ANNOUNCE", "ERROR"
	Device      string                 `json:"device"`
	Message     string                 `json:"message"`
	MessageType string                 `json:"message_type"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Raw         []byte                 `json:"raw,omitempty"`
	Direction   string                 `json:"direction"` // "OUTGOING", "INCOMING"
}

// NewTerminalLogger creates a new terminal logger with file tee
func NewTerminalLogger(logFilePath string, maxEntries int) (*TerminalLogger, error) {
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}

	// Create tee writer that writes to both stdout and file
	teeWriter := io.MultiWriter(os.Stdout, logFile)

	return &TerminalLogger{
		logFile:    logFile,
		entries:    make([]LogEntry, 0),
		maxEntries: maxEntries,
		teeWriter:  teeWriter,
	}, nil
}

// LogProtobufMessage logs a protobuf message with full details
func (tl *TerminalLogger) LogProtobufMessage(msgType, device, direction string, msg interface{}, raw []byte) {
	entry := LogEntry{
		Timestamp:   time.Now(),
		Type:        msgType,
		Device:      device,
		Direction:   direction,
		MessageType: fmt.Sprintf("%T", msg),
		Raw:         raw,
	}

	// Convert message to map for data field
	if msg != nil {
		if data, err := json.Marshal(msg); err == nil {
			var msgData map[string]interface{}
			if json.Unmarshal(data, &msgData) == nil {
				entry.Data = msgData
			}
		}
	}

	// Generate human-readable message
	entry.Message = fmt.Sprintf("[%s] %s: %s", direction, msgType, device)

	tl.addEntry(entry)
}

// LogError logs an error message
func (tl *TerminalLogger) LogError(device, message string, err error) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      "ERROR",
		Device:    device,
		Message:   fmt.Sprintf("ERROR: %s - %v", message, err),
		Direction: "SYSTEM",
	}

	tl.addEntry(entry)
}

// addEntry adds an entry to the log with thread safety
func (tl *TerminalLogger) addEntry(entry LogEntry) {
	tl.mutex.Lock()
	defer tl.mutex.Unlock()

	tl.entries = append(tl.entries, entry)

	// Keep only maxEntries entries
	if len(tl.entries) > tl.maxEntries {
		tl.entries = tl.entries[len(tl.entries)-tl.maxEntries:]
	}

	// Also print to terminal and file
	tl.printToTerminal(entry)
}

// printToTerminal prints log entry to terminal and file
func (tl *TerminalLogger) printToTerminal(entry LogEntry) {
	if tl.teeWriter != nil {
		timestamp := entry.Timestamp.Format("15:04:05.000")

		// Terminal output (colored and formatted)
		fmt.Printf("[%s] 📡 %s\n", timestamp, entry.Message)

		// File output (structured and searchable)
		if tl.logFile != nil {
			tl.logFile.WriteString(fmt.Sprintf("[%s] TYPE=%s DEVICE=%s DIR=%s MSG=%s\n",
				entry.Timestamp.Format("2006-01-02 15:04:05.000"),
				entry.Type,
				entry.Device,
				entry.Direction,
				entry.Message))

			// Add data if available
			if entry.Data != nil {
				if jsonData, err := json.Marshal(entry.Data); err == nil {
					tl.logFile.WriteString(fmt.Sprintf("  Data: %s\n", string(jsonData)))
				}
			}
		}

		tl.logFile.Sync() // Flush to disk
	}
}

// GetRecentEntries returns recent log entries for web display
func (tl *TerminalLogger) GetRecentEntries(limit int) []LogEntry {
	tl.mutex.RLock()
	defer tl.mutex.RUnlock()

	if limit <= 0 || limit > len(tl.entries) {
		limit = len(tl.entries)
	}

	result := make([]LogEntry, limit)
	copy(result, tl.entries[len(tl.entries)-limit:])

	return result
}

// GetRecentEntriesForDevice returns recent log entries filtered by device
func (tl *TerminalLogger) GetRecentEntriesForDevice(deviceSerial string, limit int) []LogEntry {
	tl.mutex.RLock()
	defer tl.mutex.RUnlock()

	// Filter entries by device
	var filteredEntries []LogEntry
	for _, entry := range tl.entries {
		if entry.Device == deviceSerial {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Apply limit
	if limit <= 0 || limit > len(filteredEntries) {
		limit = len(filteredEntries)
	}

	if limit == 0 {
		return []LogEntry{}
	}

	result := make([]LogEntry, limit)
	copy(result, filteredEntries[len(filteredEntries)-limit:])

	return result
}

// GetAllDevicesInTerminal returns a list of all devices that have terminal entries
func (tl *TerminalLogger) GetAllDevicesInTerminal() []string {
	tl.mutex.RLock()
	defer tl.mutex.RUnlock()

	deviceSet := make(map[string]bool)
	for _, entry := range tl.entries {
		if entry.Device != "" {
			deviceSet[entry.Device] = true
		}
	}

	var devices []string
	for device := range deviceSet {
		devices = append(devices, device)
	}

	return devices
}

// Close closes the log file
func (tl *TerminalLogger) Close() error {
	if tl.logFile != nil {
		return tl.logFile.Close()
	}
	return nil
}

// ClearAll clears all terminal entries
func (tl *TerminalLogger) ClearAll() {
	tl.mutex.Lock()
	defer tl.mutex.Unlock()

	tl.entries = make([]LogEntry, 0, tl.maxEntries)
	log.Printf("🗑️ Terminal cleared - all entries removed")
}

// ClearDevice clears terminal entries for a specific device
func (tl *TerminalLogger) ClearDevice(deviceSerial string) {
	tl.mutex.Lock()
	defer tl.mutex.Unlock()

	// Create new slice without entries from the specified device
	filteredEntries := make([]LogEntry, 0, len(tl.entries))
	removedCount := 0

	for _, entry := range tl.entries {
		if entry.Device != deviceSerial {
			filteredEntries = append(filteredEntries, entry)
		} else {
			removedCount++
		}
	}

	tl.entries = filteredEntries
	log.Printf("🗑️ Terminal cleared for device %s - removed %d entries", deviceSerial, removedCount)
}
