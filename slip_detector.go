package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/tarm/serial"
)

// SLIP protocol constants
const (
	SLIP_END     = 0xC0
	SLIP_ESC     = 0xDB
	SLIP_ESC_END = 0xDC
	SLIP_ESC_ESC = 0xDD
)

// Device represents a detected SLIP device
type SlipDevice struct {
	IP       string    // Format: 169.254.X.Y
	LastByte uint8     // Y value 
	NextLast uint8     // X value
	SerialNo [18]byte  // 18-byte serial number
	LastSeen time.Time
}

// SlipDetector handles SLIP device detection
type SlipDetector struct {
	port     *serial.Port
	devices  map[string]*SlipDevice
	onDevice func(*SlipDevice) // Callback for new device detection
}

// NewSlipDetector creates a new SLIP device detector
func NewSlipDetector(portName string, onDevice func(*SlipDevice)) (*SlipDetector, error) {
	config := &serial.Config{
		Name: portName,
		Baud: 115200, // HS_BAUDRATE from C code
	}
	
	port, err := serial.OpenPort(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port %s: %v", portName, err)
	}
	
	detector := &SlipDetector{
		port:     port,
		devices:  make(map[string]*SlipDevice),
		onDevice: onDevice,
	}
	
	return detector, nil
}

// Start begins listening for SLIP messages and sending topology messages
func (sd *SlipDetector) Start() error {
	// Start topology broadcaster
	go sd.sendTopologyMessages()
	
	// Start message listener
	go sd.listenForMessages()
	
	return nil
}

// sendTopologyMessages sends topology messages every 4 seconds (TOPOLOGY_SECS)
func (sd *SlipDetector) sendTopologyMessages() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		sd.sendTopologyMessage()
	}
}

// sendTopologyMessage creates and sends a topology message
func (sd *SlipDetector) sendTopologyMessage() {
	deviceCount := uint8(len(sd.devices))
	
	// Create topology message (simplified version based on C code structure)
	// This is a basic implementation - you may need to adjust based on actual protocol
	message := make([]byte, 32)
	message[0] = deviceCount // device count at start
	
	// Add timestamp (simplified)
	now := uint32(time.Now().Unix())
	binary.LittleEndian.PutUint32(message[1:5], now)
	
	// Send as SLIP packet
	slipPacket := sd.encodeSlip(message)
	
	if _, err := sd.port.Write(slipPacket); err != nil {
		log.Printf("Error sending topology message: %v", err)
	} else {
		log.Printf("Sent topology message with %d devices", deviceCount)
	}
}

// listenForMessages listens for incoming SLIP messages
func (sd *SlipDetector) listenForMessages() {
	buffer := make([]byte, 1024)
	packet := make([]byte, 0, 512)
	
	for {
		n, err := sd.port.Read(buffer)
		if err != nil {
			log.Printf("Error reading from serial port: %v", err)
			time.Sleep(time.Second)
			continue
		}
		
		// Process each byte for SLIP decoding
		for i := 0; i < n; i++ {
			b := buffer[i]
			
			if b == SLIP_END {
				if len(packet) > 0 {
					// Complete packet received
					sd.processPacket(packet)
					packet = packet[:0] // Reset packet buffer
				}
			} else if b == SLIP_ESC {
				// Next byte is escaped
				if i+1 < n {
					i++ // Move to next byte
					nextByte := buffer[i]
					if nextByte == SLIP_ESC_END {
						packet = append(packet, SLIP_END)
					} else if nextByte == SLIP_ESC_ESC {
						packet = append(packet, SLIP_ESC)
					} else {
						// Invalid escape sequence
						packet = append(packet, nextByte)
					}
				}
			} else {
				packet = append(packet, b)
			}
		}
	}
}

// processPacket processes a received SLIP packet to check for Announce messages
func (sd *SlipDetector) processPacket(packet []byte) {
	// Check for Announce message (60 bytes total as per C code)
	if len(packet) == 60 {
		// Check validation byte at position 28 should be 0x55
		if packet[28] == 0x55 {
			// Extract device information
			lastByte := packet[29]    // Last byte of IP
			nextLast := packet[30]    // Next-to-last byte of IP
			
			// Extract 18-byte serial number starting at byte 32
			var serialNo [18]byte
			copy(serialNo[:], packet[32:50])
			
			// Create device IP string (169.254.X.Y format)
			deviceIP := fmt.Sprintf("169.254.%d.%d", nextLast, lastByte)
			
			// Check if this is a new device
			if _, exists := sd.devices[deviceIP]; !exists {
				device := &SlipDevice{
					IP:       deviceIP,
					LastByte: lastByte,
					NextLast: nextLast,
					SerialNo: serialNo,
					LastSeen: time.Now(),
				}
				
				sd.devices[deviceIP] = device
				
				log.Printf("NEW SLIP Device detected: %s (Serial: %x)", deviceIP, serialNo[:8])
				
				// Call callback if provided
				if sd.onDevice != nil {
					sd.onDevice(device)
				}
			} else {
				// Update last seen time
				sd.devices[deviceIP].LastSeen = time.Now()
			}
		}
	}
}

// encodeSlip encodes data as a SLIP packet
func (sd *SlipDetector) encodeSlip(data []byte) []byte {
	encoded := make([]byte, 0, len(data)*2+2)
	encoded = append(encoded, SLIP_END) // Start delimiter
	
	for _, b := range data {
		if b == SLIP_END {
			encoded = append(encoded, SLIP_ESC, SLIP_ESC_END)
		} else if b == SLIP_ESC {
			encoded = append(encoded, SLIP_ESC, SLIP_ESC_ESC)
		} else {
			encoded = append(encoded, b)
		}
	}
	
	encoded = append(encoded, SLIP_END) // End delimiter
	return encoded
}

// GetDevices returns the current list of detected devices
func (sd *SlipDetector) GetDevices() map[string]*SlipDevice {
	return sd.devices
}

// GetDeviceCount returns the number of detected devices
func (sd *SlipDetector) GetDeviceCount() int {
	return len(sd.devices)
}

// Close closes the serial port
func (sd *SlipDetector) Close() error {
	return sd.port.Close()
}
