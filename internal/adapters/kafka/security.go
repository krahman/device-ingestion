package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/scram"
)

// CheckBrokers verifies that Kafka metadata can be read using the configured security settings.
func CheckBrokers(ctx context.Context, brokers []string, security SecurityConfig) error {
	addresses, err := normalizeBrokers(brokers)
	if err != nil {
		return err
	}
	transport, err := security.Transport()
	if err != nil {
		return err
	}
	client := &kafkago.Client{Addr: kafkago.TCP(addresses[0]), Transport: transport, Timeout: 2 * time.Second}
	metadata, err := client.Metadata(ctx, &kafkago.MetadataRequest{Topics: []string{TelemetryTopic, ChargingEventsTopic, TelemetryTopic + ".dlq", ChargingEventsTopic + ".dlq"}})
	if err != nil {
		return errors.New("Kafka metadata check failed")
	}
	if len(metadata.Topics) != 4 {
		return errors.New("Kafka topics are not ready")
	}
	for _, topic := range metadata.Topics {
		if topic.Error != nil || len(topic.Partitions) == 0 {
			return errors.New("Kafka topics are not ready")
		}
	}
	return nil
}

// SecurityConfig controls TLS and SASL/SCRAM for every Kafka client.
type SecurityConfig struct {
	TLS       bool
	Mechanism string
	Username  string
	Password  string
}

func (c SecurityConfig) Dialer() (*kafkago.Dialer, error) {
	mechanism, tlsConfig, err := c.transportConfig()
	if err != nil {
		return nil, err
	}
	return &kafkago.Dialer{TLS: tlsConfig, SASLMechanism: mechanism}, nil
}

func (c SecurityConfig) Transport() (*kafkago.Transport, error) {
	mechanism, tlsConfig, err := c.transportConfig()
	if err != nil {
		return nil, err
	}
	return &kafkago.Transport{TLS: tlsConfig, SASL: mechanism}, nil
}

func (c SecurityConfig) transportConfig() (sasl.Mechanism, *tls.Config, error) {
	mechanismName := strings.ToUpper(strings.TrimSpace(c.Mechanism))
	usernameSet, passwordSet := strings.TrimSpace(c.Username) != "", c.Password != ""
	if usernameSet != passwordSet || mechanismName == "" && usernameSet || mechanismName != "" && !usernameSet {
		return nil, nil, errors.New("Kafka SASL mechanism, username, and password must be configured together")
	}
	if mechanismName != "" && !c.TLS {
		return nil, nil, errors.New("Kafka SASL/SCRAM requires TLS")
	}
	var tlsConfig *tls.Config
	if c.TLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	var mechanism sasl.Mechanism
	var err error
	switch mechanismName {
	case "":
	case "SCRAM-SHA-256":
		mechanism, err = scram.Mechanism(scram.SHA256, c.Username, c.Password)
	case "SCRAM-SHA-512":
		mechanism, err = scram.Mechanism(scram.SHA512, c.Username, c.Password)
	default:
		return nil, nil, errors.New("Kafka SASL mechanism must be SCRAM-SHA-256 or SCRAM-SHA-512")
	}
	if err != nil {
		return nil, nil, err
	}
	return mechanism, tlsConfig, nil
}
