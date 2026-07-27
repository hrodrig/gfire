package config_test

import (
	"strings"
	"testing"

	"github.com/hrodrig/gfire/internal/config"
)

func TestStorageSummaryRedactsPassword(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Backend = "postgres"
	cfg.Storage.Postgres.DSN = "postgres://gfire:secret@localhost:5432/gfire?sslmode=disable"

	got := cfg.StorageSummary()
	if strings.Contains(got, "secret") {
		t.Fatalf("DSN password leaked: %s", got)
	}
	if !strings.Contains(got, "postgres (") {
		t.Fatalf("expected postgres summary, got %q", got)
	}
}

func TestHandlersSummaryDefault(t *testing.T) {
	cfg := config.Defaults()
	if got := cfg.HandlersSummary(); got != "echo (dev default)" {
		t.Fatalf("HandlersSummary = %q", got)
	}
}

func TestValidate_RejectsUnknownBackend(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Backend = "cassandra"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
	if !strings.Contains(err.Error(), "unknown storage.backend") {
		t.Fatalf("expected 'unknown storage.backend', got %q", err.Error())
	}
}

func TestValidate_AcceptsKnownBackends(t *testing.T) {
	for _, backend := range []string{"memory", "postgres", "redis"} {
		cfg := config.Defaults()
		cfg.Storage.Backend = backend
		if err := cfg.Validate(); err != nil {
			t.Fatalf("backend %q should be valid, got error: %v", backend, err)
		}
	}
}
