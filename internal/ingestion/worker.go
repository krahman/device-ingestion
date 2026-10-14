package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"

	"device-ingestion/internal/models"

	"github.com/segmentio/kafka-go"
)

// KafkaWorkerPool processes Kafka records and commits offsets after the database
// write succeeds. Each reader is a member of the same consumer group.
type KafkaWorkerPool struct {
	readers []*kafka.Reader
	dlq     *kafka.Writer
	topic   string
	wg      sync.WaitGroup
}

func NewKafkaWorkerPool(brokers []string, topic, groupID string, workerCount int) (*KafkaWorkerPool, error) {
	addresses, err := kafkaAddresses(brokers)
	if err != nil {
		return nil, err
	}
	if workerCount < 1 {
		return nil, errors.New("Kafka worker count must be at least 1")
	}

	pool := &KafkaWorkerPool{
		readers: make([]*kafka.Reader, 0, workerCount),
		topic:   topic,
		dlq: &kafka.Writer{
			Addr:         kafka.TCP(addresses...),
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireAll,
		},
	}
	for i := 0; i < workerCount; i++ {
		pool.readers = append(pool.readers, kafka.NewReader(kafka.ReaderConfig{
			Brokers:     addresses,
			Topic:       topic,
			GroupID:     groupID,
			StartOffset: kafka.FirstOffset,
			MinBytes:    1,
			MaxBytes:    10e6,
		}))
	}
	return pool, nil
}

func (p *KafkaWorkerPool) Start(ctx context.Context) {
	for _, reader := range p.readers {
		p.wg.Add(1)
		go p.consume(ctx, reader)
	}
}

func (p *KafkaWorkerPool) consume(ctx context.Context, reader *kafka.Reader) {
	defer p.wg.Done()
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("Kafka worker fetch failed; retrying: %v", err)
			if !waitForRetry(ctx) {
				return
			}
			continue
		}

		var packet models.TelemetryPacket
		if err := json.Unmarshal(message.Value, &packet); err != nil {
			decodeErr := err
			log.Printf("Invalid telemetry at %s[%d] offset %d: %v", message.Topic, message.Partition, message.Offset, decodeErr)
			for {
				if err := p.writeDeadLetter(ctx, message, decodeErr); err == nil {
					break
				} else {
					log.Printf("Failed to write telemetry to dead-letter topic: %v", err)
				}
				if !waitForRetry(ctx) {
					return
				}
			}
		} else {
			for {
				if err := saveToDatabase(ctx, packet); err == nil {
					break
				} else {
					log.Printf("Database write failed for device %s: %v", packet.DeviceID, err)
				}
				if !waitForRetry(ctx) {
					return
				}
			}
		}

		if err := reader.CommitMessages(ctx, message); err != nil {
			log.Printf("Failed to commit Kafka offset %s[%d] %d: %v", message.Topic, message.Partition, message.Offset, err)
			return
		}
	}
}

func (p *KafkaWorkerPool) writeDeadLetter(ctx context.Context, message kafka.Message, cause error) error {
	return p.dlq.WriteMessages(ctx, kafka.Message{
		Topic: p.topic + ".dlq",
		Key:   message.Key,
		Value: message.Value,
		Headers: []kafka.Header{
			{Key: "source-topic", Value: []byte(message.Topic)},
			{Key: "source-partition", Value: []byte(strconv.Itoa(message.Partition))},
			{Key: "source-offset", Value: []byte(strconv.FormatInt(message.Offset, 10))},
			{Key: "failure", Value: []byte(cause.Error())},
		},
	})
}

func waitForRetry(ctx context.Context) bool {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func saveToDatabase(ctx context.Context, packet models.TelemetryPacket) error {
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	select {
	case <-time.After(50 * time.Millisecond):
		return nil
	case <-dbCtx.Done():
		return dbCtx.Err()
	}
}

func (p *KafkaWorkerPool) Stop() error {
	var closeErrors []error
	for _, reader := range p.readers {
		if err := reader.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	p.wg.Wait()
	if err := p.dlq.Close(); err != nil {
		closeErrors = append(closeErrors, err)
	}
	return errors.Join(closeErrors...)
}
