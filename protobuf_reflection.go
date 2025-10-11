package main

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// ProtobufReflectionEngine dynamically discovers and manages protobuf message types
type ProtobufReflectionEngine struct {
	messageTypes map[string]protoreflect.MessageType
	fieldInfo    map[string][]FieldDescriptor
	mutex        sync.RWMutex
}

// FieldDescriptor describes a protobuf field for UI generation
type FieldDescriptor struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Label        string      `json:"label"`
	Required     bool        `json:"required"`
	Repeated     bool        `json:"repeated"`
	DefaultValue interface{} `json:"default_value,omitempty"`
	EnumValues   []string    `json:"enum_values,omitempty"`
	Min          interface{} `json:"min,omitempty"`
	Max          interface{} `json:"max,omitempty"`
	Description  string      `json:"description,omitempty"`
}

// MessageDescriptor describes a complete protobuf message
type MessageDescriptor struct {
	Name        string            `json:"name"`
	Package     string            `json:"package"`
	Fields      []FieldDescriptor `json:"fields"`
	IsRequest   bool              `json:"is_request"`
	IsResponse  bool              `json:"is_response"`
	IsTelemetry bool              `json:"is_telemetry"`
	Category    string            `json:"category"`
}

// NewProtobufReflectionEngine creates a new reflection engine
func NewProtobufReflectionEngine() *ProtobufReflectionEngine {
	return &ProtobufReflectionEngine{
		messageTypes: make(map[string]protoreflect.MessageType),
		fieldInfo:    make(map[string][]FieldDescriptor),
	}
}

// DiscoverMessages scans all registered protobuf messages
func (pre *ProtobufReflectionEngine) DiscoverMessages() error {
	pre.mutex.Lock()
	defer pre.mutex.Unlock()

	log.Println("🔍 Starting protobuf message discovery...")

	// Iterate through all registered message types
	protoregistry.GlobalTypes.RangeMessages(func(messageType protoreflect.MessageType) bool {
		descriptor := messageType.Descriptor()
		fullName := string(descriptor.FullName())

		log.Printf("📋 Discovered message: %s", fullName)

		pre.messageTypes[fullName] = messageType
		pre.fieldInfo[fullName] = pre.analyzeMessageFields(descriptor)

		return true
	})

	log.Printf("✅ Discovery complete. Found %d message types", len(pre.messageTypes))
	return nil
}

// analyzeMessageFields extracts field information from a message descriptor
func (pre *ProtobufReflectionEngine) analyzeMessageFields(desc protoreflect.MessageDescriptor) []FieldDescriptor {
	var fields []FieldDescriptor

	fieldDescs := desc.Fields()
	for i := 0; i < fieldDescs.Len(); i++ {
		field := fieldDescs.Get(i)

		fieldDesc := FieldDescriptor{
			Name:     string(field.Name()),
			Label:    string(field.Name()),
			Required: field.Cardinality() == protoreflect.Required,
			Repeated: field.Cardinality() == protoreflect.Repeated,
		}

		// Determine field type and constraints
		switch field.Kind() {
		case protoreflect.BoolKind:
			fieldDesc.Type = "boolean"
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
			fieldDesc.Type = "int32"
			fieldDesc.Min = int32(-2147483648)
			fieldDesc.Max = int32(2147483647)
		case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
			fieldDesc.Type = "int64"
		case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
			fieldDesc.Type = "uint32"
			fieldDesc.Min = uint32(0)
			fieldDesc.Max = uint32(4294967295)
		case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			fieldDesc.Type = "uint64"
			fieldDesc.Min = uint64(0)
		case protoreflect.FloatKind:
			fieldDesc.Type = "float"
		case protoreflect.DoubleKind:
			fieldDesc.Type = "double"
		case protoreflect.StringKind:
			fieldDesc.Type = "string"
		case protoreflect.BytesKind:
			fieldDesc.Type = "bytes"
		case protoreflect.EnumKind:
			fieldDesc.Type = "enum"
			fieldDesc.EnumValues = pre.getEnumValues(field.Enum())
		case protoreflect.MessageKind:
			fieldDesc.Type = "message"
			fieldDesc.Label = string(field.Message().FullName())
		}

		// Set default values
		if field.HasDefault() {
			fieldDesc.DefaultValue = field.Default().Interface()
		}

		// Add semantic information based on field name
		fieldDesc.Description = pre.generateFieldDescription(fieldDesc.Name, fieldDesc.Type)

		fields = append(fields, fieldDesc)
	}

	return fields
}

// getEnumValues extracts enum value names
func (pre *ProtobufReflectionEngine) getEnumValues(enumDesc protoreflect.EnumDescriptor) []string {
	var values []string
	enumValues := enumDesc.Values()
	for i := 0; i < enumValues.Len(); i++ {
		values = append(values, string(enumValues.Get(i).Name()))
	}
	return values
}

// generateFieldDescription creates helpful descriptions for fields
func (pre *ProtobufReflectionEngine) generateFieldDescription(name, fieldType string) string {
	name = strings.ToLower(name)

	switch {
	case strings.Contains(name, "percentage"):
		return "Percentage value (0-100)"
	case strings.Contains(name, "temp"):
		return "Temperature value"
	case strings.Contains(name, "voltage"):
		return "Voltage measurement"
	case strings.Contains(name, "current"):
		return "Current measurement"
	case strings.Contains(name, "ppm") || strings.Contains(name, "salt"):
		return "Parts per million (salt concentration)"
	case strings.Contains(name, "serial"):
		return "Device serial number"
	case strings.Contains(name, "id"):
		return "Unique identifier"
	case strings.Contains(name, "status"):
		return "Device status or state"
	default:
		return fmt.Sprintf("%s field", fieldType)
	}
}

// GetAllMessages returns all discovered message descriptors
func (pre *ProtobufReflectionEngine) GetAllMessages() map[string]MessageDescriptor {
	pre.mutex.RLock()
	defer pre.mutex.RUnlock()

	result := make(map[string]MessageDescriptor)

	for fullName, fields := range pre.fieldInfo {
		parts := strings.Split(fullName, ".")
		name := parts[len(parts)-1]
		pkg := strings.Join(parts[:len(parts)-1], ".")

		desc := MessageDescriptor{
			Name:        name,
			Package:     pkg,
			Fields:      fields,
			IsRequest:   pre.isRequestMessage(name),
			IsResponse:  pre.isResponseMessage(name),
			IsTelemetry: pre.isTelemetryMessage(name),
			Category:    pre.getCategoryFromName(fullName),
		}

		result[fullName] = desc
	}

	return result
}

// Helper methods to classify message types
func (pre *ProtobufReflectionEngine) isRequestMessage(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "set") || strings.Contains(name, "command") ||
		strings.Contains(name, "request") || strings.Contains(name, "cmd")
}

func (pre *ProtobufReflectionEngine) isResponseMessage(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "response") || strings.Contains(name, "reply") ||
		strings.Contains(name, "ack") || strings.Contains(name, "result")
}

func (pre *ProtobufReflectionEngine) isTelemetryMessage(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "telemetry") || strings.Contains(name, "data") ||
		strings.Contains(name, "status") || strings.Contains(name, "announce")
}

func (pre *ProtobufReflectionEngine) getCategoryFromName(fullName string) string {
	name := strings.ToLower(fullName)

	switch {
	case strings.Contains(name, "sanitizer"):
		return "sanitizerGen2"
	case strings.Contains(name, "pump") || strings.Contains(name, "vsp"):
		return "VSP"
	case strings.Contains(name, "light") || strings.Contains(name, "icl"):
		return "ICL"
	case strings.Contains(name, "sensor") || strings.Contains(name, "trusense"):
		return "TruSense"
	case strings.Contains(name, "heater"):
		return "Heater"
	case strings.Contains(name, "heatpump"):
		return "HeatPump"
	default:
		return "Generic"
	}
}

// CreateMessage creates a new message instance with default values
func (pre *ProtobufReflectionEngine) CreateMessage(fullName string) (proto.Message, error) {
	pre.mutex.RLock()
	messageType, exists := pre.messageTypes[fullName]
	pre.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("message type not found: %s", fullName)
	}

	return messageType.New().Interface(), nil
}

// PopulateMessage fills a message with values from a map
func (pre *ProtobufReflectionEngine) PopulateMessage(msg proto.Message, values map[string]interface{}) error {
	reflectMsg := msg.ProtoReflect()
	descriptor := reflectMsg.Descriptor()

	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		fieldName := string(field.Name())

		if value, exists := values[fieldName]; exists && value != nil {
			if err := pre.setFieldValue(reflectMsg, field, value); err != nil {
				return fmt.Errorf("failed to set field %s: %v", fieldName, err)
			}
		}
	}

	return nil
}

// setFieldValue sets a field value with type conversion
func (pre *ProtobufReflectionEngine) setFieldValue(msg protoreflect.Message, field protoreflect.FieldDescriptor, value interface{}) error {
	switch field.Kind() {
	case protoreflect.BoolKind:
		if v, ok := value.(bool); ok {
			msg.Set(field, protoreflect.ValueOfBool(v))
		}
	case protoreflect.Int32Kind:
		if v, ok := value.(float64); ok {
			msg.Set(field, protoreflect.ValueOfInt32(int32(v)))
		}
	case protoreflect.Int64Kind:
		if v, ok := value.(float64); ok {
			msg.Set(field, protoreflect.ValueOfInt64(int64(v)))
		}
	case protoreflect.Uint32Kind:
		if v, ok := value.(float64); ok {
			msg.Set(field, protoreflect.ValueOfUint32(uint32(v)))
		}
	case protoreflect.Uint64Kind:
		if v, ok := value.(float64); ok {
			msg.Set(field, protoreflect.ValueOfUint64(uint64(v)))
		}
	case protoreflect.FloatKind:
		if v, ok := value.(float64); ok {
			msg.Set(field, protoreflect.ValueOfFloat32(float32(v)))
		}
	case protoreflect.DoubleKind:
		if v, ok := value.(float64); ok {
			msg.Set(field, protoreflect.ValueOfFloat64(v))
		}
	case protoreflect.StringKind:
		if v, ok := value.(string); ok {
			msg.Set(field, protoreflect.ValueOfString(v))
		}
	}

	return nil
}
