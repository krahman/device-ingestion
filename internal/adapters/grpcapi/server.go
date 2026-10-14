package grpcapi

import (
	"context"
	"errors"
	"time"

	"device-ingestion/internal/domain"
	pb "device-ingestion/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type telemetryIngestor interface {
	Execute(context.Context, domain.TelemetryPacket) error
}

type Server struct {
	pb.UnimplementedIngestionServiceServer
	ingestor telemetryIngestor
}

func NewServer(ingestor telemetryIngestor) *Server {
	return &Server{ingestor: ingestor}
}

func (s *Server) IngestTelemetry(ctx context.Context, req *pb.TelemetryRequest) (*pb.TelemetryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "telemetry request is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	packet := domain.TelemetryPacket{
		DeviceID: req.DeviceId,
		Voltage:  req.Voltage,
		Current:  req.Current,
		PowerKw:  req.PowerKw,
	}
	if err := s.ingestor.Execute(ctx, packet); err != nil {
		var validationErr *domain.ValidationError
		if errors.As(err, &validationErr) {
			return nil, status.Error(codes.InvalidArgument, validationErr.Error())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "request timeout")
		}
		return nil, status.Error(codes.Unavailable, "failed to queue telemetry")
	}
	return &pb.TelemetryResponse{Status: "queued"}, nil
}
