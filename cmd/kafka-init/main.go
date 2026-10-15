package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	kafkaadapter "device-ingestion/internal/adapters/kafka"

	kafkago "github.com/segmentio/kafka-go"
)

func main() {
	brokers := strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ",")
	security := kafkaadapter.SecurityConfig{
		TLS:       strings.EqualFold(env("KAFKA_TLS", "false"), "true"),
		Mechanism: os.Getenv("KAFKA_SASL_MECHANISM"),
		Username:  os.Getenv("KAFKA_SASL_USERNAME"),
		Password:  os.Getenv("KAFKA_SASL_PASSWORD"),
	}
	transport, err := security.Transport()
	if err != nil {
		log.Fatal(err)
	}
	factor, err := strconv.Atoi(env("KAFKA_TOPIC_REPLICATION_FACTOR", "2"))
	if err != nil || factor < 1 {
		log.Fatal("KAFKA_TOPIC_REPLICATION_FACTOR must be a positive integer")
	}
	addresses := make([]string, 0, len(brokers))
	for _, broker := range brokers {
		if broker = strings.TrimSpace(broker); broker != "" {
			addresses = append(addresses, broker)
		}
	}
	if len(addresses) == 0 {
		log.Fatal("KAFKA_BROKERS must include at least one broker")
	}
	client := &kafkago.Client{Addr: kafkago.TCP(addresses[0]), Transport: transport, Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	configs := []kafkago.TopicConfig{
		{Topic: "device.telemetry", NumPartitions: 4, ReplicationFactor: factor},
		{Topic: "device.telemetry.dlq", NumPartitions: 1, ReplicationFactor: factor},
		{Topic: "charging.events", NumPartitions: 4, ReplicationFactor: factor},
		{Topic: "charging.events.dlq", NumPartitions: 1, ReplicationFactor: factor},
	}
	response, err := client.CreateTopics(ctx, &kafkago.CreateTopicsRequest{Addr: kafkago.TCP(addresses[0]), Topics: configs})
	if err != nil {
		log.Fatal(err)
	}
	for topic, topicErr := range response.Errors {
		if topicErr != nil && !errors.Is(topicErr, kafkago.TopicAlreadyExists) {
			log.Fatal(fmt.Errorf("create Kafka topic %s: %w", topic, topicErr))
		}
		log.Printf("Kafka topic %s is ready", topic)
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
