package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"device-ingestion/app"
	kafkaadapter "device-ingestion/internal/adapters/kafka"
	"device-ingestion/internal/adapters/persistence/postgres"
	"device-ingestion/internal/application"
)

func main() {
	config, err := app.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	workers := flag.Int("workers", min(config.DBMaxOpen, 2), "maximum Kafka partitions to reconcile concurrently (1-32)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(config.DatabaseURL, config.DBMaxOpen)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	processor := application.NewProcessTelemetry(postgres.NewRepository(db))
	stats, err := kafkaadapter.ReplayTopic(ctx, config.KafkaBrokers, kafkaadapter.TelemetryTopic, *workers, processor)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Reconciliation complete: replayed=%d skipped=%d", stats.Replayed, stats.Skipped)
}
