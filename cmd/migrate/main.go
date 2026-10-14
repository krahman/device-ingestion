package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"device-ingestion/app"
	"device-ingestion/internal/adapters/persistence/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(app.DatabaseURLFromEnv(), 1)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := waitForDatabase(ctx, db); err != nil {
		log.Fatal(err)
	}

	if err := postgres.Migrate(ctx, db); err != nil {
		log.Fatal(err)
	}
	log.Println("PostgreSQL migrations applied")
}

func waitForDatabase(ctx context.Context, db interface{ PingContext(context.Context) error }) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("PostgreSQL did not become ready: %w (last connection error: %v)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}
