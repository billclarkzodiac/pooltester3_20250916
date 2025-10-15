// Create protobuf_parser.go without ned dependency
package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// ProtobufMessageParser parses protobuf messages and extracts structured data for display
type ProtobufMessageParser struct {
	reflectionEngine *ProtobufReflectionEngine
}

// ParsedProtobufField represents a single field from a parsed protobuf message
type ParsedProtobufField struct {
	Name        string      `json:"name"`        // Field name (user-friendly)
	Type        string      `json:"type"`        // Field type (int32, string, bool, etc.)
	Value       interface{} `json:"value"`       // Actual value
	RawValue    string      `json:"raw_value"`   // String representation of value
	IsSet       bool        `json:"is_set"`      // Whether the field has a value
	Description string      `json:"description"` // Human-readable description
	Unit        string      `json:"unit"`        // Unit of measurement (%, V, °C, etc.)
}

// ParsedProtobufMessage represents a complete parsed protobuf message
type ParsedProtobufMessage struct {
	MessageType string                `json:"message_type"` // Full protobuf message type
	Category    string                `json:"category"`     // Device category (sanitizerGen2, etc.)
	Fields      []ParsedProtobufField `json:"fields"`       // All parsed fields
	Timestamp   time.Time             `json:"timestamp"`    // When parsed
	RawData     []byte                `json:"raw_data"`     // Original raw bytes
	ParsedOK    bool                  `json:"parsed_ok"`    // Whether parsing succeeded
}

// NewProtobufMessageParser creates a new protobuf message parser
func NewProtobufMessageParser(reflectionEngine *ProtobufReflectionEngine) *ProtobufMessageParser {
	return &ProtobufMessageParser{
		reflectionEngine: reflectionEngine,
	}
}

// ParseAnnounceMessage attempts to parse an announcement message using existing code
func (p *ProtobufMessageParser) ParseAnnounceMessage(rawData []byte, deviceSerial string) (*ParsedProtobufMessage, error) {
	parsed := &ParsedProtobufMessage{
		MessageType: "DeviceAnnouncement",
		Category:    "announce",
		Timestamp:   time.Now(),
		RawData:     rawData,
		Fields:      []ParsedProtobufField{},
		ParsedOK:    false,
	}

	// Try to extract basic info from raw data (simplified approach)
	dataStr := string(rawData)

	// Look for common patterns in announce messages
	if len(rawData) > 10 {
		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Device Serial",
			Type:        "string",
			Value:       deviceSerial,
			RawValue:    deviceSerial,
			IsSet:       true,
			Description: "Device serial number from MQTT topic",
			Unit:        "",
		})

		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Message Type",
			Type:        "string",
			Value:       "ANNOUNCE",
			RawValue:    "ANNOUNCE",
			IsSet:       true,
			Description: "Device announcement message",
			Unit:        "",
		})

		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Data Size",
			Type:        "int32",
			Value:       len(rawData),
			RawValue:    fmt.Sprintf("%d bytes", len(rawData)),
			IsSet:       true,
			Description: "Raw protobuf message size",
			Unit:        "bytes",
		})

		// Look for printable strings in the data
		if len(dataStr) > 0 && strings.Contains(dataStr, "sanitizer") {
			parsed.Fields = append(parsed.Fields, ParsedProtobufField{
				Name:        "Device Category",
				Type:        "string",
				Value:       "sanitizerGen2",
				RawValue:    "sanitizerGen2",
				IsSet:       true,
				Description: "Detected device category",
				Unit:        "",
			})
		}

		parsed.ParsedOK = true
		log.Printf("📋 Parsed announce message: %d fields extracted (basic parsing)", len(parsed.Fields))
	}

	return parsed, nil
}

// ParseTelemetryMessage attempts to parse a telemetry message using existing code
func (p *ProtobufMessageParser) ParseTelemetryMessage(rawData []byte, deviceSerial string) (*ParsedProtobufMessage, error) {
	parsed := &ParsedProtobufMessage{
		MessageType: "DeviceTelemetry",
		Category:    "sanitizer_telemetry",
		Timestamp:   time.Now(),
		RawData:     rawData,
		Fields:      []ParsedProtobufField{},
		ParsedOK:    false,
	}

	// Try to extract basic info from raw data (simplified approach)
	if len(rawData) > 10 {
		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Device Serial",
			Type:        "string",
			Value:       deviceSerial,
			RawValue:    deviceSerial,
			IsSet:       true,
			Description: "Device serial number from MQTT topic",
			Unit:        "",
		})

		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Message Type",
			Type:        "string",
			Value:       "TELEMETRY",
			RawValue:    "TELEMETRY",
			IsSet:       true,
			Description: "Device telemetry message",
			Unit:        "",
		})

		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Data Size",
			Type:        "int32",
			Value:       len(rawData),
			RawValue:    fmt.Sprintf("%d bytes", len(rawData)),
			IsSet:       true,
			Description: "Raw protobuf message size",
			Unit:        "bytes",
		})

		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Timestamp",
			Type:        "string",
			Value:       parsed.Timestamp.Format("15:04:05"),
			RawValue:    parsed.Timestamp.Format("15:04:05"),
			IsSet:       true,
			Description: "Message received time",
			Unit:        "",
		})

		// Add hex dump of first few bytes for debugging
		hexDump := fmt.Sprintf("%x", rawData[:min(8, len(rawData))])
		parsed.Fields = append(parsed.Fields, ParsedProtobufField{
			Name:        "Raw Data (hex)",
			Type:        "string",
			Value:       hexDump,
			RawValue:    hexDump,
			IsSet:       true,
			Description: "First 8 bytes in hexadecimal",
			Unit:        "",
		})

		parsed.ParsedOK = true
		log.Printf("📊 Parsed telemetry message: %d fields extracted (basic parsing)", len(parsed.Fields))
	}

	return parsed, nil
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FormatFieldForDisplay returns a formatted string for displaying a field in the terminal
func (field ParsedProtobufField) FormatFieldForDisplay() string {
	if !field.IsSet {
		return fmt.Sprintf("%-20s: <not set>", field.Name)
	}

	switch field.Type {
	case "bool":
		if field.Value.(bool) {
			return fmt.Sprintf("%-20s: ✅ true", field.Name)
		} else {
			return fmt.Sprintf("%-20s: ❌ false", field.Name)
		}
	default:
		return fmt.Sprintf("%-20s: %v", field.Name, field.RawValue)
	}
}
