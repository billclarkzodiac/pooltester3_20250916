# NgaSim SLIP Topology Messaging for Sanitizer Board

## Overview
The NgaSim now has enhanced SLIP topology messaging specifically designed to work with your Sanitizer board. The topology message is the critical first message that Sanitizer devices expect to determine their IP address and establish communication.

## Key Features Implemented

### 1. Automatic Topology Messages
- **Frequency**: Every 4 seconds (matches original C poller)
- **Protocol**: UDP packets to port 30000
- **Format**: 4-byte messages `[169, 254, ip_byte_3, ip_byte_4]`
- **Multiple IPs**: Sends to known Sanitizer IPs (169.254.20.84, .85, .86)
- **Broadcast**: Also sends broadcast topology message

### 2. Enhanced Logging
When running on Raspberry Pi with sl0 interface, you'll see:
```
✓ Sent topology message to 169.254.20.84: [169, 254, 20, 84]
✓ Sent topology message to 169.254.20.85: [169, 254, 20, 85]
✓ Sent topology broadcast from 169.254.x.x: [169, 254, x, x]
Topology message cycle complete - sent to 3 devices
```

### 3. Manual Topology Trigger
- **Web Interface**: "SEND TOPOLOGY" button on Sanitizer device page
- **Purpose**: Test topology messaging without waiting for 4-second cycle
- **API Command**: `{"device_id": "SALT001", "command": "send_topology"}`

## Setup Instructions for Raspberry Pi

### 1. Hardware Setup
1. Connect your Sanitizer board to RPi via SLIP interface
2. Ensure sl0 interface is configured and up
3. Verify Sanitizer is powered and ready

### 2. Run NgaSim
```bash
cd /home/bill/projects/pooltester3
sudo ./ngasim  # May need sudo for raw socket access
```

### 3. Monitor Topology Messages
Watch for these log messages:
- `SLIP detector started - topology messages will be sent every 4 seconds`
- `Sending periodic topology message (device count: X)`
- `✓ Sent topology message to 169.254.20.84: [169, 254, 20, 84]`

### 4. Test Manual Topology
1. Open http://localhost:8080
2. Click on "Pool Sanitizer" device
3. Click "SEND TOPOLOGY" button
4. Check logs for immediate topology message

## Topology Message Protocol

### Original C Poller Behavior (Replicated)
```c
// Original C code sends 4-byte UDP messages
message[0] = 169;        // SLIP network prefix
message[1] = 254;        // SLIP network prefix  
message[2] = ip_byte_3;  // Third octet of target IP
message[3] = ip_byte_4;  // Fourth octet of target IP
```

### NgaSim Implementation
```go
// Send topology to each known Sanitizer IP
message := []byte{169, 254, sanitizerBytes[2], sanitizerBytes[3]}
syscall.Sendto(fd, message, 0, remoteSockAddr)
```

### Expected Sanitizer Response
After receiving topology message, Sanitizer should:
1. **Determine its IP address** from the topology data
2. **Send MQTT announcement** to announce its presence
3. **Begin sending telemetry** data via MQTT
4. **Respond to commands** via protobuf over SLIP/MQTT

## Troubleshooting

### No sl0 Interface (Current x86 System)
```
Warning: SLIP initialization failed: failed to find interface sl0
```
**Solution**: This is expected on x86. Run on Raspberry Pi with SLIP setup.

### Sanitizer Not Responding
1. **Check SLIP Interface**: `ip addr show sl0`
2. **Check Connectivity**: `ping -I sl0 169.254.20.84`  
3. **Monitor Traffic**: `tcpdump -i sl0 -X`
4. **Manual Topology**: Use "SEND TOPOLOGY" button
5. **Check Logs**: Look for "Sent topology message" confirmations

### Debug Topology Messages
```bash
# Monitor SLIP traffic
sudo tcpdump -i sl0 -X port 30000

# Check if sl0 is up
ip link show sl0

# Manual IP configuration if needed
sudo ip addr add 169.254.20.1/24 dev sl0
```

## Integration with NgaSim

### Device Discovery Flow
1. **NgaSim starts** → Initializes SLIP detector
2. **Topology messages sent** → Every 4 seconds to Sanitizer IPs
3. **Sanitizer receives topology** → Determines its IP address
4. **Sanitizer announces** → Sends MQTT announcement message
5. **NgaSim discovers device** → Adds to device list
6. **Web interface updates** → Shows discovered Sanitizer

### Expected MQTT Topics (After Topology Success)
- `async/sanitizer/SERIAL123/anc` - Device announcements
- `async/sanitizer/SERIAL123/dt` - Telemetry data
- `async/sanitizer/SERIAL123/info` - Device information
- `cmd/sanitizer/SERIAL123/req` - Command requests

## Next Steps

Once topology messaging is working and your Sanitizer responds:
1. **Verify announcements** in NgaSim logs
2. **Check web interface** for discovered Sanitizer
3. **Test commands** using device detail page
4. **Monitor telemetry** for real-time data

The NgaSim is now ready to communicate with your real Sanitizer hardware! 🚀

---

**File Location**: `/home/bill/projects/pooltester3/.vscode/SLIP-TOPOLOGY-GUIDE.md`
**NgaSim Version**: 2.0.0
**Date**: 2025-09-16