#!/bin/bash

echo "Cleaning up SLIP interface and processes..."

# Kill background processes if they exist
if [ -f /tmp/ngasim-socat.pid ]; then
    echo "Stopping socat process..."
    sudo kill $(cat /tmp/ngasim-socat.pid) 2>/dev/null
    rm -f /tmp/ngasim-socat.pid
fi

if [ -f /tmp/ngasim-slattach.pid ]; then
    echo "Stopping slattach process..."
    sudo kill $(cat /tmp/ngasim-slattach.pid) 2>/dev/null  
    rm -f /tmp/ngasim-slattach.pid
fi

# Kill any remaining slip-related processes
sudo pkill socat 2>/dev/null
sudo pkill slattach 2>/dev/null

# Bring down sl0 interface
echo "Bringing down sl0 interface..."
sudo ip link set sl0 down 2>/dev/null
sudo ip addr flush dev sl0 2>/dev/null

echo "✓ SLIP cleanup complete!"
echo ""
echo "All SLIP processes stopped and interface removed."