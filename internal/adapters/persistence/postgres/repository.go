package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"device-ingestion/internal/domain"

	_ "github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) SaveTelemetry(ctx context.Context, packet domain.TelemetryPacket) (runErr error) {
	if err := packet.Validate(); err != nil {
		return err
	}
	if packet.Timestamp.IsZero() {
		return &domain.ValidationError{Field: "timestamp", Reason: "is required"}
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin telemetry transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			runErr = errors.Join(runErr, fmt.Errorf("rollback telemetry transaction: %w", rollbackErr))
		}
	}()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO telemetry (device_id, idempotency_key, observed_at, voltage, current, power_kw)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (device_id, idempotency_key) DO NOTHING`,
		packet.DeviceID, packet.IdempotencyKey, packet.Timestamp, packet.Voltage, packet.Current, packet.PowerKw)
	if err != nil {
		return fmt.Errorf("insert telemetry: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect telemetry insert: %w", err)
	}
	if inserted == 0 {
		var deviceID string
		var voltage, current, powerKw float64
		err := tx.QueryRowContext(ctx, `
			SELECT device_id, voltage, current, power_kw
			FROM telemetry
			WHERE device_id = $1 AND idempotency_key = $2`,
			packet.DeviceID, packet.IdempotencyKey).Scan(&deviceID, &voltage, &current, &powerKw)
		if err != nil {
			return fmt.Errorf("read existing idempotent telemetry: %w", err)
		}
		if deviceID != packet.DeviceID || voltage != packet.Voltage || current != packet.Current || powerKw != packet.PowerKw {
			return &domain.IdempotencyConflictError{Key: packet.IdempotencyKey}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit telemetry transaction: %w", err)
	}
	return nil
}

func (r *Repository) SaveChargingEvent(ctx context.Context, event domain.ChargingEvent) (runErr error) {
	if err := event.Validate(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin charging event transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			runErr = errors.Join(runErr, fmt.Errorf("rollback charging event transaction: %w", rollbackErr))
		}
	}()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO charging_events (event_id, schema_version, station_id, source_protocol, protocol_version, cpo_country_code, cpo_party_id, idempotency_key, correlation_id, event_type, payload, occurred_at, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13)
		ON CONFLICT DO NOTHING`,
		event.EventID, event.SchemaVersion, event.StationID, event.SourceProtocol, event.ProtocolVersion, event.CPOCountryCode, event.CPOPartyID, event.IdempotencyKey, event.CorrelationID, event.EventType, string(event.Payload), event.OccurredAt, event.ReceivedAt)
	if err != nil {
		return fmt.Errorf("insert charging event: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect charging event insert: %w", err)
	}
	if inserted == 0 {
		var eventType string
		var payload []byte
		if err := tx.QueryRowContext(ctx, `SELECT event_type, payload FROM charging_events WHERE station_id = $1 AND idempotency_key = $2`, event.StationID, event.IdempotencyKey).Scan(&eventType, &payload); err != nil {
			return fmt.Errorf("read existing charging event: %w", err)
		}
		var current, previous any
		if err := json.Unmarshal(payload, &current); err != nil {
			return fmt.Errorf("decode stored charging event: %w", err)
		}
		if err := json.Unmarshal(event.Payload, &previous); err != nil {
			return fmt.Errorf("decode charging event payload: %w", err)
		}
		currentJSON, _ := json.Marshal(current)
		previousJSON, _ := json.Marshal(previous)
		if eventType != event.EventType || string(currentJSON) != string(previousJSON) {
			return &domain.ChargingEventConflictError{Key: event.IdempotencyKey}
		}
		return tx.Commit()
	}

	switch event.EventType {
	case domain.ChargingEventStatus:
		var status domain.ConnectorStatus
		if err := json.Unmarshal(event.Payload, &status); err != nil {
			return fmt.Errorf("decode connector status: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO charging_connector_status (station_id, connector_id, status, error_code, status_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			ON CONFLICT (station_id, connector_id) DO UPDATE
			SET status = EXCLUDED.status, error_code = EXCLUDED.error_code,
			    status_at = EXCLUDED.status_at, updated_at = NOW()
			WHERE charging_connector_status.status_at <= EXCLUDED.status_at`,
			event.StationID, event.ConnectorID, status.Status, status.ErrorCode, event.OccurredAt); err != nil {
			return fmt.Errorf("upsert connector status: %w", err)
		}
	case domain.ChargingEventMeter:
		var samples []domain.MeterSample
		if err := json.Unmarshal(event.Payload, &samples); err != nil {
			return fmt.Errorf("decode meter samples: %w", err)
		}
		for i, sample := range samples {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO charging_meter_samples (event_id, sample_index, station_id, connector_id, sampled_at, measurand, value, unit, context, format, phase, location)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				event.EventID, i, event.StationID, event.ConnectorID, sample.SampledAt,
				sample.Measurand, sample.Value, sample.Unit, sample.Context, sample.Format, sample.Phase, sample.Location); err != nil {
				return fmt.Errorf("insert meter sample: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit charging event transaction: %w", err)
	}
	return nil
}

func (r *Repository) ConnectorStatus(ctx context.Context, stationID string, connectorID int) (domain.ConnectorStatus, time.Time, error) {
	var status domain.ConnectorStatus
	var occurredAt time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT status, error_code, status_at
		FROM charging_connector_status WHERE station_id = $1 AND connector_id = $2`, stationID, connectorID).
		Scan(&status.Status, &status.ErrorCode, &occurredAt)
	if err != nil {
		return domain.ConnectorStatus{}, time.Time{}, err
	}
	return status, occurredAt, nil
}

func (r *Repository) ConnectorStatuses(ctx context.Context) (map[string]map[int]domain.ConnectorStatus, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT station_id, connector_id, status, error_code, status_at FROM charging_connector_status`)
	if err != nil {
		return nil, fmt.Errorf("query connector statuses: %w", err)
	}
	defer rows.Close()
	statuses := make(map[string]map[int]domain.ConnectorStatus)
	for rows.Next() {
		var stationID string
		var connectorID int
		var status domain.ConnectorStatus
		if err := rows.Scan(&stationID, &connectorID, &status.Status, &status.ErrorCode, &status.StatusAt); err != nil {
			return nil, fmt.Errorf("scan connector status: %w", err)
		}
		if statuses[stationID] == nil {
			statuses[stationID] = make(map[int]domain.ConnectorStatus)
		}
		statuses[stationID][connectorID] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector statuses: %w", err)
	}
	return statuses, nil
}

func Open(dsn string, maxOpen int) (*sql.DB, error) {
	if maxOpen < 1 || maxOpen > 32 {
		return nil, errors.New("PostgreSQL max open connections must be between 1 and 32")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	maxIdle := min(maxOpen, 8)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}
