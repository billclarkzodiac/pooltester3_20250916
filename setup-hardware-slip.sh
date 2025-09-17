#!/bin/bash

echo "Setting up HARDWARE SLIP interface on /dev/ttyUSB0 for NgaSim..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root: sudo ./setup-hardware-slip.sh"
    exit 1
fi

# Check if ttyUSB0 exists
if [ ! -e "/dev/ttyUSB0" ]; then
    echo "Error: /dev/ttyUSB0 not found. Please check USB-to-serial connection."
    exit 1
fi

# Kill any existing slip processes
echo "Cleaning up existing SLIP processes..."
pkill slattach 2>/dev/null
ip link set sl0 down 2>/dev/null

# Set serial port parameters
echo "Configuring serial port /dev/ttyUSB0..."
stty -F /dev/ttyUSB0 115200 cs8 -cstopb -parenb raw -echo

# Create SLIP interface on hardware serial port  
echo "Creating SLIP interface on /dev/ttyUSB0..."
/usr/sbin/slattach -p slip -s 115200 /dev/ttyUSB0 &
SLATTACH_PID=$!

# Wait for sl0 to appear
echo "Waiting for sl0 interface..."
for i in {1..10}; do
    if ip link show sl0 &>/dev/null; then
        echo "sl0 interface detected!"
        break
    fi
    echo "  Waiting... ($i/10)"
    sleep 1
done

if ! ip link show sl0 &>/dev/null; then
    echo "Error: sl0 interface not created"
    kill $SLATTACH_PID 2>/dev/null
    exit 1
fi

# Configure sl0 interface with original RPi SLIP network address
# Using 169.254.1.1 to match what Sanitizer devices expect
echo "Configuring sl0 interface with original RPi IP..."
ip link set sl0 up
ip addr add 169.254.1.1/16 dev sl0

# Add route for SLIP network
ip route add 169.254.0.0/16 dev sl0 2>/dev/null

# Set MTU for SLIP (important for serial communication)
ip link set sl0 mtu 296

# Verify setup
echo ""
echo "=== HARDWARE SLIP Interface Status ==="
ip addr show sl0
echo ""
ip route show dev sl0
echo ""

# Save PID for cleanup
echo $SLATTACH_PID > /tmp/ngasim-hardware-slattach.pid

echo "✓ HARDWARE SLIP setup complete!"
echo ""
echo "Serial Configuration:"
echo "  Device: /dev/ttyUSB0"
echo "  Speed:  115200 baud"
echo "  Format: 8N1 (8 data, no parity, 1 stop)"
echo ""
echo "Network Configuration:"
echo "  Interface: sl0"
echo "  IP:        169.254.20.1/16"
echo "  Network:   169.254.0.0/16 (covers full 169.254.x.x range)"
echo "  Broadcast: 169.254.255.255"
echo "  MTU:       296 (SLIP standard)"
echo ""
echo "Next steps:"
echo "1. Connect Sanitizer board to USB-to-serial RX/TX pins"
echo "2. Run NgaSim: sudo ./ngasim"
echo "3. Monitor:   sudo ./monitor-hardware-slip.sh"
echo ""
echo "To cleanup: sudo ./cleanup-hardware-slip.sh"