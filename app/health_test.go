package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"device-ingestion/internal/observability"
)

func TestHealthEndpoints(t *testing.T) {
	readyErr := false
	server := newHTTPServer(nil, observability.NewMetrics(), nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), func(context.Context) error {
		if readyErr {
			return context.DeadlineExceeded
		}
		return nil
	})

	live := httptest.NewRecorder()
	server.Handler.ServeHTTP(live, httptest.NewRequest("GET", "/health/live", nil))
	if live.Code != 200 {
		t.Fatalf("liveness status = %d, want 200", live.Code)
	}

	ready := httptest.NewRecorder()
	server.Handler.ServeHTTP(ready, httptest.NewRequest("GET", "/health/ready", nil))
	if ready.Code != 200 {
		t.Fatalf("readiness status = %d, want 200", ready.Code)
	}

	dependenciesReady := httptest.NewRecorder()
	server.Handler.ServeHTTP(dependenciesReady, httptest.NewRequest("GET", "/health/dependencies", nil))
	if dependenciesReady.Code != 200 {
		t.Fatalf("dependency status = %d, want 200", dependenciesReady.Code)
	}

	readyErr = true
	notReady := httptest.NewRecorder()
	server.Handler.ServeHTTP(notReady, httptest.NewRequest("GET", "/health/ready", nil))
	if notReady.Code != 503 {
		t.Fatalf("unready status = %d, want 503", notReady.Code)
	}

	dependenciesNotReady := httptest.NewRecorder()
	server.Handler.ServeHTTP(dependenciesNotReady, httptest.NewRequest("GET", "/health/dependencies", nil))
	if dependenciesNotReady.Code != 503 {
		t.Fatalf("unavailable dependency status = %d, want 503", dependenciesNotReady.Code)
	}
}

func TestSandboxHTTPHealthAndUnavailableRoutes(t *testing.T) {
	server := newSandboxHTTPServer()
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "live", method: http.MethodGet, path: "/health/live", wantStatus: http.StatusOK},
		{name: "ready", method: http.MethodGet, path: "/health/ready", wantStatus: http.StatusOK},
		{name: "dependencies disabled", method: http.MethodGet, path: "/health/dependencies", wantStatus: http.StatusServiceUnavailable},
		{name: "telemetry rejected", method: http.MethodPost, path: "/telemetry", wantStatus: http.StatusServiceUnavailable},
		{name: "ocpi rejected", method: http.MethodGet, path: "/ocpi/versions", wantStatus: http.StatusServiceUnavailable},
		{name: "metrics rejected", method: http.MethodGet, path: "/metrics", wantStatus: http.StatusServiceUnavailable},
		{name: "unknown route rejected", method: http.MethodGet, path: "/anything", wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}
