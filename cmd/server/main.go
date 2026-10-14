package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"device-ingestion/app"
)

func main() {
	config, err := app.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, config); err != nil {
		log.Fatal(err)
	}
}
