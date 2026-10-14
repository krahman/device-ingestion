package application

import (
	"context"
	"strings"
	"time"

	"device-ingestion/internal/domain"
)

type IngestTelemetry struct {
	publisher TelemetryPublisher
	now       func() time.Time
}

func NewIngestTelemetry(publisher TelemetryPublisher) *IngestTelemetry {
	return &IngestTelemetry{publisher: publisher, now: time.Now}
}

func (u *IngestTelemetry) Execute(ctx context.Context, packet domain.TelemetryPacket) error {
	packet.DeviceID = strings.TrimSpace(packet.DeviceID)
	if err := packet.Validate(); err != nil {
		return err
	}
	packet.Timestamp = u.now()
	return u.publisher.PublishTelemetry(ctx, packet)
}
