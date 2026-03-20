package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"pinn-connect-service/internal/domain"
	"strings"
	"testing"
)

func TestHandleHealth_OK(t *testing.T) {
	srv := testServer(nil, nil, &mockHealthSvc{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp domain.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}
}

func TestHandleHealth_ServiceError_Returns503(t *testing.T) {
	srv := testServer(nil, nil, &mockHealthSvc{
		checkFunc: func(_ context.Context) error { return errors.New("db unreachable") },
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.HandleHealth(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp domain.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if !strings.Contains(resp.Status, "error") {
		t.Errorf("expected 'error' in status, got %q", resp.Status)
	}
}

func TestHandleHealth_ErrorMessage_ContainsCause(t *testing.T) {
	srv := testServer(nil, nil, &mockHealthSvc{
		checkFunc: func(_ context.Context) error { return errors.New("redis timeout") },
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.HandleHealth(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "redis timeout") {
		t.Errorf("expected error cause in body, got: %s", body)
	}
}

// HandleStats calls sysstats.GetHostResources directly (no injection),
// so we only validate the contract: 200 with a parseable body, or 500 on
// unsupported environments.
func TestHandleStats_ResponseContract(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	srv.HandleStats(rec, req)

	switch rec.Code {
	case http.StatusOK:
		var resp domain.StatsResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Errorf("failed to decode stats response: %v", err)
		}
	case http.StatusInternalServerError:
		// acceptable in environments where host stats are unavailable
	default:
		t.Errorf("unexpected status %d", rec.Code)
	}
}

func TestHandleStats_ContentTypeJSON(t *testing.T) {
	srv := testServer(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	srv.HandleStats(rec, req)

	if rec.Code == http.StatusOK {
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("expected application/json content-type, got %q", ct)
		}
	}
}
