package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"device-ingestion/internal/ingestion"
	pb "device-ingestion/proto"

	"google.golang.org/grpc"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kafkaBrokers := strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ",")
	kafkaPublisher, err := ingestion.NewKafkaPublisher(kafkaBrokers)
	if err != nil {
		log.Fatalf("Failed to configure Kafka publisher: %v", err)
	}
	defer kafkaPublisher.Close()

	workers, err := ingestion.NewKafkaWorkerPool(kafkaBrokers, ingestion.TelemetryTopic, "telemetry-db-writers", intEnv("KAFKA_DB_WORKERS", 4))
	if err != nil {
		log.Fatalf("Failed to configure Kafka workers: %v", err)
	}
	workers.Start(ctx)

	httpHandler := ingestion.NewHandler(kafkaPublisher)
	mux := http.NewServeMux()
	mux.HandleFunc("/telemetry", httpHandler.HandleTelemetry)
	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	go func() {
		log.Println("HTTP ingestion gateway listening on :8080")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	grpcListener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port 50051: %v", err)
	}
	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(1 << 20))
	pb.RegisterIngestionServiceServer(grpcServer, ingestion.NewGRPCServer(kafkaPublisher))
	go func() {
		log.Println("gRPC ingestion server listening on :50051")
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down ingestion services")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := workers.Stop(); err != nil {
		log.Printf("Failed to stop Kafka workers: %v", err)
	}
	grpcServer.GracefulStop()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil {
		log.Fatalf("Invalid %s: %v", key, err)
	}
	return value
}
