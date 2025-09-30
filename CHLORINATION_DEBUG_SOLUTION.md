# 🔍 Chlorination Command Logging Debug Solution

## **Current State Analysis**

### ✅ What's Working:
- **Device Discovery**: `1234567890ABCDEF00` sanitizer is being discovered
- **Telemetry Reception**: Receiving telemetry every ~8 seconds showing `percentage_output: 0%`
- **Basic Logging**: `log.Printf()` statements are working
- **HTTP API**: `/api/sanitizer/command` endpoint exists and should work

### ❌ What's Missing:
- **DeviceLogger Integration**: `NewDeviceLogger()` is never called in main.go
- **Command Logging**: No structured logging of outgoing chlorination commands
- **Request Tracking**: No correlation between commands sent and responses received

## **The Fix: Integrate DeviceLogger**

### **1. Modify main.go to use DeviceLogger**

Add this to your `NgaSim` struct:
```go
type NgaSim struct {
    devices   map[string]*Device
    mutex     sync.RWMutex
    mqtt      mqtt.Client
    server    *http.Server
    pollerCmd *exec.Cmd
    logger    *DeviceLogger    // <-- ADD THIS
    registry  *ProtobufRegistry // <-- ADD THIS
}
```

### **2. Initialize DeviceLogger in NewNgaSim()**

```go
func NewNgaSim() *NgaSim {
    // Create protobuf registry
    registry := NewProtobufRegistry()
    
    // Create device logger with structured logging
    logger, err := NewDeviceLogger(1000, "device_commands.log", registry)
    if err != nil {
        log.Printf("Warning: Failed to create device logger: %v", err)
        logger = nil
    }
    
    return &NgaSim{
        devices:  make(map[string]*Device),
        logger:   logger,
        registry: registry,
    }
}
```

### **3. Update sendSanitizerCommand() with Proper Logging**

```go
func (sim *NgaSim) sendSanitizerCommand(deviceSerial, category string, targetPercentage int) error {
    // Log the outgoing request
    correlationID := ""
    if sim.logger != nil {
        // Create the command data first
        saltCmd := &ned.SetSanitizerTargetPercentageRequestPayload{
            TargetPercentage: int32(targetPercentage),
        }
        wrapper := &ned.SanitizerRequestPayloads{
            RequestType: &ned.SanitizerRequestPayloads_SetSanitizerOutputPercentage{
                SetSanitizerOutputPercentage: saltCmd,
            },
        }
        data, _ := proto.Marshal(wrapper)
        
        // Log the request with structured logging
        correlationID = sim.logger.LogRequest(
            deviceSerial, 
            "SetSanitizerTargetPercentage", 
            data, 
            "chlorination", 
            fmt.Sprintf("target_%d_percent", targetPercentage),
        )
    }
    
    log.Printf("Sending sanitizer command: %s -> %d%% (Correlation: %s)", 
        deviceSerial, targetPercentage, correlationID)
    
    // ... rest of existing code ...
    
    // After successful MQTT publish
    if token.Error() != nil {
        // Log the error
        if sim.logger != nil {
            sim.logger.LogError(deviceSerial, "SetSanitizerTargetPercentage", 
                token.Error().Error(), correlationID, "chlorination", "mqtt_failure")
        }
        return fmt.Errorf("failed to publish sanitizer command: %v", token.Error())
    }

    log.Printf("✅ Sanitizer command sent successfully: %s -> %d%% (Correlation: %s)", 
        deviceSerial, targetPercentage, correlationID)
    return nil
}
```

## **Quick Debug: Check Current Logging**

### **1. Check Standard Logs:**
```bash
# Start the controller and check logs
./pool-controller 2>&1 | tee debug.log &

# Send a test command
curl -X POST http://localhost:8082/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":75}'

# Check what was logged
grep -i "sanitizer\|command\|percentage" debug.log
```

### **2. Monitor MQTT Traffic:**
```bash
# Install mosquitto clients if not available
sudo apt-get install mosquitto-clients

# Monitor all MQTT traffic
mosquitto_sub -h 169.254.1.1 -t "#" -v
```

### **3. Check Device State:**
```bash
# Check if device exists in NgaSim
curl -s http://localhost:8082/api/devices | jq '.[].serial'
```

## **Expected Behavior After Fix**

### **Before Fix (Current State):**
```
2025/09/30 14:07:31 Sending sanitizer command: 1234567890ABCDEF00 -> 75%
2025/09/30 14:07:31 ✅ Sanitizer command sent successfully: 1234567890ABCDEF00 -> 75%
```

### **After Fix (With DeviceLogger):**
```
2025/09/30 14:07:31 [INFO] REQUEST 1234567890ABCDEF00 -> SetSanitizerTargetPercentage: Success
2025/09/30 14:07:31 Sending sanitizer command: 1234567890ABCDEF00 -> 75% (Correlation: corr_1727711251000)
2025/09/30 14:07:31 ✅ Sanitizer command sent successfully: 1234567890ABCDEF00 -> 75% (Correlation: corr_1727711251000)
```

Plus structured JSON logging in `device_commands.log`:
```json
{
  "id": "log_1727711251000",
  "timestamp": "2025-09-30T14:07:31Z",
  "device_id": "1234567890ABCDEF00",
  "direction": "REQUEST",
  "message_type": "SetSanitizerTargetPercentage",
  "parsed_data": {
    "target_percentage": 75
  },
  "success": true,
  "correlation_id": "corr_1727711251000",
  "tags": ["chlorination", "target_75_percent"]
}
```

## **Root Cause Summary**

The chlorination commands **ARE being sent** via MQTT, but:
1. **No structured logging** - Only basic `log.Printf()` statements
2. **No correlation tracking** - Can't match requests with responses  
3. **DeviceLogger unused** - The sophisticated logging system exists but isn't connected

The device logger will show you **exactly** what protobuf data is being sent, when, and whether it succeeded.