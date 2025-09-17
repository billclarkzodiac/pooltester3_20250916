#!/bin/bash

echo "=== NgaSim SLIP Interface Monitor ==="
echo "Monitoring sl0 interface and NgaSim topology messaging"
echo "Press Ctrl+C to stop"
echo ""

# Show current sl0 status
echo "=== SLIP Interface Status ==="
ip addr show sl0
echo ""
ip route show dev sl0
echo ""

# Monitor UDP traffic on port 30000 (topology messages)
echo "=== Monitoring UDP Port 30000 (Topology Messages) ==="
echo "Listening for topology packets sent by NgaSim..."
echo ""

# Use tcpdump to monitor SLIP traffic
sudo tcpdump -i sl0 -v -X port 30000