package stub

import (
	"context"
	"time"

	"device-ingestion/internal/domain"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) SaveTelemetry(ctx context.Context, _ domain.TelemetryPacket) error {
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	select {
	case <-time.After(50 * time.Millisecond):
		return nil
	case <-dbCtx.Done():
		return dbCtx.Err()
	}
}
