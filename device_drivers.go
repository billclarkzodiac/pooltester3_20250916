package main

import (
    "fmt"
)

// DeviceDriver interface - Like your C function pointers but type-safe
type DeviceDriver interface {
    ParseMessage(data []byte, msgType string) (*ParsedProtobuf, error)
    HandleCommand(cmd interface{}) error
    GetDeviceType() string
    GetMessageTypes() []string
}

// Factory function - Like your New() functions
func NewDeviceDriver(deviceType string) DeviceDriver {
    switch deviceType {
    case "sanitizerGen2":
        return &SanitizerDriver{}
    case "lights", "digitalControllerTransformer":
        return &LightsDriver{}
    default:
        return &GenericDriver{deviceType: deviceType}
    }
}

// SanitizerDriver implementation (simplified for now)
type SanitizerDriver struct{}

func (d *SanitizerDriver) ParseMessage(data []byte, msgType string) (*ParsedProtobuf, error) {
    // Use the real parser from protobuf_parser.go
    return ParseProtobufMessage(data, msgType, "sanitizerGen2")
}

func (d *SanitizerDriver) HandleCommand(cmd interface{}) error {
    return fmt.Errorf("sanitizer commands not implemented yet")
}

func (d *SanitizerDriver) GetDeviceType() string {
    return "sanitizerGen2"
}

func (d *SanitizerDriver) GetMessageTypes() []string {
    return []string{
        "SetSanitizerTargetPercentageRequestPayload",
        "GetSanitizerStatusRequestPayload",
    }
}

// LightsDriver implementation (simplified for now)
type LightsDriver struct{}

func (d *LightsDriver) ParseMessage(data []byte, msgType string) (*ParsedProtobuf, error) {
    // Use the real parser from protobuf_parser.go
    return ParseProtobufMessage(data, msgType, "lights")
}

func (d *LightsDriver) HandleCommand(cmd interface{}) error {
    return fmt.Errorf("lights commands not implemented yet")
}

func (d *LightsDriver) GetDeviceType() string {
    return "lights"
}

func (d *LightsDriver) GetMessageTypes() []string {
    return []string{"LightsCommand"}
}

// Generic fallback driver
type GenericDriver struct {
    deviceType string
}

func (d *GenericDriver) ParseMessage(data []byte, msgType string) (*ParsedProtobuf, error) {
    // Use the real parser from protobuf_parser.go
    return ParseProtobufMessage(data, msgType, d.deviceType)
}

func (d *GenericDriver) HandleCommand(cmd interface{}) error {
    return fmt.Errorf("generic driver does not support commands")
}

func (d *GenericDriver) GetDeviceType() string {
    return d.deviceType
}

func (d *GenericDriver) GetMessageTypes() []string {
    return []string{"Generic"}
}
