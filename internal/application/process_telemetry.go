package application

import (
	"context"

	"device-ingestion/internal/domain"
)

type ProcessTelemetry struct {
	repository TelemetryRepository
}

func NewProcessTelemetry(repository TelemetryRepository) *ProcessTelemetry {
	return &ProcessTelemetry{repository: repository}
}

func (u *ProcessTelemetry) Execute(ctx context.Context, packet domain.TelemetryPacket) error {
	return u.repository.SaveTelemetry(ctx, packet)
}
