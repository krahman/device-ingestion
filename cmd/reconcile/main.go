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
	charging := flag.Bool("charging", false, "replay normalized charging events instead of generic telemetry")
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

	repository := postgres.NewRepository(db)
	var stats kafkaadapter.ReconcileStats
	if *charging {
		processor := application.NewProcessChargingEvent(repository)
		stats, err = kafkaadapter.ReplayChargingEvents(ctx, config.KafkaBrokers, *workers, processor, config.KafkaSecurity)
	} else {
		processor := application.NewProcessTelemetry(repository)
		stats, err = kafkaadapter.ReplayTopic(ctx, config.KafkaBrokers, kafkaadapter.TelemetryTopic, *workers, processor, config.KafkaSecurity)
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Reconciliation complete: replayed=%d skipped=%d", stats.Replayed, stats.Skipped)
}
