package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"device-ingestion/internal/ingestion"
	pb "device-ingestion/proto"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
)

func main() {
	// 1. Connect to NATS message broker
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Start shared bounded worker pool (max 50 concurrent DB writers)
	workerPool := ingestion.NewWorkerPool(nc, 50)
	if err := workerPool.Start(ctx); err != nil {
		log.Fatalf("Failed to start worker pool: %v", err)
	}

	// 3. Start HTTP Gateway Server (:8080)
	httpHandler := ingestion.NewHandler(nc)
	mux := http.NewServeMux()
	mux.HandleFunc("/telemetry", httpHandler.HandleTelemetry)

	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		log.Println("HTTP Ingestion Gateway listening on :8080...")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// 4. Start gRPC Server (:50051)
	grpcLis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port 50051: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterIngestionServiceServer(grpcServer, ingestion.NewGRPCServer(nc))

	go func() {
		log.Println("gRPC Ingestion Server listening on :50051...")
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	// 5. Graceful Shutdown Management
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down servers gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Stop workers and servers
	workerPool.Stop()
	grpcServer.GracefulStop()
	_ = httpServer.Shutdown(shutdownCtx)

	log.Println("All services stopped successfully.")
}
