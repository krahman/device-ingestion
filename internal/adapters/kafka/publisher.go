package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"

	kafkago "github.com/segmentio/kafka-go"
)

const TelemetryTopic = "device.telemetry"

type Publisher struct {
	writer  *kafkago.Writer
	topic   string
	metrics *observability.Metrics
}

func NewPublisher(brokers []string, topic string, metrics *observability.Metrics) (*Publisher, error) {
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return nil, err
	}
	return &Publisher{
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(addresses...),
			Balancer:     &kafkago.Hash{},
			BatchSize:    500,
			BatchBytes:   1 << 20,
			BatchTimeout: 10 * time.Millisecond,
			RequiredAcks: kafkago.RequireAll,
		},
		topic: topic, metrics: metrics,
	}, nil
}

func normalizeBrokers(brokers []string) ([]string, error) {
	addresses := make([]string, 0, len(brokers))
	for _, broker := range brokers {
		if broker = strings.TrimSpace(broker); broker != "" {
			addresses = append(addresses, broker)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("at least one Kafka broker address is required")
	}
	return addresses, nil
}

func (p *Publisher) PublishTelemetry(ctx context.Context, packet domain.TelemetryPacket) error {
	started := time.Now()
	result := "success"
	defer func() {
		p.metrics.AddCounter("device_ingestion_kafka_published_total", map[string]string{"result": result}, 1)
		p.metrics.Observe("device_ingestion_kafka_publish_duration_seconds", nil, time.Since(started))
	}()
	data, err := json.Marshal(packet)
	if err != nil {
		result = "error"
		slog.Error("failed to encode telemetry for Kafka", "device_id", packet.DeviceID, "error", err)
		return err
	}
	err = p.writer.WriteMessages(ctx, kafkago.Message{
		Topic: p.topic,
		Key:   []byte(packet.DeviceID),
		Value: data,
	})
	if err != nil {
		result = "error"
		slog.Error("Kafka publish failed", "topic", p.topic, "device_id", packet.DeviceID, "error", err)
	}
	return err
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}
