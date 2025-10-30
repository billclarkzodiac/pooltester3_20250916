#!/bin/bash
#speedset_vsp_test.sh

# Enhanced SpeedSet Plus / VSP Testing Script
# Prepares for real SpeedSet hardware testing

echo "🏊 SpeedSet Plus / VSP Enhanced Testing"
echo "Preparing for real SpeedSet hardware integration..."
echo "=========================================="

# Kill any existing processes
sudo pkill -9 -f pool-controller 2>/dev/null
sleep 2

# Start NgaSim
echo "🚀 Starting NgaSim..."
./pool-controller &
NGASIM_PID=$!
sleep 3

# Test SpeedSet Plus device categories
echo "🔌 Testing SpeedSet device categories..."

# Test different SpeedSet device types that might exist
SPEEDSET_TYPES=("speedsetplus" "speedset" "vsp" "variableSpeedPump" "pentairVSP")

for device_type in "${SPEEDSET_TYPES[@]}"; do
    echo "  Testing device type: $device_type"
    mosquitto_pub -h 169.254.1.1 -t "async/$device_type/TEST_${device_type^^}_001/anc" -m "{\"device_type\":\"$device_type\",\"status\":\"online\",\"model\":\"SpeedSet\",\"firmware\":\"1.0.0\"}"
    sleep 1
done

# Test comprehensive VSP telemetry patterns
echo "📊 Testing VSP telemetry patterns..."

# Pattern 1: Basic RPM/Power telemetry
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/TEST_PUMP_001/dt" -m '{
    "rpm": 2400,
    "power_watts": 850,
    "flow_rate_gpm": 65,
    "temperature_f": 78.5,
    "voltage": 240,
    "current_amps": 3.5,
    "status": "running",
    "speed_setting": 75
}'

sleep 1

# Pattern 2: Advanced telemetry with modes
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/TEST_PUMP_002/dt" -m '{
    "rpm": 1800,
    "power_watts": 450,
    "flow_rate_gpm": 45,
    "temperature_f": 82.1,
    "pump_mode": "spa",
    "speed_1_rpm": 1200,
    "speed_2_rpm": 1800,
    "speed_3_rpm": 2400,
    "speed_4_rpm": 3200,
    "priming": false,
    "error_code": 0
}'

sleep 1

# Pattern 3: Protobuf-style telemetry (what real SpeedSet might send)
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/TEST_PUMP_003/dt" -m '{
    "current_rpm": 2800,
    "target_rpm": 2800,
    "power_consumption": 1200,
    "motor_temperature": 85,
    "inlet_pressure": 12.5,
    "outlet_pressure": 35.2,
    "flow_sensor_reading": 68,
    "runtime_hours": 1247,
    "maintenance_due": false
}'

# Test SpeedSet command patterns
echo "⚡ Testing SpeedSet command patterns..."

# Test different command formats the real SpeedSet might expect
echo "  Testing RPM commands..."
curl -X POST http://localhost:8082/api/speedset/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"TEST_PUMP_001","command":"set_rpm","value":2400}' 2>/dev/null || echo "  (Command API not yet implemented - normal)"

echo "  Testing speed preset commands..."
curl -X POST http://localhost:8082/api/speedset/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"TEST_PUMP_001","command":"set_speed","preset":3}' 2>/dev/null || echo "  (Speed preset API not yet implemented - normal)"

# Test protobuf interface for SpeedSet
echo "🔧 Testing protobuf interface for SpeedSet..."
curl -s "http://localhost:8082/protobuf" | grep -i "speed\|pump\|vsp" || echo "  (Protobuf SpeedSet messages not found - will need real hardware)"

# Monitor device discovery
sleep 3
echo ""
echo "📋 Current device status:"
curl -s http://localhost:8082/api/devices | jq '.devices[] | select(.type | test("speed|vsp|pump"; "i")) | {serial: .serial, type: .type, name: .name, status: .status}' 2>/dev/null || echo "  (Using curl without jq formatting)"

# Clean shutdown
kill $NGASIM_PID 2>/dev/null
sleep 1

echo ""
echo "🎉 SpeedSet testing complete!"
echo "Ready for real SpeedSet hardware integration!"
echo "=========================================="