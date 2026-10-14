package ingestion

import (
	"context"
	"encoding/json"
	"time"

	"device-ingestion/internal/models"
	pb "device-ingestion/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GRPCServer struct {
	pb.UnimplementedIngestionServiceServer
	publisher Publisher
}

func NewGRPCServer(publisher Publisher) *GRPCServer {
	return &GRPCServer{publisher: publisher}
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
		return nil, status.Error(codes.Internal, "internal serialization error")
	}

	select {
	case <-tCtx.Done():
		return nil, status.Error(codes.DeadlineExceeded, "request timeout")
	default:
		if err := s.publisher.Publish(tCtx, TelemetryTopic, packet.DeviceID, data); err != nil {
			return nil, status.Error(codes.Unavailable, "failed to queue telemetry")
		}
	}

	return &pb.TelemetryResponse{Status: "queued"}, nil
}
