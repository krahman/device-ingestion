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
	readers       []*kafkago.Reader
	deadLetter    *kafkago.Writer
	topic         string
	processor     telemetryProcessor
	metrics       *observability.Metrics
	wg            sync.WaitGroup
	fetchCancel   context.CancelFunc
	processCancel context.CancelFunc
	done          chan struct{}
	started       bool
	stopOnce      sync.Once
}

func NewWorkerPool(brokers []string, topic, groupID string, workerCount int, processor telemetryProcessor, metrics *observability.Metrics, security ...SecurityConfig) (*WorkerPool, error) {
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

	var secure SecurityConfig
	if len(security) > 0 {
		secure = security[0]
	}
	dialer, err := secure.Dialer()
	if err != nil {
		return nil, err
	}
	transport, err := secure.Transport()
	if err != nil {
		return nil, err
	}
	pool := &WorkerPool{
		readers:   make([]*kafkago.Reader, 0, workerCount),
		topic:     topic,
		processor: processor,
		metrics:   metrics,
		deadLetter: &kafkago.Writer{
			Addr:         kafkago.TCP(addresses...),
			Transport:    transport,
			Balancer:     &kafkago.Hash{},
			RequiredAcks: kafkago.RequireAll,
		},
	}
	for range workerCount {
		pool.readers = append(pool.readers, kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:       addresses,
			Dialer:        dialer,
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
	processCtx, cancelProcess := context.WithCancel(ctx)
	fetchCtx, cancelFetch := context.WithCancel(processCtx)
	p.processCancel = cancelProcess
	p.fetchCancel = cancelFetch
	p.done = make(chan struct{})
	p.started = true
	for _, reader := range p.readers {
		p.wg.Add(1)
		go p.consume(fetchCtx, processCtx, reader)
	}
	go func() {
		p.wg.Wait()
		close(p.done)
	}()
}

func (p *WorkerPool) consume(fetchCtx, processCtx context.Context, reader *kafkago.Reader) {
	defer p.wg.Done()
	for {
		message, err := reader.FetchMessage(fetchCtx)
		if err != nil {
			if errors.Is(err, context.Canceled) || fetchCtx.Err() != nil {
				return
			}
			p.metrics.AddCounter("device_ingestion_kafka_worker_errors_total", map[string]string{"stage": "fetch"}, 1)
			slog.Error("Kafka worker fetch failed; retrying", "error", err)
			if !waitForRetry(fetchCtx) {
				return
			}
			continue
		}

		var packet domain.TelemetryPacket
		if err := json.Unmarshal(message.Value, &packet); err != nil {
			if !p.writeDeadLetterWithRetry(processCtx, message, err) {
				return
			}
		} else {
			for {
				started := time.Now()
				err := p.processor.Execute(processCtx, packet)
				p.metrics.Observe("device_ingestion_telemetry_processing_duration_seconds", nil, time.Since(started))
				if err == nil {
					p.metrics.AddCounter("device_ingestion_kafka_processed_total", map[string]string{"result": "success"}, 1)
					break
				}
				var validationErr *domain.ValidationError
				var idempotencyErr *domain.IdempotencyConflictError
				if errors.As(err, &validationErr) || errors.As(err, &idempotencyErr) {
					if !p.writeDeadLetterWithRetry(processCtx, message, err) {
						return
					}
					break
				}
				p.metrics.AddCounter("device_ingestion_kafka_processed_total", map[string]string{"result": "retry"}, 1)
				slog.Error("telemetry processing failed; retrying without committing offset", "device_id", packet.DeviceID, "topic", message.Topic, "partition", message.Partition, "offset", message.Offset, "error", err)
				if !waitForRetry(processCtx) {
					return
				}
			}
		}

		if err := reader.CommitMessages(processCtx, message); err != nil {
			p.metrics.AddCounter("device_ingestion_kafka_worker_errors_total", map[string]string{"stage": "offset_commit"}, 1)
			slog.Error("Kafka offset commit failed", "topic", message.Topic, "partition", message.Partition, "offset", message.Offset, "error", err)
			return
		}
	}
}

// Shutdown stops fetching new records, then waits for records already fetched
// by workers to finish processing and commit their offsets. If the deadline
// expires, it cancels those operations and waits for them to exit before
// closing the readers.
func (p *WorkerPool) Shutdown(ctx context.Context) error {
	if !p.started {
		return p.Stop()
	}
	p.fetchCancel()
	var shutdownErr error
	select {
	case <-p.done:
	case <-ctx.Done():
		shutdownErr = ctx.Err()
		p.processCancel()
		<-p.done
	}
	p.processCancel()
	for _, reader := range p.readers {
		if err := reader.Close(); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	if err := p.closeDeadLetter(); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	return shutdownErr
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
	var closeErr error
	p.stopOnce.Do(func() {
		if p.fetchCancel != nil {
			p.fetchCancel()
		}
		if p.processCancel != nil {
			p.processCancel()
		}
		for _, reader := range p.readers {
			if err := reader.Close(); err != nil {
				closeErr = errors.Join(closeErr, err)
			}
		}
		if p.started {
			<-p.done
		}
		closeErr = errors.Join(closeErr, p.closeDeadLetter())
	})
	return closeErr
}

func (p *WorkerPool) closeDeadLetter() error {
	return p.deadLetter.Close()
}
