package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"device-ingestion/internal/domain"
)

type telemetryPublisherSpy struct {
	packet domain.TelemetryPacket
	err    error
}

func (s *telemetryPublisherSpy) PublishTelemetry(_ context.Context, packet domain.TelemetryPacket) error {
	s.packet = packet
	return s.err
}

type telemetryRepositorySpy struct {
	packet domain.TelemetryPacket
	err    error
}

func (s *telemetryRepositorySpy) SaveTelemetry(_ context.Context, packet domain.TelemetryPacket) error {
	s.packet = packet
	return s.err
}

func TestTelemetryUseCases(t *testing.T) {
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	kafkaErr := errors.New("kafka down")
	databaseErr := errors.New("database down")
	tests := []struct {
		name      string
		operation string
		packet    domain.TelemetryPacket
		depErr    error
		wantErr   error
	}{
		{name: "ingest trims and timestamps", operation: "ingest", packet: domain.TelemetryPacket{DeviceID: " device-1 ", IdempotencyKey: " key-1 ", Voltage: 230}},
		{name: "ingest rejects invalid packet", operation: "ingest", wantErr: &domain.ValidationError{}},
		{name: "ingest returns publisher error", operation: "ingest", packet: domain.TelemetryPacket{DeviceID: "device", IdempotencyKey: "key"}, depErr: kafkaErr, wantErr: kafkaErr},
		{name: "process forwards packet", operation: "process", packet: domain.TelemetryPacket{DeviceID: "device", IdempotencyKey: "key"}},
		{name: "process returns repository error", operation: "process", packet: domain.TelemetryPacket{DeviceID: "device", IdempotencyKey: "key"}, depErr: databaseErr, wantErr: databaseErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			publisher := &telemetryPublisherSpy{err: tc.depErr}
			repository := &telemetryRepositorySpy{err: tc.depErr}
			var execute func(context.Context, domain.TelemetryPacket) error
			if tc.operation == "ingest" {
				useCase := NewIngestTelemetry(publisher)
				useCase.now = func() time.Time { return stamp }
				execute = useCase.Execute
			} else {
				execute = NewProcessTelemetry(repository).Execute
			}
			err := execute(context.Background(), tc.packet)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if tc.wantErr != nil {
				if tc.wantErr == kafkaErr || tc.wantErr == databaseErr {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("Execute() error = %v, want %v", err, tc.wantErr)
					}
				} else {
					var validation *domain.ValidationError
					if !errors.As(err, &validation) {
						t.Fatalf("Execute() error = %v, want validation error", err)
					}
				}
			}
			got := repository.packet
			if tc.operation == "ingest" {
				got = publisher.packet
			}
			if tc.wantErr != nil && tc.wantErr != kafkaErr && tc.wantErr != databaseErr {
				if got.DeviceID != "" {
					t.Fatalf("dependency called for invalid packet: %#v", got)
				}
				return
			}
			if tc.operation == "ingest" {
				if got.DeviceID != strings.TrimSpace(tc.packet.DeviceID) || got.IdempotencyKey != strings.TrimSpace(tc.packet.IdempotencyKey) || !got.Timestamp.Equal(stamp) {
					t.Fatalf("published packet = %#v", got)
				}
			} else if got != tc.packet {
				t.Fatalf("saved packet = %#v, want %#v", got, tc.packet)
			}
		})
	}
}
