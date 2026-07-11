package engine_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrodrig/gfire/internal/engine"
	"github.com/hrodrig/gfire/internal/handler"
	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/storage/memory"
)

func TestEngine_Processes100Jobs(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := engine.DefaultConfig()
	cfg.Workers = 4
	cfg.DequeueTimeout = 500 * time.Millisecond

	var processed atomic.Int64
	runner := handler.Func(func(_ context.Context, _ *domain.Job) ([]byte, error) {
		processed.Add(1)
		return []byte(`{"ok":true}`), nil
	})

	eng := engine.New(store, cfg, runner, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	}()

	const n = 100
	for i := 0; i < n; i++ {
		_, err := store.Enqueue(ctx, "default", &domain.Job{
			Name:  "work",
			Args:  []byte(fmt.Sprintf(`{"i":%d}`, i)),
			Queue: "default",
		})
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := waitForSucceeded(waitCtx, store, n); err != nil {
		t.Fatal(err)
	}
	if got := processed.Load(); got != n {
		t.Fatalf("handler runs: got %d want %d", got, n)
	}
}

func TestEngine_ResultCapture(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := engine.DefaultConfig()
	cfg.Workers = 1

	runner := handler.Func(func(_ context.Context, job *domain.Job) ([]byte, error) {
		return []byte(`{"n":` + string(job.Args) + `}`), nil
	})
	eng := engine.New(store, cfg, runner, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer stopEngine(t, eng)

	id, err := store.Enqueue(ctx, "default", &domain.Job{
		Name:  "work",
		Args:  []byte(`1`),
		Queue: "default",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := waitForJobState(waitCtx, store, id, domain.StateSucceeded); err != nil {
		t.Fatal(err)
	}
	jw, err := store.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if len(jw.Job.Result) == 0 {
		t.Fatal("expected Job.Result set")
	}
}

func TestEngine_DeadAfterRetries(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := engine.DefaultConfig()
	cfg.Workers = 1
	cfg.SchedulerInterval = 50 * time.Millisecond

	runner := handler.Func(func(_ context.Context, _ *domain.Job) ([]byte, error) {
		return nil, errors.New("boom")
	})
	eng := engine.New(store, cfg, runner, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer stopEngine(t, eng)

	id, err := store.Enqueue(ctx, "default", &domain.Job{
		Name:     "fail",
		Queue:    "default",
		RetryMax: 1,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := waitForJobState(waitCtx, store, id, domain.StateDead); err != nil {
		t.Fatal(err)
	}
}

func TestEngine_CancelInFlight(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := engine.DefaultConfig()
	cfg.Workers = 1

	block := make(chan struct{})
	runner := handler.Func(func(runCtx context.Context, _ *domain.Job) ([]byte, error) {
		select {
		case <-runCtx.Done():
			return nil, runCtx.Err()
		case <-block:
			return []byte(`{}`), nil
		}
	})
	eng := engine.New(store, cfg, runner, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer stopEngine(t, eng)

	id, err := store.Enqueue(ctx, "default", &domain.Job{Name: "slow", Queue: "default"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jw, err := store.GetJob(ctx, id)
		if err == nil && jw.CurrentState() == domain.StateProcessing {
			if err := eng.CancelJob(id); err != nil {
				t.Fatalf("CancelJob: %v", err)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := waitForJobState(waitCtx, store, id, domain.StateCancelled); err != nil {
		t.Fatal(err)
	}
	close(block)
}

func stopEngine(t *testing.T, eng *engine.Engine) {
	t.Helper()
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := eng.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func waitForSucceeded(ctx context.Context, store *memory.Storage, want int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		jobs, err := store.GetJobsByState(ctx, domain.StateSucceeded, 0, want+1)
		if err != nil {
			return err
		}
		if len(jobs) >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout: got %d succeeded, want %d", len(jobs), want)
		case <-ticker.C:
		}
	}
}

func waitForJobState(ctx context.Context, store *memory.Storage, id, state string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		jw, err := store.GetJob(ctx, id)
		if err == nil && jw.CurrentState() == state {
			return nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return err
			}
			return fmt.Errorf("timeout: job %s state %s", id, jw.CurrentState())
		case <-ticker.C:
		}
	}
}
