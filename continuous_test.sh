#!/bin/bash
# NgaSim Continuous Multi-Device Testing
# Tests all supported device types continuously

echo "🧪 NgaSim Multi-Device Continuous Testing"
echo "Testing: Sanitizer, Digital Controller, SpeedSet Plus"
echo "=========================================="

# Kill any existing processes
sudo pkill -9 -f pool-controller 2>/dev/null
sleep 2

# Start NgaSim
echo "🚀 Starting NgaSim..."
./pool-controller &
NGASIM_PID=$!
sleep 3

# Test basic web interface
echo "📡 Testing web interface..."
if curl -s http://localhost:8082 > /dev/null; then
    echo "✅ Web interface responding"
else
    echo "❌ Web interface not responding"
    exit 1
fi

# Test device creation for each type
echo "🔌 Testing device discovery..."

# Test Sanitizer Gen2
echo "  Testing Sanitizer Gen2..."
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/TEST_SANITIZER_001/anc" -m '{"device_type":"sanitizer","status":"online"}'
sleep 1

# Test Digital Controller
echo "  Testing Digital Controller..."
mosquitto_pub -h 169.254.1.1 -t "async/digitalControllerGen2/TEST_CONTROLLER_001/anc" -m '{"device_type":"controller","status":"online"}'
sleep 1

# Test SpeedSet Plus
echo "  Testing SpeedSet Plus..."
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/TEST_PUMP_001/anc" -m '{"device_type":"pump","status":"online"}'
sleep 1

# Check device count
DEVICE_COUNT=$(curl -s http://localhost:8082 | grep -o "Devices in memory: [0-9]*" | grep -o "[0-9]*")
echo "📊 Devices discovered: $DEVICE_COUNT"

if [ "$DEVICE_COUNT" -ge 3 ]; then
    echo "✅ Multi-device discovery working!"
else
    echo "⚠️  Expected 3+ devices, got $DEVICE_COUNT"
fi

# Test telemetry for each device type
echo "📈 Testing telemetry..."
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/TEST_SANITIZER_001/dt" -m '{"chlorine_level":2.5,"ph":7.2}'
mosquitto_pub -h 169.254.1.1 -t "async/digitalControllerGen2/TEST_CONTROLLER_001/dt" -m '{"temperature":78.5,"flow_rate":45}'
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/TEST_PUMP_001/dt" -m '{"rpm":2800,"power":1200}'

sleep 2
echo "✅ Telemetry sent for all device types"

# Clean shutdown
kill $NGASIM_PID 2>/dev/null
sleep 1

echo ""
echo "🎉 Continuous test cycle complete!"
echo "=========================================="
