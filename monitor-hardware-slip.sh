#!/bin/bash

echo "=== NgaSim HARDWARE SLIP Interface Monitor ==="
echo "Monitoring sl0 interface and /dev/ttyUSB0 for real SLIP communication"
echo "Press Ctrl+C to stop"
echo ""

# Show current hardware status
echo "=== USB-to-Serial Device Status ==="
if [ -e "/dev/ttyUSB0" ]; then
    ls -la /dev/ttyUSB0
    echo "Serial port settings:"
    stty -F /dev/ttyUSB0 -a | head -2
else
    echo "❌ /dev/ttyUSB0 not found!"
fi
echo ""

# Show current sl0 status
echo "=== SLIP Interface Status ==="
if ip link show sl0 &>/dev/null; then
    ip addr show sl0
    echo ""
    ip route show dev sl0
    echo ""
    
    # Show interface statistics
    echo "=== SLIP Interface Statistics ==="
    cat /proc/net/dev | grep sl0
    echo ""
    
    echo "=== Monitoring SLIP Traffic on Hardware Serial Line ==="
    echo "Watching for SLIP-encoded packets on sl0 interface..."
    echo "Topology messages should appear as UDP packets to port 30000"
    echo ""
    
    # Monitor UDP traffic on port 30000 (topology messages)
    sudo tcpdump -i sl0 -v -X port 30000 &
    TCPDUMP_PID=$!
    
    echo "Also monitoring raw serial data on /dev/ttyUSB0..."
    echo "SLIP frames will show as escaped data with 0xC0 delimiters"
    echo ""
    
    # Monitor raw serial data (this shows the actual SLIP-encoded bytes)
    timeout 30 sudo cat /dev/ttyUSB0 | hexdump -C &
    HEXDUMP_PID=$!
    
    # Wait for user to stop
    echo "Press Enter to stop monitoring..."
    read
    
    # Cleanup background processes
    sudo kill $TCPDUMP_PID 2>/dev/null
    sudo kill $HEXDUMP_PID 2>/dev/null
    
else
    echo "❌ sl0 interface not found! Run 'sudo ./setup-hardware-slip.sh' first"
fi