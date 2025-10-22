[Paste the complete document above]
## 📚 **NgaSim Testing Guide for Successors**

**Document:** NgaSim Pool Controller Testing Manual  
**Version:** 2.2.0-clean  
**Author:** Retirement Handoff Documentation  
**Date:** October 22, 2025  

---

## 🎯 **Overview**

This document provides comprehensive testing procedures for the NgaSim Pool Controller system. The system supports multiple pool device types using protobuf messaging and MQTT communication.

### **Supported Device Types:**
- ✅ **Sanitizer Gen2** - Pool chemical sanitizers
- ✅ **Digital Controller Transformer** - Pool automation controllers  
- ✅ **SpeedSet Plus** - Variable speed pumps
- 🔄 **VSP Booster** - Booster pumps (can be added later)

---

## 🚀 **Quick Start Testing**

### **Prerequisites:**
```bash
cd /home/test/projects/pooltester3_20250916

# Ensure NgaSim is built
go build -o pool-controller

# Verify MQTT broker is accessible
ping -c 2 169.254.1.1
```

### **Basic System Test:**
```bash
# 1. Start NgaSim
./pool-controller &

# 2. Test web interface
curl http://localhost:8082

# 3. Create a test device
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/TEST001/anc" -m "test"

# 4. Verify device appears in web interface
curl http://localhost:8082 | grep "TEST001"
```

---

## 🧪 **Automated Testing Scripts**

### **1. Continuous Multi-Device Testing**

**File:** continuous_test.sh  
**Purpose:** Validates all device types and basic functionality

```bash
# Run single test cycle
./continuous_test.sh

# Run continuous testing (stops with Ctrl+C)
while true; do
    ./continuous_test.sh
    sleep 30
done
```

**Expected Output:**
```
🧪 NgaSim Multi-Device Continuous Testing
✅ Web interface responding  
✅ Multi-device discovery working!
📊 Devices discovered: 3
🎉 Continuous test cycle complete!
```

### **2. Real-Time Device Monitor**

**File:** monitor_devices.sh  
**Purpose:** Continuous monitoring of NgaSim status and device count

```bash
# Start monitoring (runs in background)
./monitor_devices.sh &

# Stop monitoring
pkill -f monitor_devices.sh
```

**Expected Output:**
```
[14:32:15] 🟢 RUNNING | Devices: 3
[14:32:20] 🟢 RUNNING | Devices: 5  
[14:32:25] 🟢 RUNNING | Devices: 5
```

### **3. Load Testing**

**File:** load_test.sh  
**Purpose:** Tests system with multiple devices (30 devices total)

```bash
# Run load test
./load_test.sh

# Expected: Creates 10 devices of each type
# Check final count in web interface
```

---

## 🔍 **Manual Testing Procedures**

### **Device Discovery Testing**

Test each device type individually:

```bash
# Start NgaSim
./pool-controller &

# Test Sanitizer Gen2
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/MANUAL_SANITIZER_001/anc" -m '{"test":"sanitizer"}'

# Test Digital Controller  
mosquitto_pub -h 169.254.1.1 -t "async/digitalControllerGen2/MANUAL_CONTROLLER_001/anc" -m '{"test":"controller"}'

# Test SpeedSet Plus
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/MANUAL_PUMP_001/anc" -m '{"test":"pump"}'

# Verify in web interface
curl http://localhost:8082
```

### **Telemetry Testing**

Send telemetry data for each device type:

```bash
# Sanitizer telemetry
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/TEST_SANITIZER/dt" -m '{"chlorine":2.5,"ph":7.2,"temp":78}'

# Controller telemetry  
mosquitto_pub -h 169.254.1.1 -t "async/digitalControllerGen2/TEST_CONTROLLER/dt" -m '{"temperature":78.5,"flow_rate":45}'

# Pump telemetry
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/TEST_PUMP/dt" -m '{"rpm":2800,"power":1200,"flow":65}'
```

### **Web Interface Testing**

```bash
# Basic connectivity
curl -v http://localhost:8082

# Check device count  
curl -s http://localhost:8082 | grep "Devices in memory"

# Check for specific device
curl -s http://localhost:8082 | grep "TEST_SANITIZER"

# Test API endpoints (if implemented)
curl http://localhost:8082/api/devices
```

---

## 🔧 **Troubleshooting Guide**

### **Common Issues and Solutions**

#### **1. Build Errors**
```bash
# If protobuf compilation fails:
go clean -cache
go mod tidy
go build -o pool-controller

# If duplicate message errors:
./resolve_all_protobuf_duplicates.sh
```

#### **2. MQTT Connection Issues**
```bash
# Test MQTT broker connectivity
ping 169.254.1.1
nc -zv 169.254.1.1 1883

# Test MQTT pub/sub manually
mosquitto_sub -h 169.254.1.1 -t "async/#" &
mosquitto_pub -h 169.254.1.1 -t "test/connection" -m "test"
```

#### **3. Web Interface Not Responding**
```bash
# Check if NgaSim is running
ps aux | grep pool-controller

# Check port availability
netstat -tlnp | grep 8082

# Kill and restart
sudo pkill -9 -f pool-controller
./pool-controller &
```

#### **4. Devices Not Appearing**
```bash
# Check NgaSim logs for MQTT messages
./pool-controller 2>&1 | grep -E "Received MQTT|Device|DEBUG"

# Verify MQTT topic format
# Correct: async/{deviceType}/{deviceSerial}/anc
# Example: async/sanitizerGen2/1234567890ABCDEF00/anc
```

---

## 📊 **Testing Scenarios**

### **Scenario 1: New Installation Validation**
1. Build and start NgaSim
2. Run continuous_test.sh  
3. Verify all 3 device types are created
4. Check web interface shows correct device count
5. Send telemetry for each device type
6. Verify data appears in interface

### **Scenario 2: Production Environment Testing**
1. Start NgaSim with real pool devices
2. Monitor with monitor_devices.sh
3. Observe real device discovery
4. Verify telemetry data processing
5. Test system stability over 24 hours

### **Scenario 3: New Device Type Integration**
1. Add new `.pb.go` files to ned directory
2. Run `resolve_all_protobuf_duplicates.sh` if needed
3. Test build: `go build -o pool-controller`
4. Create test device of new type
5. Verify discovery and telemetry processing

---

## 🎯 **Success Criteria**

### **System is Working Correctly When:**
- ✅ NgaSim builds without errors
- ✅ Web interface responds on port 8082
- ✅ All 3 device types can be discovered
- ✅ Device count updates correctly in web interface  
- ✅ Telemetry messages are processed
- ✅ MQTT connection remains stable
- ✅ No memory leaks during continuous operation

### **Performance Benchmarks:**
- **Device Discovery:** < 1 second per device
- **Web Interface Response:** < 500ms  
- **MQTT Message Processing:** < 100ms
- **Memory Usage:** Stable over 24 hours
- **Maximum Devices:** Successfully tested with 30+ devices

---

## 📚 **Advanced Testing**

### **Protobuf Message Validation**
```bash
# Test with actual protobuf binary data
# (This requires real pool devices or protobuf test data)

# Monitor protobuf parsing
./pool-controller 2>&1 | grep -E "protobuf|proto|Parse"
```

### **Performance Testing**
```bash
# Monitor resource usage
top -p $(pgrep pool-controller)

# Memory usage tracking
while true; do
    ps -o pid,vsz,rss,comm -p $(pgrep pool-controller)
    sleep 60
done
```

### **Integration Testing with Real Devices**
```bash
# When real pool devices are available:
# 1. Start NgaSim
# 2. Monitor device announcements
mosquitto_sub -h 169.254.1.1 -t "async/+/+/anc" -v

# 3. Observe automatic device discovery  
# 4. Monitor telemetry
mosquitto_sub -h 169.254.1.1 -t "async/+/+/dt" -v
```

---

## 🚀 **Getting Started Checklist**

**For New Team Members:**

1. **Environment Setup**
   - [ ] Clone repository
   - [ ] Verify Go installation  
   - [ ] Test MQTT broker connectivity
   - [ ] Build NgaSim successfully

2. **Basic Testing**
   - [ ] Run continuous_test.sh successfully
   - [ ] Verify web interface works
   - [ ] Create manual test devices
   - [ ] Observe device discovery

3. **Advanced Understanding**  
   - [ ] Review protobuf files in ned directory
   - [ ] Understand device-specific vs common messages
   - [ ] Test load scenarios
   - [ ] Monitor system performance

4. **Production Readiness**
   - [ ] Test with real pool devices
   - [ ] Validate 24-hour stability  
   - [ ] Document any new device types added
   - [ ] Update testing procedures as needed

---

## 📞 **Support Information**

**Key Files:**
- continuous_test.sh - Main testing script
- monitor_devices.sh - Real-time monitoring  
- load_test.sh - Performance testing
- `resolve_all_protobuf_duplicates.sh` - Fix build issues
- `DEVELOPMENT_STATUS.md` - Project history and status

**Architecture:**
- **Main Application:** main.go
- **Protobuf Definitions:** `ned/*.pb.go`
- **Web Interface:** Built into main.go
- **MQTT Broker:** `169.254.1.1:1883`
- **Web Interface:** `http://localhost:8082`

**Final Note:** This system was designed for easy extension. Adding new device types requires only adding protobuf files to the ned directory and resolving any duplicate message definitions using the provided scripts.

---

**🎉 Good luck with NgaSim development!**

---

```bash
# Save this document
cat > TESTING_GUIDE.md << 'EOF'
[Paste the complete document above]
EOF

git add TESTING_GUIDE.md
git commit -m "Add comprehensive testing guide for successors"

echo "✅ Testing guide saved to TESTING_GUIDE.md"
```

**Perfect handoff documentation for your successor!** 📚🎯
