// Package redis provides a Redis/ValKey implementation of storage.Storage.
//
// Redis and ValKey share the same RESP protocol; connect via address only.
// Dequeue uses BRPOP plus Lua scripts for atomic state transitions.
package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/hrodrig/gfire/internal/storage"
)

// Storage implements storage.Storage backed by Redis or ValKey.
type Storage struct {
	client   goredis.UniversalClient
	serverID string
}

// Options configures a Redis/ValKey connection.
type Options struct {
	Addr     string
	Password string
	DB       int
}

// New creates a Storage from an existing Redis client.
func New(client goredis.UniversalClient) *Storage {
	return &Storage{client: client, serverID: "local"}
}

// NewWithServerID creates a Storage with an explicit server ID written into
// Processing job_state data (for orphan recovery).
func NewWithServerID(client goredis.UniversalClient, serverID string) *Storage {
	if serverID == "" {
		serverID = "local"
	}
	return &Storage{client: client, serverID: serverID}
}

// SetServerID sets the server ID written into Processing job_state data.
func (s *Storage) SetServerID(serverID string) {
	if serverID == "" {
		serverID = "local"
	}
	s.serverID = serverID
}

// Open connects to Redis/ValKey and returns a Storage.
func Open(ctx context.Context, opts Options) (*Storage, error) {
	if opts.Addr == "" {
		opts.Addr = "localhost:6379"
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}
	return New(client), nil
}

// Compile-time check that Storage implements storage.Storage.
var _ storage.Storage = (*Storage)(nil)

// Close shuts down the Redis client.
func (s *Storage) Close() error {
	return s.client.Close()
}

// FlushDB removes all keys in the current Redis database (integration tests only).
func (s *Storage) FlushDB(ctx context.Context) error {
	return s.client.FlushDB(ctx).Err()
}

func (s *Storage) bumpCounter(ctx context.Context, key string, delta int64) error {
	if err := s.client.IncrBy(ctx, counterKey(key), delta).Err(); err != nil {
		return fmt.Errorf("bump counter %s: %w", key, err)
	}
	return nil
}
