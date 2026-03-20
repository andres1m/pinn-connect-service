package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─────────────────────────────────────────────
// AuthMiddleware
// ─────────────────────────────────────────────

func TestAuthMiddleware_NoTokenConfigured_PassesThrough(t *testing.T) {
	srv := testServer(nil, nil, nil)
	// APIToken is empty → all requests must pass regardless of headers

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	srv.AuthMiddleware(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Error("expected next handler to be called when no token is configured")
	}
}

func TestAuthMiddleware_ValidToken_PassesThrough(t *testing.T) {
	srv := testServer(nil, nil, nil)
	srv.config.Server.APIToken = "secret"

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Token", "secret")
	srv.AuthMiddleware(next).ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Error("expected next handler to be called with a valid token")
	}
}

func TestAuthMiddleware_WrongToken_Returns401(t *testing.T) {
	srv := testServer(nil, nil, nil)
	srv.config.Server.APIToken = "secret"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must NOT be called with a wrong token")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Token", "wrong")
	rec := httptest.NewRecorder()
	srv.AuthMiddleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MissingTokenHeader_Returns401(t *testing.T) {
	srv := testServer(nil, nil, nil)
	srv.config.Server.APIToken = "secret"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must NOT be called without a token header")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Intentionally no X-API-Token header
	rec := httptest.NewRecorder()
	srv.AuthMiddleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_EmptyTokenHeader_Returns401(t *testing.T) {
	srv := testServer(nil, nil, nil)
	srv.config.Server.APIToken = "secret"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must NOT be called with an empty token")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Token", "")
	rec := httptest.NewRecorder()
	srv.AuthMiddleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ─────────────────────────────────────────────
// Server construction and route smoke test
// ─────────────────────────────────────────────

func TestNew_RoutesRegistered(t *testing.T) {
	srv := New(&mockTaskSvc{}, &mockModelSvc{}, &mockHealthSvc{}, testServer(nil, nil, nil).config)

	// Health is public — no token required.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	srv.router.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Error("/health route not registered")
	}
}

func TestNew_ProtectedRouteRequiresToken(t *testing.T) {
	cfg := testServer(nil, nil, nil).config
	cfg.Server.APIToken = "tok"

	srv := New(&mockTaskSvc{}, &mockModelSvc{}, &mockHealthSvc{}, cfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/task/list", nil)
	// No token → should be 401
	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on protected route without token, got %d", rec.Code)
	}
}

func TestNew_ProtectedRouteWithToken(t *testing.T) {
	cfg := testServer(nil, nil, nil).config
	cfg.Server.APIToken = "tok"

	srv := New(&mockTaskSvc{}, &mockModelSvc{}, &mockHealthSvc{}, cfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/task/list", nil)
	req.Header.Set("X-API-Token", "tok")
	srv.router.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Error("expected request to pass auth middleware with valid token")
	}
}
