package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const ChargingEventSchemaVersion = 1

const (
	ChargingEventBoot      = "station.boot"
	ChargingEventHeartbeat = "station.heartbeat"
	ChargingEventStatus    = "connector.status"
	ChargingEventMeter     = "meter.values"
)

// ChargingEvent is the protocol-neutral event persisted through Kafka.
type ChargingEvent struct {
	EventID         string          `json:"event_id"`
	SchemaVersion   int             `json:"schema_version"`
	EventType       string          `json:"event_type"`
	SourceProtocol  string          `json:"source_protocol"`
	ProtocolVersion string          `json:"protocol_version"`
	CPOCountryCode  string          `json:"cpo_country_code"`
	CPOPartyID      string          `json:"cpo_party_id"`
	StationID       string          `json:"station_id"`
	ConnectorID     int             `json:"connector_id,omitempty"`
	OccurredAt      time.Time       `json:"occurred_at"`
	ReceivedAt      time.Time       `json:"received_at"`
	IdempotencyKey  string          `json:"idempotency_key"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

type StationBoot struct {
	Vendor          string `json:"vendor"`
	Model           string `json:"model"`
	SerialNumber    string `json:"serial_number,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
}

type ConnectorStatus struct {
	Status          string    `json:"status"`
	ErrorCode       string    `json:"error_code"`
	Info            string    `json:"info,omitempty"`
	VendorID        string    `json:"vendor_id,omitempty"`
	VendorErrorCode string    `json:"vendor_error_code,omitempty"`
	StatusAt        time.Time `json:"status_at"`
}

type MeterSample struct {
	SampledAt time.Time `json:"sampled_at"`
	Value     string    `json:"value"`
	Measurand string    `json:"measurand"`
	Unit      string    `json:"unit"`
	Context   string    `json:"context"`
	Format    string    `json:"format"`
	Phase     string    `json:"phase,omitempty"`
	Location  string    `json:"location"`
}

type ChargingEventConflictError struct {
	Key string
}

type ChargingConnector struct {
	ConnectorID      int    `json:"connector_id"`
	EVSEUID          string `json:"evse_uid"`
	Standard         string `json:"standard"`
	Format           string `json:"format"`
	PowerType        string `json:"power_type"`
	MaxVoltage       int    `json:"max_voltage"`
	MaxAmperage      int    `json:"max_amperage"`
	MaxElectricPower int    `json:"max_electric_power"`
}

type ChargingStation struct {
	ID         string              `json:"id"`
	LocationID string              `json:"location_id"`
	Name       string              `json:"name"`
	Address    string              `json:"address"`
	City       string              `json:"city"`
	PostalCode string              `json:"postal_code"`
	Country    string              `json:"country"`
	Latitude   float64             `json:"latitude"`
	Longitude  float64             `json:"longitude"`
	TimeZone   string              `json:"time_zone"`
	Connectors []ChargingConnector `json:"connectors"`
}

func (e *ChargingEventConflictError) Error() string {
	return "charging event idempotency key was already used with different data"
}

func (e ChargingEvent) Validate() error {
	if strings.TrimSpace(e.EventID) == "" {
		return &ValidationError{Field: "event_id", Reason: "is required"}
	}
	if e.SchemaVersion != ChargingEventSchemaVersion {
		return &ValidationError{Field: "schema_version", Reason: "is unsupported"}
	}
	if e.SourceProtocol != "ocpp" || e.ProtocolVersion != "1.6J" {
		return &ValidationError{Field: "source_protocol", Reason: "must identify OCPP 1.6J"}
	}
	if len(e.CPOCountryCode) != 2 || len(e.CPOPartyID) != 3 {
		return &ValidationError{Field: "cpo_identity", Reason: "country code and party ID are required"}
	}
	if stationID := strings.TrimSpace(e.StationID); stationID == "" || len(stationID) > 128 {
		return &ValidationError{Field: "station_id", Reason: "must contain 1 to 128 bytes"}
	}
	if e.OccurredAt.IsZero() || e.ReceivedAt.IsZero() {
		return &ValidationError{Field: "timestamp", Reason: "occurred_at and received_at are required"}
	}
	if strings.TrimSpace(e.IdempotencyKey) == "" || len(e.IdempotencyKey) > 255 {
		return &ValidationError{Field: "idempotency_key", Reason: "must contain 1 to 255 bytes"}
	}
	if !json.Valid(e.Payload) {
		return &ValidationError{Field: "payload", Reason: "must be valid JSON"}
	}
	switch e.EventType {
	case ChargingEventBoot, ChargingEventHeartbeat:
	case ChargingEventStatus:
		var status ConnectorStatus
		if err := json.Unmarshal(e.Payload, &status); err != nil || strings.TrimSpace(status.Status) == "" {
			return &ValidationError{Field: "payload.status", Reason: "is required"}
		}
		if e.ConnectorID < 0 {
			return &ValidationError{Field: "connector_id", Reason: "cannot be negative"}
		}
	case ChargingEventMeter:
		var samples []MeterSample
		if err := json.Unmarshal(e.Payload, &samples); err != nil || len(samples) == 0 {
			return &ValidationError{Field: "payload", Reason: "must contain meter samples"}
		}
		for i, sample := range samples {
			if sample.SampledAt.IsZero() || strings.TrimSpace(sample.Value) == "" {
				return &ValidationError{Field: fmt.Sprintf("payload[%d]", i), Reason: "timestamp and value are required"}
			}
			value, err := strconv.ParseFloat(sample.Value, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return &ValidationError{Field: fmt.Sprintf("payload[%d].value", i), Reason: "must be a finite number"}
			}
		}
	default:
		return &ValidationError{Field: "event_type", Reason: "is unsupported"}
	}
	return nil
}
