package app

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"device-ingestion/internal/observability"
	pb "device-ingestion/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestConfigFromEnvSandboxDoesNotRequireDependencies(t *testing.T) {
	t.Setenv("APP_PROFILE", ProfileSandbox)
	t.Setenv("DATABASE_URL", "not-a-database-url")
	t.Setenv("DATABASE_TLS_VERIFY", "not-a-boolean")
	t.Setenv("KAFKA_TLS", "not-a-boolean")
	t.Setenv("KAFKA_SASL_MECHANISM", "invalid")
	t.Setenv("OCPP_STATIONS", "not-json")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config.Profile != ProfileSandbox {
		t.Fatalf("Profile = %q, want %q", config.Profile, ProfileSandbox)
	}
	if config.DatabaseURL != "" || len(config.KafkaBrokers) != 0 || len(config.OCPPStations) != 0 {
		t.Fatalf("sandbox config contains dependency settings: %#v", config)
	}
}

func TestConfigFromEnvRejectsUnknownProfile(t *testing.T) {
	t.Setenv("APP_PROFILE", "typo")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv() unexpectedly accepted an unknown profile")
	}
}

func TestSandboxGRPCHealthAndIngestionUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := newGRPCServer(nil, observability.NewMetrics(), true)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	defer conn.Close()

	healthClient := grpc_health_v1.NewHealthClient(conn)
	processHealth, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("process health check error = %v", err)
	}
	if processHealth.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("process health status = %v, want SERVING", processHealth.Status)
	}
	ingestionHealth, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "ingestion.IngestionService"})
	if err != nil {
		t.Fatalf("ingestion health check error = %v", err)
	}
	if ingestionHealth.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("ingestion health status = %v, want NOT_SERVING", ingestionHealth.Status)
	}

	response, err := pb.NewIngestionServiceClient(conn).IngestTelemetry(ctx, &pb.TelemetryRequest{})
	if response != nil {
		t.Fatalf("sandbox ingestion response = %#v, want nil", response)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("sandbox ingestion status = %v, want Unavailable (error %v)", status.Code(err), err)
	}
}

func TestSandboxRuntimeStartsWithoutDependenciesAndShutsDown(t *testing.T) {
	httpAddress := unusedTCPAddress(t)
	grpcAddress := unusedTCPAddress(t)
	runtime, err := newRuntime(Config{Profile: ProfileSandbox, HTTPAddress: httpAddress, GRPCAddress: grpcAddress})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if runtime.db != nil || runtime.publisher != nil || runtime.workers != nil || runtime.chargingPublisher != nil || runtime.chargingWorkers != nil {
		t.Fatal("sandbox runtime initialized database or Kafka components")
	}
	if len(runtime.services) != 2 || runtime.services[0].name != "HTTP" || runtime.services[1].name != "gRPC" {
		t.Fatalf("sandbox services = %#v, want HTTP and gRPC only", runtime.services)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- runtime.Run(ctx) }()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(time.Second)
	for {
		response, requestErr := client.Get("http://" + httpAddress + "/health/live")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				cancel()
				t.Fatalf("health status = %d, want 200", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("sandbox HTTP listener did not start: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("runtime.Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sandbox runtime did not shut down promptly")
	}
}

func unusedTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
