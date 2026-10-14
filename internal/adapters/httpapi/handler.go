package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"
)

type telemetryIngestor interface {
	Execute(context.Context, domain.TelemetryPacket) error
}

type Handler struct {
	ingestor telemetryIngestor
	metrics  *observability.Metrics
}

func NewHandler(ingestor telemetryIngestor, metrics *observability.Metrics) *Handler {
	return &Handler{ingestor: ingestor, metrics: metrics}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	result := "failed"
	defer func() {
		h.metrics.AddCounter("device_ingestion_requests_total", map[string]string{"protocol": "http", "result": result}, 1)
		h.metrics.Observe("device_ingestion_request_duration_seconds", map[string]string{"protocol": "http"}, time.Since(started))
	}()
	if r.Method != http.MethodPost {
		result = "method_not_allowed"
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var packet domain.TelemetryPacket
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&packet); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			result = "payload_too_large"
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		result = "bad_request"
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		result = "bad_request"
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	packet.IdempotencyKey = r.Header.Get("Idempotency-Key")

	if err := h.ingestor.Execute(ctx, packet); err != nil {
		if validationErr, ok := errors.AsType[*domain.ValidationError](err); ok {
			result = "rejected"
			http.Error(w, validationErr.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			result = "timeout"
			http.Error(w, "Request timeout", http.StatusGatewayTimeout)
			return
		}
		result = "unavailable"
		http.Error(w, "Failed to queue telemetry", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	result = "queued"
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"queued"}`))
}
