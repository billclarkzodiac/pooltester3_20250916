package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProtobufCommandData represents pre-filled command data for a device
type ProtobufCommandData struct {
	DeviceSerial string                 `json:"device_serial"`
	DeviceName   string                 `json:"device_name"`
	Category     string                 `json:"category"`
	CommandName  string                 `json:"command_name"`
	DisplayName  string                 `json:"display_name"`
	Description  string                 `json:"description"`
	Fields       map[string]interface{} `json:"fields"`
	UUID         string                 `json:"uuid"`
	CurrentState map[string]interface{} `json:"current_state"`
}

// Enhanced handleProtobufMessages with Go-heavy architecture
func (n *NgaSim) handleEnhancedProtobufMessages(w http.ResponseWriter, r *http.Request) {
	log.Println("🧬 Serving enhanced protobuf command interface (Go-heavy)")

	// Get current devices with sorting (addressing TODO #1)
	n.mutex.RLock()
	devices := make([]*Device, 0, len(n.devices))
	for _, device := range n.devices {
		devices = append(devices, device)
	}
	n.mutex.RUnlock()

	// Sort devices by serial number (MSB first) - TODO #1 fix
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Serial < devices[j].Serial
	})

	// Generate pre-filled command data for each device
	commandData := n.generatePrefilledCommands(devices)

	data := struct {
		Title       string
		Version     string
		Devices     []*Device
		CommandData []ProtobufCommandData
	}{
		Title:       "NgaSim - Enhanced Protobuf Command Interface",
		Version:     NgaSimVersion,
		Devices:     devices,
		CommandData: commandData,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := enhancedProtobufTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}
}

// generatePrefilledCommands creates pre-filled command forms for each device
func (n *NgaSim) generatePrefilledCommands(devices []*Device) []ProtobufCommandData {
	var commandData []ProtobufCommandData

	for _, device := range devices {
		switch device.Category {
		case "sanitizerGen2":
			// Get current device state for pre-filling
			currentPowerLevel := device.PowerLevel
			if currentPowerLevel == 0 && device.Status == "ONLINE" {
				currentPowerLevel = 50 // Reasonable default if no data
			}

			// Set Chlorine Output Command - pre-filled with current state
			commandData = append(commandData, ProtobufCommandData{
				DeviceSerial: device.Serial,
				DeviceName:   device.Name,
				Category:     device.Category,
				CommandName:  "set_sanitizer_output_percentage",
				DisplayName:  "Set Chlorine Output Level",
				Description:  "Control the chlorine generator output percentage (0-100%)",
				Fields: map[string]interface{}{
					"target_percentage": currentPowerLevel, // Pre-fill with current state!
					"uuid":              n.generateCommandUUID(),
					"timestamp":         time.Now().Unix(),
				},
				UUID: n.generateCommandUUID(),
				CurrentState: map[string]interface{}{
					"current_power_level": currentPowerLevel,
					"device_status":       device.Status,
					"last_seen":           device.LastSeen.Format("15:04:05"),
				},
			})

			// Get Status Command
			commandData = append(commandData, ProtobufCommandData{
				DeviceSerial: device.Serial,
				DeviceName:   device.Name,
				Category:     device.Category,
				CommandName:  "get_status",
				DisplayName:  "Get Device Status",
				Description:  "Retrieve current operational status and telemetry data",
				Fields: map[string]interface{}{
					"uuid":      n.generateCommandUUID(),
					"timestamp": time.Now().Unix(),
				},
				UUID: n.generateCommandUUID(),
				CurrentState: map[string]interface{}{
					"device_status": device.Status,
					"last_seen":     device.LastSeen.Format("15:04:05"),
				},
			})

			// Get Device Information Command
			commandData = append(commandData, ProtobufCommandData{
				DeviceSerial: device.Serial,
				DeviceName:   device.Name,
				Category:     device.Category,
				CommandName:  "get_device_information",
				DisplayName:  "Get Device Information",
				Description:  "Retrieve device details, serial numbers, and firmware information",
				Fields: map[string]interface{}{
					"uuid":      n.generateCommandUUID(),
					"timestamp": time.Now().Unix(),
				},
				UUID: n.generateCommandUUID(),
				CurrentState: map[string]interface{}{
					"firmware_version": device.FirmwareVersion,
					"model_version":    device.ModelVersion,
					"product_name":     device.ProductName,
				},
			})
		}
		// Future: Add VSP, ICL, Heater, etc. device types here
	}

	return commandData
}

// generateCommandUUID creates a unique command identifier
func (n *NgaSim) generateCommandUUID() string {
	return fmt.Sprintf("cmd_%d_%s", time.Now().UnixNano(),
		strings.ToLower(uuid.New().String()[:8]))
}

// handleProtobufCommandSubmission processes command form submissions (Go-heavy)
func (n *NgaSim) handleProtobufCommandSubmission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("🚀 Enhanced protobuf command submission received")

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	deviceSerial := r.FormValue("device_serial")
	commandName := r.FormValue("command_name")
	commandUUID := r.FormValue("uuid")

	log.Printf("📡 Processing command: %s for device %s (UUID: %s)", commandName, deviceSerial, commandUUID)

	// Handle specific commands using existing infrastructure
	switch commandName {
	case "set_sanitizer_output_percentage":
		targetPercentage := r.FormValue("target_percentage")
		percentage, err := strconv.Atoi(targetPercentage)
		if err != nil || percentage < 0 || percentage > 101 {
			http.Error(w, "Invalid percentage value (must be 0-101)", http.StatusBadRequest)
			return
		}

		// Use existing sanitizer command infrastructure - no duplicate logic!
		err = n.sendSanitizerCommand(deviceSerial, "sanitizerGen2", percentage)
		if err != nil {
			log.Printf("❌ Command failed: %v", err)
			http.Error(w, fmt.Sprintf("Command failed: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("✅ Sanitizer command sent: %s -> %d%% (UUID: %s)", deviceSerial, percentage, commandUUID)

	case "get_status", "get_device_information":
		// For query commands, we could trigger a status request
		log.Printf("📊 Status/Info request for device: %s (UUID: %s)", deviceSerial, commandUUID)
		// Future implementation: trigger MQTT status request

	default:
		log.Printf("⚠️  Unknown command: %s", commandName)
		http.Error(w, "Unknown command type", http.StatusBadRequest)
		return
	}

	// Redirect back to protobuf interface with success message (PRG pattern)
	redirectURL := fmt.Sprintf("/protobuf?success=%s&device=%s", commandName, deviceSerial)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// Enhanced Protobuf Interface Template (Go-heavy, minimal JS)
var enhancedProtobufTemplateHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { 
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; 
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            color: #2d3748;
        }
        
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        
        .header {
            background: rgba(255,255,255,0.95);
            padding: 20px;
            border-radius: 12px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
        }
        
        .header h1 { 
            color: #2d3748; 
            font-size: 1.8em; 
            margin-bottom: 8px;
        }
        
        .nav-links { margin-top: 15px; }
        .nav-links a { 
            display: inline-block; 
            padding: 8px 16px; 
            background: #4a5568; 
            color: white; 
            text-decoration: none; 
            border-radius: 6px; 
            margin-right: 10px; 
            font-size: 0.9em;
        }
        .nav-links a:hover { background: #667eea; }
        
        .device-section {
            background: rgba(255,255,255,0.95);
            border-radius: 12px;
            padding: 25px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0,0,0,0.1);
        }
        
        .device-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 20px;
            padding-bottom: 15px;
            border-bottom: 2px solid #e2e8f0;
        }
        
        .device-info h2 {
            color: #2d3748;
            font-size: 1.4em;
            margin-bottom: 5px;
        }
        
        .device-info .serial {
            color: #718096;
            font-family: 'Courier New', monospace;
            font-size: 0.9em;
        }
        
        .device-status {
            text-align: right;
        }
        
        .status-indicator {
            display: inline-block;
            width: 12px;
            height: 12px;
            border-radius: 50%;
            margin-right: 8px;
        }
        
        .status-online { background: #48bb78; }
        .status-offline { background: #f56565; }
        .status-unknown { background: #ed8936; }
        
        .current-state {
            background: #e6fffa;
            padding: 15px;
            border-radius: 8px;
            border-left: 4px solid #38b2ac;
            margin-bottom: 20px;
        }
        
        .current-state h4 {
            color: #2d3748;
            margin-bottom: 10px;
        }
        
        .state-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 10px;
        }
        
        .state-item {
            display: flex;
            justify-content: space-between;
        }
        
        .state-label {
            color: #4a5568;
            font-size: 0.9em;
        }
        
        .state-value {
            font-weight: 600;
            color: #2d3748;
        }
        
        .commands-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
            gap: 20px;
        }
        
        .command-form {
            background: #f7fafc;
            padding: 20px;
            border-radius: 8px;
            border-left: 4px solid #4a5568;
        }
        
        .command-header {
            margin-bottom: 15px;
        }
        
        .command-title {
            color: #2d3748;
            font-size: 1.1em;
            font-weight: 600;
            margin-bottom: 5px;
        }
        
        .command-description {
            color: #4a5568;
            font-size: 0.9em;
            line-height: 1.4;
        }
        
        .field-group {
            margin-bottom: 15px;
        }
        
        .field-label {
            display: block;
            font-weight: 600;
            margin-bottom: 6px;
            color: #2d3748;
            font-size: 0.9em;
        }
        
        .field-input {
            width: 100%;
            padding: 10px;
            border: 2px solid #e2e8f0;
            border-radius: 6px;
            font-size: 1em;
            background: white;
        }
        
        .field-input:focus {
            outline: none;
            border-color: #667eea;
        }
        
        .range-container {
            display: flex;
            align-items: center;
            gap: 10px;
        }
        
        .range-input {
            flex: 1;
        }
        
        .range-display {
            font-weight: 600;
            color: #2d3748;
            min-width: 50px;
            text-align: center;
            background: #edf2f7;
            padding: 8px;
            border-radius: 4px;
        }
        
        .uuid-field {
            font-family: 'Courier New', monospace;
            background: #edf2f7;
            font-size: 0.85em;
        }
        
        .field-help {
            font-size: 0.8em;
            color: #718096;
            margin-top: 4px;
        }
        
        .submit-btn {
            width: 100%;
            padding: 12px;
            background: #4299e1;
            color: white;
            border: none;
            border-radius: 6px;
            font-size: 1em;
            font-weight: 600;
            cursor: pointer;
            transition: background 0.2s;
        }
        
        .submit-btn:hover {
            background: #3182ce;
        }
        
        .success-message {
            background: #c6f6d5;
            color: #276749;
            padding: 15px;
            border-radius: 6px;
            margin-bottom: 20px;
            border-left: 4px solid #48bb78;
        }
        
        .no-devices {
            text-align: center;
            color: #718096;
            padding: 40px;
            background: rgba(255,255,255,0.8);
            border-radius: 12px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🧬 Enhanced Protobuf Command Interface</h1>
            <p>NgaSim v{{.Version}} - Go-Heavy Architecture with Pre-filled Commands</p>
            <div class="nav-links">
                <a href="/">🏠 Main Dashboard</a>
                <a href="/terminal">📺 Terminal View</a>
                <a href="/api/protobuf/messages">📊 Raw API</a>
            </div>
        </div>

        {{if .CommandData}}
        {{$currentDevice := ""}}
        {{range .CommandData}}
            {{if ne .DeviceSerial $currentDevice}}
                {{if ne $currentDevice ""}}
                    </div> <!-- Close commands-grid -->
                </div> <!-- Close device-section -->
                {{end}}
                {{$currentDevice = .DeviceSerial}}
                
                <div class="device-section">
                    <div class="device-header">
                        <div class="device-info">
                            <h2>{{.DeviceName}}</h2>
                            <div class="serial">{{.DeviceSerial}}</div>
                        </div>
                        <div class="device-status">
                            <span class="status-indicator status-{{if eq (index .CurrentState "device_status") "ONLINE"}}online{{else if eq (index .CurrentState "device_status") "OFFLINE"}}offline{{else}}unknown{{end}}"></span>
                            {{index .CurrentState "device_status"}}
                            <div style="font-size: 0.8em; color: #718096;">
                                Last seen: {{index .CurrentState "last_seen"}}
                            </div>
                        </div>
                    </div>
                    
                    {{if index .CurrentState "current_power_level"}}
                    <div class="current-state">
                        <h4>📊 Current Device State</h4>
                        <div class="state-grid">
                            <div class="state-item">
                                <span class="state-label">Power Level:</span>
                                <span class="state-value">{{index .CurrentState "current_power_level"}}%</span>
                            </div>
                            {{if index .CurrentState "firmware_version"}}
                            <div class="state-item">
                                <span class="state-label">Firmware:</span>
                                <span class="state-value">{{index .CurrentState "firmware_version"}}</span>
                            </div>
                            {{end}}
                        </div>
                    </div>
                    {{end}}
                    
                    <div class="commands-grid">
            {{end}}
            
            <form method="POST" action="/api/protobuf/command" class="command-form">
                <input type="hidden" name="device_serial" value="{{.DeviceSerial}}">
                <input type="hidden" name="command_name" value="{{.CommandName}}">
                
                <div class="command-header">
                    <div class="command-title">{{.DisplayName}}</div>
                    <div class="command-description">{{.Description}}</div>
                </div>
                
                {{if eq .CommandName "set_sanitizer_output_percentage"}}
                <div class="field-group">
                    <label class="field-label">🎚️ Chlorine Output Level (%)</label>
                    <div class="range-container">
                        <input type="range" name="target_percentage" class="range-input" 
                               min="0" max="100" step="5" value="{{index .Fields "target_percentage"}}"
                               oninput="updateRangeDisplay(this)">
                        <div class="range-display">{{index .Fields "target_percentage"}}%</div>
                    </div>
                    <div class="field-help">
                        💡 Pre-filled with current device setting ({{index .CurrentState "current_power_level"}}%)
                    </div>
                </div>
                {{end}}
                
                <div class="field-group">
                    <label class="field-label">🆔 Command UUID</label>
                    <input type="text" name="uuid" value="{{index .Fields "uuid"}}" class="field-input uuid-field">
                    <div class="field-help">Auto-generated unique identifier - editable for testing</div>
                </div>
                
                <div class="field-group">
                    <label class="field-label">⏰ Timestamp</label>
                    <input type="text" name="timestamp" value="{{index .Fields "timestamp"}}" class="field-input" readonly>
                    <div class="field-help">Command generation time (Unix timestamp)</div>
                </div>
                
                <button type="submit" class="submit-btn">
                    🚀 Send {{.DisplayName}}
                </button>
            </form>
        {{end}}
        {{if ne $currentDevice ""}}
        </div> <!-- Close final commands-grid -->
        </div> <!-- Close final device-section -->
        {{end}}
        
        {{else}}
        <div class="no-devices">
            <h3>No Devices Available</h3>
            <p>No devices are currently connected. Please check your MQTT connection and device status.</p>
        </div>
        {{end}}
    </div>

    <script>
        // Minimal JavaScript - just range display updates and success messages
        function updateRangeDisplay(rangeInput) {
            const rangeDisplay = rangeInput.parentNode.querySelector('.range-display');
            rangeDisplay.textContent = rangeInput.value + '%';
        }

        // Show success message if redirected with success parameter
        const urlParams = new URLSearchParams(window.location.search);
        const success = urlParams.get('success');
        const device = urlParams.get('device');
        if (success) {
            const header = document.querySelector('.header');
            const successDiv = document.createElement('div');
            successDiv.className = 'success-message';
            successDiv.innerHTML = '✅ Command "' + success + '" executed successfully for device ' + device + '!';
            header.appendChild(successDiv);
            
            // Clean URL without page reload
            const newUrl = window.location.pathname;
            window.history.replaceState({}, document.title, newUrl);
        }
    </script>
</body>
</html>
`

// Create the enhanced template
var enhancedProtobufTemplate = template.Must(template.New("enhancedProtobuf").Parse(enhancedProtobufTemplateHTML))
