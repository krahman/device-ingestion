package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"device-ingestion/internal/models"
	pb "device-ingestion/proto"

	"github.com/nats-io/nats.go"
)

type GRPCServer struct {
	pb.UnimplementedIngestionServiceServer
	nc *nats.Conn
}

func NewGRPCServer(nc *nats.Conn) *GRPCServer {
	return &GRPCServer{nc: nc}
}

func (s *GRPCServer) IngestTelemetry(ctx context.Context, req *pb.TelemetryRequest) (*pb.TelemetryResponse, error) {
	// Enforce strict timeout using context
	tCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	packet := models.TelemetryPacket{
		DeviceID:  req.DeviceId,
		Timestamp: time.Now(),
		Voltage:   req.Voltage,
		Current:   req.Current,
		PowerKw:   req.PowerKw,
	}

	data, err := json.Marshal(packet)
	if err != nil {
		return nil, statusError("Internal serialization error")
	}

	select {
	case <-tCtx.Done():
		return nil, statusError("Request timeout")
	default:
		if err := s.nc.Publish("device.telemetry", data); err != nil {
			return nil, statusError("Failed to queue telemetry")
		}
	}

	return &pb.TelemetryResponse{Status: "queued"}, nil
}

func statusError(msg string) error {
	return grpcError(msg)
}

func grpcError(msg string) error {
	return errors.New(msg)
}
