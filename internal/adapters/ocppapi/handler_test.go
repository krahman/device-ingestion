package ocppapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"

	"github.com/gorilla/websocket"
)

type recordingIngestor struct {
	events []domain.ChargingEvent
	err    error
}

func (r *recordingIngestor) Execute(_ context.Context, event domain.ChargingEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func TestHandlerAcceptsStatusNotificationAndPersistsBeforeResponse(t *testing.T) {
	ingestor := &recordingIngestor{}
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1", Connectors: []domain.ChargingConnector{{ConnectorID: 1}}}, Password: "secret", CPOCountryCode: "NL", CPOPartyID: "ABC"}
	handler := NewHandler(ingestor, observability.NewMetrics(), []StationConfig{station}, 2, 1<<20)
	server := httptest.NewServer(handler)
	defer server.Close()

	header := http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}}
	header.Set("Authorization", "Basic Y3AtMTpzZWNyZXQ=")
	dialer := websocket.Dialer{Subprotocols: []string{"ocpp1.6"}}
	conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ocpp/cp-1", header)
	if err != nil {
		t.Fatalf("Dial() error = %v, response = %#v", err, response)
	}
	defer conn.Close()

	request := []any{2, "call-1", "StatusNotification", map[string]any{
		"connectorId": 1, "errorCode": "NoError", "status": "Available", "timestamp": time.Now().UTC().Format(time.RFC3339),
	}}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	var reply []json.RawMessage
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if len(reply) != 3 || string(reply[0]) != "3" || string(reply[1]) != `"call-1"` {
		t.Fatalf("unexpected CALLRESULT: %s", reply)
	}
	if len(ingestor.events) != 1 || ingestor.events[0].EventType != domain.ChargingEventStatus {
		t.Fatalf("ingested events = %#v", ingestor.events)
	}
}

func TestHandlerRejectsInvalidCredentials(t *testing.T) {
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1"}, Password: "secret"}
	handler := NewHandler(&recordingIngestor{}, observability.NewMetrics(), []StationConfig{station}, 1, 1<<20)
	request := httptest.NewRequest(http.MethodGet, "/ocpp/cp-1", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Protocol", "ocpp1.6")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestHandlerNegotiationRejectsMissingOCPPSubprotocol(t *testing.T) {
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1"}, Password: "secret"}
	handler := NewHandler(&recordingIngestor{}, observability.NewMetrics(), []StationConfig{station}, 1, 1<<20)
	request := httptest.NewRequest(http.MethodGet, "/ocpp/cp-1", nil)
	request.SetBasicAuth("cp-1", "secret")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestHandlerProcessesOCPPCallsAndNormalizesMeterValues(t *testing.T) {
	cases := []struct {
		name, action, payload, eventType string
	}{
		{"boot", "BootNotification", `{"chargePointVendor":"Acme","chargePointModel":"X1"}`, domain.ChargingEventBoot},
		{"heartbeat", "Heartbeat", `{}`, domain.ChargingEventHeartbeat},
		{"meter", "MeterValues", `{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":"10.25"}]}]}`, domain.ChargingEventMeter},
		{"unknown action", "RemoteStartTransaction", `{}`, ""},
		{"unknown connector", "StatusNotification", `{"connectorId":9,"errorCode":"NoError","status":"Available","timestamp":"2026-01-01T00:00:00Z"}`, ""},
		{"signed meter", "MeterValues", `{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":"abc","format":"SignedData"}]}]}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ingestor := &recordingIngestor{}
			station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1", Connectors: []domain.ChargingConnector{{ConnectorID: 1}}}, Password: "secret", CPOCountryCode: "NL", CPOPartyID: "ABC"}
			server := httptest.NewServer(NewHandler(ingestor, observability.NewMetrics(), []StationConfig{station}, 2, 1<<20))
			defer server.Close()
			conn, response, err := (&websocket.Dialer{Subprotocols: []string{"ocpp1.6"}}).Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ocpp/cp-1", authHeader())
			if err != nil {
				t.Fatalf("Dial(): %v (response=%v)", err, response)
			}
			defer conn.Close()
			if err := conn.WriteMessage(websocket.TextMessage, []byte("[2,\"id-1\",\""+tc.action+"\","+tc.payload+"]")); err != nil {
				t.Fatal(err)
			}
			var reply []json.RawMessage
			if err := conn.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
			if tc.eventType == "" {
				if string(reply[0]) != "4" {
					t.Fatalf("reply = %s, want CALLERROR", reply)
				}
				if len(ingestor.events) != 0 {
					t.Fatalf("unexpected ingested events: %#v", ingestor.events)
				}
				return
			}
			if string(reply[0]) != "3" || len(ingestor.events) != 1 || ingestor.events[0].EventType != tc.eventType {
				t.Fatalf("reply=%s events=%#v", reply, ingestor.events)
			}
			if tc.action == "MeterValues" {
				var samples []domain.MeterSample
				if err := json.Unmarshal(ingestor.events[0].Payload, &samples); err != nil {
					t.Fatal(err)
				}
				if len(samples) != 1 || samples[0].Unit != "Wh" || samples[0].Format != "Raw" {
					t.Fatalf("normalized samples = %#v", samples)
				}
			}
		})
	}
}

func TestHandlerReturnsCALLERRORWhenBrokerPublishFails(t *testing.T) {
	ingestor := &recordingIngestor{err: context.DeadlineExceeded}
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1"}, Password: "secret", CPOCountryCode: "NL", CPOPartyID: "ABC"}
	server := httptest.NewServer(NewHandler(ingestor, observability.NewMetrics(), []StationConfig{station}, 1, 1<<20))
	defer server.Close()
	conn, response, err := (&websocket.Dialer{Subprotocols: []string{"ocpp1.6"}}).Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ocpp/cp-1", authHeader())
	if err != nil {
		t.Fatalf("Dial(): %v (response=%v)", err, response)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`[2,"id-1","Heartbeat",{}]`)); err != nil {
		t.Fatal(err)
	}
	var reply []json.RawMessage
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatal(err)
	}
	if string(reply[0]) != "4" || string(reply[2]) != `"InternalError"` {
		t.Fatalf("reply = %s", reply)
	}
}

func TestHandlerRejectsInvalidRoutesAndHeaders(t *testing.T) {
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1"}, Password: "secret"}
	handler := NewHandler(&recordingIngestor{}, observability.NewMetrics(), []StationConfig{station}, 1, 1<<20)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/ocpp/cp-1", http.StatusNotFound},
		{http.MethodGet, "/wrong/cp-1", http.StatusNotFound},
		{http.MethodGet, "/ocpp/missing", http.StatusUnauthorized},
	} {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		request.SetBasicAuth("cp-1", "secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Errorf("%s %s status=%d want=%d", tc.method, tc.path, response.Code, tc.status)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/ocpp/cp-1", nil)
	request.SetBasicAuth("cp-1", "secret")
	request.Header.Set("Sec-WebSocket-Protocol", "ocpp1.6")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("subprotocol response = %d", response.Code)
	}
}

func TestHandlerCallErrorsForMalformedAndUnsupportedFrames(t *testing.T) {
	cases := []string{`{}`, `[3,"id",{}]`, `[2,"", "Heartbeat", {}]`, `[2,"id","StatusNotification",{"connectorId":1,"errorCode":"Bad","status":"Connected","timestamp":"2026-01-01T00:00:00Z"}]`}
	for _, frame := range cases {
		t.Run(frame, func(t *testing.T) {
			station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1", Connectors: []domain.ChargingConnector{{ConnectorID: 1}}}, Password: "secret"}
			server := httptest.NewServer(NewHandler(&recordingIngestor{}, observability.NewMetrics(), []StationConfig{station}, 1, 1<<20))
			defer server.Close()
			conn, response, err := (&websocket.Dialer{Subprotocols: []string{"ocpp1.6"}}).Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ocpp/cp-1", authHeader())
			if err != nil {
				t.Fatalf("Dial(): %v (response=%v)", err, response)
			}
			defer conn.Close()
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				t.Fatal(err)
			}
			var reply []json.RawMessage
			if err := conn.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
			if len(reply) != 5 || string(reply[0]) != "4" {
				t.Fatalf("reply = %s, want CALLERROR", reply)
			}
		})
	}
}

func TestHandlerRejectsBinaryWebSocketMessages(t *testing.T) {
	station := StationConfig{ChargingStation: domain.ChargingStation{ID: "cp-1"}, Password: "secret"}
	server := httptest.NewServer(NewHandler(&recordingIngestor{}, observability.NewMetrics(), []StationConfig{station}, 1, 1<<20))
	defer server.Close()
	conn, response, err := (&websocket.Dialer{Subprotocols: []string{"ocpp1.6"}}).Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ocpp/cp-1", authHeader())
	if err != nil {
		t.Fatalf("Dial(): %v (response=%v)", err, response)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("not-text")); err != nil {
		t.Fatal(err)
	}
	var reply []json.RawMessage
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatal(err)
	}
	if len(reply) != 5 || string(reply[0]) != "4" || string(reply[2]) != `"FormationViolation"` {
		t.Fatalf("reply = %s", reply)
	}
}

func TestProtocolValidatorsRejectUnknownValues(t *testing.T) {
	if validStatus("Connected") {
		t.Fatal("accepted unsupported connector status")
	}
	if validErrorCode("UnknownError") {
		t.Fatal("accepted unsupported error code")
	}
	if validStatus("Faulted") != true || validErrorCode("NoError") != true {
		t.Fatal("rejected valid protocol values")
	}
}

func authHeader() http.Header {
	var header http.Header = make(http.Header)
	header.Set("Authorization", "Basic Y3AtMTpzZWNyZXQ=")
	return header
}
