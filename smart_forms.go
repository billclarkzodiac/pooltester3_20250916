package main

import (
	"fmt"
	"strings"
	"time"
)

// SmartFormGenerator creates intuitive forms instead of raw protobuf forms
type SmartFormGenerator struct {
	popupGenerator *PopupUIGenerator
}

// UserFieldConfig defines how to make a field user-friendly
type UserFieldConfig struct {
	DisplayName  string      `json:"display_name"`
	HelpText     string      `json:"help_text"`
	ShowToUser   bool        `json:"show_to_user"`
	AutoGenerate bool        `json:"auto_generate"`
	DefaultValue interface{} `json:"default_value"`
	ControlType  string      `json:"control_type"` // slider, switch, dropdown, hidden
	ChoiceList   []string    `json:"choice_list"`  // For dropdown controls
	MinValue     float64     `json:"min_value"`
	MaxValue     float64     `json:"max_value"`
	StepSize     float64     `json:"step_size"`
	DisplayUnit  string      `json:"display_unit"` // %, °C, V, etc.
}

// NewSmartFormGenerator creates a new user-friendly form generator
func NewSmartFormGenerator(popupGenerator *PopupUIGenerator) *SmartFormGenerator {
	return &SmartFormGenerator{
		popupGenerator: popupGenerator,
	}
}

// GenerateUserFriendlyForm creates an intuitive form for a protobuf command
func (sfg *SmartFormGenerator) GenerateUserFriendlyForm(messageType, deviceSerial, deviceName string) string {
	// Get the raw protobuf form
	messages := sfg.popupGenerator.reflectionEngine.GetAllMessages()
	msgDesc, exists := messages[messageType]
	if !exists {
		return "<p>❌ Command not available</p>"
	}

	// Create user-friendly version
	var html strings.Builder

	// Intuitive header
	html.WriteString(fmt.Sprintf(`
<div class="popup-header smart-design">
    <h3>🎛️ %s</h3>
    <p>Device: <strong>%s</strong> (%s)</p>
    <button class="close-btn" onclick="closePopup()">&times;</button>
</div>
<div class="popup-body smart-design">
    <form id="smart-command-form">
        <input type="hidden" name="message_type" value="%s">
        <input type="hidden" name="device_serial" value="%s">
        <input type="hidden" name="category" value="sanitizerGen2">
`, sfg.getSimpleCommandName(messageType), deviceName, deviceSerial, messageType, deviceSerial))

	// Process each field with user-friendly configuration
	for _, field := range msgDesc.Fields {
		config := sfg.getUserFieldConfig(messageType, field.Name, field.Type)

		if config.ShowToUser {
			html.WriteString(sfg.generateUserFieldHTML(field, config, deviceSerial))
		} else if config.AutoGenerate {
			// Hidden auto-generated fields
			html.WriteString(fmt.Sprintf(`
        <input type="hidden" name="%s" value="%v">
`, field.Name, config.DefaultValue))
		}
	}

	// User-friendly footer
	html.WriteString(`
    </form>
    <div class="command-preview-area" id="command-preview-area" style="display: none;">
        <h4>📋 Command Details:</h4>
        <div class="smart-preview" id="smart-preview"></div>
    </div>
</div>
<div class="popup-footer smart-design">
    <button type="button" class="btn btn-secondary" onclick="closePopup()">❌ Cancel</button>
    <button type="button" class="btn btn-info" onclick="previewSmartCommand()">👁️ Preview</button>
    <button type="button" class="btn btn-success btn-large" onclick="executeSmartCommand()">🚀 Send Command</button>
</div>
`)

	return html.String()
}

// getSimpleCommandName converts technical message names to simple names
func (sfg *SmartFormGenerator) getSimpleCommandName(messageType string) string {
	switch {
	case strings.Contains(messageType, "SetSanitizerTargetPercentage"):
		return "Set Chlorine Output Level"
	case strings.Contains(messageType, "GetSanitizerStatus"):
		return "Get Current Status"
	case strings.Contains(messageType, "GetSanitizerConfiguration"):
		return "Get Device Settings"
	case strings.Contains(messageType, "SetSanitizerConfiguration"):
		return "Update Device Settings"
	case strings.Contains(messageType, "GetSanitizerActiveErrors"):
		return "Check for Errors"
	case strings.Contains(messageType, "GetSanitizerDeviceInformation"):
		return "Get Device Information"
	default:
		// Remove technical suffixes and make it readable
		name := strings.ReplaceAll(messageType, "RequestPayload", "")
		name = strings.ReplaceAll(name, "Sanitizer", "")
		name = strings.ReplaceAll(name, "Get", "Get ")
		name = strings.ReplaceAll(name, "Set", "Set ")
		return name
	}
}

// getUserFieldConfig returns user-friendly configuration for a field
func (sfg *SmartFormGenerator) getUserFieldConfig(messageType, fieldName, fieldType string) UserFieldConfig {
	// Default configuration
	config := UserFieldConfig{
		DisplayName:  fieldName,
		HelpText:     "",
		ShowToUser:   true,
		AutoGenerate: false,
		ControlType:  "input",
	}

	// Field-specific configurations
	switch strings.ToLower(fieldName) {
	case "percentage", "targetpercentage", "target_percentage":
		config.DisplayName = "Chlorine Output Level"
		config.HelpText = "Set the chlorine generator output (0% = Off, 100% = Maximum)"
		config.ControlType = "slider"
		config.MinValue = 0
		config.MaxValue = 100
		config.StepSize = 5
		config.DisplayUnit = "%"

	case "uuid", "id", "device_id", "correlation_id":
		config.ShowToUser = false
		config.AutoGenerate = true
		config.DefaultValue = fmt.Sprintf("cmd_%d", time.Now().UnixNano())

	case "timestamp", "time":
		config.ShowToUser = false
		config.AutoGenerate = true
		config.DefaultValue = time.Now().Unix()

	case "enable", "enabled", "active":
		config.DisplayName = "Enable Device"
		config.HelpText = "Turn the device on or off"
		config.ControlType = "switch"

	case "temperature", "temp":
		config.DisplayName = "Temperature"
		config.HelpText = "Temperature setting"
		config.DisplayUnit = "°C"
		config.MinValue = 10
		config.MaxValue = 40

	case "voltage":
		config.DisplayName = "Voltage"
		config.HelpText = "Operating voltage"
		config.DisplayUnit = "V"
		config.ShowToUser = false // Usually not user-settable

	case "mode", "operation_mode":
		config.DisplayName = "Operation Mode"
		config.HelpText = "Select how the device should operate"
		config.ControlType = "dropdown"
		config.ChoiceList = []string{"Auto", "Manual", "Off"}

	default:
		// Make any remaining technical field names more user-friendly
		config.DisplayName = strings.ReplaceAll(fieldName, "_", " ")
		config.DisplayName = strings.Title(config.DisplayName)
	}

	return config
}

// generateUserFieldHTML creates HTML for a user-friendly field
func (sfg *SmartFormGenerator) generateUserFieldHTML(field FieldDescriptor, config UserFieldConfig, deviceSerial string) string {
	var html strings.Builder

	html.WriteString(fmt.Sprintf(`
<div class="smart-form-group">
    <label for="field_%s" class="smart-label">%s</label>
`, field.Name, config.DisplayName))

	if config.HelpText != "" {
		html.WriteString(fmt.Sprintf(`
    <div class="smart-help">%s</div>
`, config.HelpText))
	}

	switch config.ControlType {
	case "slider":
		defaultVal := config.MinValue
		if field.DefaultValue != nil {
			if val, ok := field.DefaultValue.(float64); ok {
				defaultVal = val
			}
		}

		html.WriteString(fmt.Sprintf(`
    <div class="slider-control">
        <input type="range" 
               name="%s" 
               id="field_%s" 
               class="smart-slider" 
               min="%g" 
               max="%g" 
               step="%g" 
               value="%g"
               oninput="updateSliderDisplay('%s', this.value, '%s')">
        <div class="slider-display">
            <span id="display_%s">%g</span>%s
        </div>
    </div>
`, field.Name, field.Name, config.MinValue, config.MaxValue, config.StepSize, defaultVal, field.Name, config.DisplayUnit, field.Name, defaultVal, config.DisplayUnit))

	case "switch":
		html.WriteString(fmt.Sprintf(`
    <div class="switch-control">
        <label class="smart-switch">
            <input type="checkbox" name="%s" id="field_%s" class="switch-input">
            <span class="switch-slider"></span>
        </label>
        <span class="switch-text">Enable</span>
    </div>
`, field.Name, field.Name))

	case "dropdown":
		html.WriteString(fmt.Sprintf(`
    <select name="%s" id="field_%s" class="smart-dropdown">
`, field.Name, field.Name))

		for _, option := range config.ChoiceList {
			html.WriteString(fmt.Sprintf(`
        <option value="%s">%s</option>`, option, option))
		}

		html.WriteString(`
    </select>`)

	default: // Regular input
		inputType := "text"
		if field.Type == "int32" || field.Type == "float" {
			inputType = "number"
		}

		html.WriteString(fmt.Sprintf(`
    <input type="%s" 
           name="%s" 
           id="field_%s" 
           class="smart-input" 
           placeholder="Enter %s">
`, inputType, field.Name, field.Name, config.DisplayName))
	}

	html.WriteString(`</div>`)

	return html.String()
}
