package postgres

import (
	"context"
	"database/sql"
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
