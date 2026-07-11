package config

import (
	"context"
	"fmt"

	"github.com/hrodrig/gfire/internal/storage"
	"github.com/hrodrig/gfire/internal/storage/memory"
	"github.com/hrodrig/gfire/internal/storage/postgres"
	"github.com/hrodrig/gfire/internal/storage/redis"
)

// OpenStorage creates a Storage backend from configuration.
func OpenStorage(ctx context.Context, cfg StorageConfig, serverID string) (storage.Storage, error) {
	switch cfg.Backend {
	case "", "memory":
		return memory.New(), nil
	case "postgres", "postgresql":
		s, err := postgres.Open(ctx, cfg.Postgres.DSN)
		if err != nil {
			return nil, err
		}
		if serverID != "" {
			s.SetServerID(serverID)
		}
		return s, nil
	case "redis", "valkey":
		s, err := redis.Open(ctx, redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err != nil {
			return nil, err
		}
		if serverID != "" {
			s.SetServerID(serverID)
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unknown storage backend %q", cfg.Backend)
	}
}
