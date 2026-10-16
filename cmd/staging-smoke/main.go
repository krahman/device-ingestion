package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"device-ingestion/app"
	"device-ingestion/internal/adapters/persistence/postgres"
	pb "device-ingestion/proto"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("staging protocol and persistence smoke checks passed")
}

func run(ctx context.Context) error {
	apiURL := strings.TrimRight(os.Getenv("STAGING_API_URL"), "/")
	wsURL := strings.TrimRight(os.Getenv("STAGING_OCPP_URL"), "/")
	grpcTarget := strings.TrimSpace(os.Getenv("STAGING_GRPC_TARGET"))
	if apiURL == "" || wsURL == "" || grpcTarget == "" {
		return fmt.Errorf("STAGING_API_URL, STAGING_OCPP_URL, and STAGING_GRPC_TARGET are required")
	}
	stations, err := decodeSecret[[]app.OCPPStationConfig]("OCPP_STATIONS")
	if err != nil || len(stations) == 0 {
		return fmt.Errorf("OCPP_STATIONS must contain a configured staging station")
	}
	partners, err := decodeSecret[[]app.OCPIEMSPPartner]("OCPI_EMSP_PARTNERS")
	if err != nil || len(partners) == 0 {
		return fmt.Errorf("OCPI_EMSP_PARTNERS must contain a configured staging partner")
	}
	station, partner := stations[0], partners[0]
	db, err := postgres.Open(app.DatabaseURLFromEnv(), 2)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := checkHTTPAndOCPI(ctx, apiURL, partner); err != nil {
		return err
	}
	key := uniqueKey()
	if err := checkHTTPIngestion(ctx, apiURL, db, key+"-http"); err != nil {
		return err
	}
	if err := checkGRPCIngestion(ctx, grpcTarget, db, key+"-grpc"); err != nil {
		return err
	}
	if err := checkOCPP(ctx, wsURL, station, db, key); err != nil {
		return err
	}
	return nil
}

func checkHTTPAndOCPI(ctx context.Context, apiURL string, partner app.OCPIEMSPPartner) error {
	client := &http.Client{Timeout: 10 * time.Second}
	for _, endpoint := range []string{"/health/ready", "/ocpi/versions"} {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+endpoint, nil)
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("GET %s: %w", endpoint, err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s returned %s", endpoint, response.Status)
		}
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/ocpi/cpo/2.2.1/locations?limit=1", nil)
	request.Header.Set("Authorization", "Token "+partner.Token)
	request.Header.Set("OCPI-from-country-code", partner.CountryCode)
	request.Header.Set("OCPI-from-party-id", partner.PartyID)
	request.Header.Set("X-Request-ID", uniqueKey())
	request.Header.Set("X-Correlation-ID", uniqueKey())
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("OCPI Locations request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OCPI Locations returned %s", response.Status)
	}
	return nil
}

func checkHTTPIngestion(ctx context.Context, baseURL string, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) error {
	deviceID := "staging-smoke-http-" + key
	payload, _ := json.Marshal(map[string]any{"device_id": deviceID, "timestamp": time.Now().UTC(), "voltage": 230, "current": 3, "power_kw": 0.69})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/telemetry", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("HTTP telemetry request: %w", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("HTTP telemetry returned %s", response.Status)
	}
	return waitFor(ctx, func() (bool, error) {
		var exists bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM telemetry WHERE device_id=$1 AND idempotency_key=$2)", deviceID, key).Scan(&exists)
		return exists, err
	})
}

func checkGRPCIngestion(ctx context.Context, target string, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) error {
	transportCredentials := grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}))
	if strings.EqualFold(strings.TrimSpace(os.Getenv("STAGING_GRPC_TLS")), "false") {
		transportCredentials = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	conn, err := grpc.NewClient(target, transportCredentials)
	if err != nil {
		return fmt.Errorf("create gRPC connection: %w", err)
	}
	defer conn.Close()
	deviceID := "staging-smoke-grpc-" + key
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := pb.NewIngestionServiceClient(conn).IngestTelemetry(callCtx, &pb.TelemetryRequest{DeviceId: deviceID, IdempotencyKey: key, Voltage: 230, Current: 3, PowerKw: 0.69}); err != nil {
		return fmt.Errorf("gRPC telemetry request: %w", err)
	}
	return waitFor(ctx, func() (bool, error) {
		var exists bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM telemetry WHERE device_id=$1 AND idempotency_key=$2)", deviceID, key).Scan(&exists)
		return exists, err
	})
}

func checkOCPP(ctx context.Context, baseURL string, station app.OCPPStationConfig, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) error {
	target, err := url.Parse(baseURL + "/" + url.PathEscape(station.ID))
	if err != nil {
		return err
	}
	if target.Scheme == "https" {
		target.Scheme = "wss"
	} else if target.Scheme == "http" {
		target.Scheme = "ws"
	}
	dialer := websocket.Dialer{Subprotocols: []string{"ocpp1.6"}, HandshakeTimeout: 10 * time.Second}
	header := http.Header{}
	header.Set("Authorization", "Basic "+basicAuth(station.ID, station.Password))
	conn, response, err := dialer.DialContext(ctx, target.String(), header)
	if err != nil {
		if response != nil {
			return fmt.Errorf("OCPP websocket handshake: %s: %w", response.Status, err)
		}
		return fmt.Errorf("OCPP websocket handshake: %w", err)
	}
	defer conn.Close()
	if err := ocppCall(ctx, conn, "smoke-boot-"+key, "BootNotification", map[string]any{"chargePointVendor": "staging-smoke", "chargePointModel": "deployment-check"}); err != nil {
		return err
	}
	connectorID := 0
	if len(station.Connectors) > 0 {
		connectorID = station.Connectors[0].ConnectorID
	}
	if connectorID == 0 {
		return fmt.Errorf("staging smoke station must have at least one connector")
	}
	statusKey := "status-" + key
	statusTime := time.Now().UTC()
	if err := ocppCall(ctx, conn, statusKey, "StatusNotification", map[string]any{"connectorId": connectorID, "errorCode": "NoError", "status": "Available", "timestamp": statusTime.Format(time.RFC3339Nano)}); err != nil {
		return err
	}
	meterTime := time.Now().UTC()
	if err := ocppCall(ctx, conn, "meter-"+key, "MeterValues", map[string]any{"connectorId": connectorID, "meterValue": []any{map[string]any{"timestamp": meterTime.Format(time.RFC3339Nano), "sampledValue": []any{map[string]string{"value": "12345", "measurand": "Energy.Active.Import.Register", "unit": "Wh"}}}}}); err != nil {
		return err
	}
	if err := waitFor(ctx, func() (bool, error) {
		var status string
		err := db.QueryRowContext(ctx, "SELECT status FROM charging_connector_status WHERE station_id=$1 AND connector_id=$2 AND status_at >= $3", station.ID, connectorID, statusTime).Scan(&status)
		return status == "Available", noRowIsFalse(err)
	}); err != nil {
		return err
	}
	return waitFor(ctx, func() (bool, error) {
		var exists bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM charging_meter_samples WHERE station_id=$1 AND sampled_at=$2)", station.ID, meterTime).Scan(&exists)
		return exists, err
	})
}

func ocppCall(ctx context.Context, conn *websocket.Conn, id, action string, payload any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON([]any{2, id, action, payload}); err != nil {
		return fmt.Errorf("send OCPP %s: %w", action, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read OCPP %s result: %w", action, err)
	}
	var fields []json.RawMessage
	if err := json.Unmarshal(message, &fields); err != nil || len(fields) < 3 {
		return fmt.Errorf("invalid OCPP %s response", action)
	}
	var responseType int
	var responseID string
	_ = json.Unmarshal(fields[0], &responseType)
	_ = json.Unmarshal(fields[1], &responseID)
	if responseType != 3 || responseID != id {
		return fmt.Errorf("OCPP %s was not accepted (type=%d id=%q)", action, responseType, responseID)
	}
	return nil
}

func waitFor(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := check()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("persistence was not visible before timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func noRowIsFalse(err error) error {
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}

func uniqueKey() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}

func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func decodeSecret[T any](key string) (T, error) {
	var value T
	err := json.Unmarshal([]byte(os.Getenv(key)), &value)
	return value, err
}
