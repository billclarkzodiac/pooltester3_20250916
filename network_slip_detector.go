package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"syscall"
	"time"
)

// NetworkSlipDetector handles device discovery over SLIP network interface
type NetworkSlipDetector struct {
	slipInterface string
	localIP       net.IP
	devices       map[string]*NetworkSlipDevice
	onDevice      func(*NetworkSlipDevice)
	running       bool
	stopChan      chan bool
}

// NetworkSlipDevice represents a device discovered on SLIP network
type NetworkSlipDevice struct {
	IP       net.IP
	LastSeen time.Time
	DeviceID string
}

// NewNetworkSlipDetector creates a network-based SLIP device detector
func NewNetworkSlipDetector(interfaceName string, onDevice func(*NetworkSlipDevice)) (*NetworkSlipDetector, error) {
	// Get the SLIP interface
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to find interface %s: %v", interfaceName, err)
	}
	
	// Get interface addresses
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("failed to get interface addresses: %v", err)
	}
	
	var localIP net.IP
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				localIP = ipNet.IP
				break
			}
		}
	}
	
	if localIP == nil {
		return nil, fmt.Errorf("no valid IP address found on interface %s", interfaceName)
	}
	
	detector := &NetworkSlipDetector{
		slipInterface: interfaceName,
		localIP:       localIP,
		devices:       make(map[string]*NetworkSlipDevice),
		onDevice:      onDevice,
		stopChan:      make(chan bool),
	}
	
	log.Printf("Network SLIP detector initialized on %s with IP %s", interfaceName, localIP)
	return detector, nil
}

// Start begins the network-based device discovery
func (nsd *NetworkSlipDetector) Start() error {
	nsd.running = true
	
	// Start raw packet listener to detect announce responses (like original C filter())
	go nsd.listenForAnnounces()
	
	// Start device polling (like original C poll())
	go nsd.pollDevices()
	
	log.Println("Network SLIP detector started - polling devices and listening for announces")
	return nil
}

// Stop stops the detector
func (nsd *NetworkSlipDetector) Stop() error {
	if nsd.running {
		nsd.running = false
		close(nsd.stopChan)
		log.Println("Network SLIP detector stopped")
	}
	return nil
}

// sendSLIPTopologyPacket sends a raw topology packet over the SLIP interface
func (nsd *NetworkSlipDetector) sendSLIPTopologyPacket(deviceCount int) {
	// The key insight: we need to send packets that will go through sl0 interface
	// and be SLIP-encoded onto the serial line where the sanitizer can see them
	
	// Method 1: Send ARP request to discover sanitizer - this often triggers responses
	sanitizerIP := "169.254.20.84"
	cmd := exec.Command("arping", "-c", "1", "-I", "sl0", sanitizerIP)
	cmd.Run()
	
	// Method 2: Send ping to sanitizer
	cmd2 := exec.Command("ping", "-c", "1", "-W", "1", "-I", "sl0", sanitizerIP)
	cmd2.Run()
	
	// Method 3: UDP broadcast specifically bound to sl0 interface
	// This ensures the packet goes through SLIP
	iface, err := net.InterfaceByName("sl0")
	if err != nil {
		log.Printf("Could not find sl0 interface: %v", err)
		return
	}
	
	// Get sl0 IP address
	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		log.Printf("Could not get sl0 addresses: %v", err)
		return
	}
	
	var slipIP net.IP
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				slipIP = ipnet.IP
				break
			}
		}
	}
	
	if slipIP == nil {
		log.Printf("Could not find sl0 IPv4 address")
		return
	}
	
	// This function now implements the correct protocol from the original C code
	
	// CORRECT PROTOCOL FROM ORIGINAL C CODE!
	// Send 4-byte UDP messages to port 30000 containing IP address bytes
	// The original poller sends [169, 254, ip_msb, ip_lsb] for each known device
	
	// Use the sanitizer IP we know: 169.254.20.84
	
	// Create raw UDP socket like original C code for better control
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		log.Printf("Failed to create UDP socket: %v", err)
		return
	}
	defer syscall.Close(fd)
	
	// Enable broadcast
	err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	if err != nil {
		log.Printf("Failed to enable broadcast: %v", err)
		return
	}
	
	// Bind to sl0 interface by IP
	localSockAddr := &syscall.SockaddrInet4{
		Port: 0,
	}
	copy(localSockAddr.Addr[:], slipIP.To4())
	
	err = syscall.Bind(fd, localSockAddr)
	if err != nil {
		log.Printf("Failed to bind to sl0 IP: %v", err)
		return
	}
	
	// Create destination address - send directly to sanitizer, not broadcast
	// SLIP is point-to-point, so direct addressing works better
	remoteSockAddr := &syscall.SockaddrInet4{
		Port: 30000,
		Addr: [4]byte{169, 254, 20, 84}, // Direct to sanitizer IP
	}
	
	// Send 4-byte message for sanitizer IP: [169, 254, 20, 84]
	// This is exactly what the original C poller does!
	message := []byte{169, 254, 20, 84}
	
	err = syscall.Sendto(fd, message, 0, remoteSockAddr)
	if err != nil {
		log.Printf("Failed to send device poll message: %v", err)
		return
	}
	
	log.Printf("Sent device poll to sanitizer: [%d, %d, %d, %d]", message[0], message[1], message[2], message[3])
	
	// Small delay as in original code
	time.Sleep(100 * time.Millisecond)
	
	log.Printf("Sent device poll to sanitizer: [%d, %d, %d, %d]", message[0], message[1], message[2], message[3])
	
	// Small delay as in original code
	time.Sleep(100 * time.Millisecond)
}

// listenForAnnounces captures raw packets on sl0 to detect device announce responses
// This replaces the filter() function from the original C code
func (nsd *NetworkSlipDetector) listenForAnnounces() {
	// Create raw packet socket bound to sl0 (like original C code)
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ALL)))
	if err != nil {
		log.Printf("Failed to create raw socket: %v", err)
		return
	}
	defer syscall.Close(fd)
	
	// Get interface index for sl0
	iface, err := net.InterfaceByName("sl0")
	if err != nil {
		log.Printf("Failed to get sl0 interface: %v", err)
		return
	}
	
	// Bind to sl0 interface
	addr := syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_ALL),
		Ifindex:  iface.Index,
	}
	
	err = syscall.Bind(fd, &addr)
	if err != nil {
		log.Printf("Failed to bind to sl0: %v", err)
		return
	}
	
	log.Printf("Listening for announce packets on sl0...")
	
	buffer := make([]byte, 2048)
	for nsd.running {
		n, _, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			continue
		}
		
		// Check for 60-byte announce message with magic 0x55 at offset 28
		// This is exactly what the original C filter() does
		if n == 60 && len(buffer) > 28 && buffer[28] == 0x55 {
			log.Printf("*** ANNOUNCE DETECTED! 60-byte packet with magic 0x55 ***")
			
			// Extract device info from announce packet
			// IP bytes should be at specific offsets in the packet
			if len(buffer) >= 32 {
				ip3 := buffer[29] // IP byte 3
				ip4 := buffer[30] // IP byte 4  
				deviceIP := fmt.Sprintf("169.254.%d.%d", ip3, ip4)
				
				log.Printf("Device announced from IP: %s", deviceIP)
				
				// Create device entry
				deviceID := fmt.Sprintf("REAL_%s", deviceIP)
				device := &NetworkSlipDevice{
					IP:       net.ParseIP(deviceIP),
					LastSeen: time.Now(),
					DeviceID: deviceID,
				}
				
				nsd.devices[deviceID] = device
				
				if nsd.onDevice != nil {
					nsd.onDevice(device)
				}
			}
		}
	}
}

// pollDevices sends periodic device polls like the original C poll() function
func (nsd *NetworkSlipDetector) pollDevices() {
	for nsd.running {
		// Poll known sanitizer device
		nsd.sendSLIPTopologyPacket(len(nsd.devices))
		
		// Wait before next poll cycle (original C code polls continuously)
		time.Sleep(4 * time.Second)
	}
}

// htons converts host byte order to network byte order
func htons(i uint16) uint16 {
	return (i<<8)&0xff00 | i>>8
}

// Note: Device discovery now happens via raw packet capture
// The topology broadcast triggers devices to send MQTT announces
// which are handled by the main NgaSim MQTT message handler

// GetDevices returns discovered devices
func (nsd *NetworkSlipDetector) GetDevices() map[string]*NetworkSlipDevice {
	return nsd.devices
}

// SendCommandToDevice sends a command to a specific device IP
func (nsd *NetworkSlipDetector) SendCommandToDevice(deviceIP net.IP, command []byte) error {
	// This would send the protobuf command directly to the device
	// For now, just log it
	log.Printf("Would send command to device %s: %d bytes", deviceIP, len(command))
	
	// In real implementation, this would:
	// 1. Open TCP/UDP connection to device IP
	// 2. Send the protobuf command
	// 3. Handle the response
	
	return nil
}