#!/bin/bash

echo "Starting pool controller..."
./pool-controller &
POOL_PID=$!

echo "Waiting for startup..."
sleep 20

echo "Testing sanitizer command API..."
curl -v -X POST http://localhost:8081/api/sanitizer/command \
  -H "Content-Type: application/json" \
  -d '{"serial":"1234567890ABCDEF00","percentage":75}'

echo ""
echo "Stopping pool controller..."
kill $POOL_PID
wait $POOL_PID 2>/dev/null
echo "Test complete!"