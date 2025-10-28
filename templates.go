package main

import (
	"html/template"
	"strings" // This will stay now because we're using it below
)

// Add custom template functions - THIS IS WHAT MAKES strings STAY
var templateFuncs = template.FuncMap{
	"lower": strings.ToLower,
	"upper": strings.ToUpper,
	"title": strings.Title,
}

// HTML templates for the web interface

// Main Go-centric demo template with dynamic protobuf integration
var goDemoTemplateHTML = `
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
            color: #333;
        }
        
        .container { 
            max-width: 1200px; 
            margin: 0 auto; 
            padding: 20px; 
        }
        
        .header {
            background: rgba(255, 255, 255, 0.95);
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        
        .header h1 {
            color: #4a5568;
            margin-bottom: 10px;
        }
        
        .nav-links {
            display: flex;
            gap: 15px;
            flex-wrap: wrap;
        }
        
        .nav-links a {
            color: #667eea;
            text-decoration: none;
            padding: 8px 15px;
            border: 2px solid #667eea;
            border-radius: 5px;
            transition: all 0.3s ease;
        }
        
        .nav-links a:hover {
            background: #667eea;
            color: white;
        }
        
        .devices-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        
        .device-card {
            background: rgba(255, 255, 255, 0.95);
            border-radius: 10px;
            padding: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
            transition: transform 0.3s ease;
        }
        
        .device-card:hover {
            transform: translateY(-5px);
        }
        
        .device-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 15px;
            padding-bottom: 10px;
            border-bottom: 2px solid #e2e8f0;
        }
        
        .device-title {
            font-size: 1.2em;
            font-weight: bold;
            color: #2d3748;
        }
        
        .status-indicator {
            padding: 4px 12px;
            border-radius: 20px;
            font-size: 0.8em;
            font-weight: bold;
            text-transform: uppercase;
        }
        
        .status-online {
            background: #68d391;
            color: #22543d;
        }
        
        .status-offline {
            background: #fc8181;
            color: #742a2a;
        }
        
        .device-info {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 10px;
            margin-bottom: 15px;
            font-size: 0.9em;
        }
        
        .info-item {
            display: flex;
            justify-content: space-between;
        }
        
        .info-label {
            font-weight: bold;
            color: #4a5568;
        }
        
        .info-value {
            color: #2d3748;
        }
        
        .control-group {
            margin-bottom: 20px;
        }
        
        .control-label {
            font-weight: bold;
            color: #4a5568;
            margin-bottom: 8px;
            display: block;
        }
        
        .controls {
            display: flex;
            gap: 8px;
            flex-wrap: wrap;
        }
        
        .btn {
            padding: 8px 16px;
            border: none;
            border-radius: 5px;
            cursor: pointer;
            font-size: 0.9em;
            font-weight: bold;
            transition: all 0.3s ease;
            text-decoration: none;
            display: inline-block;
            text-align: center;
        }
        
        .btn-primary {
            background: #667eea;
            color: white;
        }
        
        .btn-primary:hover {
            background: #5a67d8;
        }
        
        .btn-secondary {
            background: #a0aec0;
            color: white;
        }
        
        .btn-secondary:hover {
            background: #718096;
        }
        
        .btn-success {
            background: #48bb78;
            color: white;
        }
        
        .btn-success:hover {
            background: #38a169;
        }
        
        .btn-warning {
            background: #ed8936;
            color: white;
        }
        
        .btn-warning:hover {
            background: #dd6b20;
        }
        
        .btn-danger {
            background: #f56565;
            color: white;
        }
        
        .btn-danger:hover {
            background: #e53e3e;
        }
        
        .protobuf-section {
            background: rgba(255, 255, 255, 0.95);
            border-radius: 10px;
            padding: 20px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        
        .protobuf-commands {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 10px;
        }
        
        .protobuf-cmd-btn {
            background: #9f7aea;
            color: white;
            border: none;
            padding: 10px 15px;
            border-radius: 5px;
            cursor: pointer;
            font-size: 0.9em;
            transition: all 0.3s ease;
        }
        
        .protobuf-cmd-btn:hover {
            background: #805ad5;
        }
        
        .log-entry {
            margin-bottom: 5px;
            padding: 2px 0;
        }
        
        .log-timestamp {
            color: #a0aec0;
        }
        
        .log-request {
            color: #63b3ed;
        }
        
        .log-response {
            color: #68d391;
        }
        
        .log-error {
            color: #fc8181;
        }
        
        .log-telemetry {
            color: #81e6d9;
        }
        
        /* Modal/Popup Styles */
        .modal {
            display: none;
            position: fixed;
            z-index: 1000;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background-color: rgba(0, 0, 0, 0.5);
        }
        
        .modal-content {
            position: relative;
            background-color: white;
            margin: 5% auto;
            padding: 0;
            border-radius: 10px;
            width: 90%;
            max-width: 600px;
            max-height: 80vh;
            overflow-y: auto;
            box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        }
        
        .popup-header {
            background: #667eea;
            color: white;
            padding: 20px;
            border-radius: 10px 10px 0 0;
            position: relative;
        }
        
        .popup-header h3 {
            margin: 0;
            font-size: 1.3em;
        }
        
        .close-btn {
            position: absolute;
            right: 15px;
            top: 15px;
            background: none;
            border: none;
            color: white;
            font-size: 24px;
            cursor: pointer;
        }
        
        .popup-body {
            padding: 20px;
        }
        
        .form-group {
            margin-bottom: 15px;
        }
        
        .form-group label {
            display: block;
            font-weight: bold;
            margin-bottom: 5px;
            color: #4a5568;
        }
        
        .field-description {
            font-size: 0.8em;
            color: #718096;
            margin-bottom: 5px;
        }
        
        .form-control {
            width: 100%;
            padding: 8px 12px;
            border: 1px solid #cbd5e0;
            border-radius: 5px;
            font-size: 0.9em;
        }
        
        .form-control:focus {
            outline: none;
            border-color: #667eea;
            box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
        }
        
        .required-indicator {
            color: #e53e3e;
            font-size: 0.8em;
        }
        
        .popup-footer {
            background: #f7fafc;
            padding: 15px 20px;
            border-radius: 0 0 10px 10px;
            display: flex;
            gap: 10px;
            justify-content: flex-end;
        }
        
        .command-preview {
            background: #f7fafc;
            border: 1px solid #e2e8f0;
            border-radius: 5px;
            padding: 15px;
            margin-top: 15px;
        }
        
        .command-preview pre {
            background: #1a202c;
            color: #e2e8f0;
            padding: 10px;
            border-radius: 5px;
            font-size: 0.8em;
            overflow-x: auto;
        }
        
        .btn-info {
            background: #4299e1;
            color: white;
        }
        
        .btn-info:hover {
            background: #3182ce;
        }
        
        .footer {
            background: rgba(255, 255, 255, 0.95);
            border-radius: 10px;
            padding: 15px;
            text-align: center;
            margin-top: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        
        .emergency-controls {
            background: #fed7d7;
            border: 2px solid #fc8181;
            border-radius: 10px;
            padding: 15px;
            margin-bottom: 20px;
        }
        
        .emergency-controls h3 {
            color: #742a2a;
            margin-bottom: 10px;
        }
        
        /* Smart Form Styles */
        .smart-design .popup-header {
            background: linear-gradient(135deg, #4299e1 0%, #2b6cb0 100%);
            color: white;
        }
        
        .popup-body.smart-design {
            padding: 25px;
        }
        
        .smart-form-group {
            margin-bottom: 20px;
            padding: 15px;
            background: #f7fafc;
            border-radius: 8px;
            border-left: 4px solid #4299e1;
        }
        
        .smart-label {
            font-size: 1.1em;
            font-weight: bold;
            color: #2d3748;
            margin-bottom: 8px;
            display: block;
        }
        
        .smart-help {
            font-size: 0.9em;
            color: #4a5568;
            margin-bottom: 12px;
            padding: 6px 10px;
            background: #edf2f7;
            border-radius: 4px;
        }
        
        /* Enhanced Slider */
        .slider-control {
            margin: 10px 0;
        }
        
        .smart-slider {
            width: 100%;
            height: 6px;
            border-radius: 3px;
            background: #e2e8f0;
            outline: none;
            -webkit-appearance: none;
        }
        
        .smart-slider::-webkit-slider-thumb {
            -webkit-appearance: none;
            appearance: none;
            width: 20px;
            height: 20px;
            border-radius: 50%;
            background: #4299e1;
            cursor: pointer;
            box-shadow: 0 2px 4px rgba(0,0,0,0.2);
        }
        
        .slider-display {
            text-align: center;
            margin-top: 8px;
            font-size: 1.3em;
            font-weight: bold;
            color: #2d3748;
            padding: 8px;
            background: white;
            border-radius: 4px;
            border: 2px solid #4299e1;
        }
        
        /* Device Terminal Styles */
        .device-terminal {
            background: #1a202c;
            color: #e2e8f0;
            font-family: 'Courier New', monospace;
            font-size: 0.8em;
            padding: 10px;
            border-radius: 5px;
            height: 200px;
            overflow-y: auto;
            margin-top: 10px;
            border: 1px solid #4a5568;
        }
        
        .terminal-entry {
            margin-bottom: 3px;
            padding: 1px 0;
        }
        
        .terminal-announce { color: #f6e05e; }
        .terminal-telemetry { color: #68d391; }
        .terminal-command { color: #63b3ed; }
        .terminal-response { color: #9ae6b4; }
        .terminal-error { color: #fc8181; }
    </style>
</head>
<body>
    <div class="container">
        <!-- Header -->
        <div class="header">
            <h1>🏊 NgaSim Pool Controller v{{.Version}}</h1>
            <p>Dynamic Protobuf Interface with Live Terminal</p>
            <div class="nav-links">
                <a href="/">🏠 Main</a>
                <a href="/protobuf">🧬 Protobuf Messages</a>
                <a href="/terminal">📺 Live Terminal</a>
                <a href="/js-demo">🎮 JS Demo</a>
                <a href="/old">🏠 Original</a>
                <a href="/api/devices">📊 API</a>
                <a href="/goodbye">👋 Exit</a>
            </div>
        </div>

        <!-- Emergency Controls -->
        <div class="emergency-controls">
            <h3>🚨 Emergency Controls</h3>
            <button class="btn btn-danger" onclick="emergencyStop()">🛑 EMERGENCY STOP ALL</button>
            <button class="btn btn-warning" onclick="refreshDevices()">🔄 Refresh Devices</button>
        </div>

        <!-- Devices Grid -->
        <div class="devices-grid">
            {{range .Devices}}
            <div class="device-card">
                <!-- Device Header -->
                <div class="device-header">
                    <div class="device-title">{{.Name}}</div>
                    <div class="status-indicator {{if eq .Status "ONLINE"}}status-online{{else}}status-offline{{end}}">
                        {{.Status}}
                    </div>
                </div>

                <!-- Device Information -->
                <div class="device-info">
                    <div class="info-item">
                        <span class="info-label">Serial:</span>
                        <span class="info-value">{{.Serial}}</span>
                    </div>
                    <div class="info-item">
                        <span class="info-label">Type:</span>
                        <span class="info-value">{{.Type}}</span>
                    </div>
                    {{if .ProductName}}
                    <div class="info-item">
                        <span class="info-label">Product:</span>
                        <span class="info-value">{{.ProductName}}</span>
                    </div>
                    {{end}}
                    {{if .FirmwareVersion}}
                    <div class="info-item">
                        <span class="info-label">Firmware:</span>
                        <span class="info-value">{{.FirmwareVersion}}</span>
                    </div>
                    {{end}}
                    <div class="info-item">
                        <span class="info-label">Last Seen:</span>
                        <span class="info-value">{{.LastSeen.Format "15:04:05"}}</span>
                    </div>
                </div>

                <!-- Device-Specific Controls -->
                {{if eq .Type "sanitizerGen2"}}
                <div class="control-group">
                    <div class="control-label">💧 Sanitizer Control (Current: {{.ActualPercentage}}%)</div>
                    <div class="controls">
                        <button class="btn btn-secondary" onclick="sendSanitizerCommand('{{.Serial}}', 0)">OFF</button>
                        <button class="btn btn-primary" onclick="sendSanitizerCommand('{{.Serial}}', 10)">10%</button>
                        <button class="btn btn-primary" onclick="sendSanitizerCommand('{{.Serial}}', 50)">50%</button>
                        <button class="btn btn-primary" onclick="sendSanitizerCommand('{{.Serial}}', 100)">100%</button>
                        <button class="btn btn-warning" onclick="sendSanitizerCommand('{{.Serial}}', 101)">BOOST</button>
                    </div>
                    {{if .PPMSalt}}<p style="font-size: 0.8em; color: #666; margin-top: 5px;">Salt: {{.PPMSalt}} ppm | Voltage: {{.LineInputVoltage}}V | RSSI: {{.RSSI}} dBm</p>{{end}}
                </div>
                {{end}}

                <!-- Dynamic Protobuf Commands -->
                <div class="control-group">
                    <div class="control-label">🧬 Protobuf Commands</div>
                    <div class="controls">
                        <button class="btn protobuf-cmd-btn" onclick="showProtobufCommands('{{.Serial}}', '{{.Type}}')">
                            📋 Show Available Commands
                        </button>
                        <button class="btn btn-success" onclick="showDeviceTerminal('{{.Serial}}')">
                            📺 Device Terminal
                        </button>
                    </div>
                </div>

                <!-- Telemetry Display -->
                {{if or .Temp .Power .RPM .PH .ORP}}
                <div class="control-group">
                    <div class="control-label">📊 Live Telemetry</div>
                    <div class="device-info">
                        {{if .Temp}}<div class="info-item"><span class="info-label">Temperature:</span><span class="info-value">{{printf "%.1f" .Temp}}°C</span></div>{{end}}
                        {{if .Power}}<div class="info-item"><span class="info-label">Power:</span><span class="info-value">{{.Power}}W</span></div>{{end}}
                        {{if .RPM}}<div class="info-item"><span class="info-label">RPM:</span><span class="info-value">{{.RPM}}</span></div>{{end}}
                        {{if .PH}}<div class="info-item"><span class="info-label">pH:</span><span class="info-value">{{printf "%.1f" .PH}}</span></div>{{end}}
                        {{if .ORP}}<div class="info-item"><span class="info-label">ORP:</span><span class="info-value">{{.ORP}} mV</span></div>{{end}}
                    </div>
                </div>
                {{end}}

                <!-- Device Live Terminal -->
                <div class="control-group">
                    <div class="control-label">📺 Live Device Terminal</div>
                    <div class="device-terminal" id="terminal-{{.Serial}}">
                        {{range .LiveTerminal}}
                        <div class="terminal-entry terminal-{{.Type | lower}}">
                            <div class="terminal-header">
                                <span style="color: #a0aec0;">[{{.Timestamp.Format "15:04:05"}}]</span>
                                <span class="terminal-{{.Type | lower}}">[{{.Type}}]</span>
                                <span>{{.Message}}</span>
                            </div>
                            {{if .ParsedProtobuf}}
                            <div class="protobuf-details" style="margin-left: 20px; padding: 5px; background: rgba(255,255,255,0.05); border-radius: 3px; font-size: 0.85em;">
                                <div style="color: #63b3ed; font-weight: bold; margin-bottom: 3px;">📋 {{.ParsedProtobuf.MessageType}} ({{len .ParsedProtobuf.Fields}} fields)</div>
                                {{range .ParsedProtobuf.Fields}}
                                {{if .IsSet}}
                                <div style="margin-bottom: 1px;">
                                    <span style="color: #a0aec0; font-weight: bold;">{{.Name}}:</span>
                                    <span style="color: #e2e8f0; margin-left: 8px;">{{.RawValue}}</span>
                                    {{if .Description}}<span style="color: #718096; font-style: italic; font-size: 0.8em;"> - {{.Description}}</span>{{end}}
                                </div>
                                {{end}}
                                {{end}}
                            </div>
                            {{end}}
                        </div>
                        {{end}}
                        {{if not .LiveTerminal}}
                        <div class="terminal-entry">
                            <span style="color: #a0aec0;">Waiting for device activity...</span>
                        </div>
                        {{end}}
                    </div>
                    <button class="btn btn-secondary btn-small" onclick="clearDeviceTerminal('{{.Serial}}')">🗑️ Clear</button>
                </div>
            </div>
            {{end}}
        </div>

        <!-- Footer -->
        <div class="footer">
            <p>NgaSim Pool Controller v{{.Version}} | 
            <a href="/api/devices">JSON API</a> | 
            <a href="/protobuf">Protobuf Interface</a> | 
            <a href="/terminal">Live Terminal</a></p>
        </div>
    </div>

    <!-- Protobuf Command Modal -->
    <div id="protobufModal" class="modal">
        <div class="modal-content" id="protobufModalContent">
            <!-- Dynamic content will be loaded here -->
        </div>
    </div>

    <script>
        // Global variables
        let currentDevice = null;

        // Sanitizer control function
        async function sendSanitizerCommand(serial, percentage) {
            console.log('Sending sanitizer command:', serial, percentage);
            
            try {
                const response = await fetch('/api/sanitizer/command', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ serial: serial, percentage: percentage })
                });
                
                const result = await response.json();
                
                if (result.success) {
                    console.log('✅ Command successful:', result);
                    // Refresh the page after a short delay to show updated values
                    setTimeout(() => location.reload(), 1000);
                } else {
                    console.error('❌ Command failed:', result.error);
                    alert('Command failed: ' + result.error);
                }
            } catch (error) {
                console.error('Network error:', error);
                alert('Network error: ' + error.message);
            }
        }

        // Show available protobuf commands for a device
        async function showProtobufCommands(deviceSerial, deviceType) {
            console.log('Showing protobuf commands for:', deviceSerial, deviceType);
            
            try {
                // Get available message types
                const response = await fetch('/api/protobuf/messages');
                const messages = await response.json();
                
                // Filter messages for this device type/category
                const relevantMessages = Object.values(messages).filter(msg => 
                    msg.is_request && (msg.category === deviceType || msg.category === 'Generic')
                );
                
                if (relevantMessages.length === 0) {
                    alert('No protobuf commands available for device type: ' + deviceType);
                    return;
                }
                
                // Show selection modal
                showMessageSelectionModal(deviceSerial, deviceType, relevantMessages);
                
            } catch (error) {
                console.error('Error loading protobuf commands:', error);
                alert('Error loading commands: ' + error.message);
            }
        }

        // Show message selection modal
        function showMessageSelectionModal(deviceSerial, deviceType, messages) {
            let html = '<div class="popup-header">';
            html += '<h3>Select Protobuf Command</h3>';
            html += '<p>Device: <strong>' + deviceSerial + '</strong> | Type: <strong>' + deviceType + '</strong></p>';
            html += '<button class="close-btn" onclick="closePopup()">&times;</button>';
            html += '</div>';
            html += '<div class="popup-body">';
            html += '<div class="protobuf-commands">';
            
            messages.forEach(msg => {
                html += '<button class="protobuf-cmd-btn" onclick="showProtobufPopup(\'' + 
                        msg.package + '.' + msg.name + '\', \'' + deviceSerial + '\', \'' + deviceType + '\')">';
                html += '<strong>' + msg.name + '</strong><br>';
                html += '<small>' + msg.package + '</small>';
                html += '</button>';
            });
            
            html += '</div>';
            html += '</div>';
            html += '<div class="popup-footer">';
            html += '<button type="button" class="btn btn-secondary" onclick="closePopup()">Cancel</button>';
            html += '</div>';
            
            document.getElementById('protobufModalContent').innerHTML = html;
            document.getElementById('protobufModal').style.display = 'block';
        }

        // Show protobuf command popup
        async function showProtobufPopup(messageType, deviceSerial, category) {
            console.log('Generating popup for:', messageType, deviceSerial, category);
            
            try {
                const response = await fetch('/api/protobuf/popup', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        message_type: messageType,
                        device_serial: deviceSerial,
                        category: category
                    })
                });
                
                const result = await response.json();
                
                if (result.html) {
                    document.getElementById('protobufModalContent').innerHTML = result.html;
                    document.getElementById('protobufModal').style.display = 'block';
                } else {
                    alert('Failed to generate popup: ' + (result.error || 'Unknown error'));
                }
            } catch (error) {
                console.error('Error generating popup:', error);
                alert('Error: ' + error.message);
            }
        }

        // Execute protobuf command from form
        async function executeProtobufCommand() {
            const form = document.getElementById('protobuf-command-form');
            const formData = new FormData(form);
            
            // Convert form data to object
            const fieldValues = {};
            for (let [key, value] of formData.entries()) {
                if (key !== 'message_type' && key !== 'device_serial' && key !== 'category') {
                    // Try to convert to appropriate type
                    if (value === 'true' || value === 'false') {
                        fieldValues[key] = value === 'true';
                    } else if (!isNaN(value) && value !== '') {
                        fieldValues[key] = parseFloat(value);
                    } else {
                        fieldValues[key] = value;
                    }
                }
            }
            
            const commandData = {
                message_type: formData.get('message_type'),
                device_serial: formData.get('device_serial'),
                category: formData.get('category'),
                field_values: fieldValues
            };
            
            console.log('Executing protobuf command:', commandData);
            
            try {
                const response = await fetch('/api/protobuf/command', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(commandData)
                });
                
                const result = await response.json();
                
                if (result.success) {
                    alert('✅ Command sent successfully!\\nCorrelation ID: ' + result.correlation_id);
                    closePopup();
                    // Refresh page to see the command in device terminals
                    setTimeout(() => location.reload(), 1000);
                } else {
                    alert('❌ Command failed: ' + result.error);
                }
            } catch (error) {
                console.error('Error executing command:', error);
                alert('Error: ' + error.message);
            }
        }

        // Preview command before sending
        function previewCommand() {
            const form = document.getElementById('protobuf-command-form');
            const formData = new FormData(form);
            
            const fieldValues = {};
            for (let [key, value] of formData.entries()) {
                if (key !== 'message_type' && key !== 'device_serial' && key !== 'category') {
                    fieldValues[key] = value;
                }
            }
            
            const preview = {
                message_type: formData.get('message_type'),
                device_serial: formData.get('device_serial'),
                category: formData.get('category'),
                field_values: fieldValues
            };
            
            const previewDiv = document.getElementById('command-preview');
            const previewContent = document.getElementById('preview-content');
            
            previewContent.textContent = JSON.stringify(preview, null, 2);
            previewDiv.style.display = 'block';
        }

        // Show device-specific terminal
        function showDeviceTerminal(deviceSerial) {
            // Open new window with device-specific terminal
            window.open('/terminal?device=' + deviceSerial, '_blank', 'width=800,height=600');
        }

        // Emergency stop function
        async function emergencyStop() {
            if (confirm('🚨 This will stop ALL sanitizers immediately. Are you sure?')) {
                try {
                    const response = await fetch('/api/emergency-stop', { method: 'POST' });
                    const result = await response.json();
                    
                    if (result.success) {
                        alert('🛑 Emergency stop executed successfully!');
                        location.reload();
                    } else {
                        alert('❌ Emergency stop failed: ' + result.error);
                    }
                } catch (error) {
                    console.error('Emergency stop error:', error);
                    alert('Error: ' + error.message);
                }
            }
        }

        // Refresh devices
        function refreshDevices() {
            location.reload();
        }

        // Modal functions
        function closePopup() {
            document.getElementById('protobufModal').style.display = 'none';
        }

        // Close modal when clicking outside
        window.onclick = function(event) {
            const modal = document.getElementById('protobufModal');
            if (event.target === modal) {
                closePopup();
            }
        };

        // Enhanced JavaScript for smart forms and live terminals
        function updateSliderDisplay(fieldName, value, unit) {
            document.getElementById('display_' + fieldName).textContent = value;
        }

        function executeSmartCommand() {
            const form = document.getElementById('smart-command-form');
            const formData = new FormData(form);
            
            // Convert form data to object with smart processing
            const fieldValues = {};
            for (let [key, value] of formData.entries()) {
                if (key !== 'message_type' && key !== 'device_serial' && key !== 'category') {
                    // Smart type conversion
                    if (value === 'true' || value === 'false') {
                        fieldValues[key] = value === 'true';
                    } else if (!isNaN(value) && value !== '') {
                        fieldValues[key] = parseFloat(value);
                    } else {
                        fieldValues[key] = value;
                    }
                }
            }
            
            const commandData = {
                message_type: formData.get('message_type'),
                device_serial: formData.get('device_serial'),
                category: formData.get('category'),
                field_values: fieldValues
            };
            
            console.log('🚀 Executing smart command:', commandData);
            
            // Execute the command (same as before but with better feedback)
            executeProtobufCommand(); // Reuse existing function
        }

        function clearDeviceTerminal(deviceSerial) {
            const terminal = document.getElementById('terminal-' + deviceSerial);
            if (terminal) {
                terminal.innerHTML = '<div class="terminal-entry"><span style="color: #a0aec0;">Terminal cleared</span></div>';
            }
        }

        // Auto-refresh device terminals every 5 seconds
        setInterval(function() {
            // Refresh page to get updated device terminals
            // In a real implementation, this would be WebSocket updates
            if (document.querySelector('.device-terminal')) {
                location.reload();
            }
        }, 5000);
    </script>
</body>
</html>
`

// Protobuf Messages Interface Template
var protobufInterfaceTemplateHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <style>
        /* Include the same CSS as above, plus specific protobuf styles */
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); min-height: 100vh; color: #333; }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        
        .header { background: rgba(255, 255, 255, 0.95); padding: 20px; border-radius: 10px; margin-bottom: 20px; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1); }
        
        .messages-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
        
        .message-card { background: rgba(255, 255, 255, 0.95); border-radius: 10px; padding: 20px; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1); }
        
        .message-type-request { border-left: 4px solid #48bb78; }
        .message-type-response { border-left: 4px solid #4299e1; }
        .message-type-telemetry { border-left: 4px solid #ed8936; }
        
        .message-header { display: flex; justify-content: between; align-items: flex-start; margin-bottom: 15px; }
        
        .message-name { font-size: 1.2em; font-weight: bold; color: #2d3748; }
        .message-package { font-size: 0.8em; color: #718096; }
        
        .message-category { background: #edf2f7; color: #4a5568; padding: 4px 8px; border-radius: 12px; font-size: 0.7em; }
        
        .fields-list { margin: 15px 0; }
        .field-item { padding: 8px; margin: 4px 0; background: #f7fafc; border-radius: 4px; font-size: 0.85em; }
        .field-name { font-weight: bold; color: #2d3748; }
        .field-type { color: #718096; }
        
        .btn { padding: 8px 16px; border: none; border-radius: 5px; cursor: pointer; font-size: 0.9em; font-weight: bold; transition: all 0.3s ease; text-decoration: none; display: inline-block; text-align: center; }
        .btn-primary { background: #667eea; color: white; }
        .btn-primary:hover { background: #5a67d8; }
        
        .nav-links {
            display: flex;
            gap: 15px;
            flex-wrap: wrap;
        }
        
        .nav-links a {
            color: #667eea;
            text-decoration: none;
            padding: 8px 15px;
            border: 2px solid #667eea;
            border-radius: 5px;
            transition: all 0.3s ease;
        }
        
        .nav-links a:hover {
            background: #667eea;
            color: white;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🧬 Protobuf Message Interface</h1>
            <p>NgaSim v{{.Version}} - Dynamic Message Discovery</p>
            <div class="nav-links">
                <a href="/">🏠 Main</a>
                <a href="/terminal">📺 Terminal</a>
                <a href="/api/protobuf/messages">📊 API</a>
            </div>
        </div>

        <div class="messages-grid">
            {{range $msgType, $msg := .Messages}}
            <div class="message-card {{if $msg.IsRequest}}message-type-request{{else if $msg.IsResponse}}message-type-response{{else if $msg.IsTelemetry}}message-type-telemetry{{end}}">
                <div class="message-header">
                    <div>
                        <div class="message-name">{{$msg.Name}}</div>
                        <div class="message-package">{{$msg.Package}}</div>
                    </div>
                    <div class="message-category">{{$msg.Category}}</div>
                </div>
                
                <div class="fields-list">
                    {{range $msg.Fields}}
                    <div class="field-item">
                        <span class="field-name">{{.Name}}</span>: 
                        <span class="field-type">{{.Type}}</span>
                        {{if .Required}}<span style="color: #e53e3e;">*</span>{{end}}
                    </div>
                    {{end}}
                </div>
                
                {{if $msg.IsRequest}}
                <select id="device-{{$msgType}}" style="width: 100%; margin: 10px 0; padding: 5px;">
                    <option value="">Select Device...</option>
                    {{range $.Devices}}
                    <option value="{{.Serial}}">{{.Name}} ({{.Serial}})</option>
                    {{end}}
                </select>
                <button class="btn btn-primary" onclick="openCommandPopup('{{$msgType}}', '{{$msg.Category}}')">
                    🚀 Send Command
                </button>
                {{end}}
            </div>
            {{end}}
        </div>
    </div>

    <script>
        function openCommandPopup(messageType, category) {
            const select = document.getElementById('device-' + messageType);
            const deviceSerial = select.value;
            
            if (!deviceSerial) {
                alert('Please select a device first');
                return;
            }
            
            // Open popup in main window
            window.opener.showProtobufPopup(messageType, deviceSerial, category);
        }
    </script>
</body>
</html>
`

// Terminal View Template
var terminalViewTemplateHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}} - Live Terminal</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: #1a202c; color: #e2e8f0; height: 100vh; }
        
        .header { background: #2d3748; padding: 15px; border-bottom: 1px solid #4a5568; }
        .header h1 { color: #e2e8f0; font-size: 1.2em; }
        
        .terminal-container { height: calc(100vh - 80px); display: flex; flex-direction: column; }
        
        .terminal-display { 
            flex: 1; 
            font-family: 'Courier New', monospace; 
            font-size: 0.85em; 
            padding: 15px; 
            overflow-y: auto; 
            background: #1a202c; 
            line-height: 1.4;
        }
        
        .log-entry { margin-bottom: 3px; padding: 2px 0; }
        .log-timestamp { color: #a0aec0; }
        .log-request { color: #63b3ed; }
        .log-response { color: #68d391; }
        .log-error { color: #fc8181; }
        .log-telemetry { color: #81e6d9; }
        .log-announce { color: #d69e2e; }
        
        .controls { background: #2d3748; padding: 10px; border-top: 1px solid #4a5568; display: flex; gap: 10px; }
        .btn { padding: 8px 15px; background: #4a5568; color: #e2e8f0; border: none; border-radius: 4px; cursor: pointer; }
        .btn:hover { background: #718096; }
        .btn-success { background: #38a169; }
        .btn-danger { background: #e53e3e; }
    </style>
</head>
<body>
    <div class="header">
        <h1>📺 NgaSim Live Terminal v{{.Version}}{{if .DeviceFilter}} - Device: {{.DeviceFilter}}{{end}}</h1>
    </div>
    
    <div class="terminal-container">
        <div class="terminal-display" id="terminal-display">
            <div class="log-entry">
                <span class="log-timestamp">[Loading...]</span>
                <span>Terminal initializing...</span>
            </div>
        </div>
        
        <div class="controls">
            <button class="btn btn-success" onclick="refreshTerminal()">🔄 Refresh</button>
            <button class="btn btn-danger" onclick="clearTerminal()">🗑️ Clear</button>
            <button class="btn" onclick="toggleAutoRefresh()">⏸️ Pause Auto-Refresh</button>
            <span id="status">Auto-refresh: ON</span>
            {{if .AvailableDevices}}
            <select id="device-filter" onchange="changeDeviceFilter()">
                <option value="">All Devices</option>
                {{range .AvailableDevices}}
                <option value="{{.}}"{{if eq . $.DeviceFilter}} selected{{end}}>{{.}}</option>
                {{end}}
            </select>
            {{end}}
        </div>
    </div>

    <script>
        let autoRefresh = true;
        let refreshInterval;
        const urlParams = new URLSearchParams(window.location.search);
        const deviceFilter = urlParams.get('device');

        document.addEventListener('DOMContentLoaded', function() {
            refreshTerminal();
            startAutoRefresh();
        });

        async function refreshTerminal() {
            try {
                let url = '/api/terminal/logs?limit=200';
                if (deviceFilter) {
                    url += '&device=' + encodeURIComponent(deviceFilter);
                }
                
                const response = await fetch(url);
                const data = await response.json();
                const logs = data.entries || data; // Handle both old and new format
                
                const terminal = document.getElementById('terminal-display');
                terminal.innerHTML = '';
                
                logs.forEach(log => {
                    const entry = document.createElement('div');
                    entry.className = 'log-entry';
                    
                    const timestamp = new Date(log.timestamp).toLocaleTimeString();
                    const typeClass = 'log-' + log.type.toLowerCase();
                    
                    entry.innerHTML = '<span class="log-timestamp">[' + timestamp + ']</span> ' +
                                    '<span class="' + typeClass + '">[' + log.type + ']</span> ' +
                                    '<span>' + log.message + '</span>';
                    
                    terminal.appendChild(entry);
                });
                
                terminal.scrollTop = terminal.scrollHeight;
            } catch (error) {
                console.error('Error refreshing terminal:', error);
            }
        }

        function clearTerminal() {
            document.getElementById('terminal-display').innerHTML = 
                '<div class="log-entry"><span class="log-timestamp">[' + 
                new Date().toLocaleTimeString() + ']</span> Terminal cleared by user</div>';
        }

        function toggleAutoRefresh() {
            autoRefresh = !autoRefresh;
            const btn = event.target;
            const status = document.getElementById('status');
            
            if (autoRefresh) {
                btn.textContent = '⏸️ Pause Auto-Refresh';
                status.textContent = 'Auto-refresh: ON';
                startAutoRefresh();
            } else {
                btn.textContent = '▶️ Resume Auto-Refresh';
                status.textContent = 'Auto-refresh: OFF';
                if (refreshInterval) {
                    clearInterval(refreshInterval);
                }
            }
        }

        function startAutoRefresh() {
            if (refreshInterval) {
                clearInterval(refreshInterval);
            }
            refreshInterval = setInterval(() => {
                if (autoRefresh) {
                    refreshTerminal();
                }
            }, 1000);
        }

        function changeDeviceFilter() {
            const select = document.getElementById('device-filter');
            const newDevice = select.value;
            
            if (newDevice) {
                window.location.search = '?device=' + encodeURIComponent(newDevice);
            } else {
                window.location.search = '';
            }
        }
    </script>
</body>
</html>
`

// Compile templates
var goDemoTemplate = template.Must(template.New("goDemo").Funcs(templateFuncs).Parse(goDemoTemplateHTML))
var protobufInterfaceTemplate = template.Must(template.New("protobufInterface").Funcs(templateFuncs).Parse(protobufInterfaceTemplateHTML))
var terminalViewTemplate = template.Must(template.New("terminalView").Funcs(templateFuncs).Parse(terminalViewTemplateHTML))

var tmpl = template.Must(template.New("home").Funcs(templateFuncs).Parse(`
<!DOCTYPE html>
<html>
<head><title>NgaSim Pool Controller</title></head>
<body>
    <h1>NgaSim Pool Controller v{{.Version}}</h1>
    <p>Simple device list:</p>
    <ul>
    {{range .Devices}}
        <li>{{.Name}} ({{.Serial}}) - {{.Status}}</li>
    {{end}}
    </ul>
</body>
</html>
`))

var goodbyeTemplate = template.Must(template.New("goodbye").Funcs(templateFuncs).Parse(`
<!DOCTYPE html>
<html>
<head><title>NgaSim - Goodbye</title></head>
<body>
    <h1>👋 NgaSim Pool Controller</h1>
    <p>Application is shutting down...</p>
    <p>Thank you for using NgaSim!</p>
</body>
</html>
`))
