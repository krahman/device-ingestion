package application

import (
	"context"

	"device-ingestion/internal/domain"
)

type IngestChargingEvent struct {
	publisher ChargingEventPublisher
}

func NewIngestChargingEvent(publisher ChargingEventPublisher) *IngestChargingEvent {
	return &IngestChargingEvent{publisher: publisher}
}

func (u *IngestChargingEvent) Execute(ctx context.Context, event domain.ChargingEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	return u.publisher.PublishChargingEvent(ctx, event)
}

type ProcessChargingEvent struct {
	repository ChargingEventRepository
}

func NewProcessChargingEvent(repository ChargingEventRepository) *ProcessChargingEvent {
	return &ProcessChargingEvent{repository: repository}
}

func (u *ProcessChargingEvent) Execute(ctx context.Context, event domain.ChargingEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	return u.repository.SaveChargingEvent(ctx, event)
}
