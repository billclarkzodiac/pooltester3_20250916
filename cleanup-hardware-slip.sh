#!/bin/bash

echo "Cleaning up HARDWARE SLIP interface and processes..."

# Kill background processes if they exist
if [ -f /tmp/ngasim-hardware-slattach.pid ]; then
    echo "Stopping hardware slattach process..."
    sudo kill $(cat /tmp/ngasim-hardware-slattach.pid) 2>/dev/null  
    rm -f /tmp/ngasim-hardware-slattach.pid
fi

# Kill any remaining slip-related processes
sudo pkill slattach 2>/dev/null

# Bring down sl0 interface
echo "Bringing down sl0 interface..."
sudo ip link set sl0 down 2>/dev/null
sudo ip addr flush dev sl0 2>/dev/null

# Reset USB serial port to normal state
if [ -e "/dev/ttyUSB0" ]; then
    echo "Resetting /dev/ttyUSB0 to normal state..."
    sudo stty -F /dev/ttyUSB0 sane
fi

echo "✓ Hardware SLIP cleanup complete!"
echo ""
echo "USB-to-serial port /dev/ttyUSB0 reset to normal state."