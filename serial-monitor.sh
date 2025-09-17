#!/bin/bash

echo "=== Real-time SLIP Frame Monitor ==="
echo "Monitoring /dev/ttyUSB0 for SLIP-encoded topology messages"
echo "SLIP frames use 0xC0 as frame delimiter"
echo "Press Ctrl+C to stop"
echo ""

if [ ! -e "/dev/ttyUSB0" ]; then
    echo "❌ /dev/ttyUSB0 not found!"
    exit 1
fi

echo "Serial Port Configuration:"
stty -F /dev/ttyUSB0 -a | head -2
echo ""

echo "Monitoring raw SLIP data (hexdump format):"
echo "Look for 0xC0 delimiters and UDP topology packets inside"
echo ""

# Monitor raw serial data in real-time
sudo hexdump -C /dev/ttyUSB0