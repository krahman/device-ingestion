package grpcapi

import (
	"context"
	"errors"
	"time"

	"device-ingestion/internal/domain"
	"device-ingestion/internal/observability"
	pb "device-ingestion/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type telemetryIngestor interface {
	Execute(context.Context, domain.TelemetryPacket) error
}

type Server struct {
	pb.UnimplementedIngestionServiceServer
	ingestor  telemetryIngestor
	metrics   *observability.Metrics
	available bool
}

func NewServer(ingestor telemetryIngestor, metrics *observability.Metrics) *Server {
	return &Server{ingestor: ingestor, metrics: metrics, available: true}
}

func NewUnavailableServer(metrics *observability.Metrics) *Server {
	return &Server{metrics: metrics}
}

func (s *Server) IngestTelemetry(ctx context.Context, req *pb.TelemetryRequest) (response *pb.TelemetryResponse, retErr error) {
	started := time.Now()
	result := "failed"
	defer func() {
		s.metrics.AddCounter("device_ingestion_requests_total", map[string]string{"protocol": "grpc", "result": result}, 1)
		s.metrics.Observe("device_ingestion_request_duration_seconds", map[string]string{"protocol": "grpc"}, time.Since(started))
	}()
	if !s.available {
		result = "unavailable"
		return nil, status.Error(codes.Unavailable, "ingestion is unavailable in sandbox profile")
	}
	if req == nil {
		result = "rejected"
		return nil, status.Error(codes.InvalidArgument, "telemetry request is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	packet := domain.TelemetryPacket{
		DeviceID:       req.DeviceId,
		IdempotencyKey: req.IdempotencyKey,
		Voltage:        req.Voltage,
		Current:        req.Current,
		PowerKw:        req.PowerKw,
	}
	if err := s.ingestor.Execute(ctx, packet); err != nil {
		var validationErr *domain.ValidationError
		if errors.As(err, &validationErr) {
			result = "rejected"
			return nil, status.Error(codes.InvalidArgument, validationErr.Error())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			result = "timeout"
			return nil, status.Error(codes.DeadlineExceeded, "request timeout")
		}
		result = "unavailable"
		return nil, status.Error(codes.Unavailable, "failed to queue telemetry")
	}
	result = "queued"
	return &pb.TelemetryResponse{Status: "queued"}, nil
}
