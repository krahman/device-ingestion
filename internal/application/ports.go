package application

import (
	"context"

	"device-ingestion/internal/domain"
)

type TelemetryPublisher interface {
	PublishTelemetry(context.Context, domain.TelemetryPacket) error
}

type TelemetryRepository interface {
	SaveTelemetry(context.Context, domain.TelemetryPacket) error
}
