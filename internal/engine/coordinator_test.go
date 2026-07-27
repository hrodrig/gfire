package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/hrodrig/gfire/internal/engine"
	"github.com/hrodrig/gfire/internal/handler"
	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/storage/memory"
)

func TestStaleServerUnregister(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	// Register a dead server with a very old heartbeat.
	deadServer := &domain.ServerInfo{
		ID:            "dead-node",
		StartedAt:     time.Now().Add(-1 * time.Hour),
		LastHeartbeat: time.Now().Add(-1 * time.Hour),
		WorkerCount:   4,
		Queues:        []string{"default"},
		Status:        domain.ServerStatusActive,
	}
	if err := store.RegisterServer(ctx, deadServer, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	cfg := engine.DefaultConfig()
	cfg.ServerID = "alive-node"
	cfg.Workers = 1
	cfg.ServerHeartbeatTTL = 1 * time.Second          // stale after 1s
	cfg.ServerHeartbeatEvery = 500 * time.Millisecond // tick fast
	cfg.OrphanJobStaleAge = 5 * time.Minute
	eng := engine.New(store, cfg, handler.NopRunner{}, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	// Unregister threshold = heartbeatTTL × 3 = 3s.
	// The dead server's heartbeat is 1h old, so it should be unregistered
	// on the first stale sweep (~3s after start).
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		servers, err := store.GetServers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		gone := true
		for _, sv := range servers {
			if sv.ID == "dead-node" {
				gone = false
				break
			}
		}
		if gone {
			return // success
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("dead server was never unregistered")
}

func TestAliveServerStaysRegistered(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	cfg := engine.DefaultConfig()
	cfg.ServerID = "alive-node"
	cfg.Workers = 1
	cfg.ServerHeartbeatTTL = 5 * time.Second
	cfg.ServerHeartbeatEvery = 1 * time.Second
	cfg.OrphanJobStaleAge = 5 * time.Minute
	eng := engine.New(store, cfg, handler.NopRunner{}, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	// Wait for at least one heartbeat + stale sweep cycle.
	time.Sleep(4 * time.Second)

	servers, err := store.GetServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range servers {
		if sv.ID == "alive-node" {
			return // still registered = good
		}
	}
	t.Fatal("alive server was incorrectly unregistered")
}
