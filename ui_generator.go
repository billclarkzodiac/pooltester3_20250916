package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// UIGenerator generates dynamic web interfaces based on protobuf definitions
type UIGenerator struct {
	registry  *ProtobufCommandRegistry
	logger    *DeviceLogger
	jobEngine *JobEngine
}

// NewUIGenerator creates a new UI generator
func NewUIGenerator(registry *ProtobufCommandRegistry, logger *DeviceLogger, jobEngine *JobEngine) *UIGenerator {
	return &UIGenerator{
		registry:  registry,
		logger:    logger,
		jobEngine: jobEngine,
	}
}

// DeviceDetailPage represents a complete device detail page
type DeviceDetailPage struct {
	Device       *Device                 `json:"device"`
	MessageTypes []string                `json:"message_types"`
	Forms        map[string]*MessageForm `json:"forms"`
	LogEntries   []*DeviceLogEntry       `json:"log_entries"`
	Stats        map[string]interface{}  `json:"stats"`
}

// MessageForm represents a dynamically generated form for a protobuf message
type MessageForm struct {
	MessageType string       `json:"message_type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Fields      []*FormField `json:"fields"`
	Actions     []string     `json:"actions"`
}

// FormField represents a single form field
type FormField struct {
	Name        string                 `json:"name"`
	Label       string                 `json:"label"`
	Type        string                 `json:"type"`
	Required    bool                   `json:"required"`
	Default     interface{}            `json:"default"`
	Options     []FormOption           `json:"options,omitempty"`
	Constraints map[string]interface{} `json:"constraints,omitempty"`
	Description string                 `json:"description"`
	Group       string                 `json:"group,omitempty"`
}

// FormOption represents an option for select/enum fields
type FormOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// GenerateDeviceDetailPage generates a complete device detail page
func (ug *UIGenerator) GenerateDeviceDetailPage(deviceID string) (*DeviceDetailPage, error) {
	// Get device information (this would come from your device registry)
	device := &Device{
		ID:     deviceID,
		Type:   "Unknown",
		Name:   fmt.Sprintf("Device %s", deviceID),
		Status: "ONLINE",
	}

	// Get available message types for this device
	messageTypes := ug.getMessageTypesForDevice(deviceID)

	// Generate forms for each message type
	forms := make(map[string]*MessageForm)
	for _, msgType := range messageTypes {
		if form, err := ug.GenerateMessageForm(msgType); err == nil {
			forms[msgType] = form
		}
	}

	// Get recent log entries for this device
	filter := LogFilter{DeviceID: deviceID}
	logEntries := ug.logger.GetEntries(filter)

	// Get device statistics
	stats := ug.getDeviceStats(deviceID)

	return &DeviceDetailPage{
		Device:       device,
		MessageTypes: messageTypes,
		Forms:        forms,
		LogEntries:   logEntries,
		Stats:        stats,
	}, nil
}

// GenerateMessageForm generates a form for a specific protobuf message type
func (ug *UIGenerator) GenerateMessageForm(messageType string) (*MessageForm, error) {
	form := &MessageForm{
		MessageType: messageType,
		Title:       "Generated Form",
		Description: "Auto-generated form",
		Fields:      make([]*FormField, 0),
		Actions:     []string{"Send", "Clear"},
	}
	// 		return nil, err
	//	}

	//	form := &MessageForm{
	//		MessageType: messageType,
	// 		Title:       ug.generateFormTitle(msgInfo.Name),
	// 		Description: msgInfo.Description,
	// 		Fields:      make([]*FormField, 0),
	// 		Actions:     []string{"Send", "Clear", "Save Template"},
	// 	}
	// //
	// 	// Generate fields for each protobuf field
	// 	for fieldName, fieldInfo := range msgInfo.Fields {
	// 		formField := ug.generateFormField(fieldName, fieldInfo)
	//		form.Fields = append(form.Fields, formField)
	//	}

	// Sort fields by name for consistent ordering
	// Implementation for sorting would go here

	return form, nil
}

// generateFormField creates a form field from protobuf field info
// func (ug *UIGenerator) generateFormField(fieldName string, fieldInfo *interface{}) *FormField {
// 	field := &FormField{
// 		Name:        fieldName,
// 		Label:       fieldInfo.Label,
// 		Required:    fieldInfo.Required,
// 		Description: fieldInfo.Description,
// 		Constraints: make(map[string]interface{}),
// 	}
//
// 	// Set field type and constraints based on protobuf type
// 	switch fieldInfo.Type {
// 	case "bool":
// 		field.Type = "checkbox"
// 		field.Default = false
//
// 	case "int32", "int64":
// 		field.Type = "number"
// 		field.Constraints["step"] = 1
// 		if fieldInfo.Type == "int32" {
// 			field.Constraints["min"] = -2147483648
// 			field.Constraints["max"] = 2147483647
// 		}
//
// 	case "uint32", "uint64":
// 		field.Type = "number"
// 		field.Constraints["step"] = 1
// 		field.Constraints["min"] = 0
// 		if fieldInfo.Type == "uint32" {
// 			field.Constraints["max"] = 4294967295
// 		}
//
// 	case "float", "double":
// 		field.Type = "number"
// 		field.Constraints["step"] = 0.01
//
// 	case "string":
// 		field.Type = "text"
// 		field.Constraints["maxlength"] = 255
//
// 	case "bytes":
// 		field.Type = "file"
// 		field.Constraints["accept"] = "application/octet-stream"
//
// 	case "enum":
// 		field.Type = "select"
// 		field.Options = make([]FormOption, 0)
// 		for name, value := range fieldInfo.Enum {
// 			field.Options = append(field.Options, FormOption{
// 				Value: strconv.Itoa(int(value)),
// 				Label: ug.generateEnumLabel(name),
// 			})
// 		}
//
// 	case "message":
// 		field.Type = "object"
// 		// For nested messages, we could generate nested forms
// 		field.Constraints["message_type"] = fieldInfo.Constraints["message_type"]
//
// 	default:
// 		field.Type = "text"
// 	}
//
// 	// Handle repeated fields
// 	if fieldInfo.Repeated {
// 		field.Type = "array"
// 		field.Constraints["item_type"] = field.Type
// 	}
//
// 	return field
// }

// generateFormTitle creates a human-readable title from a message name
func (ug *UIGenerator) generateFormTitle(messageName string) string {
	// Remove common suffixes
	title := strings.TrimSuffix(messageName, "Request")
	title = strings.TrimSuffix(title, "Response")
	title = strings.TrimSuffix(title, "Payload")

	// Convert camelCase to Title Case
	result := ""
	for i, r := range title {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result += " "
		}
		result += string(r)
	}

	return result
}

// generateEnumLabel creates a human-readable label from an enum value name
func (ug *UIGenerator) generateEnumLabel(enumName string) string {
	// Convert UPPER_CASE to Title Case
	parts := strings.Split(enumName, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

// getMessageTypesForDevice returns available message types for a device
func (ug *UIGenerator) getMessageTypesForDevice(deviceID string) []string {
	// This would typically be based on device type/capabilities
	// For now, return all available message types
	// return []string{} // 	// 	return ug.registry.ListAvailableMessages()
	return []string{"SetSanitizerTargetPercentageRequestPayload", "GetSanitizerStatusRequestPayload"}
}

// getDeviceStats returns statistics for a specific device
func (ug *UIGenerator) getDeviceStats(deviceID string) map[string]interface{} {
	filter := LogFilter{DeviceID: deviceID}
	entries := ug.logger.GetEntries(filter)

	stats := map[string]interface{}{
		"total_messages": len(entries),
		"last_activity":  "",
		"success_rate":   0.0,
		"message_types":  make(map[string]int),
		"error_count":    0,
	}

	if len(entries) == 0 {
		return stats
	}

	successCount := 0
	for _, entry := range entries {
		if entry.Success {
			successCount++
		} else {
			stats["error_count"] = stats["error_count"].(int) + 1
		}

		// Count message types
		messageTypes := stats["message_types"].(map[string]int)
		messageTypes[entry.MessageType]++
	}

	// Calculate success rate
	stats["success_rate"] = float64(successCount) / float64(len(entries)) * 100

	// Get last activity time
	if len(entries) > 0 {
		stats["last_activity"] = entries[len(entries)-1].Timestamp.Format("2006-01-02 15:04:05")
	}

	return stats
}

// HandleDeviceDetailRequest handles HTTP requests for device detail pages
func (ug *UIGenerator) HandleDeviceDetailRequest(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		http.Error(w, "device_id parameter required", http.StatusBadRequest)
		return
	}

	pageData, err := ug.GenerateDeviceDetailPage(deviceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error generating device page: %v", err), http.StatusInternalServerError)
		return
	}

	// Render HTML template
	tmpl := ug.createDeviceDetailTemplate()
	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, pageData); err != nil {
		http.Error(w, fmt.Sprintf("Error rendering template: %v", err), http.StatusInternalServerError)
	}
}

// createDeviceDetailTemplate creates the HTML template for device detail pages
func (ug *UIGenerator) createDeviceDetailTemplate() *template.Template {
	templateHTML := `
<!DOCTYPE html>
<html>
<head>
    <title>{{.Device.Name}} - Device Details</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .device-header { background: #f0f0f0; padding: 20px; border-radius: 8px; margin-bottom: 20px; }
        .status-online { color: green; }
        .status-offline { color: red; }
        .tabs { border-bottom: 1px solid #ccc; margin-bottom: 20px; }
        .tab { display: inline-block; padding: 10px 20px; cursor: pointer; border-bottom: 2px solid transparent; }
        .tab.active { border-bottom-color: #007cba; background: #f9f9f9; }
        .tab-content { display: none; }
        .tab-content.active { display: block; }
        .form-group { margin-bottom: 15px; }
        .form-label { display: block; font-weight: bold; margin-bottom: 5px; }
        .form-input { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        .form-select { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        .form-checkbox { margin-right: 8px; }
        .button { background: #007cba; color: white; padding: 10px 20px; border: none; border-radius: 4px; cursor: pointer; }
        .button:hover { background: #005a87; }
        .log-entry { border: 1px solid #ddd; margin-bottom: 10px; padding: 10px; border-radius: 4px; }
        .log-success { border-left: 4px solid green; }
        .log-error { border-left: 4px solid red; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 15px; }
        .stat-card { background: #f9f9f9; padding: 15px; border-radius: 8px; text-align: center; }
    </style>
</head>
<body>
    <div class="device-header">
        <h1>{{.Device.Name}}</h1>
        <p><strong>Device ID:</strong> {{.Device.ID}}</p>
        <p><strong>Type:</strong> {{.Device.Type}}</p>
        <p><strong>Status:</strong> <span class="status-{{.Device.Status | lower}}">{{.Device.Status}}</span></p>
    </div>

    <div class="tabs">
        <div class="tab active" onclick="showTab('commands')">Commands</div>
        <div class="tab" onclick="showTab('logs')">Communication Log</div>
        <div class="tab" onclick="showTab('stats')">Statistics</div>
    </div>

    <div id="commands" class="tab-content active">
        {{range $msgType, $form := .Forms}}
        <div class="message-form">
            <h3>{{$form.Title}}</h3>
            {{if $form.Description}}<p>{{$form.Description}}</p>{{end}}
            
            <form id="form-{{$msgType}}">
                {{range $form.Fields}}
                <div class="form-group">
                    <label class="form-label">{{.Label}}{{if .Required}} *{{end}}</label>
                    {{if eq .Type "text"}}
                        <input type="text" name="{{.Name}}" class="form-input" {{if .Required}}required{{end}} {{if .Default}}value="{{.Default}}"{{end}}>
                    {{else if eq .Type "number"}}
                        <input type="number" name="{{.Name}}" class="form-input" {{if .Required}}required{{end}} {{if .Default}}value="{{.Default}}"{{end}}>
                    {{else if eq .Type "checkbox"}}
                        <input type="checkbox" name="{{.Name}}" class="form-checkbox" {{if .Default}}checked{{end}}>
                    {{else if eq .Type "select"}}
                        <select name="{{.Name}}" class="form-select" {{if .Required}}required{{end}}>
                            {{range .Options}}
                            <option value="{{.Value}}">{{.Label}}</option>
                            {{end}}
                        </select>
                    {{end}}
                    {{if .Description}}<small>{{.Description}}</small>{{end}}
                </div>
                {{end}}
                
                <button type="button" class="button" onclick="sendCommand('{{$msgType}}')">Send Command</button>
                <button type="button" class="button" onclick="clearForm('{{$msgType}}')">Clear</button>
            </form>
        </div>
        <hr>
        {{end}}
    </div>

    <div id="logs" class="tab-content">
        <h3>Communication Log</h3>
        {{range .LogEntries}}
        <div class="log-entry {{if .Success}}log-success{{else}}log-error{{end}}">
            <strong>{{.Timestamp.Format "15:04:05"}}</strong> 
            {{.Direction}} {{.MessageType}}
            {{if not .Success}} - Error: {{.Error}}{{end}}
        </div>
        {{end}}
    </div>

    <div id="stats" class="tab-content">
        <h3>Device Statistics</h3>
        <div class="stats-grid">
            <div class="stat-card">
                <h4>Total Messages</h4>
                <p>{{.Stats.total_messages}}</p>
            </div>
            <div class="stat-card">
                <h4>Success Rate</h4>
                <p>{{printf "%.1f" .Stats.success_rate}}%</p>
            </div>
            <div class="stat-card">
                <h4>Error Count</h4>
                <p>{{.Stats.error_count}}</p>
            </div>
            <div class="stat-card">
                <h4>Last Activity</h4>
                <p>{{.Stats.last_activity}}</p>
            </div>
        </div>
    </div>

    <script>
        function showTab(tabName) {
            // Hide all tab contents
            var contents = document.querySelectorAll('.tab-content');
            contents.forEach(function(content) {
                content.classList.remove('active');
            });
            
            // Remove active class from all tabs
            var tabs = document.querySelectorAll('.tab');
            tabs.forEach(function(tab) {
                tab.classList.remove('active');
            });
            
            // Show selected tab content
            document.getElementById(tabName).classList.add('active');
            
            // Mark selected tab as active
            event.target.classList.add('active');
        }

        function sendCommand(messageType) {
            var form = document.getElementById('form-' + messageType);
            var formData = new FormData(form);
            var data = {};
            
            for (var pair of formData.entries()) {
                data[pair[0]] = pair[1];
            }
            
            fetch('/api/send-command', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    device_id: '{{.Device.ID}}',
                    message_type: messageType,
                    parameters: data
                })
            })
            .then(response => response.json())
            .then(data => {
                alert('Command sent: ' + JSON.stringify(data));
                location.reload(); // Refresh to show new log entries
            })
            .catch(error => {
                alert('Error: ' + error);
            });
        }

        function clearForm(messageType) {
            document.getElementById('form-' + messageType).reset();
        }
    </script>
</body>
</html>
`

	// Add custom template functions
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
	}

	return template.Must(template.New("device-detail").Funcs(funcMap).Parse(templateHTML))
}
