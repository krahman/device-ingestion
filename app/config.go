package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	kafkaadapter "device-ingestion/internal/adapters/kafka"
	"device-ingestion/internal/domain"
)

type Config struct {
	Profile            string
	KafkaBrokers       []string
	KafkaSecurity      kafkaadapter.SecurityConfig
	KafkaWorkers       int
	DBMaxOpen          int
	DatabaseURL        string
	DatabaseTLSVerify  bool
	HTTPAddress        string
	OCPPAddress        string
	OCPPStations       []OCPPStationConfig
	OCPPMaxConnections int
	OCPPMaxFrameBytes  int64
	OCPICountryCode    string
	OCPIPartyID        string
	OCPIBaseURL        string
	OCPIEMSPPartners   []OCPIEMSPPartner
	GRPCAddress        string
	PPROFAddress       string
	ShutdownLimit      time.Duration
}

const (
	ProfileFull    = "full"
	ProfileSandbox = "sandbox"
)

type OCPPStationConfig struct {
	domain.ChargingStation
	Password string `json:"password"`
}

type OCPIEMSPPartner struct {
	CountryCode string `json:"country_code"`
	PartyID     string `json:"party_id"`
	Token       string `json:"token"`
}

func ConfigFromEnv() (Config, error) {
	profile := env("APP_PROFILE", ProfileFull)
	if profile == ProfileSandbox {
		return Config{
			Profile:       ProfileSandbox,
			HTTPAddress:   env("HTTP_ADDRESS", ":8080"),
			GRPCAddress:   env("GRPC_ADDRESS", ":50051"),
			ShutdownLimit: 10 * time.Second,
		}, nil
	}
	if profile != ProfileFull {
		return Config{}, fmt.Errorf("APP_PROFILE must be %q or %q", ProfileFull, ProfileSandbox)
	}

	kafkaTLS, err := boolEnv("KAFKA_TLS", false)
	if err != nil {
		return Config{}, err
	}
	kafkaSecurity := kafkaadapter.SecurityConfig{TLS: kafkaTLS, Mechanism: env("KAFKA_SASL_MECHANISM", ""), Username: os.Getenv("KAFKA_SASL_USERNAME"), Password: os.Getenv("KAFKA_SASL_PASSWORD")}
	if _, err := kafkaSecurity.Dialer(); err != nil {
		return Config{}, fmt.Errorf("invalid Kafka security configuration: %w", err)
	}
	databaseTLSVerify, err := boolEnv("DATABASE_TLS_VERIFY", false)
	if err != nil {
		return Config{}, err
	}
	databaseURL := DatabaseURLFromEnv()
	if databaseTLSVerify {
		parsed, parseErr := url.Parse(databaseURL)
		if parseErr != nil || parsed.Query().Get("sslmode") != "verify-full" {
			return Config{}, fmt.Errorf("DATABASE_URL must set sslmode=verify-full when DATABASE_TLS_VERIFY is enabled")
		}
	}
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
	maxConnections, err := intEnv("OCPP_MAX_CONNECTIONS", 1000)
	if err != nil || maxConnections < 1 || maxConnections > 100000 {
		return Config{}, fmt.Errorf("OCPP_MAX_CONNECTIONS must be between 1 and 100000")
	}
	maxFrame, err := intEnv("OCPP_MAX_FRAME_BYTES", 1<<20)
	if err != nil || maxFrame < 1024 || maxFrame > 16<<20 {
		return Config{}, fmt.Errorf("OCPP_MAX_FRAME_BYTES must be between 1024 and 16777216")
	}

	var stations []OCPPStationConfig
	if raw := strings.TrimSpace(os.Getenv("OCPP_STATIONS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &stations); err != nil {
			return Config{}, fmt.Errorf("invalid OCPP_STATIONS JSON")
		}
	}
	seenStations := make(map[string]struct{}, len(stations))
	locationCatalog := make(map[string]domain.ChargingStation)
	evseOwners := make(map[string]string)
	for _, station := range stations {
		if strings.TrimSpace(station.ID) == "" || strings.Contains(station.ID, "/") || len(station.ID) > 128 || strings.TrimSpace(station.Password) == "" || strings.TrimSpace(station.LocationID) == "" || strings.Contains(station.LocationID, "/") || len(station.LocationID) > 36 || strings.TrimSpace(station.Name) == "" || len(station.Name) > 255 || strings.TrimSpace(station.Address) == "" || len(station.Address) > 45 || strings.TrimSpace(station.City) == "" || len(station.City) > 45 || len(station.PostalCode) > 10 || len(station.Country) != 3 || strings.TrimSpace(station.TimeZone) == "" || len(station.Connectors) == 0 || math.IsNaN(station.Latitude) || math.IsInf(station.Latitude, 0) || station.Latitude < -90 || station.Latitude > 90 || math.IsNaN(station.Longitude) || math.IsInf(station.Longitude, 0) || station.Longitude < -180 || station.Longitude > 180 {
			return Config{}, fmt.Errorf("each OCPP_STATIONS item must include valid credentials, location metadata, coordinates, timezone, and connectors")
		}
		if _, exists := seenStations[station.ID]; exists {
			return Config{}, fmt.Errorf("OCPP_STATIONS contains a duplicate station ID")
		}
		seenStations[station.ID] = struct{}{}
		if _, err := time.LoadLocation(station.TimeZone); err != nil {
			return Config{}, fmt.Errorf("OCPP_STATIONS contains an invalid timezone")
		}
		if previous, exists := locationCatalog[station.LocationID]; exists && (previous.Name != station.Name || previous.Address != station.Address || previous.City != station.City || previous.PostalCode != station.PostalCode || previous.Country != station.Country || previous.Latitude != station.Latitude || previous.Longitude != station.Longitude || previous.TimeZone != station.TimeZone) {
			return Config{}, fmt.Errorf("OCPP_STATIONS contains inconsistent metadata for one OCPI location")
		}
		locationCatalog[station.LocationID] = station.ChargingStation
		seenConnectors := make(map[int]struct{}, len(station.Connectors))
		for _, connector := range station.Connectors {
			if connector.ConnectorID < 1 || connector.ConnectorID > 255 || strings.TrimSpace(connector.EVSEUID) == "" || strings.Contains(connector.EVSEUID, "/") || len(connector.EVSEUID) > 36 || strings.TrimSpace(connector.Standard) == "" || strings.TrimSpace(connector.Format) == "" || strings.TrimSpace(connector.PowerType) == "" || connector.MaxVoltage < 1 || connector.MaxAmperage < 1 || connector.MaxElectricPower < 1 {
				return Config{}, fmt.Errorf("OCPP_STATIONS connector metadata is incomplete or invalid")
			}
			evseKey := station.LocationID + ":" + connector.EVSEUID
			if owner, exists := evseOwners[evseKey]; exists && owner != station.ID {
				return Config{}, fmt.Errorf("OCPP_STATIONS assigns an OCPI EVSE UID to multiple stations")
			}
			evseOwners[evseKey] = station.ID
			if _, exists := seenConnectors[connector.ConnectorID]; exists {
				return Config{}, fmt.Errorf("OCPP_STATIONS contains a duplicate connector ID for one station")
			}
			seenConnectors[connector.ConnectorID] = struct{}{}
		}
	}
	var partners []OCPIEMSPPartner
	if raw := strings.TrimSpace(os.Getenv("OCPI_EMSP_PARTNERS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &partners); err != nil {
			return Config{}, fmt.Errorf("invalid OCPI_EMSP_PARTNERS JSON")
		}
	}
	seenPartners := make(map[string]struct{}, len(partners))
	seenTokens := make(map[string]string, len(partners))
	for i := range partners {
		partner := &partners[i]
		if len(partner.CountryCode) != 2 || len(partner.PartyID) != 3 || strings.TrimSpace(partner.Token) == "" {
			return Config{}, fmt.Errorf("each OCPI_EMSP_PARTNERS item must include a country code, party ID, and token")
		}
		partner.CountryCode = strings.ToUpper(partner.CountryCode)
		partner.PartyID = strings.ToUpper(partner.PartyID)
		partyKey := partner.CountryCode + ":" + partner.PartyID
		if _, exists := seenPartners[partyKey]; exists {
			return Config{}, fmt.Errorf("OCPI_EMSP_PARTNERS contains a duplicate party")
		}
		seenPartners[partyKey] = struct{}{}
		if owner, exists := seenTokens[partner.Token]; exists && owner != partyKey {
			return Config{}, fmt.Errorf("OCPI_EMSP_PARTNERS reuses a token for multiple parties")
		}
		seenTokens[partner.Token] = partyKey
	}
	cpoCountryCode := strings.ToUpper(env("OCPI_CPO_COUNTRY_CODE", ""))
	cpoPartyID := strings.ToUpper(env("OCPI_CPO_PARTY_ID", ""))
	if (len(stations) > 0 || len(partners) > 0) && (len(cpoCountryCode) != 2 || len(cpoPartyID) != 3) {
		return Config{}, fmt.Errorf("OCPI_CPO_COUNTRY_CODE and OCPI_CPO_PARTY_ID are required when charging stations or OCPI partners are configured")
	}
	ocpiBaseURL := strings.TrimRight(env("OCPI_BASE_URL", "http://localhost:8080"), "/")
	parsedOCPIBaseURL, err := url.ParseRequestURI(ocpiBaseURL)
	if err != nil || (parsedOCPIBaseURL.Scheme != "http" && parsedOCPIBaseURL.Scheme != "https") || parsedOCPIBaseURL.Host == "" {
		return Config{}, fmt.Errorf("OCPI_BASE_URL must be an absolute http or https URL")
	}

	return Config{
		Profile:            ProfileFull,
		KafkaBrokers:       strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ","),
		KafkaSecurity:      kafkaSecurity,
		KafkaWorkers:       workers,
		DBMaxOpen:          dbMaxOpen,
		DatabaseURL:        databaseURL,
		DatabaseTLSVerify:  databaseTLSVerify,
		HTTPAddress:        env("HTTP_ADDRESS", ":8080"),
		OCPPAddress:        env("OCPP_ADDRESS", ":8081"),
		OCPPStations:       stations,
		OCPPMaxConnections: maxConnections,
		OCPPMaxFrameBytes:  int64(maxFrame),
		OCPICountryCode:    cpoCountryCode,
		OCPIPartyID:        cpoPartyID,
		OCPIBaseURL:        ocpiBaseURL,
		OCPIEMSPPartners:   partners,
		GRPCAddress:        env("GRPC_ADDRESS", ":50051"),
		PPROFAddress:       env("PPROF_ADDRESS", "127.0.0.1:6060"),
		ShutdownLimit:      10 * time.Second,
	}, nil
}

func DatabaseURLFromEnv() string {
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		return dsn
	}
	host := strings.TrimSpace(os.Getenv("DATABASE_HOST"))
	if host == "" {
		return "postgres://device_ingestion:device_ingestion@127.0.0.1:5432/device_ingestion?sslmode=disable"
	}
	port := env("DATABASE_PORT", "5432")
	database := env("DATABASE_NAME", "device_ingestion")
	user := env("DATABASE_USER", "")
	password := os.Getenv("DATABASE_PASSWORD")
	mode := env("DATABASE_SSLMODE", "verify-full")
	query := url.Values{"sslmode": []string{mode}}
	if rootCert := strings.TrimSpace(os.Getenv("DATABASE_SSLROOTCERT")); rootCert != "" {
		query.Set("sslrootcert", rootCert)
	}
	return (&url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: net.JoinHostPort(host, port), Path: "/" + database, RawQuery: query.Encode()}).String()
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

func boolEnv(key string, fallback bool) (bool, error) {
	value := env(key, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}
