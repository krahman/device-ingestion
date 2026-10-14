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
	"time"

	kafkaadapter "device-ingestion/internal/adapters/kafka"

	"google.golang.org/grpc"
)

type runtime struct {
	config    Config
	db        *sql.DB
	publisher *kafkaadapter.Publisher
	workers   *kafkaadapter.WorkerPool
	services  []service
}

func (r *runtime) Run(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	r.workers.Start(workerCtx)
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

	cancelWorkers()
	if err := r.workers.Stop(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("stop Kafka workers: %w", err))
	}
	if err := r.publisher.Close(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close Kafka publisher: %w", err))
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
	for i := len(cleanup) - 1; i >= 0; i-- {
		if err := cleanup[i](); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return closeErrors
}
