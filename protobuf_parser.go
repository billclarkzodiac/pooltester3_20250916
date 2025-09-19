package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// ProtobufFieldInfo contains metadata about a protobuf field
type ProtobufFieldInfo struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Required    bool              `json:"required"`
	Repeated    bool              `json:"repeated"`
	Enum        map[string]int32  `json:"enum,omitempty"`
	Default     interface{}       `json:"default,omitempty"`
	Constraints map[string]string `json:"constraints,omitempty"`
}

// ProtobufMessageInfo contains metadata about a complete protobuf message
type ProtobufMessageInfo struct {
	Name        string                        `json:"name"`
	Package     string                        `json:"package"`
	Description string                        `json:"description"`
	Fields      map[string]*ProtobufFieldInfo `json:"fields"`
	Enums       map[string]map[string]int32   `json:"enums"`
}

// ProtobufRegistry manages all known protobuf message types
type ProtobufRegistry struct {
	Messages map[string]*ProtobufMessageInfo `json:"messages"`
	Types    map[string]protoreflect.MessageType
}

// NewProtobufRegistry creates a new registry and scans for protobuf files
func NewProtobufRegistry() *ProtobufRegistry {
	registry := &ProtobufRegistry{
		Messages: make(map[string]*ProtobufMessageInfo),
		Types:    make(map[string]protoreflect.MessageType),
	}

	// Load all known message types from the global registry
	protoregistry.GlobalTypes.RangeMessages(func(mt protoreflect.MessageType) bool {
		registry.registerMessageType(mt)
		return true
	})

	// Parse .pb.go files for additional metadata
	registry.parseProtobufFiles("ned/")

	return registry
}

// registerMessageType registers a message type using reflection
func (pr *ProtobufRegistry) registerMessageType(mt protoreflect.MessageType) {
	desc := mt.Descriptor()
	fullName := string(desc.FullName())

	msgInfo := &ProtobufMessageInfo{
		Name:    string(desc.Name()),
		Package: string(desc.ParentFile().Package()),
		Fields:  make(map[string]*ProtobufFieldInfo),
		Enums:   make(map[string]map[string]int32),
	}

	// Extract field information
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		fieldInfo := pr.extractFieldInfo(field)
		msgInfo.Fields[string(field.Name())] = fieldInfo
	}

	// Extract enum information
	enums := desc.Enums()
	for i := 0; i < enums.Len(); i++ {
		enum := enums.Get(i)
		enumMap := make(map[string]int32)
		values := enum.Values()
		for j := 0; j < values.Len(); j++ {
			value := values.Get(j)
			enumMap[string(value.Name())] = int32(value.Number())
		}
		msgInfo.Enums[string(enum.Name())] = enumMap
	}

	pr.Messages[fullName] = msgInfo
	pr.Types[fullName] = mt
}

// extractFieldInfo extracts detailed field information
func (pr *ProtobufRegistry) extractFieldInfo(field protoreflect.FieldDescriptor) *ProtobufFieldInfo {
	fieldInfo := &ProtobufFieldInfo{
		Name:        string(field.Name()),
		Required:    field.Cardinality() == protoreflect.Required,
		Repeated:    field.Cardinality() == protoreflect.Repeated,
		Constraints: make(map[string]string),
	}

	// Determine field type
	switch field.Kind() {
	case protoreflect.BoolKind:
		fieldInfo.Type = "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		fieldInfo.Type = "int32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		fieldInfo.Type = "int64"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		fieldInfo.Type = "uint32"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		fieldInfo.Type = "uint64"
	case protoreflect.FloatKind:
		fieldInfo.Type = "float"
	case protoreflect.DoubleKind:
		fieldInfo.Type = "double"
	case protoreflect.StringKind:
		fieldInfo.Type = "string"
	case protoreflect.BytesKind:
		fieldInfo.Type = "bytes"
	case protoreflect.EnumKind:
		fieldInfo.Type = "enum"
		// Extract enum values
		enumDesc := field.Enum()
		enumMap := make(map[string]int32)
		values := enumDesc.Values()
		for i := 0; i < values.Len(); i++ {
			value := values.Get(i)
			enumMap[string(value.Name())] = int32(value.Number())
		}
		fieldInfo.Enum = enumMap
	case protoreflect.MessageKind:
		fieldInfo.Type = "message"
		fieldInfo.Constraints["message_type"] = string(field.Message().FullName())
	}

	// Generate human-readable label from field name
	fieldInfo.Label = pr.generateLabel(string(field.Name()))

	return fieldInfo
}

// generateLabel converts snake_case field names to human-readable labels
func (pr *ProtobufRegistry) generateLabel(fieldName string) string {
	// Convert snake_case to Title Case
	words := strings.Split(fieldName, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

// parseProtobufFiles parses .pb.go files to extract additional metadata like comments
func (pr *ProtobufRegistry) parseProtobufFiles(dir string) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(path, ".pb.go") {
			pr.parseProtobufFile(path)
		}

		return nil
	})
}

// parseProtobufFile parses a single .pb.go file for comments and metadata
func (pr *ProtobufRegistry) parseProtobufFile(filename string) {
	fset := token.NewFileSet()
	src, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file %s: %v\n", filename, err)
		return
	}

	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		fmt.Printf("Error parsing file %s: %v\n", filename, err)
		return
	}

	// Extract comments and associate them with structures
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.TypeSpec:
			if structType, ok := node.Type.(*ast.StructType); ok {
				pr.extractStructComments(node.Name.Name, structType, file.Comments)
			}
		case *ast.GenDecl:
			if node.Tok == token.CONST {
				pr.extractEnumComments(node, file.Comments)
			}
		}
		return true
	})
}

// extractStructComments extracts comments from struct definitions
func (pr *ProtobufRegistry) extractStructComments(structName string, structType *ast.StructType, comments []*ast.CommentGroup) {
	// Find matching message in registry
	var msgInfo *ProtobufMessageInfo
	for _, msg := range pr.Messages {
		if strings.Contains(structName, msg.Name) {
			msgInfo = msg
			break
		}
	}

	if msgInfo == nil {
		return
	}

	// Extract field comments
	for _, field := range structType.Fields.List {
		if field.Comment != nil && len(field.Names) > 0 {
			fieldName := strings.ToLower(field.Names[0].Name)
			if fieldInfo, exists := msgInfo.Fields[fieldName]; exists {
				fieldInfo.Description = strings.TrimSpace(field.Comment.Text())
			}
		}
	}
}

// extractEnumComments extracts comments from enum definitions
func (pr *ProtobufRegistry) extractEnumComments(genDecl *ast.GenDecl, comments []*ast.CommentGroup) {
	// Implementation for extracting enum comments
	// This would parse const declarations and their associated comments
}

// GetMessageInfo returns information about a specific message type
func (pr *ProtobufRegistry) GetMessageInfo(messageType string) (*ProtobufMessageInfo, error) {
	if info, exists := pr.Messages[messageType]; exists {
		return info, nil
	}
	return nil, fmt.Errorf("message type %s not found in registry", messageType)
}

// CreateMessage creates a new message instance of the specified type
func (pr *ProtobufRegistry) CreateMessage(messageType string) (protoreflect.ProtoMessage, error) {
	if mt, exists := pr.Types[messageType]; exists {
		return mt.New().Interface(), nil
	}
	return nil, fmt.Errorf("message type %s not found in registry", messageType)
}

// ListAvailableMessages returns all available message types
func (pr *ProtobufRegistry) ListAvailableMessages() []string {
	var types []string
	for msgType := range pr.Messages {
		types = append(types, msgType)
	}
	return types
}

// ValidateField validates a field value against its constraints
func (pr *ProtobufRegistry) ValidateField(fieldInfo *ProtobufFieldInfo, value interface{}) error {
	// Implementation for field validation based on type and constraints
	switch fieldInfo.Type {
	case "int32":
		if _, ok := value.(int32); !ok {
			return fmt.Errorf("field %s expects int32, got %T", fieldInfo.Name, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field %s expects string, got %T", fieldInfo.Name, value)
		}
	case "enum":
		if enumVal, ok := value.(string); ok {
			if _, exists := fieldInfo.Enum[enumVal]; !exists {
				return fmt.Errorf("invalid enum value %s for field %s", enumVal, fieldInfo.Name)
			}
		} else {
			return fmt.Errorf("field %s expects enum string, got %T", fieldInfo.Name, value)
		}
	}

	return nil
}
