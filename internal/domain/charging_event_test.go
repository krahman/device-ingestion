package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func validChargingEvent() ChargingEvent {
	return ChargingEvent{
		EventID: "event-1", SchemaVersion: ChargingEventSchemaVersion, EventType: ChargingEventMeter,
		SourceProtocol: "ocpp", ProtocolVersion: "1.6J", CPOCountryCode: "NL", CPOPartyID: "ABC",
		StationID: "station-1", OccurredAt: time.Now().UTC(), ReceivedAt: time.Now().UTC(),
		IdempotencyKey: "idempotency-1", Payload: json.RawMessage(`[{"sampled_at":"2026-01-01T00:00:00Z","value":"12.5"}]`),
	}
}

func TestChargingEventValidate(t *testing.T) {
	base := validChargingEvent()
	tests := []struct {
		name    string
		mutate  func(*ChargingEvent)
		wantErr bool
	}{
		{name: "valid meter sample", mutate: func(*ChargingEvent) {}},
		{name: "valid status", mutate: func(e *ChargingEvent) {
			e.EventType = ChargingEventStatus
			e.Payload = json.RawMessage(`{"status":"Available"}`)
		}},
		{name: "missing event id", mutate: func(e *ChargingEvent) { e.EventID = " " }, wantErr: true},
		{name: "unsupported schema version", mutate: func(e *ChargingEvent) { e.SchemaVersion++ }, wantErr: true},
		{name: "invalid source", mutate: func(e *ChargingEvent) { e.SourceProtocol = "ocpi" }, wantErr: true},
		{name: "invalid cpo identity", mutate: func(e *ChargingEvent) { e.CPOPartyID = "" }, wantErr: true},
		{name: "missing station", mutate: func(e *ChargingEvent) { e.StationID = " " }, wantErr: true},
		{name: "missing occurred time", mutate: func(e *ChargingEvent) { e.OccurredAt = time.Time{} }, wantErr: true},
		{name: "missing received time", mutate: func(e *ChargingEvent) { e.ReceivedAt = time.Time{} }, wantErr: true},
		{name: "missing idempotency key", mutate: func(e *ChargingEvent) { e.IdempotencyKey = " " }, wantErr: true},
		{name: "invalid json", mutate: func(e *ChargingEvent) { e.Payload = json.RawMessage("{") }, wantErr: true},
		{name: "unsupported event", mutate: func(e *ChargingEvent) { e.EventType = "unknown" }, wantErr: true},
		{name: "empty status", mutate: func(e *ChargingEvent) { e.EventType = ChargingEventStatus; e.Payload = json.RawMessage(`{}`) }, wantErr: true},
		{name: "negative status connector", mutate: func(e *ChargingEvent) {
			e.EventType = ChargingEventStatus
			e.ConnectorID = -1
			e.Payload = json.RawMessage(`{"status":"Available"}`)
		}, wantErr: true},
		{name: "malformed status payload", mutate: func(e *ChargingEvent) { e.EventType = ChargingEventStatus; e.Payload = json.RawMessage(`[]`) }, wantErr: true},
		{name: "empty meter list", mutate: func(e *ChargingEvent) { e.Payload = json.RawMessage(`[]`) }, wantErr: true},
		{name: "malformed meter list", mutate: func(e *ChargingEvent) { e.Payload = json.RawMessage(`{}`) }, wantErr: true},
		{name: "missing meter timestamp", mutate: func(e *ChargingEvent) { e.Payload = json.RawMessage(`[{"value":"1"}]`) }, wantErr: true},
		{name: "missing meter value", mutate: func(e *ChargingEvent) { e.Payload = json.RawMessage(`[{"sampled_at":"2026-01-01T00:00:00Z"}]`) }, wantErr: true},
		{name: "non numeric meter value", mutate: func(e *ChargingEvent) {
			e.Payload = json.RawMessage(`[{"sampled_at":"2026-01-01T00:00:00Z","value":"oops"}]`)
		}, wantErr: true},
		{name: "non-finite meter value", mutate: func(e *ChargingEvent) {
			e.Payload = json.RawMessage(`[{"sampled_at":"2026-01-01T00:00:00Z","value":"NaN"}]`)
		}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := base
			tc.mutate(&event)
			if err := event.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestChargingEventConflictErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "conflict", err: &ChargingEventConflictError{Key: "key"}},
		{name: "validation", err: &ValidationError{Field: "event_id", Reason: "required"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got == "" {
				t.Fatal("Error() returned an empty message")
			}
		})
	}
}
