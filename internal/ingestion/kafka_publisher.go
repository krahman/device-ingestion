package ingestion

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type KafkaPublisher struct {
	writer *kafka.Writer
}

func kafkaAddresses(brokers []string) ([]string, error) {
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

func NewKafkaPublisher(brokers []string) (*KafkaPublisher, error) {
	addresses, err := kafkaAddresses(brokers)
	if err != nil {
		return nil, err
	}
	return &KafkaPublisher{writer: &kafka.Writer{
		Addr:         kafka.TCP(addresses...),
		Balancer:     &kafka.Hash{},
		BatchSize:    500,
		BatchBytes:   1 << 20,
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireAll,
	}}, nil
}

func (p *KafkaPublisher) Publish(ctx context.Context, topic, key string, data []byte) error {
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	})
}

func (p *KafkaPublisher) Close() error {
	return p.writer.Close()
}
