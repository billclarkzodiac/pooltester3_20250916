#!/bin/bash

echo "Setting up SLIP interface for NgaSim testing on Ubuntu x86..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root: sudo ./setup-slip.sh"
    exit 1
fi

# Install required packages
echo "Installing required packages..."
apt update
apt install -y socat sl-utils net-tools

# Kill any existing socat/slattach processes
echo "Cleaning up existing SLIP processes..."
pkill socat 2>/dev/null
pkill slattach 2>/dev/null
ip link set sl0 down 2>/dev/null

# Create virtual serial devices in background
echo "Creating virtual serial devices..."
socat -d -d pty,raw,echo=0 pty,raw,echo=0 &
SOCAT_PID=$!

# Wait for socat to create devices
sleep 3

# Find the first PTY device created by socat
PTS_DEVICE=$(ps aux | grep "socat.*pty" | grep -v grep | head -1 | sed -n 's/.*\(\/dev\/pts\/[0-9]*\).*/\1/p')

if [ -z "$PTS_DEVICE" ]; then
    echo "Error: Could not find PTY device. Trying alternative method..."
    
    # Alternative: use first available pts device
    for pts in /dev/pts/*; do
        if [[ "$pts" =~ /dev/pts/[0-9]+ ]] && [ -e "$pts" ]; then
            PTS_DEVICE="$pts"
            break
        fi
    done
    
    if [ -z "$PTS_DEVICE" ]; then
        echo "Error: No PTY devices available"
        kill $SOCAT_PID 2>/dev/null
        exit 1
    fi
fi

echo "Using PTY device: $PTS_DEVICE"

# Create SLIP interface
echo "Creating SLIP interface on $PTS_DEVICE..."
slattach -p slip -s 115200 $PTS_DEVICE &
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
    kill $SOCAT_PID $SLATTACH_PID 2>/dev/null
    exit 1
fi

# Configure sl0 interface
echo "Configuring sl0 interface..."
ip link set sl0 up
ip addr add 169.254.20.1/24 dev sl0

# Add route for SLIP network
ip route add 169.254.20.0/24 dev sl0 2>/dev/null

# Verify setup
echo ""
echo "=== SLIP Interface Status ==="
ip addr show sl0
echo ""
ip route show dev sl0
echo ""

# Save PIDs for cleanup
echo $SOCAT_PID > /tmp/ngasim-socat.pid
echo $SLATTACH_PID > /tmp/ngasim-slattach.pid

echo "✓ SLIP setup complete!"
echo ""
echo "Next steps:"
echo "1. Build NgaSim: go build -o ngasim"
echo "2. Run NgaSim:   sudo ./ngasim"
echo "3. Open browser: http://localhost:8080"
echo ""
echo "To cleanup: sudo ./cleanup-slip.sh"