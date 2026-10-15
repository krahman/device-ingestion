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

	readyErr = true
	notReady := httptest.NewRecorder()
	server.Handler.ServeHTTP(notReady, httptest.NewRequest("GET", "/health/ready", nil))
	if notReady.Code != 503 {
		t.Fatalf("unready status = %d, want 503", notReady.Code)
	}
}
