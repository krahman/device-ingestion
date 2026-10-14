package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"

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
	metrics    *observability.Metrics
	wg         sync.WaitGroup
}

func NewWorkerPool(brokers []string, topic, groupID string, workerCount int, processor telemetryProcessor, metrics *observability.Metrics) (*WorkerPool, error) {
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return nil, err
	}
	if workerCount < 1 {
		return nil, errors.New("kafka worker count must be at least 1")
	}
	if workerCount > maxWorkerCount {
		return nil, errors.New("kafka worker count cannot exceed 32")
	}

	pool := &WorkerPool{
		readers:   make([]*kafkago.Reader, 0, workerCount),
		topic:     topic,
		processor: processor,
		metrics:   metrics,
		deadLetter: &kafkago.Writer{
			Addr:         kafkago.TCP(addresses...),
			Balancer:     &kafkago.Hash{},
			RequiredAcks: kafkago.RequireAll,
		},
	}
	for range workerCount {
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
			p.metrics.AddCounter("device_ingestion_kafka_worker_errors_total", map[string]string{"stage": "fetch"}, 1)
			slog.Error("Kafka worker fetch failed; retrying", "error", err)
			if !waitForRetry(ctx) {
				return
			}
			continue
		}

		var packet domain.TelemetryPacket
		if err := json.Unmarshal(message.Value, &packet); err != nil {
			if !p.writeDeadLetterWithRetry(ctx, message, err) {
				return
			}
		} else {
			for {
				started := time.Now()
				err := p.processor.Execute(ctx, packet)
				p.metrics.Observe("device_ingestion_telemetry_processing_duration_seconds", nil, time.Since(started))
				if err == nil {
					p.metrics.AddCounter("device_ingestion_kafka_processed_total", map[string]string{"result": "success"}, 1)
					break
				}
				var validationErr *domain.ValidationError
				var idempotencyErr *domain.IdempotencyConflictError
				if errors.As(err, &validationErr) || errors.As(err, &idempotencyErr) {
					if !p.writeDeadLetterWithRetry(ctx, message, err) {
						return
					}
					break
				}
				p.metrics.AddCounter("device_ingestion_kafka_processed_total", map[string]string{"result": "retry"}, 1)
				slog.Error("telemetry processing failed; retrying without committing offset", "device_id", packet.DeviceID, "topic", message.Topic, "partition", message.Partition, "offset", message.Offset, "error", err)
				if !waitForRetry(ctx) {
					return
				}
			}
		}

		if err := reader.CommitMessages(ctx, message); err != nil {
			p.metrics.AddCounter("device_ingestion_kafka_worker_errors_total", map[string]string{"stage": "offset_commit"}, 1)
			slog.Error("Kafka offset commit failed", "topic", message.Topic, "partition", message.Partition, "offset", message.Offset, "error", err)
			return
		}
	}
}

func (p *WorkerPool) writeDeadLetterWithRetry(ctx context.Context, message kafkago.Message, cause error) bool {
	for {
		if err := p.writeDeadLetter(ctx, message, cause); err == nil {
			p.metrics.AddCounter("device_ingestion_kafka_dlq_total", nil, 1)
			return true
		} else {
			p.metrics.AddCounter("device_ingestion_kafka_worker_errors_total", map[string]string{"stage": "dlq_write"}, 1)
			slog.Error("dead-letter write failed; retrying", "topic", message.Topic, "partition", message.Partition, "offset", message.Offset, "error", err)
		}
		if !waitForRetry(ctx) {
			return false
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
