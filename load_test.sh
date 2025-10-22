#!/bin/bash
echo "🏋️ NgaSim Load Testing - Multiple Devices"

# Create 10 devices of each type
for i in {1..10}; do
    mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/SANITIZER_$i/anc" -m "{\"id\":$i}" &
    mosquitto_pub -h 169.254.1.1 -t "async/digitalControllerGen2/CONTROLLER_$i/anc" -m "{\"id\":$i}" &  
    mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/PUMP_$i/anc" -m "{\"id\":$i}" &
done

wait
echo "📊 Created 30 test devices (10 of each type)"

# Check final device count
sleep 5
FINAL_COUNT=$(curl -s http://localhost:8082 | grep -o "Devices in memory: [0-9]*" | grep -o "[0-9]*")
echo "🎯 Final device count: $FINAL_COUNT"
