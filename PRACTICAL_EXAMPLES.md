# 🛠️ NgaSim Practical Examples

**Real-world scenarios your successor will encounter**

## 📡 **Example 1: Real Device Message**

When a sanitizer device boots up, here's what actually happens:

### **Device Sends:**
```
Topic: "async/sanitizerGen2/1234567890ABCDEF00/anc"
Binary protobuf message containing:
- Device serial: "1234567890ABCDEF00"  
- Device type: "sanitizerGen2"
- Firmware version: "2.1.3"
- Status: "ONLINE"
```

### **NgaSim Receives and Processes:**
```go
// 1. MQTT message arrives
topic := "async/sanitizerGen2/1234567890ABCDEF00/anc"
message := [binary protobuf data]

// 2. Parse topic
parts := strings.Split(topic, "/")
deviceType := "sanitizerGen2"
deviceSerial := "1234567890ABCDEF00"  
messageType := "anc"

// 3. Create device object
device := &Device{
    Serial: "1234567890ABCDEF00",
    Type: "sanitizerGen2", 
    Status: "ONLINE",
    LastSeen: time.Now(),
}

// 4. Store in memory
sim.Devices["1234567890ABCDEF00"] = device

// 5. Device appears in web interface immediately
```

### **Result in Web Interface:**
```
NgaSim Pool Controller
Devices in memory: 1

Sanitizer 1234567890ABCDEF00 (sanitizerGen2) - ONLINE
Last seen: 14:32:15
```

## 📊 **Example 2: Telemetry Processing**

Device sends sensor data every 30 seconds:

### **Raw MQTT Message:**
```
Topic: "async/sanitizerGen2/1234567890ABCDEF00/dt"
Protobuf message with:
- Chlorine level: 2500 ppm
- pH level: 7.2
- Water temperature: 78.5°F
- Flow rate: 45 GPM
```

### **NgaSim Processing:**
```go
// Parse protobuf telemetry
var telemetry SanitizerTelemetryMessage
proto.Unmarshal(message.Payload(), &telemetry)

// Update device with latest readings  
device := sim.Devices["1234567890ABCDEF00"]
device.Telemetry = &telemetry
device.LastSeen = time.Now()

// Values now available for web interface, logging, alerts
```

## 🔧 **Example 3: Adding New Device Type**

Boss says: "We just got new LED pool lights that use MQTT. Add support!"

### **Steps:**
1. **Get protobuf files** from manufacturer
2. **Add to ned/ directory**
3. **Test compilation**
4. **No other changes needed!**

### **What happens automatically:**
```go
// System sees new topic pattern:
"async/poolLightGen1/LIGHT_001/anc"

// Automatically creates device:
device := &Device{
    Serial: "LIGHT_001",
    Type: "poolLightGen1",    // ← New device type!
    Status: "ONLINE",
}

// Shows in web interface immediately:
"Pool Light LIGHT_001 (poolLightGen1) - ONLINE"
```

**That's the power of the architecture - it's self-extending!**
