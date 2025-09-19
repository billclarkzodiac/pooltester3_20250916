package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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
