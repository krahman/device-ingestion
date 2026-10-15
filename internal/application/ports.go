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

type ChargingEventPublisher interface {
	PublishChargingEvent(context.Context, domain.ChargingEvent) error
}

type ChargingEventRepository interface {
	SaveChargingEvent(context.Context, domain.ChargingEvent) error
}
