#!/bin/bash

echo "🔍 Enhanced Chlorination Command Logging Test"
echo "=============================================="

# Start the enhanced pool controller
echo "Starting enhanced pool controller..."
./pool-controller &
POOL_PID=$!

echo "Waiting for device discovery..."
sleep 15

echo "📤 Sending chlorination command: 75%"
curl -s -X POST http://localhost:8082/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":75}' \
  || echo "❌ HTTP request failed (expected if port conflict)"

echo "📤 Sending chlorination command: 50%"
curl -s -X POST http://localhost:8082/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":50}' \
  || echo "❌ HTTP request failed (expected if port conflict)"

sleep 5

echo ""
echo "📋 Checking structured logs..."
echo "device_commands.log size: $(wc -c < device_commands.log 2>/dev/null || echo '0') bytes"
if [ -s device_commands.log ]; then
    echo "📄 Latest log entries:"
    tail -5 device_commands.log | jq . 2>/dev/null || cat device_commands.log | tail -5
else
    echo "📄 No structured logs yet (commands may not have reached sendSanitizerCommand)"
fi

echo ""
echo "🛑 Stopping pool controller..."
kill $POOL_PID 2>/dev/null
wait $POOL_PID 2>/dev/null

echo "✅ Test completed"