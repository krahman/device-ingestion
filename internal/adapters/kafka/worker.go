package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"

	"device-ingestion/internal/domain"

	kafkago "github.com/segmentio/kafka-go"
)

const (
	maxWorkerCount = 32
	maxFetchBytes  = 1 << 20
)

type telemetryProcessor interface {
	Execute(context.Context, domain.TelemetryPacket) error
}

type WorkerPool struct {
	readers    []*kafkago.Reader
	deadLetter *kafkago.Writer
	topic      string
	processor  telemetryProcessor
	wg         sync.WaitGroup
}

func NewWorkerPool(brokers []string, topic, groupID string, workerCount int, processor telemetryProcessor) (*WorkerPool, error) {
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return nil, err
	}
	if workerCount < 1 {
		return nil, errors.New("Kafka worker count must be at least 1")
	}
	if workerCount > maxWorkerCount {
		return nil, errors.New("Kafka worker count cannot exceed 32")
	}

	pool := &WorkerPool{
		readers:   make([]*kafkago.Reader, 0, workerCount),
		topic:     topic,
		processor: processor,
		deadLetter: &kafkago.Writer{
			Addr:         kafkago.TCP(addresses...),
			Balancer:     &kafkago.Hash{},
			RequiredAcks: kafkago.RequireAll,
		},
	}
	for i := 0; i < workerCount; i++ {
		pool.readers = append(pool.readers, kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:       addresses,
			Topic:         topic,
			GroupID:       groupID,
			StartOffset:   kafkago.FirstOffset,
			MinBytes:      1,
			QueueCapacity: 1,
			// Bound both each broker fetch and the reader's message queue.
			MaxBytes: maxFetchBytes,
		}))
	}
	return pool, nil
}

func (p *WorkerPool) Start(ctx context.Context) {
	for _, reader := range p.readers {
		p.wg.Add(1)
		go p.consume(ctx, reader)
	}
}

func (p *WorkerPool) consume(ctx context.Context, reader *kafkago.Reader) {
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

		var packet domain.TelemetryPacket
		if err := json.Unmarshal(message.Value, &packet); err != nil {
			for {
				if writeErr := p.writeDeadLetter(ctx, message, err); writeErr == nil {
					break
				} else {
					log.Printf("Failed to write telemetry to dead-letter topic: %v", writeErr)
				}
				if !waitForRetry(ctx) {
					return
				}
			}
		} else {
			for {
				if err := p.processor.Execute(ctx, packet); err == nil {
					break
				} else {
					log.Printf("Telemetry processing failed for device %s: %v", packet.DeviceID, err)
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

func (p *WorkerPool) writeDeadLetter(ctx context.Context, message kafkago.Message, cause error) error {
	return p.deadLetter.WriteMessages(ctx, kafkago.Message{
		Topic: p.topic + ".dlq",
		Key:   message.Key,
		Value: message.Value,
		Headers: []kafkago.Header{
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

func (p *WorkerPool) Stop() error {
	var closeErrors []error
	for _, reader := range p.readers {
		if err := reader.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	p.wg.Wait()
	if err := p.deadLetter.Close(); err != nil {
		closeErrors = append(closeErrors, err)
	}
	return errors.Join(closeErrors...)
}
