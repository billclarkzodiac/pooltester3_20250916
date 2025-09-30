# Protobuf Architecture and JSON Tags Explained

**Date:** September 30, 2025  
**Context:** NgaSim Pool Controller - Understanding protobuf Go struct tags and architectural design

## 🔍 **Protobuf Go Struct Tags Explained**

### **The Struct Tag Example from sanitizer.pb.go line 797:**
```go
TargetPercentage int32 `protobuf:"varint,1,opt,name=target_percentage,json=targetPercentage,proto3" json:"target_percentage,omitempty"`
```

This is called a **struct tag** in Go, containing metadata that tells the protobuf library how to handle this field during serialization/deserialization.

### **Breaking Down the `protobuf:` Tag Components:**

#### **1. `varint`** - Wire Type
- Specifies how this field is encoded on the wire
- `varint` = variable-length integer encoding (efficient for small numbers)
- Other types: `fixed32`, `fixed64`, `bytes`, `group`

#### **2. `1`** - Field Number  
- **Critical**: This is the field's unique identifier in the protobuf schema
- Must match the `.proto` file definition
- Used for backward/forward compatibility
- **Never change this number** once deployed!

#### **3. `opt`** - Field Rule
- `opt` = optional field (can be omitted)
- Other options: `req` (required), `rep` (repeated/array)

#### **4. `name=target_percentage`** - Proto Field Name
- The original field name from the `.proto` file
- Used for text format serialization and debugging

#### **5. `json=targetPercentage`** - JSON Field Name
- How this field appears when serialized to JSON
- Converts `target_percentage` → `targetPercentage` (camelCase)

#### **6. `proto3`** - Protocol Version
- Indicates this uses proto3 syntax (vs older proto2)

### **Breaking Down the `json:` Tag:**
```go
json:"target_percentage,omitempty"
```
- **`target_percentage`** - JSON field name for standard JSON marshaling
- **`omitempty`** - Omit field from JSON if it has zero/empty value

## 🤔 **Architecture Question: Why JSON Tags in NgaSim but Not Device Protobuf?**

### **Different Use Cases & Requirements:**

#### **🎯 NgaSim (Your Go Code):**
```go
TargetPercentage int32 `protobuf:"varint,1,opt,name=target_percentage,json=targetPercentage,proto3" json:"target_percentage,omitempty"`
```
**Purpose:** Serves **dual roles**:
1. **Device Communication** - Pure protobuf to EDGE-SWC-IM sanitizer
2. **Web API Interface** - JSON REST API for your web GUI

#### **🔧 Device's Protobuf (EDGE-SWC-IM):**
```go
// Probably looks like this (no JSON tags):
TargetPercentage int32 `protobuf:"varint,1,opt,name=target_percentage,proto3"`
```
**Purpose:** **Single role**:
1. **Device-to-Device Communication** - Only protobuf over SLIP/MQTT

### **Architectural Design Pattern:**

#### **NgaSim is a Bridge/Gateway**
```
Web Browser (JSON) ← HTTP → NgaSim ← Protobuf/MQTT → Sanitizer Device
     ↑                        ↑                           ↑
  JSON needed            Dual format              Protobuf only
```

#### **Device is Embedded/Specialized**  
```
EDGE-SWC-IM Device: Pure protobuf, no web interface, no JSON needed
- Smaller memory footprint
- Faster serialization  
- No HTTP/REST requirements
```

## 📝 **Real Examples from NgaSim Code:**

### **NgaSim Web API (Needs JSON):**
```go
// Your REST endpoint at /api/sanitizer/command
func (n *NgaSim) handleSanitizerCommand(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Serial     string `json:"serial"`      // ← JSON for web
        Percentage int    `json:"percentage"`  // ← JSON for web  
    }
    
    // Convert to protobuf for device
    saltCmd := &ned.SetSanitizerTargetPercentageRequestPayload{
        TargetPercentage: int32(percentage), // ← Protobuf for device
    }
}
```

### **Device Communication (Pure Protobuf):**
```go
// Device only needs this for MQTT/SLIP communication
wrapper := &ned.SanitizerRequestPayloads{
    RequestType: &ned.SanitizerRequestPayloads_SetSanitizerOutputPercentage{
        SetSanitizerOutputPercentage: saltCmd,
    },
}
data, _ := proto.Marshal(wrapper) // ← Pure protobuf bytes
```

### **Real-World Usage in NgaSim Commands:**
```go
// When you create a command like this:
saltCmd := &ned.SetSanitizerTargetPercentageRequestPayload{
    TargetPercentage: int32(75), // Field number 1, wire type varint
}

// Protobuf serializes it using the struct tag metadata:
// - Field 1 (from tag)  
// - As varint encoding (from tag)
// - Value: 75

// When converted to JSON:
// {"targetPercentage": 75}  // Uses json tag camelCase name
```

## 🎯 **Why This Design Makes Sense:**

### **✅ NgaSim Benefits:**
- **Flexibility** - Can serve both web GUI and device protocols
- **Developer Experience** - Easy JSON debugging and testing
- **API Compatibility** - REST endpoints work with any HTTP client
- **Protocol Translation** - Perfect bridge between human-friendly and machine-efficient formats

### **✅ Device Benefits:**  
- **Performance** - No JSON overhead, pure binary efficiency
- **Memory** - Smaller code footprint, no JSON library needed
- **Reliability** - Simpler code path, fewer dependencies
- **Speed** - Faster serialization/deserialization

### **Code Generation Differences:**

#### **NgaSim Proto Generation:**
```bash
# Probably generated with JSON support enabled:
protoc --go_out=. --go_opt=paths=source_relative \
       --go-json_out=. sanitizer.proto  # ← Includes JSON tags
```

#### **Device Proto Generation:**  
```bash
# Probably generated without JSON support:
protoc --go_out=. --go_opt=paths=source_relative sanitizer.proto  # ← No JSON
```

## 🚀 **Why This Architecture is Smart:**

```
Browser ←JSON→ NgaSim ←Protobuf→ EDGE-SWC-IM
   ↑              ↑                  ↑
Human-friendly   Protocol         Efficient
   REST API      Translator      Binary Protocol
```

**NgaSim acts as the perfect protocol translator**, giving you both:
- Human-friendly JSON APIs for web development
- Efficient device communication with binary protobuf

## 🔧 **Key Technical Points:**

### **Field Number Importance:**
- **Field number `1`** ensures your Go code matches the Jandy device's protobuf schema
- **Never change field numbers** once in production - breaks compatibility
- Field numbers can be non-sequential and have gaps

### **Wire Type Efficiency:**
- **`varint` encoding** makes percentage values (0-101) very compact on the wire  
- More efficient than fixed-width integers for small values
- Self-describing variable length format

### **JSON Compatibility Benefits:**
- **JSON compatibility** allows your REST API to work seamlessly
- **`omitempty`** keeps JSON clean when percentage is 0
- Automatic camelCase conversion (`target_percentage` → `targetPercentage`)

### **Why This Matters for Sanitizer Commands:**

1. **Compatibility** - Field numbers match device expectations
2. **Efficiency** - Varint encoding optimizes small percentage values  
3. **Flexibility** - Same struct works for both JSON API and protobuf device communication
4. **Maintainability** - Single source of truth for data structures

## 📖 **Modern IoT Gateway Pattern:**

This is exactly how modern IoT gateways should work:
- **Embedded devices** stay lean with pure binary protocols
- **Gateway services** provide rich APIs for applications  
- **Protocol translation** happens at the boundary
- **Best of both worlds** - efficiency + developer experience

---

**This documentation explains the protobuf struct tag system that makes NgaSim commands work with the real EDGE-SWC-IM sanitizer while also providing a clean REST API for web development.** 🎯

**File saved for permanent reference and future development work.**