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

// TestRecurring_FiresViaEngine starts an engine, registers a recurring entry,
// and verifies the job is enqueued within a few seconds.
func TestRecurring_FiresViaEngine(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	entry := &domain.RecurringJobEntry{
		ID:       "test-cron",
		JobName:  "work",
		Args:     []byte(`{"x":1}`),
		Queue:    "default",
		CronExpr: "@every 1s",
		Enabled:  true,
	}
	if err := store.UpsertRecurring(ctx, entry); err != nil {
		t.Fatalf("UpsertRecurring: %v", err)
	}

	cfg := engine.DefaultConfig()
	cfg.Workers = 2
	eng := engine.New(store, cfg, handler.NopRunner{}, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := store.GetJobsByState(ctx, "", 0, 50)
		if err != nil {
			continue
		}
		for _, jw := range jobs {
			if jw.Job.Name == "work" {
				return // success — cron fired and enqueued a "work" job
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("recurring job was never enqueued")
}

// TestRecurring_DisabledEntryDoesNotFire is a smoke test: a disabled entry
// should not produce any jobs within a short window.
func TestRecurring_DisabledEntryDoesNotFire(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	entry := &domain.RecurringJobEntry{
		ID:       "ghost-cron",
		JobName:  "ghost",
		Args:     []byte("{}"),
		Queue:    "default",
		CronExpr: "@every 1s",
		Enabled:  false,
	}
	if err := store.UpsertRecurring(ctx, entry); err != nil {
		t.Fatalf("UpsertRecurring: %v", err)
	}

	cfg := engine.DefaultConfig()
	cfg.Workers = 1
	eng := engine.New(store, cfg, handler.NopRunner{}, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := store.GetJobsByState(ctx, "", 0, 50)
		if err != nil {
			continue
		}
		for _, jw := range jobs {
			if jw.Job.Name == "ghost" {
				t.Fatal("disabled recurring job fired")
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}
