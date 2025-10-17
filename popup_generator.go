package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PopupUIGenerator creates dynamic UI pop-ups based on protobuf reflection
type PopupUIGenerator struct {
	reflectionEngine *ProtobufReflectionEngine
	terminalLogger   *TerminalLogger
	ngaSim           *NgaSim
}

// PopupRequest represents a request to show a protobuf command popup
type PopupRequest struct {
	MessageType  string `json:"message_type"`
	DeviceSerial string `json:"device_serial"`
	Category     string `json:"category"`
}

// PopupResponse represents the generated popup HTML and metadata
type PopupResponse struct {
	HTML        string            `json:"html"`
	MessageType string            `json:"message_type"`
	Fields      []FieldDescriptor `json:"fields"`
	Title       string            `json:"title"`
}

// CommandExecutionRequest represents a command execution from popup
type CommandExecutionRequest struct {
	MessageType  string                 `json:"message_type"`
	DeviceSerial string                 `json:"device_serial"`
	Category     string                 `json:"category"`
	FieldValues  map[string]interface{} `json:"field_values"`
}

// CommandExecutionResponse represents the result of command execution
type CommandExecutionResponse struct {
	Success       bool        `json:"success"`
	Error         string      `json:"error,omitempty"`
	CorrelationID string      `json:"correlation_id"`
	Timestamp     time.Time   `json:"timestamp"`
	MessageSent   interface{} `json:"message_sent,omitempty"`
}

// NewPopupUIGenerator creates a new popup UI generator
func NewPopupUIGenerator(reflectionEngine *ProtobufReflectionEngine, terminalLogger *TerminalLogger, ngaSim *NgaSim) *PopupUIGenerator {
	return &PopupUIGenerator{
		reflectionEngine: reflectionEngine,
		terminalLogger:   terminalLogger,
		ngaSim:           ngaSim,
	}
}

// GeneratePopupHTML generates HTML for a protobuf message popup
func (pug *PopupUIGenerator) GeneratePopupHTML(messageType, deviceSerial, category string) (*PopupResponse, error) {
	// Get message descriptor
	messages := pug.reflectionEngine.GetAllMessages()
	msgDesc, exists := messages[messageType]
	if !exists {
		return nil, fmt.Errorf("message type not found: %s", messageType)
	}

	// Generate HTML form
	html := pug.generateFormHTML(msgDesc, deviceSerial, category)

	response := &PopupResponse{
		HTML:        html,
		MessageType: messageType,
		Fields:      msgDesc.Fields,
		Title:       fmt.Sprintf("%s - %s", msgDesc.Name, deviceSerial),
	}

	return response, nil
}

// generateFormHTML creates the HTML form for a protobuf message
func (pug *PopupUIGenerator) generateFormHTML(msgDesc MessageDescriptor, deviceSerial, category string) string {
	var html strings.Builder

	// Popup header
	html.WriteString(fmt.Sprintf(`
<div class="popup-header">
    <h3>%s</h3>
    <p>Device: <strong>%s</strong> | Category: <strong>%s</strong></p>
    <button class="close-btn" onclick="closePopup()">&times;</button>
</div>
<div class="popup-body">
    <form id="protobuf-command-form">
        <input type="hidden" name="message_type" value="%s">
        <input type="hidden" name="device_serial" value="%s">
        <input type="hidden" name="category" value="%s">
`, msgDesc.Name, deviceSerial, category, msgDesc.Package+"."+msgDesc.Name, deviceSerial, category))

	// Generate form fields
	for _, field := range msgDesc.Fields {
		html.WriteString(pug.generateFieldHTML(field))
	}

	// Popup footer with buttons
	html.WriteString(`
    </form>
</div>
<div class="popup-footer">
    <button type="button" class="btn-secondary" onclick="closePopup()">Cancel</button>
    <button type="button" class="btn-primary" onclick="executeProtobufCommand()">Send Command</button>
    <button type="button" class="btn-info" onclick="previewCommand()">Preview Message</button>
</div>
<div id="command-preview" class="command-preview" style="display: none;">
    <h4>Message Preview:</h4>
    <pre id="preview-content"></pre>
</div>
`)

	return html.String()
}

// generateFieldHTML creates HTML input for a specific field
func (pug *PopupUIGenerator) generateFieldHTML(field FieldDescriptor) string {
	var html strings.Builder

	html.WriteString(fmt.Sprintf(`
<div class="form-group">
    <label for="field_%s">%s</label>
    <div class="field-description">%s</div>
`, field.Name, field.Label, field.Description))

	switch field.Type {
	case "boolean":
		html.WriteString(fmt.Sprintf(`
    <select name="%s" id="field_%s" class="form-control">
        <option value="false">False</option>
        <option value="true">True</option>
    </select>`, field.Name, field.Name))

	case "int32", "int64", "uint32", "uint64":
		min := ""
		max := ""
		if field.Min != nil {
			min = fmt.Sprintf(`min="%v"`, field.Min)
		}
		if field.Max != nil {
			max = fmt.Sprintf(`max="%v"`, field.Max)
		}
		defaultVal := ""
		if field.DefaultValue != nil {
			defaultVal = fmt.Sprintf(`value="%v"`, field.DefaultValue)
		}

		html.WriteString(fmt.Sprintf(`
    <input type="number" name="%s" id="field_%s" class="form-control" %s %s %s>
`, field.Name, field.Name, min, max, defaultVal))

	case "float", "double":
		defaultVal := ""
		if field.DefaultValue != nil {
			defaultVal = fmt.Sprintf(`value="%v"`, field.DefaultValue)
		}

		html.WriteString(fmt.Sprintf(`
    <input type="number" name="%s" id="field_%s" class="form-control" step="0.01" %s>
`, field.Name, field.Name, defaultVal))

	case "string":
		defaultVal := ""
		if field.DefaultValue != nil {
			defaultVal = fmt.Sprintf(`value="%v"`, field.DefaultValue)
		}

		html.WriteString(fmt.Sprintf(`
    <input type="text" name="%s" id="field_%s" class="form-control" %s>
`, field.Name, field.Name, defaultVal))

	case "enum":
		html.WriteString(fmt.Sprintf(`
    <select name="%s" id="field_%s" class="form-control">`, field.Name, field.Name))

		for _, enumValue := range field.EnumValues {
			selected := ""
			if field.DefaultValue != nil && fmt.Sprintf("%v", field.DefaultValue) == enumValue {
				selected = "selected"
			}
			html.WriteString(fmt.Sprintf(`
        <option value="%s" %s>%s</option>`, enumValue, selected, enumValue))
		}

		html.WriteString(`
    </select>`)

	case "bytes":
		html.WriteString(fmt.Sprintf(`
    <textarea name="%s" id="field_%s" class="form-control" rows="3" placeholder="Enter hex data (e.g., 0x1A2B3C or 1A2B3C)"></textarea>
`, field.Name, field.Name))

	default:
		// Default to text input for unknown types
		html.WriteString(fmt.Sprintf(`
    <input type="text" name="%s" id="field_%s" class="form-control" placeholder="%s">
`, field.Name, field.Name, field.Type))
	}

	// Add required indicator
	if field.Required {
		html.WriteString(`<small class="required-indicator">* Required</small>`)
	}

	html.WriteString(`</div>`)

	return html.String()
}

// ExecuteProtobufCommand executes a protobuf command from popup form data
func (pug *PopupUIGenerator) ExecuteProtobufCommand(req CommandExecutionRequest) (*CommandExecutionResponse, error) {
	log.Printf("🚀 Executing protobuf command: %s for device %s", req.MessageType, req.DeviceSerial)

	// Create the protobuf message
	msg, err := pug.reflectionEngine.CreateMessage(req.MessageType)
	if err != nil {
		return &CommandExecutionResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to create message: %v", err),
		}, err
	}

	// Populate the message with form values
	if err := pug.reflectionEngine.PopulateMessage(msg, req.FieldValues); err != nil {
		return &CommandExecutionResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to populate message: %v", err),
		}, err
	}

	// Serialize the message
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return &CommandExecutionResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to serialize message: %v", err),
		}, err
	}

	// Generate correlation ID
	correlationID := fmt.Sprintf("popup_%d", time.Now().UnixNano())

	// Log to terminal
	pug.terminalLogger.LogProtobufMessage("REQUEST", req.DeviceSerial, "OUTGOING", msg, msgBytes)

	// Send via MQTT if connected, otherwise simulate
	var sendErr error
	if pug.ngaSim.mqtt != nil && pug.ngaSim.mqtt.IsConnected() {
		sendErr = pug.sendMQTTCommand(req.DeviceSerial, req.Category, req.MessageType, msgBytes, correlationID)
	} else {
		// Demo mode - simulate response
		go pug.simulateResponse(req.DeviceSerial, req.MessageType, correlationID)
	}

	response := &CommandExecutionResponse{
		Success:       sendErr == nil,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		MessageSent:   msg,
	}

	if sendErr != nil {
		response.Error = sendErr.Error()
		pug.terminalLogger.LogError(req.DeviceSerial, "Command execution failed", sendErr)
	}

	return response, sendErr
}

// sendMQTTCommand sends a protobuf command via MQTT
func (pug *PopupUIGenerator) sendMQTTCommand(deviceSerial, category, messageType string, msgBytes []byte, correlationID string) error {
	// Construct MQTT topic
	topic := fmt.Sprintf("async/%s/%s/cmd", category, deviceSerial)

	// Send via MQTT
	token := pug.ngaSim.mqtt.Publish(topic, 1, false, msgBytes)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT publish failed: %v", token.Error())
	}

	log.Printf("✅ MQTT command sent: %s -> %s (correlation: %s)", deviceSerial, messageType, correlationID)
	return nil
}

// simulateResponse simulates a device response in demo mode
func (pug *PopupUIGenerator) simulateResponse(deviceSerial, messageType, correlationID string) {
	// Simulate processing delay
	time.Sleep(1 * time.Second)

	// Create simulated response
	responseData := map[string]interface{}{
		"correlation_id": correlationID,
		"success":        true,
		"timestamp":      time.Now().Unix(),
		"device_serial":  deviceSerial,
		"message_type":   messageType,
		"result":         "Command executed successfully (simulated)",
	}

	// Log simulated response
	pug.terminalLogger.LogProtobufMessage("RESPONSE", deviceSerial, "INCOMING", responseData, nil)

	log.Printf("🎭 Simulated response for %s: %s", deviceSerial, correlationID)
}

// HTTP Handlers for Popup UI

// handleProtobufPopup handles requests for protobuf command popups
func (pug *PopupUIGenerator) handleProtobufPopup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PopupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	response, err := pug.GeneratePopupHTML(req.MessageType, req.DeviceSerial, req.Category)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate popup: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(response)
}

// handleProtobufCommand handles protobuf command execution from popups
func (pug *PopupUIGenerator) handleProtobufCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CommandExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	response, err := pug.ExecuteProtobufCommand(req)
	if err != nil {
		log.Printf("Command execution error: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(response)
}

// handleMessageTypes returns all available protobuf message types
func (pug *PopupUIGenerator) handleMessageTypes(w http.ResponseWriter, r *http.Request) {
	messages := pug.reflectionEngine.GetAllMessages()

	// Filter for request messages only
	requestMessages := make(map[string]MessageDescriptor)
	for name, desc := range messages {
		if desc.IsRequest {
			requestMessages[name] = desc
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(requestMessages)
}

// handleTerminalLogs returns recent terminal log entries
func (pug *PopupUIGenerator) handleTerminalLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100 // default

	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil {
			limit = parsed
		}
	}

	entries := pug.terminalLogger.GetRecentEntries(limit)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(entries)
}
