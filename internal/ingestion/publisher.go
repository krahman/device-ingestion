package ingestion

import "context"

const TelemetryTopic = "device.telemetry"

// Publisher writes telemetry to the primary durable log.
type Publisher interface {
	Publish(ctx context.Context, destination, key string, data []byte) error
	Close() error
}
