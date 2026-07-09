// Package postgres provides a PostgreSQL implementation of storage.Storage.
//
// Schema: all tables live in the `gfire` schema.
// Dequeue: uses FOR UPDATE SKIP LOCKED for concurrency-safe atomic fetch.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hrodrig/gfire/internal/storage"
)

// Storage implements storage.Storage backed by PostgreSQL.
type Storage struct {
	pool     *pgxpool.Pool
	serverID string
}

// New creates a new PostgreSQL Storage backed by the given connection pool.
// The default server ID written into Processing job_state data is "local".
func New(pool *pgxpool.Pool) *Storage {
	return &Storage{pool: pool, serverID: "local"}
}

// NewWithServerID creates a PostgreSQL Storage with an explicit server ID
// stored in Processing job_state data (for orphan recovery).
func NewWithServerID(pool *pgxpool.Pool, serverID string) *Storage {
	if serverID == "" {
		serverID = "local"
	}
	return &Storage{pool: pool, serverID: serverID}
}

// SetServerID sets the server ID written into Processing job_state data.
// Empty string resets to "local".
func (s *Storage) SetServerID(serverID string) {
	if serverID == "" {
		serverID = "local"
	}
	s.serverID = serverID
}

// Open creates a connection pool from DSN and returns a Storage.
func Open(ctx context.Context, dsn string) (*Storage, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return New(pool), nil
}

// Compile-time check that Storage implements storage.Storage.
var _ storage.Storage = (*Storage)(nil)

// Close releases the connection pool.
func (s *Storage) Close() error {
	s.pool.Close()
	return nil
}

func timeoutMS(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return d.Milliseconds()
}

func durationFromMS(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}
