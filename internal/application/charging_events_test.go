package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"device-ingestion/internal/domain"
)

type eventPublisherSpy struct {
	event domain.ChargingEvent
	err   error
}

func (s *eventPublisherSpy) PublishChargingEvent(_ context.Context, event domain.ChargingEvent) error {
	s.event = event
	return s.err
}

type eventRepositorySpy struct {
	event domain.ChargingEvent
	err   error
}

func (s *eventRepositorySpy) SaveChargingEvent(_ context.Context, event domain.ChargingEvent) error {
	s.event = event
	return s.err
}

func applicationEvent() domain.ChargingEvent {
	return domain.ChargingEvent{EventID: "event-1", SchemaVersion: domain.ChargingEventSchemaVersion, EventType: domain.ChargingEventBoot, SourceProtocol: "ocpp", ProtocolVersion: "1.6J", CPOCountryCode: "NL", CPOPartyID: "ABC", StationID: "station-1", OccurredAt: time.Now(), ReceivedAt: time.Now(), IdempotencyKey: "key-1", Payload: json.RawMessage(`{"vendor":"Acme","model":"X"}`)}
}

func TestChargingEventUseCases(t *testing.T) {
	brokerErr := errors.New("broker unavailable")
	databaseErr := errors.New("database unavailable")
	tests := []struct {
		name      string
		operation string
		invalid   bool
		depErr    error
		wantDep   bool
		wantErr   error
	}{
		{name: "ingest publishes", operation: "ingest", wantDep: true},
		{name: "ingest validates", operation: "ingest", invalid: true, wantErr: &domain.ValidationError{}},
		{name: "ingest returns publisher error", operation: "ingest", depErr: brokerErr, wantDep: true, wantErr: brokerErr},
		{name: "process saves", operation: "process", wantDep: true},
		{name: "process validates", operation: "process", invalid: true, wantErr: &domain.ValidationError{}},
		{name: "process returns repository error", operation: "process", depErr: databaseErr, wantDep: true, wantErr: databaseErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			publisher := &eventPublisherSpy{err: tc.depErr}
			repository := &eventRepositorySpy{err: tc.depErr}
			var execute func(context.Context, domain.ChargingEvent) error
			if tc.operation == "ingest" {
				execute = NewIngestChargingEvent(publisher).Execute
			} else {
				execute = NewProcessChargingEvent(repository).Execute
			}
			event := applicationEvent()
			if tc.invalid {
				event.EventID = ""
			}
			err := execute(context.Background(), event)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if tc.wantErr != nil {
				if errors.Is(tc.wantErr, brokerErr) || errors.Is(tc.wantErr, databaseErr) {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("Execute() error = %v, want %v", err, tc.wantErr)
					}
				} else {
					if _, ok := errors.AsType[*domain.ValidationError](err); !ok {
						t.Fatalf("Execute() error = %v, want validation error", err)
					}
				}
			}
			called := publisher.event.EventID != "" || repository.event.EventID != ""
			if called != tc.wantDep {
				t.Fatalf("dependency called = %v, want %v", called, tc.wantDep)
			}
		})
	}
}
