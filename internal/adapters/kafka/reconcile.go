package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"device-ingestion/internal/domain"

	kafkago "github.com/segmentio/kafka-go"
)

const maxReconcileWorkers = 32

type ReconcileStats struct {
	Replayed int64
	Skipped  int64
}

type partitionWindow struct {
	partition int
	first     int64
	end       int64
}

// ReplayTopic replays the retained Kafka topic through the idempotent
// telemetry processor. It does not change the production consumer group's
// committed offsets.
func ReplayTopic(ctx context.Context, brokers []string, topic string, workerCount int, processor telemetryProcessor) (ReconcileStats, error) {
	var stats ReconcileStats
	if workerCount < 1 || workerCount > maxReconcileWorkers {
		return stats, fmt.Errorf("reconciliation worker count must be between 1 and %d", maxReconcileWorkers)
	}
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return stats, err
	}

	client := &kafkago.Client{Addr: kafkago.TCP(addresses[0])}
	metadata, err := client.Metadata(ctx, &kafkago.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return stats, fmt.Errorf("read Kafka topic metadata: %w", err)
	}
	var topicMetadata *kafkago.Topic
	for i := range metadata.Topics {
		if metadata.Topics[i].Name == topic {
			topicMetadata = &metadata.Topics[i]
			break
		}
	}
	if topicMetadata == nil {
		return stats, fmt.Errorf("Kafka topic %q was not found", topic)
	}
	if topicMetadata.Error != nil {
		return stats, fmt.Errorf("read Kafka topic %q metadata: %w", topic, topicMetadata.Error)
	}
	if len(topicMetadata.Partitions) == 0 {
		return stats, fmt.Errorf("Kafka topic %q has no partitions", topic)
	}

	requests := make([]kafkago.OffsetRequest, 0, len(topicMetadata.Partitions)*2)
	for _, partition := range topicMetadata.Partitions {
		if partition.Error != nil {
			return stats, fmt.Errorf("read Kafka partition %d metadata: %w", partition.ID, partition.Error)
		}
		requests = append(requests, kafkago.FirstOffsetOf(partition.ID), kafkago.LastOffsetOf(partition.ID))
	}
	offsetResponse, err := client.ListOffsets(ctx, &kafkago.ListOffsetsRequest{
		Topics:         map[string][]kafkago.OffsetRequest{topic: requests},
		IsolationLevel: kafkago.ReadCommitted,
	})
	if err != nil {
		return stats, fmt.Errorf("read Kafka topic offsets: %w", err)
	}
	offsetByPartition := make(map[int]kafkago.PartitionOffsets, len(requests))
	for _, offsets := range offsetResponse.Topics[topic] {
		if offsets.Error != nil {
			return stats, fmt.Errorf("read Kafka partition %d offsets: %w", offsets.Partition, offsets.Error)
		}
		offsetByPartition[offsets.Partition] = offsets
	}

	windows := make([]partitionWindow, 0, len(topicMetadata.Partitions))
	for _, partition := range topicMetadata.Partitions {
		offsets, ok := offsetByPartition[partition.ID]
		if !ok {
			return stats, fmt.Errorf("kafka offsets missing for partition %d", partition.ID)
		}
		if offsets.FirstOffset < offsets.LastOffset {
			windows = append(windows, partitionWindow{partition: partition.ID, first: offsets.FirstOffset, end: offsets.LastOffset})
		}
	}
	if len(windows) == 0 {
		return stats, nil
	}

	replayCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan partitionWindow, workerCount)
	errCh := make(chan error, workerCount)
	var replayed atomic.Int64
	var skipped atomic.Int64
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for window := range jobs {
				count, skippedCount, err := replayPartition(replayCtx, addresses, topic, window, processor)
				replayed.Add(count)
				skipped.Add(skippedCount)
				if err != nil {
					errCh <- fmt.Errorf("replay partition %d: %w", window.partition, err)
					cancel()
					return
				}
			}
		}()
	}

enqueue:
	for _, window := range windows {
		select {
		case jobs <- window:
		case <-replayCtx.Done():
			break enqueue
		}
	}
	close(jobs)
	workers.Wait()
	close(errCh)
	var replayErrors []error
	for err := range errCh {
		replayErrors = append(replayErrors, err)
	}
	if ctx.Err() != nil {
		replayErrors = append(replayErrors, ctx.Err())
	}
	stats = ReconcileStats{Replayed: replayed.Load(), Skipped: skipped.Load()}
	return stats, errors.Join(replayErrors...)
}

func replayPartition(ctx context.Context, brokers []string, topic string, window partitionWindow, processor telemetryProcessor) (int64, int64, error) {
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		Partition:      window.partition,
		QueueCapacity:  1,
		MinBytes:       1,
		MaxBytes:       maxFetchBytes,
		IsolationLevel: kafkago.ReadCommitted,
	})
	defer reader.Close()
	if err := reader.SetOffset(window.first); err != nil {
		return 0, 0, fmt.Errorf("set starting offset %d: %w", window.first, err)
	}

	var replayed, skipped int64
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			return replayed, skipped, fmt.Errorf("fetch Kafka message: %w", err)
		}
		if message.Offset >= window.end {
			return replayed, skipped, nil
		}
		var packet domain.TelemetryPacket
		if err := json.Unmarshal(message.Value, &packet); err != nil {
			skipped++
			continue
		}
		if err := processor.Execute(ctx, packet); err != nil {
			var validationErr *domain.ValidationError
			var idempotencyErr *domain.IdempotencyConflictError
			if errors.As(err, &validationErr) || errors.As(err, &idempotencyErr) {
				skipped++
				continue
			}
			return replayed, skipped, fmt.Errorf("persist offset %d: %w", message.Offset, err)
		}
		replayed++
	}
}
