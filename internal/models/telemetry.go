package models

import "time"

type TelemetryPacket struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Voltage   float64   `json:"voltage"`
	Current   float64   `json:"current"`
	PowerKw   float64   `json:"power_kw"`
}
