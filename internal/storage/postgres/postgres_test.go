package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/storage"
	"github.com/hrodrig/gfire/internal/storage/postgres"
)

func dsn() string {
	if v := os.Getenv("GFIRE_PG_DSN"); v != "" {
		return v
	}
	return "postgres://gfire:gfire@localhost:5432/gfire?sslmode=disable"
}

func openTest(t *testing.T) *postgres.Storage {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn())
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed: %v", err)
	}

	// Reset schema data between tests (keep tables).
	_, err = pool.Exec(ctx, `
		TRUNCATE gfire.continuations, gfire.scheduled_jobs, gfire.job_states,
		         gfire.jobs, gfire.recurring_jobs, gfire.servers, gfire.counters, gfire.locks
		RESTART IDENTITY CASCADE`)
	if err != nil {
		pool.Close()
		t.Skipf("schema not migrated (run make migrate-up): %v", err)
	}

	t.Cleanup(pool.Close)
	return postgres.New(pool)
}

func TestPostgres_EnqueueDequeueSucceed(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	var store storage.Storage = s
	jobID, err := store.Enqueue(ctx, "default", &domain.Job{
		Name:  "test_job",
		Args:  []byte(`{"key":"value"}`),
		Queue: "default",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	length, err := store.GetQueueLength(ctx, "default")
	if err != nil || length != 1 {
		t.Fatalf("queue length: got %d err %v", length, err)
	}

	ticket, err := store.Dequeue(ctx, []string{"default"}, 2*time.Second)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if ticket.JobID != jobID {
		t.Fatalf("job id mismatch: %s vs %s", ticket.JobID, jobID)
	}

	js, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if js.CurrentState() != domain.StateProcessing {
		t.Fatalf("state: got %s", js.CurrentState())
	}
	if js.States[len(js.States)-1].Data["server_id"] != "local" {
		t.Fatalf("expected Processing server_id=local, got %v", js.States[len(js.States)-1].Data)
	}

	err = store.ApplyState(ctx, jobID, domain.StateProcessing, &domain.JobState{
		Name: domain.StateSucceeded,
		Data: map[string]string{"duration_ms": "42"},
	})
	if err != nil {
		t.Fatalf("ApplyState: %v", err)
	}

	js, _ = store.GetJob(ctx, jobID)
	if js.CurrentState() != domain.StateSucceeded {
		t.Fatalf("final state: %s", js.CurrentState())
	}
	if len(js.States) != 3 {
		t.Fatalf("expected 3 states, got %d", len(js.States))
	}

	enqueued, _ := store.GetCounter(ctx, "enqueued")
	succeeded, _ := store.GetCounter(ctx, "succeeded")
	if enqueued != 1 || succeeded != 1 {
		t.Fatalf("counters enqueued=%d succeeded=%d", enqueued, succeeded)
	}
}

func TestPostgres_StateConflict(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	jobID, _ := s.Enqueue(ctx, "default", &domain.Job{Name: "conflict"})
	err := s.ApplyState(ctx, jobID, domain.StateProcessing, &domain.JobState{Name: domain.StateSucceeded})
	if err == nil {
		t.Fatal("expected ErrStateConflict")
	}
}

func TestPostgres_ScheduledJob(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	jobID, err := s.AddScheduled(ctx, time.Now().Add(-time.Hour), &domain.Job{
		Name:  "sched",
		Queue: "default",
	})
	if err != nil {
		t.Fatalf("AddScheduled: %v", err)
	}

	tickets, err := s.GetDueScheduled(ctx, time.Now(), 100)
	if err != nil {
		t.Fatalf("GetDueScheduled: %v", err)
	}
	if len(tickets) != 1 || tickets[0].JobID != jobID {
		t.Fatalf("tickets: %+v", tickets)
	}

	scheduled, _ := s.GetCounter(ctx, "scheduled")
	enqueued, _ := s.GetCounter(ctx, "enqueued")
	if scheduled != 0 || enqueued != 1 {
		t.Fatalf("counters after promote: scheduled=%d enqueued=%d", scheduled, enqueued)
	}

	js, _ := s.GetJob(ctx, jobID)
	if js.CurrentState() != domain.StateEnqueued {
		t.Fatalf("state: %s", js.CurrentState())
	}
}

func TestPostgres_LockAndServers(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	lock, err := s.AcquireLock(ctx, "resource-a", 10*time.Second)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	_, err = s.AcquireLock(ctx, "resource-a", 10*time.Second)
	if err == nil {
		t.Fatal("expected second acquire fail")
	}
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	err = s.RegisterServer(ctx, &domain.ServerInfo{
		ID: "node-1", WorkerCount: 4, Queues: []string{"default"},
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("RegisterServer: %v", err)
	}
	servers, _ := s.GetServers(ctx)
	if len(servers) != 1 {
		t.Fatalf("servers: %d", len(servers))
	}
	_ = s.UnregisterServer(ctx, "node-1")
}

func TestPostgres_ContinuationsAndOrphans(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	jobID, _ := s.Enqueue(ctx, "default", &domain.Job{Name: "long"})
	ticket, _ := s.Dequeue(ctx, []string{"default"}, 2*time.Second)

	err := s.AddContinuation(ctx, jobID, &domain.ContinuationEntry{
		ChildName: "child", Condition: domain.ConditionOnSucceeded,
	})
	if err != nil {
		t.Fatalf("AddContinuation: %v", err)
	}
	entries, _ := s.GetContinuations(ctx, jobID)
	if len(entries) != 1 {
		t.Fatalf("continuations: %d", len(entries))
	}

	_ = s.HeartbeatJob(ctx, ticket.JobID)
	orphans, err := s.GetOrphanedJobs(ctx, 0)
	if err != nil {
		t.Fatalf("GetOrphanedJobs: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("orphans: %d", len(orphans))
	}
}
