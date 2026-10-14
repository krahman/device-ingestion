package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"device-ingestion/internal/models"

	"github.com/nats-io/nats.go"
)

type Handler struct {
	nc *nats.Conn
}

func NewHandler(nc *nats.Conn) *Handler {
	return &Handler{nc: nc}
}

func (h *Handler) HandleTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var packet models.TelemetryPacket
	if err := json.NewDecoder(r.Body).Decode(&packet); err != nil {
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
		if err := h.nc.Publish("device.telemetry", data); err != nil {
			http.Error(w, "Failed to queue telemetry", http.StatusServiceUnavailable)
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
	write, err := w.Write([]byte(`{"status":"queued"}`))
	if err != nil {
		return
	}

	w.WriteHeader(write)
}
