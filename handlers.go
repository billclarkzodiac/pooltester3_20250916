package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// handleGoDemo serves the Go-centric single driver demo page
func (n *NgaSim) handleGoDemo(w http.ResponseWriter, r *http.Request) {
	log.Println("🎯 Serving Go-centric demo page")

	// Get devices for template
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	// Get available commands for each device type
	deviceCommands := make(map[string]DeviceCommands)
	categories := n.commandRegistry.GetAllCategories()
	for _, category := range categories {
		commands, exists := n.commandRegistry.GetCommandsForCategory(category)
		if exists {
			deviceCommands[category] = DeviceCommands{
				Category: category,
				Commands: commands,
			}
		}
	}

	data := struct {
		Title          string
		Version        string
		Devices        []*Device
		DeviceCommands map[string]DeviceCommands
	}{
		Title:          "NgaSim Pool Controller - Go Demo",
		Version:        NgaSimVersion,
		Devices:        devices,
		DeviceCommands: deviceCommands,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := goDemoTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleDemo serves the original demo page (JS-centric)
func (n *NgaSim) handleDemo(w http.ResponseWriter, r *http.Request) {
	log.Println("🎯 Serving JS-centric demo page")
	n.handleHome(w, r) // Redirect to home for now
}

// handleHome serves the main dashboard
func (n *NgaSim) handleHome(w http.ResponseWriter, r *http.Request) {
	log.Println("🏠 Serving main dashboard")

	// Get devices for template
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	data := struct {
		Version string
		Devices []*Device
	}{
		Version: NgaSimVersion,
		Devices: devices,
	}

	w.Header().Set("Content-Type", "text/html")
    // Direct HTML response instead of template
    html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>%s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .device { border: 1px solid #ccc; margin: 10px; padding: 15px; border-radius: 5px; }
        .online { border-color: green; }
        .offline { border-color: red; }
    </style>
</head>
<body>
    <h1>NgaSim Pool Controller %s</h1>
    <h2>Discovered Devices (%d)</h2>
    %s
</body>
</html>`, "NgaSim Pool Controller", data.Version, len(data.Devices), generateDeviceHTML(data.Devices))
    
    w.Write([]byte(html))
}

// handleGoodbye serves the goodbye page
func (n *NgaSim) handleGoodbye(w http.ResponseWriter, r *http.Request) {
	log.Println("👋 Serving goodbye page")

	w.Header().Set("Content-Type", "text/html")
	if err := goodbyeTemplate.Execute(w, nil); err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleExit handles the exit API request
func (n *NgaSim) handleExit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("🚪 Exit request received")

	// Send response before cleanup
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	response := map[string]interface{}{
		"success": true,
		"message": "NgaSim shutting down...",
	}
	json.NewEncoder(w).Encode(response)

	// Start cleanup in goroutine to allow response to be sent
	go func() {
		time.Sleep(100 * time.Millisecond) // Give response time to send
		log.Println("🧹 Starting graceful shutdown...")
		n.cleanup()
		os.Exit(0)
	}()
}

// handleAPI serves the devices API
func (n *NgaSim) handleAPI(w http.ResponseWriter, r *http.Request) {
	log.Println("📡 API request: /api/devices")

	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(devices); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("📤 Sent %d devices to API client", len(devices))
}

// handleSanitizerCommand handles sanitizer command requests
func (n *NgaSim) handleSanitizerCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("🧪 Sanitizer command request received")

	var request struct {
		Serial     string `json:"serial"`
		Percentage int    `json:"percentage"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("🎯 Command: Set %s to %d%%", request.Serial, request.Percentage)

	// Validate percentage
	if request.Percentage < 0 || request.Percentage > 101 {
		http.Error(w, "Percentage must be 0-101", http.StatusBadRequest)
		return
	}

	// Send the command
	err := n.sendSanitizerCommand(request.Serial, "sanitizerGen2", request.Percentage)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	response := map[string]interface{}{
		"success":    err == nil,
		"serial":     request.Serial,
		"percentage": request.Percentage,
	}

	if err != nil {
		response["error"] = err.Error()
		log.Printf("❌ Command failed: %v", err)
	} else {
		log.Printf("✅ Command sent successfully: %s -> %d%%", request.Serial, request.Percentage)
	}

	json.NewEncoder(w).Encode(response)
}

// handleSanitizerStates handles sanitizer states API
func (n *NgaSim) handleSanitizerStates(w http.ResponseWriter, r *http.Request) {
	log.Println("📊 Sanitizer states request received")

	states := n.sanitizerController.GetAllStates()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(states); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}
}

// handlePowerLevels handles power levels API
func (n *NgaSim) handlePowerLevels(w http.ResponseWriter, r *http.Request) {
	log.Println("⚡ Power levels request received")

	levels := GetValidPowerLevels()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(levels); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleEmergencyStop handles emergency stop API
func (n *NgaSim) handleEmergencyStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("🛑 Emergency stop request received")

	// Stop all sanitizers
	n.mutex.RLock()
	sanitizers := make([]*Device, 0)
	for _, device := range n.devices {
		if device.Type == "sanitizerGen2" || device.Type == "Sanitizer" {
			sanitizers = append(sanitizers, device)
		}
	}
	n.mutex.RUnlock()

	results := make(map[string]interface{})
	for _, sanitizer := range sanitizers {
		err := n.sendSanitizerCommand(sanitizer.Serial, "sanitizerGen2", 0)
		results[sanitizer.Serial] = map[string]interface{}{
			"success": err == nil,
			"error":   err,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	response := map[string]interface{}{
		"success": true,
		"message": "Emergency stop executed",
		"results": results,
	}

	json.NewEncoder(w).Encode(response)
	log.Printf("🛑 Emergency stop completed for %d sanitizers", len(sanitizers))
}

// handleUISpecAPI serves the UI specification as JSON
func (n *NgaSim) handleUISpecAPI(w http.ResponseWriter, r *http.Request) {
	log.Println("📋 UI Spec API request received")

	// Create a basic UI spec structure
	spec := map[string]interface{}{
		"version": NgaSimVersion,
		"title":   "NgaSim Pool Controller",
		"devices": []map[string]interface{}{
			{
				"type":        "sanitizerGen2",
				"name":        "Salt Chlorinator",
				"description": "Salt water chlorine generator",
				"controls": []map[string]interface{}{
					{"type": "percentage", "min": 0, "max": 101, "step": 1},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(spec); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleDeviceCommands handles device commands API with category path
func (n *NgaSim) handleDeviceCommands(w http.ResponseWriter, r *http.Request) {
	log.Println("🔧 Device commands request received")

	// Extract category from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/device-commands/")
	category := strings.TrimSuffix(path, "/")

	if category == "" {
		http.Error(w, "Category required", http.StatusBadRequest)
		return
	}

	commands, exists := n.commandRegistry.GetCommandsForCategory(category)
	if !exists {
		http.Error(w, fmt.Sprintf("Category '%s' not found", category), http.StatusNotFound)
		return
	}

	result := DeviceCommands{
		Category: category,
		Commands: commands,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("📤 Sent %d commands for category '%s'", len(commands), category)
}

// handleProtobufMessages serves the protobuf message management page
func (n *NgaSim) handleProtobufMessages(w http.ResponseWriter, r *http.Request) {
	log.Println("🧬 Serving protobuf messages page")

	// Get all available protobuf messages
	messages := map[string]MessageDescriptor{}
	if n.reflectionEngine != nil {
		messages = n.reflectionEngine.GetAllMessages()
	}

	// Get devices for selection
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	data := struct {
		Title    string
		Version  string
		Messages map[string]MessageDescriptor
		Devices  []*Device
	}{
		Title:    "NgaSim - Protobuf Message Interface",
		Version:  NgaSimVersion,
		Messages: messages,
		Devices:  devices,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := protobufInterfaceTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleTerminalView serves the live terminal view page
func (n *NgaSim) handleTerminalView(w http.ResponseWriter, r *http.Request) {
	log.Println("📺 Serving terminal view page")

	data := struct {
		Title   string
		Version string
	}{
		Title:   "NgaSim - Live Terminal",
		Version: NgaSimVersion,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := terminalViewTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}
}

// Static file handlers
func (n *NgaSim) handleWireframeSVG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600">
        <text x="400" y="300" text-anchor="middle" font-size="24">NgaSim Wireframe</text>
    </svg>`))
}

func (n *NgaSim) handleWireframeMMD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte(`graph TD
        A[NgaSim] --> B[MQTT Broker]
        B --> C[Pool Devices]
        A --> D[Web Interface]
    `))
}

func (n *NgaSim) handleUISpecTOML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte(`[meta]
title = "NgaSim Pool Controller"
version = "` + NgaSimVersion + `"
`))
}

func (n *NgaSim) handleUISpecTXT(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte(`NgaSim Pool Controller UI Specification
Version: ` + NgaSimVersion + `
Device Types: Sanitizer, VSP, ICL, TruSense, Heater, HeatPump
`))
}

// Find your main device listing handler and add sorting:
func (n *NgaSim) handleRoot(w http.ResponseWriter, r *http.Request) {
	log.Println("🏠 Serving main interface")

	n.mutex.RLock()
	// Convert map to slice for sorting
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	// SORT BY SERIAL NUMBER - This was missing!
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Serial < devices[j].Serial
	})

	data := struct {
		Title    string
		Version  string
		Devices  []*Device
		Commands map[string][]string
	}{
		Title:    "NgaSim Pool Controller",
		Version:  NgaSimVersion,
		Devices:  devices, // Now sorted!
		Commands: n.deviceCommands,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := goDemoTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}
}

// Update the API devices handler:
func (n *NgaSim) handleDevices(w http.ResponseWriter, r *http.Request) {
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	// SORT BY SERIAL NUMBER for consistent API responses
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Serial < devices[j].Serial
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(devices)
}

// generateDeviceHTML creates HTML for device list
func generateDeviceHTML(devices []*Device) string {
    if len(devices) == 0 {
        return "<p>No devices discovered yet. Waiting for MQTT messages...</p>"
    }
    
    html := ""
    for _, device := range devices {
        status := "offline"
        if device.Status == "ONLINE" {
            status = "online"
        }
        html += fmt.Sprintf(`
            <div class="device %s">
                <h3>%s (%s)</h3>
                <p><strong>Type:</strong> %s</p>
                <p><strong>Status:</strong> %s</p>
                <p><strong>Last Seen:</strong> %s</p>
            </div>`, status, device.Name, device.Serial, device.Type, device.Status, device.LastSeen.Format("15:04:05"))
    }
    return html
}
