package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"device-ingestion/internal/domain"

	kafkago "github.com/segmentio/kafka-go"
)

type Publisher struct {
	writer *kafkago.Writer
	topic  string
}

func NewPublisher(brokers []string, topic string) (*Publisher, error) {
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
		topic: topic,
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
	data, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Topic: p.topic,
		Key:   []byte(packet.DeviceID),
		Value: data,
	})
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}
