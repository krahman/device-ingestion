package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	KafkaBrokers  []string
	KafkaWorkers  int
	DBMaxOpen     int
	DatabaseURL   string
	HTTPAddress   string
	GRPCAddress   string
	PPROFAddress  string
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
		PPROFAddress:  env("PPROF_ADDRESS", "127.0.0.1:6060"),
		ShutdownLimit: 10 * time.Second,
	}, nil
}

func DatabaseURLFromEnv() string {
	return env("DATABASE_URL", "postgres://device_ingestion:device_ingestion@127.0.0.1:5432/device_ingestion?sslmode=disable")
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
