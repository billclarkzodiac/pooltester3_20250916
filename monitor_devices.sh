#!/bin/bash
echo "📊 NgaSim Device Monitor - Real-Time"
echo "Monitoring device discovery and telemetry..."

while true; do
    TIMESTAMP=$(date '+%H:%M:%S')
    
    # Check if NgaSim is running
    if pgrep -f pool-controller > /dev/null; then
        STATUS="🟢 RUNNING"
        
        # Get device count from web interface
        DEVICE_COUNT=$(curl -s http://localhost:8082 2>/dev/null | grep -o "Devices in memory: [0-9]*" | grep -o "[0-9]*" || echo "0")
        
        echo "[$TIMESTAMP] $STATUS | Devices: $DEVICE_COUNT"
    else
        echo "[$TIMESTAMP] 🔴 STOPPED | NgaSim not running"
    fi
    
    sleep 5
done
