// Package app assembles the application's ports and adapters and manages
// their lifecycle. It is the composition root for the executable.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"device-ingestion/internal/adapters/grpcapi"
	"device-ingestion/internal/adapters/httpapi"
	kafkaadapter "device-ingestion/internal/adapters/kafka"
	"device-ingestion/internal/adapters/persistence/postgres"
	"device-ingestion/internal/application"
	"device-ingestion/internal/observability"
	pb "device-ingestion/proto"

	"google.golang.org/grpc"
)

type Config struct {
	KafkaBrokers  []string
	KafkaWorkers  int
	DBMaxOpen     int
	DatabaseURL   string
	HTTPAddress   string
	GRPCAddress   string
	ShutdownLimit time.Duration
}

func ConfigFromEnv() (Config, error) {
	workers, err := intEnv("KAFKA_DB_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	if workers < 1 || workers > 32 {
		return Config{}, fmt.Errorf("KAFKA_DB_WORKERS must be between 1 and 32")
	}
	dbMaxOpen, err := intEnv("DB_MAX_OPEN_CONNS", min(workers, 16))
	if err != nil {
		return Config{}, err
	}
	if dbMaxOpen < 1 || dbMaxOpen > 32 {
		return Config{}, fmt.Errorf("DB_MAX_OPEN_CONNS must be between 1 and 32")
	}

	return Config{
		KafkaBrokers:  strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ","),
		KafkaWorkers:  workers,
		DBMaxOpen:     dbMaxOpen,
		DatabaseURL:   DatabaseURLFromEnv(),
		HTTPAddress:   env("HTTP_ADDRESS", ":8080"),
		GRPCAddress:   env("GRPC_ADDRESS", ":50051"),
		ShutdownLimit: 10 * time.Second,
	}, nil
}

func DatabaseURLFromEnv() string {
	return env("DATABASE_URL", "postgres://device_ingestion:device_ingestion@127.0.0.1:5432/device_ingestion?sslmode=disable")
}

func Run(ctx context.Context, config Config) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	metrics := observability.NewMetrics()
	db, err := postgres.Open(config.DatabaseURL, config.DBMaxOpen)
	if err != nil {
		return err
	}
	defer db.Close()

	publisher, err := kafkaadapter.NewPublisher(config.KafkaBrokers, kafkaadapter.TelemetryTopic, metrics)
	if err != nil {
		return fmt.Errorf("configure Kafka publisher: %w", err)
	}

	processor := application.NewProcessTelemetry(postgres.NewRepository(db))
	workers, err := kafkaadapter.NewWorkerPool(config.KafkaBrokers, kafkaadapter.TelemetryTopic, "telemetry-db-writers", config.KafkaWorkers, processor, metrics)
	if err != nil {
		return errors.Join(fmt.Errorf("configure Kafka workers: %w", err), publisher.Close())
	}

	httpListener, err := net.Listen("tcp", config.HTTPAddress)
	if err != nil {
		return errors.Join(fmt.Errorf("listen for HTTP on %s: %w", config.HTTPAddress, err), publisher.Close(), workers.Stop())
	}
	grpcListener, err := net.Listen("tcp", config.GRPCAddress)
	if err != nil {
		return errors.Join(fmt.Errorf("listen for gRPC on %s: %w", config.GRPCAddress, err), httpListener.Close(), publisher.Close(), workers.Stop())
	}

	ingestor := application.NewIngestTelemetry(publisher)
	httpMux := http.NewServeMux()
	httpMux.Handle("/telemetry", httpapi.NewHandler(ingestor, metrics))
	httpMux.Handle("/metrics", metrics.Handler(func() (open, inUse, idle int) {
		stats := db.Stats()
		return stats.OpenConnections, stats.InUse, stats.Idle
	}))
	httpServer := &http.Server{
		Handler:      httpMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(1 << 20))
	pb.RegisterIngestionServiceServer(grpcServer, grpcapi.NewServer(ingestor, metrics))

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	workers.Start(workerCtx)
	defer cancelWorkers()

	serveErrors := make(chan error, 2)
	go func() { serve("HTTP", httpServer.Serve(httpListener), serveErrors) }()
	go func() { serve("gRPC", grpcServer.Serve(grpcListener), serveErrors) }()
	slog.Info("HTTP ingestion gateway listening", "address", config.HTTPAddress)
	slog.Info("gRPC ingestion server listening", "address", config.GRPCAddress)
	slog.Info("Prometheus metrics endpoint listening", "address", config.HTTPAddress, "path", "/metrics")

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serveErrors:
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), config.ShutdownLimit)
	defer cancelShutdown()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("shut down HTTP server: %w", err))
	}
	if err := stopGRPC(shutdownCtx, grpcServer); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("shut down gRPC server: %w", err))
	}
	cancelWorkers()
	if err := workers.Stop(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("stop Kafka workers: %w", err))
	}
	if err := publisher.Close(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close Kafka publisher: %w", err))
	}
	return runErr
}

func serve(name string, err error, errorsOut chan<- error) {
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

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return value, nil
}
