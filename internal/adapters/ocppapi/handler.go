package ocppapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"

	"github.com/gorilla/websocket"
)

const maxOCPPIDLength = 36

type eventIngestor interface {
	Execute(context.Context, domain.ChargingEvent) error
}

type StationConfig struct {
	domain.ChargingStation
	Password       string
	CPOCountryCode string
	CPOPartyID     string
}

type Handler struct {
	ingestor eventIngestor
	metrics  *observability.Metrics
	stations map[string]StationConfig
	limit    chan struct{}
	maxFrame int64
	active   atomic.Int64
	upgrader websocket.Upgrader
}

func NewHandler(ingestor eventIngestor, metrics *observability.Metrics, stations []StationConfig, maxConnections int, maxFrame int64) *Handler {
	byID := make(map[string]StationConfig, len(stations))
	for _, station := range stations {
		byID[station.ID] = station
	}
	return &Handler{
		ingestor: ingestor, metrics: metrics, stations: byID,
		limit: make(chan struct{}, maxConnections), maxFrame: maxFrame,
		upgrader: websocket.Upgrader{Subprotocols: []string{"ocpp1.6"}, ReadBufferSize: 4096, WriteBufferSize: 4096},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	result := "failed"
	defer func() {
		h.metrics.AddCounter("device_ingestion_ocpp_connections_total", map[string]string{"result": result}, 1)
		h.metrics.Observe("device_ingestion_ocpp_connection_duration_seconds", nil, time.Since(started))
	}()
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/ocpp/") {
		result = "bad_request"
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	stationID := strings.TrimPrefix(r.URL.Path, "/ocpp/")
	if stationID == "" || strings.Contains(stationID, "/") {
		result = "rejected"
		http.Error(w, "Unknown station", http.StatusNotFound)
		return
	}
	station, ok := h.stations[stationID]
	if !ok || !authorizedStation(r, stationID, station.Password) {
		result = "unauthorized"
		w.Header().Set("WWW-Authenticate", `Basic realm="OCPP"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !hasSubprotocol(r, "ocpp1.6") {
		result = "unsupported_protocol"
		http.Error(w, "OCPP 1.6J subprotocol required", http.StatusBadRequest)
		return
	}
	select {
	case h.limit <- struct{}{}:
	default:
		result = "capacity_rejected"
		http.Error(w, "Connection capacity reached", http.StatusServiceUnavailable)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		<-h.limit
		result = "upgrade_failed"
		return
	}
	result = "accepted"
	h.active.Add(1)
	h.metrics.SetGauge("device_ingestion_ocpp_active_connections", float64(h.active.Load()))
	defer func() {
		h.active.Add(-1)
		h.metrics.SetGauge("device_ingestion_ocpp_active_connections", float64(h.active.Load()))
		<-h.limit
		_ = conn.Close()
	}()
	conn.SetReadLimit(h.maxFrame)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(10 * time.Minute)) })
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.metrics.AddCounter("device_ingestion_ocpp_messages_total", map[string]string{"result": "connection_error"}, 1)
			}
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
		if messageType != websocket.TextMessage {
			h.writeCallError(conn, "0", "FormationViolation", "OCPP 1.6J messages must be text frames")
			h.metrics.AddCounter("device_ingestion_ocpp_messages_total", map[string]string{"result": "rejected"}, 1)
			continue
		}
		if err := h.handleCall(r.Context(), conn, station, data); err != nil {
			h.metrics.AddCounter("device_ingestion_ocpp_messages_total", map[string]string{"result": "error"}, 1)
			return
		}
	}
}

func authorizedStation(r *http.Request, stationID, expected string) bool {
	username, password, ok := r.BasicAuth()
	if !ok || username != stationID || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
}

func hasSubprotocol(r *http.Request, wanted string) bool {
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == wanted {
			return true
		}
	}
	return false
}

func (h *Handler) handleCall(ctx context.Context, conn *websocket.Conn, station StationConfig, data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 4 {
		h.writeCallError(conn, "0", "FormationViolation", "Invalid OCPP CALL frame")
		return errors.New("invalid OCPP call frame")
	}
	var messageType int
	var uniqueID, action string
	if json.Unmarshal(fields[0], &messageType) != nil || messageType != 2 || json.Unmarshal(fields[1], &uniqueID) != nil || json.Unmarshal(fields[2], &action) != nil || uniqueID == "" || len(uniqueID) > maxOCPPIDLength {
		h.writeCallError(conn, "0", "FormationViolation", "Invalid OCPP CALL header")
		return errors.New("invalid OCPP call header")
	}
	var eventType string
	var occurred time.Time
	var connectorID int
	var payload any
	callResult := any(map[string]any{})
	now := time.Now().UTC()
	switch action {
	case "BootNotification":
		var req struct {
			ChargePointVendor       string `json:"chargePointVendor"`
			ChargePointModel        string `json:"chargePointModel"`
			ChargePointSerialNumber string `json:"chargePointSerialNumber"`
			FirmwareVersion         string `json:"firmwareVersion"`
		}
		if err := json.Unmarshal(fields[3], &req); err != nil || strings.TrimSpace(req.ChargePointVendor) == "" || strings.TrimSpace(req.ChargePointModel) == "" {
			return h.callError(conn, uniqueID, "FormationViolation", "Invalid BootNotification payload")
		}
		eventType, occurred = domain.ChargingEventBoot, now
		payload = domain.StationBoot{Vendor: req.ChargePointVendor, Model: req.ChargePointModel, SerialNumber: req.ChargePointSerialNumber, FirmwareVersion: req.FirmwareVersion}
		callResult = map[string]any{"status": "Accepted", "currentTime": now.Format(time.RFC3339), "interval": 300}
	case "Heartbeat":
		eventType, occurred = domain.ChargingEventHeartbeat, now
		payload = map[string]any{}
		callResult = map[string]any{"currentTime": now.Format(time.RFC3339)}
	case "StatusNotification":
		var req struct {
			ConnectorID     int       `json:"connectorId"`
			ErrorCode       string    `json:"errorCode"`
			Info            string    `json:"info"`
			Status          string    `json:"status"`
			Timestamp       time.Time `json:"timestamp"`
			VendorID        string    `json:"vendorId"`
			VendorErrorCode string    `json:"vendorErrorCode"`
		}
		if err := json.Unmarshal(fields[3], &req); err != nil || req.ConnectorID < 0 || req.Timestamp.IsZero() || !validStatus(req.Status) || !validErrorCode(req.ErrorCode) {
			return h.callError(conn, uniqueID, "FormationViolation", "Invalid StatusNotification payload")
		}
		if req.ConnectorID > 0 && !stationHasConnector(station, req.ConnectorID) {
			return h.callError(conn, uniqueID, "PropertyConstraintViolation", "Unknown connector")
		}
		eventType, occurred, connectorID = domain.ChargingEventStatus, req.Timestamp.UTC(), req.ConnectorID
		payload = domain.ConnectorStatus{Status: req.Status, ErrorCode: req.ErrorCode, Info: req.Info, VendorID: req.VendorID, VendorErrorCode: req.VendorErrorCode}
	case "MeterValues":
		var req struct {
			ConnectorID int `json:"connectorId"`
			MeterValue  []struct {
				Timestamp    time.Time `json:"timestamp"`
				SampledValue []struct {
					Value     string `json:"value"`
					Context   string `json:"context"`
					Format    string `json:"format"`
					Measurand string `json:"measurand"`
					Phase     string `json:"phase"`
					Location  string `json:"location"`
					Unit      string `json:"unit"`
				} `json:"sampledValue"`
			} `json:"meterValue"`
		}
		if err := json.Unmarshal(fields[3], &req); err != nil || req.ConnectorID < 0 || len(req.MeterValue) == 0 {
			return h.callError(conn, uniqueID, "FormationViolation", "Invalid MeterValues payload")
		}
		if req.ConnectorID > 0 && !stationHasConnector(station, req.ConnectorID) {
			return h.callError(conn, uniqueID, "PropertyConstraintViolation", "Unknown connector")
		}
		samples := make([]domain.MeterSample, 0)
		occurred = time.Time{}
		for _, meter := range req.MeterValue {
			if meter.Timestamp.IsZero() || len(meter.SampledValue) == 0 {
				return h.callError(conn, uniqueID, "FormationViolation", "Invalid meter value timestamp or samples")
			}
			if meter.Timestamp.After(occurred) {
				occurred = meter.Timestamp.UTC()
			}
			for _, sample := range meter.SampledValue {
				value, err := strconv.ParseFloat(sample.Value, 64)
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || sample.Format == "SignedData" {
					return h.callError(conn, uniqueID, "FormationViolation", "Invalid or unsupported sampled value")
				}
				if sample.Context == "" {
					sample.Context = "Sample.Periodic"
				}
				if sample.Format == "" {
					sample.Format = "Raw"
				}
				if sample.Measurand == "" {
					sample.Measurand = "Energy.Active.Import.Register"
				}
				if sample.Location == "" {
					sample.Location = "Outlet"
				}
				if sample.Unit == "" {
					sample.Unit = "Wh"
				}
				samples = append(samples, domain.MeterSample{SampledAt: meter.Timestamp.UTC(), Value: sample.Value, Measurand: sample.Measurand, Unit: sample.Unit, Context: sample.Context, Format: sample.Format, Phase: sample.Phase, Location: sample.Location})
			}
		}
		eventType, connectorID, payload = domain.ChargingEventMeter, req.ConnectorID, samples
	default:
		return h.callError(conn, uniqueID, "NotSupported", "Action is not supported")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID, err := newEventID()
	if err != nil {
		return err
	}
	event := domain.ChargingEvent{
		EventID: eventID, SchemaVersion: domain.ChargingEventSchemaVersion, EventType: eventType,
		SourceProtocol: "ocpp", ProtocolVersion: "1.6J", CPOCountryCode: station.CPOCountryCode, CPOPartyID: station.CPOPartyID, StationID: station.ID,
		ConnectorID: connectorID, OccurredAt: occurred, ReceivedAt: now,
		IdempotencyKey: eventID, CorrelationID: uniqueID, Payload: encoded,
	}
	if err := h.ingestor.Execute(ctx, event); err != nil {
		h.writeCallError(conn, uniqueID, "InternalError", "Failed to persist event to the durable queue")
		return err
	}
	if err := conn.WriteJSON([]any{3, uniqueID, callResult}); err != nil {
		return fmt.Errorf("write OCPP CALLRESULT: %w", err)
	}
	h.metrics.AddCounter("device_ingestion_ocpp_messages_total", map[string]string{"result": "accepted"}, 1)
	return nil
}

func (h *Handler) callError(conn *websocket.Conn, uniqueID, code, description string) error {
	h.writeCallError(conn, uniqueID, code, description)
	h.metrics.AddCounter("device_ingestion_ocpp_messages_total", map[string]string{"result": "rejected"}, 1)
	return nil
}

func (h *Handler) writeCallError(conn *websocket.Conn, uniqueID, code, description string) {
	_ = conn.WriteJSON([]any{4, uniqueID, code, description, map[string]any{}})
}

func stationHasConnector(station StationConfig, connectorID int) bool {
	for _, connector := range station.Connectors {
		if connector.ConnectorID == connectorID {
			return true
		}
	}
	return false
}

func validStatus(status string) bool {
	switch status {
	case "Available", "Preparing", "Charging", "SuspendedEVSE", "SuspendedEV", "Finishing", "Reserved", "Unavailable", "Faulted":
		return true
	default:
		return false
	}
}

func validErrorCode(code string) bool {
	switch code {
	case "ConnectorLockFailure", "EVCommunicationError", "GroundFailure", "HighTemperature", "InternalError", "LocalListConflict", "NoError", "OtherError", "OverCurrentFailure", "PowerMeterFailure", "PowerSwitchFailure", "ReaderFailure", "ResetFailure", "UnderVoltage", "OverVoltage", "WeakSignal":
		return true
	default:
		return false
	}
}

func newEventID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return hex.EncodeToString(bytes[:]), nil
}
