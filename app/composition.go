package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
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

type service struct {
	name     string
	address  string
	serve    func() error
	shutdown func(context.Context) error
}

// newRuntime wires adapters and transfers their resource ownership to runtime.
func newRuntime(config Config) (_ *runtime, runErr error) {
	var cleanup []func() error
	defer func() {
		if runErr != nil {
			runErr = errors.Join(append([]error{runErr}, closeReverse(cleanup)...)...)
		}
	}()

	metrics := observability.NewMetrics()
	db, err := postgres.Open(config.DatabaseURL, config.DBMaxOpen)
	if err != nil {
		return nil, err
	}
	cleanup = append(cleanup, db.Close)

	publisher, err := kafkaadapter.NewPublisher(config.KafkaBrokers, kafkaadapter.TelemetryTopic, metrics)
	if err != nil {
		return nil, fmt.Errorf("configure Kafka publisher: %w", err)
	}
	cleanup = append(cleanup, publisher.Close)

	processor := application.NewProcessTelemetry(postgres.NewRepository(db))
	workers, err := kafkaadapter.NewWorkerPool(config.KafkaBrokers, kafkaadapter.TelemetryTopic, "telemetry-db-writers", config.KafkaWorkers, processor, metrics)
	if err != nil {
		return nil, fmt.Errorf("configure Kafka workers: %w", err)
	}
	cleanup = append(cleanup, workers.Stop)

	listen := func(name, address string) (net.Listener, error) {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("listen for %s on %s: %w", name, address, err)
		}
		cleanup = append(cleanup, listener.Close)
		return listener, nil
	}

	httpListener, err := listen("HTTP", config.HTTPAddress)
	if err != nil {
		return nil, err
	}
	grpcListener, err := listen("gRPC", config.GRPCAddress)
	if err != nil {
		return nil, err
	}
	pprofListener, err := listen("pprof", config.PPROFAddress)
	if err != nil {
		return nil, err
	}

	ingestor := application.NewIngestTelemetry(publisher)
	httpServer := newHTTPServer(ingestor, metrics, db)
	grpcServer := newGRPCServer(ingestor, metrics)
	pprofServer := newPprofServer()

	return &runtime{
		config:    config,
		db:        db,
		publisher: publisher,
		workers:   workers,
		services: []service{
			{name: "HTTP", address: config.HTTPAddress, serve: func() error { return httpServer.Serve(httpListener) }, shutdown: httpServer.Shutdown},
			{name: "gRPC", address: config.GRPCAddress, serve: func() error { return grpcServer.Serve(grpcListener) }, shutdown: func(ctx context.Context) error { return stopGRPC(ctx, grpcServer) }},
			{name: "pprof", address: config.PPROFAddress, serve: func() error { return pprofServer.Serve(pprofListener) }, shutdown: pprofServer.Shutdown},
		},
	}, nil
}

func newHTTPServer(ingestor *application.IngestTelemetry, metrics *observability.Metrics, db *sql.DB) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/telemetry", httpapi.NewHandler(ingestor, metrics))
	mux.Handle("/metrics", metrics.Handler(func() (open, inUse, idle int) {
		stats := db.Stats()
		return stats.OpenConnections, stats.InUse, stats.Idle
	}))
	return &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
}

func newGRPCServer(ingestor *application.IngestTelemetry, metrics *observability.Metrics) *grpc.Server {
	server := grpc.NewServer(grpc.MaxRecvMsgSize(1 << 20))
	pb.RegisterIngestionServiceServer(server, grpcapi.NewServer(ingestor, metrics))
	return server
}
