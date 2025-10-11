package main

import (
	"encoding/json"
	"fmt"
	"io"
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

	// Convert protobuf message to JSON for readable logging
	if jsonData, err := json.Marshal(msg); err == nil {
		var dataMap map[string]interface{}
		if err := json.Unmarshal(jsonData, &dataMap); err == nil {
			entry.Data = dataMap
		}
	}

	// Create human-readable message
	entry.Message = fmt.Sprintf("[%s] %s %s: %s",
		direction, device, msgType, entry.MessageType)

	tl.addEntry(entry)
	tl.printToTerminal(entry)
}

// LogError logs an error message
func (tl *TerminalLogger) LogError(device, message string, err error) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      "ERROR",
		Device:    device,
		Direction: "ERROR",
		Message:   fmt.Sprintf("[ERROR] %s: %s - %v", device, message, err),
	}

	tl.addEntry(entry)
	tl.printToTerminal(entry)
}

// addEntry adds an entry to the circular buffer
func (tl *TerminalLogger) addEntry(entry LogEntry) {
	tl.mutex.Lock()
	defer tl.mutex.Unlock()

	tl.entries = append(tl.entries, entry)

	// Keep only maxEntries in memory
	if len(tl.entries) > tl.maxEntries {
		tl.entries = tl.entries[len(tl.entries)-tl.maxEntries:]
	}
}

// printToTerminal prints to terminal and file via tee
func (tl *TerminalLogger) printToTerminal(entry LogEntry) {
	// Format for terminal display
	timestamp := entry.Timestamp.Format("15:04:05.000")

	// Color coding for terminal
	var color string
	switch entry.Type {
	case "REQUEST":
		color = "\033[34m" // Blue
	case "RESPONSE":
		color = "\033[32m" // Green
	case "TELEMETRY":
		color = "\033[36m" // Cyan
	case "ANNOUNCE":
		color = "\033[35m" // Magenta
	case "ERROR":
		color = "\033[31m" // Red
	default:
		color = "\033[37m" // White
	}
	reset := "\033[0m"

	// Terminal output with colors
	terminalLine := fmt.Sprintf("%s%s [%s] %s%s\n",
		color, timestamp, entry.Type, entry.Message, reset)

	// Write to terminal (stdout)
	fmt.Print(terminalLine)

	// Write to log file (plain text, no colors)
	logLine := fmt.Sprintf("%s [%s] %s\n",
		timestamp, entry.Type, entry.Message)

	if tl.logFile != nil {
		tl.logFile.WriteString(logLine)

		// Write detailed JSON data if available
		if entry.Data != nil {
			if jsonData, err := json.MarshalIndent(entry.Data, "  ", "  "); err == nil {
				tl.logFile.WriteString(fmt.Sprintf("  Data: %s\n", string(jsonData)))
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

// Close closes the log file
func (tl *TerminalLogger) Close() error {
	if tl.logFile != nil {
		return tl.logFile.Close()
	}
	return nil
}
