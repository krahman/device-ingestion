package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"device-ingestion/internal/models"
)

type Handler struct {
	publisher Publisher
}

func NewHandler(publisher Publisher) *Handler {
	return &Handler{publisher: publisher}
}

func (h *Handler) HandleTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var packet models.TelemetryPacket
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&packet); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	packet.Timestamp = time.Now()

	data, err := json.Marshal(packet)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	select {
	case <-ctx.Done():
		http.Error(w, "Request timeout", http.StatusGatewayTimeout)
		return
	default:
		if err := h.publisher.Publish(ctx, TelemetryTopic, packet.DeviceID, data); err != nil {
			http.Error(w, "Failed to queue telemetry", http.StatusServiceUnavailable)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"queued"}`))
}
