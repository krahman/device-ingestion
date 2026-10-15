package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"time"

	kafkaadapter "device-ingestion/internal/adapters/kafka"

	"google.golang.org/grpc"
)

type runtime struct {
	config            Config
	db                *sql.DB
	publisher         *kafkaadapter.Publisher
	workers           *kafkaadapter.WorkerPool
	chargingPublisher *kafkaadapter.ChargingEventPublisher
	chargingWorkers   *kafkaadapter.ChargingEventWorkerPool
	services          []service
}

func (r *runtime) Run(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	// Worker cancellation is controlled by the shutdown sequence below so a
	// signal can stop ingress before canceling in-flight Kafka processing.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	r.workers.Start(workerCtx)
	r.chargingWorkers.Start(workerCtx)
	defer cancelWorkers()

	serveErrors := make(chan error, len(r.services))
	for _, current := range r.services {
		current := current
		go func() { reportServe(current.name, current.serve(), serveErrors) }()
		slog.Info("service listener started", "service", current.name, "address", current.address)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serveErrors:
	}

	shutdownLimit := r.config.ShutdownLimit
	if shutdownLimit <= 0 {
		shutdownLimit = 10 * time.Second
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownLimit)
	defer cancelShutdown()
	for _, current := range r.services {
		if err := current.shutdown(shutdownCtx); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shut down %s server: %w", current.name, err))
		}
	}

	if err := r.workers.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("stop Kafka workers: %w", err))
	}
	if err := r.chargingWorkers.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("stop charging event Kafka workers: %w", err))
	}
	if err := r.publisher.Close(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close Kafka publisher: %w", err))
	}
	if err := r.chargingPublisher.Close(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close charging event Kafka publisher: %w", err))
	}
	if err := r.db.Close(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close PostgreSQL connection pool: %w", err))
	}
	return runErr
}

func reportServe(name string, err error, errorsOut chan<- error) {
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		errorsOut <- fmt.Errorf("%s server: %w", name, err)
	}
}

func stopGRPC(ctx context.Context, server *grpc.Server) error {
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		server.Stop()
		<-stopped
		return ctx.Err()
	}
}

func closeReverse(cleanup []func() error) []error {
	var closeErrors []error
	for _, c := range slices.Backward(cleanup) {
		if err := c(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return closeErrors
}
