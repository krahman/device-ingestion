package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type TelemetryPacket struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Voltage   float64   `json:"voltage"`
	Current   float64   `json:"current"`
	PowerKw   float64   `json:"power_kw"`
}

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

func (p TelemetryPacket) Validate() error {
	deviceID := strings.TrimSpace(p.DeviceID)
	if deviceID == "" {
		return &ValidationError{Field: "device_id", Reason: "is required"}
	}
	if len(deviceID) > 128 {
		return &ValidationError{Field: "device_id", Reason: "must be at most 128 bytes"}
	}
	for _, value := range []struct {
		name  string
		value float64
	}{
		{name: "voltage", value: p.Voltage},
		{name: "current", value: p.Current},
		{name: "power_kw", value: p.PowerKw},
	} {
		if math.IsNaN(value.value) || math.IsInf(value.value, 0) {
			return &ValidationError{Field: value.name, Reason: "must be a finite number"}
		}
		if value.value < 0 {
			return &ValidationError{Field: value.name, Reason: "cannot be negative"}
		}
	}
	return nil
}
