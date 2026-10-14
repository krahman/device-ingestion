package ingestion

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"device-ingestion/internal/models"

	"github.com/nats-io/nats.go"
)

type WorkerPool struct {
	nc         *nats.Conn
	maxWorkers int
	semaphore  chan struct{}
	sub        *nats.Subscription
}

func NewWorkerPool(nc *nats.Conn, maxWorkers int) *WorkerPool {
	return &WorkerPool{
		nc:         nc,
		maxWorkers: maxWorkers,
		semaphore:  make(chan struct{}, maxWorkers),
	}
}

func (wp *WorkerPool) Start(ctx context.Context) error {
	var err error
	wp.sub, err = wp.nc.QueueSubscribe("device.telemetry", "telemetry-workers", func(msg *nats.Msg) {
		select {
		case wp.semaphore <- struct{}{}:
		case <-ctx.Done():
			return
		}

		go func() {
			defer func() {
				<-wp.semaphore
			}()

			var packet models.TelemetryPacket
			if err := json.Unmarshal(msg.Data, &packet); err != nil {
				log.Printf("Failed to decode packet: %v", err)
				_ = msg.Nak()
				return
			}

			if err := saveToDatabase(ctx, packet); err != nil {
				log.Printf("Database write failed for device %s: %v", packet.DeviceID, err)
				_ = msg.Nak()
				return
			}

			_ = msg.Ack()
		}()
	})

	return err
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

func (wp *WorkerPool) Stop() {
	if wp.sub != nil {
		_ = wp.sub.Unsubscribe()
	}
}
