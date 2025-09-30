# NgaSim API Documentation

**NgaSim v2.1.1** - Pool Controller Simulator with Real Device Integration

## Overview

NgaSim provides a RESTful API for interacting with Jandy pool equipment discovered via MQTT/SLIP protocols. The system supports both real hardware devices and demo mode simulation.

**Base URL:** `http://localhost:8081`

---

## Endpoints

### 1. Web Interface

#### `GET /`
Returns the HTML web interface dashboard showing all discovered devices.

**Response:** HTML page with device tiles and real-time telemetry

---

### 2. Device Discovery

#### `GET /api/devices`
Returns a list of all discovered pool devices with their current status and telemetry data.

**Response Format:** JSON Array of Device objects

**Example Request:**
```bash
curl http://localhost:8081/api/devices
```

**Example Response:**
```json
[
  {
    "id": "1234567890ABCDEF00",
    "type": "sanitizerGen2",
    "name": "EDGE-SWC-IM",
    "serial": "1234567890ABCDEF00",
    "status": "ONLINE",
    "last_seen": "2025-09-29T17:04:43.957068478-07:00",
    "product_name": "EDGE-SWC-IM",
    "category": "sanitizerGen2",
    "model_id": "sanitizer-gen2",
    "model_version": "1.0",
    "firmware_version": "1.0.1",
    "ota_version": "0.0.1",
    "rssi": -45,
    "ppm_salt": 3200,
    "percentage_output": 75,
    "accelerometer_x": 12,
    "accelerometer_y": -8,
    "accelerometer_z": 1005,
    "line_input_voltage": 240,
    "is_cell_flow_reversed": false
  }
]
```

---

### 3. Device Control

#### `POST /api/sanitizer/command`
Sends power level commands to sanitizer devices using protobuf messaging over MQTT.

**Content-Type:** `application/json`

**Request Body:**
```json
{
  "serial": "string",      // Device serial number (required)
  "percentage": integer    // Power level 0-101% (required)
}
```

**Response Format:**
```json
{
  "success": boolean,
  "message": "string",
  "device": "string"
}
```

**Example Request:**
```bash
curl -X POST http://localhost:8081/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{
    "serial": "1234567890ABCDEF00",
    "percentage": 75
  }'
```

**Example Response:**
```json
{
  "success": true,
  "message": "Sanitizer command sent: 1234567890ABCDEF00 -> 75%",
  "device": "EDGE-SWC-IM"
}
```

**Error Responses:**
- `400 Bad Request` - Invalid JSON, missing serial, or percentage out of range (0-101)
- `404 Not Found` - Device serial number not found
- `405 Method Not Allowed` - Non-POST request
- `500 Internal Server Error` - MQTT publish failure

---

## Device Object Schema

### Core Fields
| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique device identifier |
| `type` | string | Device category (VSP, Sanitizer, ICL, etc.) |
| `name` | string | Human-readable device name |
| `serial` | string | Device serial number |
| `status` | string | Connection status (ONLINE, OFFLINE) |
| `last_seen` | timestamp | Last communication time |

### Protobuf Device Information
| Field | Type | Description |
|-------|------|-------------|
| `product_name` | string | Official product name |
| `category` | string | Device category from protobuf |
| `model_id` | string | Model identifier |
| `model_version` | string | Model version |
| `firmware_version` | string | Current firmware version |
| `ota_version` | string | OTA update version |

### Device-Specific Telemetry

#### Sanitizer Fields
| Field | Type | Unit | Description |
|-------|------|------|-------------|
| `rssi` | int32 | dBm | Signal strength |
| `ppm_salt` | int32 | ppm | Salt concentration |
| `percentage_output` | int32 | % | Current power output (0-101%) |
| `accelerometer_x` | int32 | - | X-axis tilt sensor |
| `accelerometer_y` | int32 | - | Y-axis tilt sensor |
| `accelerometer_z` | int32 | - | Z-axis tilt sensor |
| `line_input_voltage` | int32 | V | Input voltage |
| `is_cell_flow_reversed` | boolean | - | Flow direction indicator |

#### VSP (Variable Speed Pump) Fields
| Field | Type | Unit | Description |
|-------|------|------|-------------|
| `rpm` | int | RPM | Pump rotation speed |
| `temperature` | float64 | °C | Motor temperature |
| `power` | int | W | Power consumption |

#### ICL (Infinite Color Light) Fields
| Field | Type | Range | Description |
|-------|------|-------|-------------|
| `red` | int | 0-255 | Red color intensity |
| `green` | int | 0-255 | Green color intensity |
| `blue` | int | 0-255 | Blue color intensity |
| `white` | int | 0-255 | White LED intensity |

#### TruSense (Water Chemistry) Fields
| Field | Type | Unit | Description |
|-------|------|------|-------------|
| `ph` | float64 | pH | Water pH level |
| `orp` | int | mV | Oxidation Reduction Potential |

#### Heater/HeatPump Fields
| Field | Type | Unit | Description |
|-------|------|------|-------------|
| `set_temp` | float64 | °C | Target temperature |
| `water_temp` | float64 | °C | Current water temperature |
| `heating_mode` | string | - | Mode: OFF/HEAT/COOL |

---

## Communication Protocols

### MQTT Integration
- **Broker:** `tcp://169.254.1.1:1883`
- **Discovery Topics:** 
  - `async/+/+/anc` (Device announcements)
  - `async/+/+/dt` (Device telemetry)
  - `async/+/+/sts` (Device status)
  - `async/+/+/error` (Error messages)
- **Command Topic:** `cmd/{category}/{serial}/req`

### Protobuf Messages
NgaSim uses Protocol Buffers for device communication:
- **Commands:** `SetSanitizerTargetPercentageRequestPayload`
- **Telemetry:** `TelemetryMessage`
- **Discovery:** `GetDeviceInformationResponsePayload`

### SLIP Protocol
Devices communicate over Serial Line Internet Protocol (SLIP) with automatic discovery via C poller subprocess.

---

## System Architecture

### Components
1. **MQTT Client** - Connects to Pentair MQTT broker
2. **C Poller** - Wakes up SLIP devices for discovery
3. **Web Server** - Provides HTTP API and web interface
4. **Protobuf Parser** - Handles binary message serialization
5. **Device Manager** - Maintains device state and telemetry

### Process Management
- **Automatic Cleanup** - `defer` statements ensure proper resource cleanup
- **Signal Handling** - Graceful shutdown on SIGINT/SIGTERM/SIGQUIT
- **Orphan Prevention** - Kills lingering poller processes on exit

---

## Development Mode

### Demo Devices
When MQTT connection fails, NgaSim falls back to demo mode with simulated devices:
- VSP001 (Pool Pump)
- SALT001 (Salt Chlorinator)
- ICL001 (Pool Lights)
- TRUS001 (Water Sensors)
- HEAT001 (Gas Heater)
- HP001 (Heat Pump)
- ORION001 (ORION Sanitizer)

### Real Hardware Support
NgaSim is tested with:
- **EDGE-SWC-IM** Saltwater Chlorine Generator
- Jandy SLIP/MQTT infrastructure
- Real protobuf command/telemetry messaging

---

## Error Handling

### Command Validation
- Serial number must exist in discovered devices
- Percentage must be in range 0-101
- Device must be online and responsive

### Network Resilience
- Automatic MQTT reconnection
- Connection loss logging
- Fallback to demo mode on broker failure

### Logging
All API operations are logged with:
- Timestamp
- Operation type
- Device serial
- Command parameters
- Success/failure status
- UUID tracking for correlation

---

## Usage Examples

### Monitor All Devices
```bash
# Get device list
curl -s http://localhost:8081/api/devices | jq '.[].name'

# Watch device status
watch 'curl -s http://localhost:8081/api/devices | jq ".[].status"'
```

### Control Sanitizer
```bash
# Set sanitizer to 50% output
curl -X POST http://localhost:8081/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":50}'

# Turn off sanitizer
curl -X POST http://localhost:8081/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":0}'

# Maximum output
curl -X POST http://localhost:8081/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":101}'
```

### Monitor Telemetry
```bash
# Get sanitizer telemetry
curl -s http://localhost:8081/api/devices | \
  jq '.[] | select(.type=="sanitizerGen2") | {name, ppm_salt, percentage_output, rssi}'

# Monitor signal strength
watch 'curl -s http://localhost:8081/api/devices | jq ".[].rssi // \"N/A\""'
```

---

## Version Information
- **NgaSim Version:** 2.1.1
- **Go Version:** Compatible with Go modules
- **Dependencies:** 
  - `github.com/eclipse/paho.mqtt.golang`
  - `github.com/google/uuid`
  - `google.golang.org/protobuf`

**Last Updated:** September 29, 2025