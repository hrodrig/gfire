package config_test

import (
	"testing"

	"github.com/hrodrig/gfire/internal/config"
)

func TestLoadReadsNestedEnv(t *testing.T) {
	t.Setenv("GFIRE_STORAGE_BACKEND", "postgres")
	t.Setenv("GFIRE_STORAGE_POSTGRES_DSN", "postgres://gfire:secret@postgres:5432/gfire?sslmode=disable")
	t.Setenv("GFIRE_SERVER_SERVER_ID", "gfire-1")
	t.Setenv("GFIRE_SERVER_WORKERS", "8")
	t.Setenv("GFIRE_AUTH_ENABLED", "true")
	t.Setenv("GFIRE_AUTH_TOKEN", "tok")
	t.Setenv("GFIRE_STORAGE_REDIS_ADDR", "redis:6379")
	t.Setenv("GFIRE_STORAGE_REDIS_DB", "2")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Backend != "postgres" {
		t.Fatalf("storage.backend: got %q", cfg.Storage.Backend)
	}
	if cfg.Storage.Postgres.DSN != "postgres://gfire:secret@postgres:5432/gfire?sslmode=disable" {
		t.Fatalf("storage.postgres.dsn: got %q", cfg.Storage.Postgres.DSN)
	}
	if cfg.Server.ServerID != "gfire-1" {
		t.Fatalf("server.server_id: got %q", cfg.Server.ServerID)
	}
	if cfg.Server.Workers != 8 {
		t.Fatalf("server.workers: got %d", cfg.Server.Workers)
	}
	if !cfg.Auth.Enabled {
		t.Fatal("auth.enabled: want true")
	}
	if cfg.Auth.Token != "tok" {
		t.Fatalf("auth.token: got %q", cfg.Auth.Token)
	}
	if cfg.Storage.Redis.Addr != "redis:6379" {
		t.Fatalf("storage.redis.addr: got %q", cfg.Storage.Redis.Addr)
	}
	if cfg.Storage.Redis.DB != 2 {
		t.Fatalf("storage.redis.db: got %d", cfg.Storage.Redis.DB)
	}
}
