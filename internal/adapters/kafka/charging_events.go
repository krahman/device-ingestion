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

const ChargingEventsTopic = "charging.events"

type chargingEventProcessor interface {
	Execute(context.Context, domain.ChargingEvent) error
}

type ChargingEventPublisher struct {
	writer  *kafkago.Writer
	metrics *observability.Metrics
}

func NewChargingEventPublisher(brokers []string, metrics *observability.Metrics) (*ChargingEventPublisher, error) {
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return nil, err
	}
	return &ChargingEventPublisher{
		writer:  &kafkago.Writer{Addr: kafkago.TCP(addresses...), Balancer: &kafkago.Hash{}, BatchSize: 100, BatchBytes: maxFetchBytes, BatchTimeout: 10 * time.Millisecond, RequiredAcks: kafkago.RequireAll},
		metrics: metrics,
	}, nil
}

func (p *ChargingEventPublisher) PublishChargingEvent(ctx context.Context, event domain.ChargingEvent) error {
	started := time.Now()
	result := "success"
	defer func() {
		p.metrics.AddCounter("device_ingestion_charging_events_published_total", map[string]string{"protocol": event.SourceProtocol, "result": result}, 1)
		p.metrics.Observe("device_ingestion_charging_event_publish_duration_seconds", map[string]string{"protocol": event.SourceProtocol}, time.Since(started))
	}()
	data, err := json.Marshal(event)
	if err != nil {
		result = "error"
		return err
	}
	err = p.writer.WriteMessages(ctx, kafkago.Message{Topic: ChargingEventsTopic, Key: []byte(event.StationID), Value: data})
	if err != nil {
		result = "error"
		slog.Error("charging event publish failed", "topic", ChargingEventsTopic, "event_type", event.EventType, "error", err)
	}
	return err
}

func (p *ChargingEventPublisher) Close() error { return p.writer.Close() }

type ChargingEventWorkerPool struct {
	readers    []*kafkago.Reader
	deadLetter *kafkago.Writer
	processor  chargingEventProcessor
	metrics    *observability.Metrics
	wg         sync.WaitGroup
	fetchStop  context.CancelFunc
	workStop   context.CancelFunc
	done       chan struct{}
	started    bool
	stopOnce   sync.Once
}

func NewChargingEventWorkerPool(brokers []string, workerCount int, processor chargingEventProcessor, metrics *observability.Metrics) (*ChargingEventWorkerPool, error) {
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return nil, err
	}
	if workerCount < 1 || workerCount > maxWorkerCount {
		return nil, errors.New("charging Kafka worker count must be between 1 and 32")
	}
	pool := &ChargingEventWorkerPool{
		readers:    make([]*kafkago.Reader, 0, workerCount),
		deadLetter: &kafkago.Writer{Addr: kafkago.TCP(addresses...), Balancer: &kafkago.Hash{}, RequiredAcks: kafkago.RequireAll},
		processor:  processor, metrics: metrics,
	}
	for range workerCount {
		pool.readers = append(pool.readers, kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: addresses, Topic: ChargingEventsTopic, GroupID: "charging-db-writers",
			StartOffset: kafkago.FirstOffset, MinBytes: 1, QueueCapacity: 1, MaxBytes: maxFetchBytes,
		}))
	}
	return pool, nil
}

func (p *ChargingEventWorkerPool) Start(ctx context.Context) {
	workCtx, workStop := context.WithCancel(ctx)
	fetchCtx, fetchStop := context.WithCancel(workCtx)
	p.workStop, p.fetchStop, p.done, p.started = workStop, fetchStop, make(chan struct{}), true
	for _, reader := range p.readers {
		p.wg.Add(1)
		go p.consume(fetchCtx, workCtx, reader)
	}
	go func() { p.wg.Wait(); close(p.done) }()
}

func (p *ChargingEventWorkerPool) consume(fetchCtx, workCtx context.Context, reader *kafkago.Reader) {
	defer p.wg.Done()
	for {
		message, err := reader.FetchMessage(fetchCtx)
		if err != nil {
			if errors.Is(err, context.Canceled) || fetchCtx.Err() != nil {
				return
			}
			p.metrics.AddCounter("device_ingestion_charging_worker_errors_total", map[string]string{"stage": "fetch"}, 1)
			if !waitForRetry(fetchCtx) {
				return
			}
			continue
		}
		var event domain.ChargingEvent
		err = json.Unmarshal(message.Value, &event)
		protocol := eventProtocolLabel(event.SourceProtocol)
		if err == nil {
			started := time.Now()
			err = p.processor.Execute(workCtx, event)
			p.metrics.Observe("device_ingestion_charging_persistence_duration_seconds", map[string]string{"protocol": protocol}, time.Since(started))
		}
		if err != nil {
			if isPermanentChargingEventError(err) {
				if !p.writeDLQ(workCtx, message, err, protocol) {
					return
				}
			} else {
				p.metrics.AddCounter("device_ingestion_charging_events_processed_total", map[string]string{"protocol": protocol, "result": "retry"}, 1)
				if !waitForRetry(workCtx) {
					return
				}
				continue
			}
		} else {
			p.metrics.AddCounter("device_ingestion_charging_events_processed_total", map[string]string{"protocol": protocol, "result": "success"}, 1)
		}
		if err := reader.CommitMessages(workCtx, message); err != nil {
			p.metrics.AddCounter("device_ingestion_charging_worker_errors_total", map[string]string{"stage": "offset_commit"}, 1)
			return
		}
	}
}

func isPermanentChargingEventError(err error) bool {
	var invalid *domain.ValidationError
	var conflict *domain.ChargingEventConflictError
	return errors.As(err, &invalid) || errors.As(err, &conflict) || jsonSyntaxError(err)
}

func jsonSyntaxError(err error) bool {
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &typeErr)
}

func eventProtocolLabel(protocol string) string {
	if protocol == "ocpp" {
		return "ocpp"
	}
	return "unknown"
}

func (p *ChargingEventWorkerPool) writeDLQ(ctx context.Context, message kafkago.Message, cause error, protocol string) bool {
	for {
		err := p.deadLetter.WriteMessages(ctx, deadLetterMessage(message, cause))
		if err == nil {
			p.metrics.AddCounter("device_ingestion_charging_events_dlq_total", map[string]string{"protocol": protocol, "result": "dead_letter"}, 1)
			return true
		}
		p.metrics.AddCounter("device_ingestion_charging_worker_errors_total", map[string]string{"stage": "dlq_write"}, 1)
		if !waitForRetry(ctx) {
			return false
		}
	}
}

func deadLetterMessage(message kafkago.Message, cause error) kafkago.Message {
	return kafkago.Message{
		Topic: ChargingEventsTopic + ".dlq", Key: message.Key, Value: message.Value,
		Headers: []kafkago.Header{
			{Key: "source-topic", Value: []byte(message.Topic)},
			{Key: "source-partition", Value: []byte(strconv.Itoa(message.Partition))},
			{Key: "source-offset", Value: []byte(strconv.FormatInt(message.Offset, 10))},
			{Key: "failure", Value: []byte(cause.Error())},
		},
	}
}

func (p *ChargingEventWorkerPool) Shutdown(ctx context.Context) error {
	if !p.started {
		return p.Stop()
	}
	p.fetchStop()
	var shutdownErr error
	select {
	case <-p.done:
	case <-ctx.Done():
		shutdownErr = ctx.Err()
		p.workStop()
		<-p.done
	}
	p.workStop()
	for _, reader := range p.readers {
		if err := reader.Close(); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	if err := p.deadLetter.Close(); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	return shutdownErr
}

func (p *ChargingEventWorkerPool) Stop() error {
	var stopErr error
	p.stopOnce.Do(func() {
		if p.fetchStop != nil {
			p.fetchStop()
		}
		if p.workStop != nil {
			p.workStop()
		}
		for _, reader := range p.readers {
			stopErr = errors.Join(stopErr, reader.Close())
		}
		if p.started {
			<-p.done
		}
		stopErr = errors.Join(stopErr, p.deadLetter.Close())
	})
	return stopErr
}
