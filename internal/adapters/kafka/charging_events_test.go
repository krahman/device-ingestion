package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"device-ingestion/internal/domain"
	kafkago "github.com/segmentio/kafka-go"
)

func TestChargingEventWorkerErrorClassification(t *testing.T) {
	var event domain.ChargingEvent
	malformedJSON := json.Unmarshal([]byte(`{"event_id":`), &event)
	var wrongTypeEvent domain.ChargingEvent
	wrongJSONType := json.Unmarshal([]byte(`{"schema_version":"wrong"}`), &wrongTypeEvent)
	tests := []struct {
		name       string
		err        error
		wantDLQ    bool
		wantSyntax bool
	}{
		{name: "malformed json", err: malformedJSON, wantDLQ: true, wantSyntax: true},
		{name: "wrong json type", err: wrongJSONType, wantDLQ: true, wantSyntax: true},
		{name: "invalid event", err: &domain.ValidationError{Field: "event_id", Reason: "is required"}, wantDLQ: true},
		{name: "conflicting duplicate", err: &domain.ChargingEventConflictError{Key: "key"}, wantDLQ: true},
		{name: "transient database error", err: errors.New("database unavailable")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPermanentChargingEventError(tc.err); got != tc.wantDLQ {
				t.Fatalf("isPermanentChargingEventError() = %v, want %v", got, tc.wantDLQ)
			}
			if got := jsonSyntaxError(tc.err); got != tc.wantSyntax {
				t.Fatalf("jsonSyntaxError() = %v, want %v", got, tc.wantSyntax)
			}
		})
	}
}

func TestWaitForRetryStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForRetry(ctx) {
		t.Fatal("waitForRetry returned true for a cancelled context")
	}
}

func TestEventProtocolLabelIsBounded(t *testing.T) {
	for _, protocol := range []string{"", "unexpected", "OCPP", "unknown-value"} {
		if got := eventProtocolLabel(protocol); got != "unknown" {
			t.Fatalf("eventProtocolLabel(%q) = %q, want unknown", protocol, got)
		}
	}
	if got := eventProtocolLabel("ocpp"); got != "ocpp" {
		t.Fatalf("eventProtocolLabel(ocpp) = %q", got)
	}
}

func TestDeadLetterMessagePreservesSourceAndFailureMetadata(t *testing.T) {
	source := kafkago.Message{Topic: ChargingEventsTopic, Partition: 3, Offset: 42, Key: []byte("station-1"), Value: []byte("invalid")}
	message := deadLetterMessage(source, errors.New("invalid event"))
	if message.Topic != ChargingEventsTopic+".dlq" || string(message.Key) != "station-1" || string(message.Value) != "invalid" {
		t.Fatalf("dead-letter message = %#v", message)
	}
	headers := make(map[string]string, len(message.Headers))
	for _, header := range message.Headers {
		headers[header.Key] = string(header.Value)
	}
	want := map[string]string{"source-topic": ChargingEventsTopic, "source-partition": strconv.Itoa(source.Partition), "source-offset": strconv.FormatInt(source.Offset, 10), "failure": "invalid event"}
	for key, value := range want {
		if headers[key] != value {
			t.Errorf("header %q = %q, want %q", key, headers[key], value)
		}
	}
}
