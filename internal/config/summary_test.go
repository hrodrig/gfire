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
