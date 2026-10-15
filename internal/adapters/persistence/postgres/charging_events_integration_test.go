package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"device-ingestion/internal/domain"
)

func TestSaveChargingEventIsIdempotentAndDetectsConflicts(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated PostgreSQL database to run persistence integration coverage")
	}
	db, err := Open(dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}

	unique := time.Now().UTC().Format("20060102150405.000000000")
	event := domain.ChargingEvent{
		EventID: "integration-" + unique, SchemaVersion: domain.ChargingEventSchemaVersion,
		EventType: domain.ChargingEventMeter, SourceProtocol: "ocpp", ProtocolVersion: "1.6J",
		CPOCountryCode: "NL", CPOPartyID: "ABC", StationID: "integration-station-" + unique,
		ConnectorID: 1, OccurredAt: time.Now().UTC(), ReceivedAt: time.Now().UTC(),
		IdempotencyKey: "integration-key-" + unique,
		Payload:        json.RawMessage(`[{"sampled_at":"2026-01-01T00:00:00Z","value":"123.45","measurand":"Energy.Active.Import.Register","unit":"Wh","context":"Sample.Periodic","format":"Raw","location":"Outlet"}]`),
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM charging_meter_samples WHERE event_id = $1`, event.EventID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM charging_events WHERE event_id = $1`, event.EventID)
	})

	repository := NewRepository(db)
	if err := repository.SaveChargingEvent(ctx, event); err != nil {
		t.Fatalf("first SaveChargingEvent() error = %v", err)
	}
	if err := repository.SaveChargingEvent(ctx, event); err != nil {
		t.Fatalf("duplicate SaveChargingEvent() error = %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM charging_meter_samples WHERE event_id = $1`, event.EventID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("meter sample count = %d, want 1 after replay", count)
	}

	conflicting := event
	conflicting.Payload = json.RawMessage(`[{"sampled_at":"2026-01-01T00:00:00Z","value":"999.00","measurand":"Energy.Active.Import.Register","unit":"Wh","context":"Sample.Periodic","format":"Raw","location":"Outlet"}]`)
	err = repository.SaveChargingEvent(ctx, conflicting)
	var conflict *domain.ChargingEventConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("conflicting replay error = %v, want ChargingEventConflictError", err)
	}
}

func TestSaveChargingStatusKeepsLatestConnectorSnapshot(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated PostgreSQL database to run persistence integration coverage")
	}
	db, err := Open(dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}

	unique := time.Now().UTC().Format("20060102150405.000000000")
	stationID := "integration-status-" + unique
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM charging_connector_status WHERE station_id = $1`, stationID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM charging_events WHERE station_id = $1`, stationID)
	})

	repository := NewRepository(db)
	baseTime := time.Now().UTC().Truncate(time.Microsecond)
	makeEvent := func(sequence, status, errorCode string, occurredAt time.Time) domain.ChargingEvent {
		payload, err := json.Marshal(domain.ConnectorStatus{Status: status, ErrorCode: errorCode})
		if err != nil {
			t.Fatalf("marshal connector status: %v", err)
		}
		return domain.ChargingEvent{
			EventID: "integration-status-" + unique + "-" + sequence, SchemaVersion: domain.ChargingEventSchemaVersion,
			EventType: domain.ChargingEventStatus, SourceProtocol: "ocpp", ProtocolVersion: "1.6J",
			CPOCountryCode: "NL", CPOPartyID: "ABC", StationID: stationID, ConnectorID: 1,
			OccurredAt: occurredAt, ReceivedAt: time.Now().UTC(), IdempotencyKey: "integration-status-key-" + unique + "-" + sequence,
			Payload: payload,
		}
	}

	if err := repository.SaveChargingEvent(ctx, makeEvent("available", "Available", "NoError", baseTime)); err != nil {
		t.Fatalf("save initial connector status: %v", err)
	}
	if err := repository.SaveChargingEvent(ctx, makeEvent("charging", "Charging", "NoError", baseTime.Add(time.Second))); err != nil {
		t.Fatalf("save newer connector status: %v", err)
	}
	if err := repository.SaveChargingEvent(ctx, makeEvent("stale", "Faulted", "OtherError", baseTime.Add(-time.Second))); err != nil {
		t.Fatalf("save stale connector status event: %v", err)
	}

	var status, errorCode string
	var statusAt time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT status, error_code, status_at
		FROM charging_connector_status
		WHERE station_id = $1 AND connector_id = 1`, stationID,
	).Scan(&status, &errorCode, &statusAt); err != nil {
		t.Fatalf("read current connector status: %v", err)
	}
	if status != "Charging" || errorCode != "NoError" || !statusAt.Equal(baseTime.Add(time.Second)) {
		t.Fatalf("connector snapshot = (%q, %q, %s), want (Charging, NoError, %s)", status, errorCode, statusAt, baseTime.Add(time.Second))
	}

	var snapshotCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM charging_connector_status WHERE station_id = $1 AND connector_id = 1`, stationID).Scan(&snapshotCount); err != nil {
		t.Fatalf("count connector snapshots: %v", err)
	}
	if snapshotCount != 1 {
		t.Fatalf("connector snapshot count = %d, want 1 after status updates", snapshotCount)
	}
}
