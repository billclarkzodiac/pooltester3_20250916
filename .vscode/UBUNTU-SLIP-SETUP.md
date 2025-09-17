# Setting up SLIP Interface (sl0) on Ubuntu x86 64-bit

## Overview
This guide will help you create a SLIP (Serial Line Internet Protocol) interface on your Ubuntu x86 laptop to test NgaSim's topology messaging without needing a Raspberry Pi.

## Method 1: Virtual SLIP Interface (Recommended for Testing)

### 1. Install Required Packages
```bash
sudo apt update
sudo apt install socat sl-utils net-tools
```

### 2. Create Virtual Serial Devices
```bash
# Create a pair of virtual serial devices
sudo socat -d -d pty,raw,echo=0 pty,raw,echo=0
```

This will output something like:
```
2025/09/16 14:00:00 socat[1234] N PTY is /dev/pts/2
2025/09/16 14:00:00 socat[1234] N PTY is /dev/pts/3
```

Note the device paths (e.g., `/dev/pts/2` and `/dev/pts/3`).

### 3. Create SLIP Interface
In a new terminal, create the SLIP interface:
```bash
# Replace /dev/pts/2 with your actual device from step 2
sudo slattach -p slip -s 115200 /dev/pts/2

# The interface will be created as sl0
```

### 4. Configure IP Address
```bash
# Configure the sl0 interface with SLIP network addressing
sudo ip link set sl0 up
sudo ip addr add 169.254.20.1/24 dev sl0

# Verify the interface is up
ip addr show sl0
```

You should see:
```
sl0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 296 qdisc noqueue state UNKNOWN
    link/slip 
    inet 169.254.20.1/24 scope global sl0
```

## Method 2: Hardware Serial Port (If Available)

### 1. Check for Serial Ports
```bash
ls /dev/ttyS* /dev/ttyUSB*
```

### 2. Create SLIP Interface on Real Serial Port
```bash
# Replace ttyS0 with your actual serial port
sudo slattach -p slip -s 115200 /dev/ttyS0
sudo ip link set sl0 up
sudo ip addr add 169.254.20.1/24 dev sl0
```

## Method 3: USB-to-Serial Adapter

### 1. Connect USB-to-Serial Adapter
```bash
# Check for USB serial devices
lsusb
dmesg | grep tty

# Usually appears as /dev/ttyUSB0
```

### 2. Create SLIP Interface
```bash
sudo slattach -p slip -s 115200 /dev/ttyUSB0
sudo ip link set sl0 up
sudo ip addr add 169.254.20.1/24 dev sl0
```

## Testing the SLIP Interface

### 1. Verify Interface Status
```bash
# Check if sl0 is up and configured
ip addr show sl0
ip route show dev sl0

# Check SLIP statistics
cat /proc/net/dev | grep sl0
```

### 2. Test NgaSim with sl0
```bash
cd /home/bill/projects/pooltester3

# Now NgaSim should detect sl0 and start topology messaging
sudo ./ngasim
```

You should see:
```
Network SLIP detector initialized on sl0 with IP 169.254.20.1
SLIP detector started - topology messages will be sent every 4 seconds
✓ Sent topology message to 169.254.20.84: [169, 254, 20, 84]
```

### 3. Monitor SLIP Traffic
```bash
# In another terminal, monitor SLIP traffic
sudo tcpdump -i sl0 -v -X

# You should see UDP packets to port 30000
```

## Automated Setup Script

Save this as `setup-slip.sh`:

```bash
#!/bin/bash

echo "Setting up SLIP interface for NgaSim testing..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (sudo ./setup-slip.sh)"
    exit 1
fi

# Install required packages
echo "Installing required packages..."
apt update
apt install -y socat sl-utils net-tools

# Create virtual serial devices in background
echo "Creating virtual serial devices..."
socat -d -d pty,raw,echo=0 pty,raw,echo=0 &
SOCAT_PID=$!

# Wait a moment for socat to create devices
sleep 2

# Find the created PTY devices
PTS_DEVICES=$(ps aux | grep socat | grep pts | head -1 | grep -o '/dev/pts/[0-9]*' | head -1)

if [ -z "$PTS_DEVICES" ]; then
    echo "Error: Could not find PTY devices"
    kill $SOCAT_PID 2>/dev/null
    exit 1
fi

echo "Using PTY device: $PTS_DEVICES"

# Create SLIP interface
echo "Creating SLIP interface..."
slattach -p slip -s 115200 $PTS_DEVICES &
SLATTACH_PID=$!

# Wait for sl0 to appear
sleep 2

# Configure sl0 interface
echo "Configuring sl0 interface..."
ip link set sl0 up
ip addr add 169.254.20.1/24 dev sl0

# Verify setup
echo "SLIP interface status:"
ip addr show sl0

echo ""
echo "Setup complete! You can now run NgaSim:"
echo "sudo ./ngasim"
echo ""
echo "To stop SLIP interface:"
echo "sudo ip link set sl0 down"
echo "sudo kill $SOCAT_PID $SLATTACH_PID"

# Save PIDs for cleanup
echo $SOCAT_PID > /tmp/ngasim-socat.pid
echo $SLATTACH_PID > /tmp/ngasim-slattach.pid
```

Make it executable:
```bash
chmod +x setup-slip.sh
sudo ./setup-slip.sh
```

## Cleanup Script

Save this as `cleanup-slip.sh`:

```bash
#!/bin/bash

echo "Cleaning up SLIP interface..."

# Kill background processes if they exist
if [ -f /tmp/ngasim-socat.pid ]; then
    sudo kill $(cat /tmp/ngasim-socat.pid) 2>/dev/null
    rm -f /tmp/ngasim-socat.pid
fi

if [ -f /tmp/ngasim-slattach.pid ]; then
    sudo kill $(cat /tmp/ngasim-slattach.pid) 2>/dev/null  
    rm -f /tmp/ngasim-slattach.pid
fi

# Bring down sl0 interface
sudo ip link set sl0 down 2>/dev/null

echo "SLIP cleanup complete!"
```

## Troubleshooting

### sl0 Interface Not Found
```bash
# Check if slattach is running
ps aux | grep slattach

# Check kernel modules
lsmod | grep slip
sudo modprobe slip
```

### Permission Denied
```bash
# NgaSim needs root for raw sockets
sudo ./ngasim

# Or add capabilities (alternative)
sudo setcap cap_net_raw=eip ./ngasim
```

### Interface Configuration Issues
```bash
# Reset interface
sudo ip link set sl0 down
sudo ip addr flush dev sl0
sudo ip addr add 169.254.20.1/24 dev sl0
sudo ip link set sl0 up
```

## Testing with NgaSim

Once sl0 is set up:

1. **Start NgaSim**:
   ```bash
   cd /home/bill/projects/pooltester3
   sudo ./ngasim
   ```

2. **Expected Output**:
   ```
   Network SLIP detector initialized on sl0 with IP 169.254.20.1
   SLIP detector started - topology messages will be sent every 4 seconds
   ✓ Sent topology message to 169.254.20.84: [169, 254, 20, 84]
   ✓ Sent topology broadcast from 169.254.20.1: [169, 254, 20, 1]
   ```

3. **Monitor Traffic**:
   ```bash
   sudo tcpdump -i sl0 -v port 30000
   ```

4. **Test Manual Topology**:
   - Open http://localhost:8080
   - Click "Pool Sanitizer"
   - Click "SEND TOPOLOGY" button

Now your Ubuntu x86 laptop can test SLIP topology messaging just like the Raspberry Pi! 🚀