
#wifi_hotspot_setup.sh
#!/bin/bash
# WiFi Hotspot Setup for Raspberry Pi 4
# Modern approach using NetworkManager (works with current RPi OS)

echo "📶 Setting up WiFi Hotspot on Raspberry Pi 4"
echo "Using NetworkManager (modern RPi OS approach)"
echo "=============================================="

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "❌ Please run as root: sudo $0"
    exit 1
fi

# Check if NetworkManager is installed
if ! command -v nmcli &> /dev/null; then
    echo "📦 Installing NetworkManager..."
    apt update
    apt install -y network-manager
    systemctl enable NetworkManager
    systemctl start NetworkManager
else
    echo "✅ NetworkManager already installed"
fi

# Configuration variables
HOTSPOT_NAME="PoolController"
HOTSPOT_PASSWORD="PoolControl123"
HOTSPOT_IP="192.168.4.1"
INTERFACE="wlan0"

echo "🔧 Hotspot Configuration:"
echo "  SSID: $HOTSPOT_NAME"
echo "  Password: $HOTSPOT_PASSWORD"
echo "  IP Address: $HOTSPOT_IP"
echo "  Interface: $INTERFACE"
echo ""

# Stop any existing hotspot
echo "🛑 Stopping any existing hotspot..."
nmcli connection delete "$HOTSPOT_NAME" 2>/dev/null || true
sleep 2

# Create WiFi hotspot using NetworkManager
echo "📶 Creating WiFi hotspot..."
nmcli connection add type wifi ifname $INTERFACE con-name "$HOTSPOT_NAME" autoconnect yes ssid "$HOTSPOT_NAME"
nmcli connection modify "$HOTSPOT_NAME" 802-11-wireless.mode ap
nmcli connection modify "$HOTSPOT_NAME" 802-11-wireless.band bg
nmcli connection modify "$HOTSPOT_NAME" ipv4.method shared
nmcli connection modify "$HOTSPOT_NAME" ipv4.addresses $HOTSPOT_IP/24
nmcli connection modify "$HOTSPOT_NAME" 802-11-wireless-security.key-mgmt wpa-psk
nmcli connection modify "$HOTSPOT_NAME" 802-11-wireless-security.psk "$HOTSPOT_PASSWORD"

# Start the hotspot
echo "🚀 Starting WiFi hotspot..."
nmcli connection up "$HOTSPOT_NAME"

# Wait for interface to be ready
sleep 5

# Install and configure dnsmasq for DHCP
echo "📡 Setting up DHCP server..."
apt install -y dnsmasq

# Backup original dnsmasq config
cp /etc/dnsmasq.conf /etc/dnsmasq.conf.backup 2>/dev/null || true

# Create hotspot-specific dnsmasq config
cat > /etc/dnsmasq.d/hotspot.conf << EOF
# WiFi Hotspot DHCP Configuration
interface=wlan0
dhcp-range=192.168.4.10,192.168.4.50,255.255.255.0,24h
dhcp-option=3,192.168.4.1
dhcp-option=6,8.8.8.8,8.8.4.4
server=8.8.8.8
log-queries
log-dhcp
EOF

# Restart dnsmasq
systemctl restart dnsmasq
systemctl enable dnsmasq

# Configure IP forwarding
echo "🔀 Enabling IP forwarding..."
echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
sysctl -p

# Configure iptables for internet sharing (if eth0 connected)
echo "🔥 Setting up firewall rules..."
iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
iptables -A FORWARD -i eth0 -o wlan0 -m state --state RELATED,ESTABLISHED -j ACCEPT
iptables -A FORWARD -i wlan0 -o eth0 -j ACCEPT

# Save iptables rules
iptables-save > /etc/iptables/rules.v4 2>/dev/null || {
    mkdir -p /etc/iptables
    iptables-save > /etc/iptables/rules.v4
}

# Install iptables-persistent to restore rules on boot
apt install -y iptables-persistent

# Create systemd service to start hotspot on boot
cat > /etc/systemd/system/pool-controller-hotspot.service << EOF
[Unit]
Description=Pool Controller WiFi Hotspot
After=network.target
Wants=network.target

[Service]
Type=oneshot
ExecStart=/usr/bin/nmcli connection up "$HOTSPOT_NAME"
RemainAfterExit=yes
ExecStop=/usr/bin/nmcli connection down "$HOTSPOT_NAME"
StandardOutput=journal

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable pool-controller-hotspot.service

# Test hotspot status
echo ""
echo "🔍 Testing hotspot status..."
sleep 3

if nmcli connection show --active | grep -q "$HOTSPOT_NAME"; then
    echo "✅ WiFi hotspot is active!"
    echo ""
    echo "📱 Connection details:"
    echo "  SSID: $HOTSPOT_NAME"
    echo "  Password: $HOTSPOT_PASSWORD"
    echo "  IP Address: $HOTSPOT_IP"
    echo ""
    echo "🌐 Pool Controller will be available at:"
    echo "  http://192.168.4.1:8082"
    echo ""
    echo "📶 Hotspot will start automatically on boot"
else
    echo "❌ Hotspot failed to start"
    echo "Checking logs:"
    journalctl -u NetworkManager --no-pager -n 20
fi

echo ""
echo "🎉 WiFi Hotspot setup complete!"
echo "=============================================="