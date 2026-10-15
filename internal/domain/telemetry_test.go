package domain

import (
	"math"
	"testing"
)

func TestTelemetryPacketValidate(t *testing.T) {
	valid := TelemetryPacket{DeviceID: "device-1", IdempotencyKey: "key-1", Voltage: 230, Current: 10, PowerKw: 2.3}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid packet: %v", err)
	}
	cases := map[string]func(*TelemetryPacket){
		"missing device":   func(p *TelemetryPacket) { p.DeviceID = " " },
		"long device":      func(p *TelemetryPacket) { p.DeviceID = string(make([]byte, 129)) },
		"missing key":      func(p *TelemetryPacket) { p.IdempotencyKey = " " },
		"long key":         func(p *TelemetryPacket) { p.IdempotencyKey = string(make([]byte, 256)) },
		"nan voltage":      func(p *TelemetryPacket) { p.Voltage = math.NaN() },
		"infinite current": func(p *TelemetryPacket) { p.Current = math.Inf(1) },
		"negative power":   func(p *TelemetryPacket) { p.PowerKw = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			packet := valid
			mutate(&packet)
			if err := packet.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestTelemetryErrorMessages(t *testing.T) {
	if (&ValidationError{Field: "voltage", Reason: "must be finite"}).Error() == "" {
		t.Fatal("empty validation error")
	}
	if (&IdempotencyConflictError{Key: "key"}).Error() == "" {
		t.Fatal("empty conflict error")
	}
}
