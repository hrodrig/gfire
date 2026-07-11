package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hrodrig/gfire/internal/api"
	"github.com/hrodrig/gfire/internal/config"
	"github.com/hrodrig/gfire/internal/storage/memory"
)

func TestBearerAuth_RejectsMissingAndInvalidToken(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Auth.Enabled = true
	cfg.Auth.Token = "secret-token"
	srv := api.NewServer(&cfg, memory.New(), nil)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth: status %d want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: status %d want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: status %d want %d", rec.Code, http.StatusOK)
	}
}

func TestBearerAuth_SkipsHealthz(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Auth.Enabled = true
	cfg.Auth.Token = "secret-token"
	srv := api.NewServer(&cfg, memory.New(), nil)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: status %d want %d", rec.Code, http.StatusOK)
	}
}
