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
	"device-ingestion/internal/adapters/ocpiapi"
	"device-ingestion/internal/adapters/ocppapi"
	"device-ingestion/internal/adapters/persistence/postgres"
	"device-ingestion/internal/application"
	"device-ingestion/internal/observability"
	pb "device-ingestion/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type service struct {
	name     string
	address  string
	serve    func() error
	shutdown func(context.Context) error
}

// newRuntime wires adapters and transfers their resource ownership to runtime.
func newRuntime(config Config) (_ *runtime, runErr error) {
	if config.Profile == ProfileSandbox {
		return newSandboxRuntime(config)
	}
	if config.Profile != "" && config.Profile != ProfileFull {
		return nil, fmt.Errorf("unsupported application profile %q", config.Profile)
	}
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
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.PingContext(startupCtx); err != nil {
		cancelStartup()
		return nil, fmt.Errorf("check PostgreSQL readiness: %w", err)
	}
	if err := kafkaadapter.CheckBrokers(startupCtx, config.KafkaBrokers, config.KafkaSecurity); err != nil {
		cancelStartup()
		return nil, fmt.Errorf("check Kafka readiness: %w", err)
	}
	cancelStartup()
	repository := postgres.NewRepository(db)

	publisher, err := kafkaadapter.NewPublisher(config.KafkaBrokers, kafkaadapter.TelemetryTopic, metrics, config.KafkaSecurity)
	if err != nil {
		return nil, fmt.Errorf("configure Kafka publisher: %w", err)
	}
	cleanup = append(cleanup, publisher.Close)

	chargingPublisher, err := kafkaadapter.NewChargingEventPublisher(config.KafkaBrokers, metrics, config.KafkaSecurity)
	if err != nil {
		return nil, fmt.Errorf("configure charging event publisher: %w", err)
	}
	cleanup = append(cleanup, chargingPublisher.Close)

	processor := application.NewProcessTelemetry(repository)
	workers, err := kafkaadapter.NewWorkerPool(config.KafkaBrokers, kafkaadapter.TelemetryTopic, "telemetry-db-writers", config.KafkaWorkers, processor, metrics, config.KafkaSecurity)
	if err != nil {
		return nil, fmt.Errorf("configure Kafka workers: %w", err)
	}
	cleanup = append(cleanup, workers.Stop)
	chargingProcessor := application.NewProcessChargingEvent(repository)
	chargingWorkers, err := kafkaadapter.NewChargingEventWorkerPool(config.KafkaBrokers, config.KafkaWorkers, chargingProcessor, metrics, config.KafkaSecurity)
	if err != nil {
		return nil, fmt.Errorf("configure charging event workers: %w", err)
	}
	cleanup = append(cleanup, chargingWorkers.Stop)

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
	var ocppListener net.Listener
	if len(config.OCPPStations) > 0 {
		ocppListener, err = listen("OCPP", config.OCPPAddress)
		if err != nil {
			return nil, err
		}
	}

	ingestor := application.NewIngestTelemetry(publisher)
	chargingIngestor := application.NewIngestChargingEvent(chargingPublisher)
	chargingStations := make([]ocppapi.StationConfig, 0, len(config.OCPPStations))
	ocpiStations := make([]ocpiapi.StationConfig, 0, len(config.OCPPStations))
	for _, station := range config.OCPPStations {
		chargingStations = append(chargingStations, ocppapi.StationConfig{ChargingStation: station.ChargingStation, Password: station.Password, CPOCountryCode: config.OCPICountryCode, CPOPartyID: config.OCPIPartyID})
		ocpiStations = append(ocpiStations, ocpiapi.StationConfig{ChargingStation: station.ChargingStation})
	}
	ocpiPartners := make([]ocpiapi.Partner, 0, len(config.OCPIEMSPPartners))
	for _, partner := range config.OCPIEMSPPartners {
		ocpiPartners = append(ocpiPartners, ocpiapi.Partner{CountryCode: partner.CountryCode, PartyID: partner.PartyID, Token: partner.Token})
	}
	ocpiHandler := ocpiapi.NewHandler(config.OCPICountryCode, config.OCPIPartyID, config.OCPIBaseURL, ocpiStations, ocpiPartners, repository, metrics)
	ocppHandler := ocppapi.NewHandler(chargingIngestor, metrics, chargingStations, config.OCPPMaxConnections, config.OCPPMaxFrameBytes)
	httpServer := newHTTPServer(ingestor, metrics, db, ocpiHandler, func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return err
		}
		return kafkaadapter.CheckBrokers(ctx, config.KafkaBrokers, config.KafkaSecurity)
	})
	ocppServer := &http.Server{Handler: ocppHandler, ReadHeaderTimeout: 5 * time.Second}
	grpcServer := newGRPCServer(ingestor, metrics, false)
	pprofServer := newPprofServer()

	services := []service{
		{name: "HTTP", address: config.HTTPAddress, serve: func() error { return httpServer.Serve(httpListener) }, shutdown: httpServer.Shutdown},
		{name: "gRPC", address: config.GRPCAddress, serve: func() error { return grpcServer.Serve(grpcListener) }, shutdown: func(ctx context.Context) error { return stopGRPC(ctx, grpcServer) }},
		{name: "pprof", address: config.PPROFAddress, serve: func() error { return pprofServer.Serve(pprofListener) }, shutdown: pprofServer.Shutdown},
	}
	if ocppListener != nil {
		services = append(services, service{name: "OCPP", address: config.OCPPAddress, serve: func() error { return ocppServer.Serve(ocppListener) }, shutdown: ocppServer.Shutdown})
	}
	return &runtime{
		config:            config,
		db:                db,
		publisher:         publisher,
		workers:           workers,
		chargingPublisher: chargingPublisher,
		chargingWorkers:   chargingWorkers,
		services:          services,
	}, nil
}

func newHTTPServer(ingestor *application.IngestTelemetry, metrics *observability.Metrics, db *sql.DB, ocpi http.Handler, ready func(context.Context) error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		writeDependencyHealth(w, r, ready, "ready", "not ready")
	})
	mux.HandleFunc("GET /health/dependencies", func(w http.ResponseWriter, r *http.Request) {
		writeDependencyHealth(w, r, ready, "dependencies ready", "dependencies unavailable")
	})
	mux.Handle("/telemetry", httpapi.NewHandler(ingestor, metrics))
	mux.Handle("/ocpi/", ocpi)
	mux.Handle("/metrics", metrics.Handler(func() (open, inUse, idle int) {
		stats := db.Stats()
		return stats.OpenConnections, stats.InUse, stats.Idle
	}))
	return &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
}

func newSandboxRuntime(config Config) (_ *runtime, runErr error) {
	if config.HTTPAddress == "" {
		config.HTTPAddress = ":8080"
	}
	if config.GRPCAddress == "" {
		config.GRPCAddress = ":50051"
	}
	if config.ShutdownLimit <= 0 {
		config.ShutdownLimit = 10 * time.Second
	}

	var cleanup []func() error
	defer func() {
		if runErr != nil {
			runErr = errors.Join(append([]error{runErr}, closeReverse(cleanup)...)...)
		}
	}()
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
	metrics := observability.NewMetrics()
	httpServer := newSandboxHTTPServer()
	grpcServer := newGRPCServer(nil, metrics, true)
	services := []service{
		{name: "HTTP", address: config.HTTPAddress, serve: func() error { return httpServer.Serve(httpListener) }, shutdown: httpServer.Shutdown},
		{name: "gRPC", address: config.GRPCAddress, serve: func() error { return grpcServer.Serve(grpcListener) }, shutdown: func(ctx context.Context) error { return stopGRPC(ctx, grpcServer) }},
	}
	return &runtime{config: config, services: services}, nil
}

func newSandboxHTTPServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready: sandbox health-only profile"))
	})
	mux.HandleFunc("GET /health/dependencies", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "PostgreSQL and Kafka are disabled in sandbox profile", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "operation unavailable in sandbox profile", http.StatusServiceUnavailable)
	})
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func writeDependencyHealth(w http.ResponseWriter, r *http.Request, ready func(context.Context) error, success, failure string) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := ready(ctx); err != nil {
		http.Error(w, failure, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(success))
}

func newGRPCServer(ingestor *application.IngestTelemetry, metrics *observability.Metrics, sandbox bool) *grpc.Server {
	server := grpc.NewServer(grpc.MaxRecvMsgSize(1 << 20))
	var ingestionServer *grpcapi.Server
	if sandbox {
		ingestionServer = grpcapi.NewUnavailableServer(metrics)
	} else {
		ingestionServer = grpcapi.NewServer(ingestor, metrics)
	}
	pb.RegisterIngestionServiceServer(server, ingestionServer)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	if sandbox {
		healthServer.SetServingStatus("ingestion.IngestionService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	} else {
		healthServer.SetServingStatus("ingestion.IngestionService", grpc_health_v1.HealthCheckResponse_SERVING)
	}
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	return server
}
