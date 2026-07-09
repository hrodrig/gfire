package memory_test

import (
	"context"
	"testing"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/storage/memory"
)

// TestStorage_EnqueueDequeueSucceed validates the happy path:
// 1. Enqueue a job
// 2. Dequeue it (state → Processing)
// 3. Apply Succeeded state
// 4. Verify full state history
func TestStorage_EnqueueDequeueSucceed(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	// ── Enqueue ────────────────────────────────────────────
	job := &domain.Job{
		Name:  "test_job",
		Args:  []byte(`{"key":"value"}`),
		Queue: "default",
	}

	jobID, err := s.Enqueue(ctx, "default", job)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if jobID == "" {
		t.Fatal("Enqueue returned empty job ID")
	}

	// Verify queue length
	length, err := s.GetQueueLength(ctx, "default")
	if err != nil {
		t.Fatalf("GetQueueLength: %v", err)
	}
	if length != 1 {
		t.Fatalf("expected queue length 1, got %d", length)
	}

	// ── Dequeue ────────────────────────────────────────────
	ticket, err := s.Dequeue(ctx, []string{"default"}, 2*time.Second)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if ticket.JobID != jobID {
		t.Fatalf("expected jobID %s, got %s", jobID, ticket.JobID)
	}
	if ticket.Token == "" {
		t.Fatal("Dequeue returned empty token")
	}

	// Queue should now be empty
	length, _ = s.GetQueueLength(ctx, "default")
	if length != 0 {
		t.Fatalf("expected queue length 0 after dequeue, got %d", length)
	}

	// ── Verify state is Processing ─────────────────────────
	js, err := s.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if js.CurrentState() != domain.StateProcessing {
		t.Fatalf("expected state Processing, got %s", js.CurrentState())
	}
	if len(js.States) != 2 {
		t.Fatalf("expected 2 state entries (Enqueued + Processing), got %d", len(js.States))
	}
	if js.States[1].Data["server_id"] != "local" {
		t.Fatalf("expected Processing server_id=local, got %v", js.States[1].Data)
	}

	// ── Apply Succeeded ────────────────────────────────────
	err = s.ApplyState(ctx, jobID, domain.StateProcessing, &domain.JobState{
		Name: domain.StateSucceeded,
		Data: map[string]string{"duration_ms": "42"},
	})
	if err != nil {
		t.Fatalf("ApplyState(Succeeded): %v", err)
	}

	// ── Verify final state ─────────────────────────────────
	js, err = s.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if js.CurrentState() != domain.StateSucceeded {
		t.Fatalf("expected state Succeeded, got %s", js.CurrentState())
	}
	if len(js.States) != 3 {
		t.Fatalf("expected 3 state entries, got %d", len(js.States))
	}

	// Verify state names in order
	expectedStates := []string{domain.StateEnqueued, domain.StateProcessing, domain.StateSucceeded}
	for i, st := range js.States {
		if st.Name != expectedStates[i] {
			t.Fatalf("state[%d]: expected %s, got %s", i, expectedStates[i], st.Name)
		}
	}

	// Verify job data is intact
	if js.Job.Name != "test_job" {
		t.Fatalf("expected job name test_job, got %s", js.Job.Name)
	}

	// ── Verify counters ────────────────────────────────────
	enqueued, _ := s.GetCounter(ctx, "enqueued")
	if enqueued != 1 {
		t.Fatalf("expected enqueued=1, got %d", enqueued)
	}
	succeeded, _ := s.GetCounter(ctx, "succeeded")
	if succeeded != 1 {
		t.Fatalf("expected succeeded=1, got %d", succeeded)
	}
}

// TestStorage_StateConflict verifies that ApplyState rejects
// a transition from the wrong current state.
func TestStorage_StateConflict(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	jobID, err := s.Enqueue(ctx, "default", &domain.Job{Name: "conflict_test"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Try to transition from Processing → Succeeded when the job is still Enqueued
	err = s.ApplyState(ctx, jobID, domain.StateProcessing, &domain.JobState{
		Name: domain.StateSucceeded,
	})
	if err == nil {
		t.Fatal("expected ErrStateConflict, got nil")
	}
}

// TestStorage_DequeueTimeout verifies that Dequeue returns
// ErrQueueEmpty when no job is available within the timeout.
func TestStorage_DequeueTimeout(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	_, err := s.Dequeue(ctx, []string{"default"}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected ErrQueueEmpty, got nil")
	}
}

// TestStorage_Requeue verifies that a failed job can be
// requeued manually and dequeued again.
func TestStorage_Requeue(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	jobID, _ := s.Enqueue(ctx, "default", &domain.Job{Name: "requeue_test"})
	_, _ = s.Dequeue(ctx, []string{"default"}, 2*time.Second)

	// Mark as failed
	s.ApplyState(ctx, jobID, domain.StateProcessing, &domain.JobState{
		Name:   domain.StateFailed,
		Reason: "something broke",
	})

	// Requeue
	err := s.Requeue(ctx, jobID, "manual retry")
	if err != nil {
		t.Fatalf("Requeue: %v", err)
	}

	// Should be dequeuable again
	ticket, err := s.Dequeue(ctx, []string{"default"}, 2*time.Second)
	if err != nil {
		t.Fatalf("Dequeue after requeue: %v", err)
	}
	if ticket.JobID != jobID {
		t.Fatalf("expected jobID %s, got %s", jobID, ticket.JobID)
	}
}

// TestStorage_ScheduledJob verifies scheduled jobs are moved
// to the queue when their time arrives.
func TestStorage_ScheduledJob(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	jobID, err := s.AddScheduled(ctx, time.Now().Add(-1*time.Hour), &domain.Job{
		Name:  "scheduled_test",
		Args:  []byte(`{"delay":true}`),
		Queue: "default",
	})
	if err != nil {
		t.Fatalf("AddScheduled: %v", err)
	}

	// GetDueScheduled should pick it up (it's in the past)
	tickets, err := s.GetDueScheduled(ctx, time.Now(), 100)
	if err != nil {
		t.Fatalf("GetDueScheduled: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 due scheduled job, got %d", len(tickets))
	}
	if tickets[0].JobID != jobID {
		t.Fatalf("expected jobID %s, got %s", jobID, tickets[0].JobID)
	}

	scheduled, _ := s.GetCounter(ctx, "scheduled")
	enqueued, _ := s.GetCounter(ctx, "enqueued")
	if scheduled != 0 {
		t.Fatalf("expected scheduled=0 after promote, got %d", scheduled)
	}
	if enqueued != 1 {
		t.Fatalf("expected enqueued=1 after promote, got %d", enqueued)
	}

	// Should now be in Enqueued state
	js, _ := s.GetJob(ctx, jobID)
	if js.CurrentState() != domain.StateEnqueued {
		t.Fatalf("expected Enqueued, got %s", js.CurrentState())
	}
}

// TestStorage_GetJobsByState verifies filtering jobs by state.
func TestStorage_GetJobsByState(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	s.Enqueue(ctx, "default", &domain.Job{Name: "job_a"})
	s.Enqueue(ctx, "default", &domain.Job{Name: "job_b"})
	s.Enqueue(ctx, "other", &domain.Job{Name: "job_c"})

	// Dequeue job_a
	_, _ = s.Dequeue(ctx, []string{"default"}, 2*time.Second)

	jobs, err := s.GetJobsByState(ctx, domain.StateEnqueued, 0, 10)
	if err != nil {
		t.Fatalf("GetJobsByState: %v", err)
	}
	// Should have job_b (default queue, enqueued) + job_c (other queue, enqueued)
	// Note: job_a was dequeued, so it's Processing
	if len(jobs) != 2 {
		t.Fatalf("expected 2 enqueued jobs, got %d", len(jobs))
	}

	// Pagination test
	paginated, err := s.GetJobsByState(ctx, domain.StateEnqueued, 0, 1)
	if err != nil {
		t.Fatalf("GetJobsByState paginated: %v", err)
	}
	if len(paginated) != 1 {
		t.Fatalf("expected 1 paginated result, got %d", len(paginated))
	}
}

// TestStorage_ServerHeartbeat verifies server registration and heartbeats.
func TestStorage_ServerHeartbeat(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	err := s.RegisterServer(ctx, &domain.ServerInfo{
		ID:          "node-1",
		WorkerCount: 4,
		Queues:      []string{"default", "critical"},
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("RegisterServer: %v", err)
	}

	servers, err := s.GetServers(ctx)
	if err != nil {
		t.Fatalf("GetServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}

	// Heartbeat
	err = s.Heartbeat(ctx, "node-1", 30*time.Second)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Unregister
	err = s.UnregisterServer(ctx, "node-1")
	if err != nil {
		t.Fatalf("UnregisterServer: %v", err)
	}

	servers, _ = s.GetServers(ctx)
	if len(servers) != 0 {
		t.Fatalf("expected 0 servers after unregister, got %d", len(servers))
	}
}

// TestStorage_Continuations verifies continuation CRUD.
func TestStorage_Continuations(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	parentID := "parent-1"
	entry := &domain.ContinuationEntry{
		ChildName:  "child_job",
		ChildArgs:  []byte(`{"msg":"hello"}`),
		ChildQueue: "default",
		Condition:  domain.ConditionOnSucceeded,
	}

	err := s.AddContinuation(ctx, parentID, entry)
	if err != nil {
		t.Fatalf("AddContinuation: %v", err)
	}

	entries, err := s.GetContinuations(ctx, parentID)
	if err != nil {
		t.Fatalf("GetContinuations: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 continuation, got %d", len(entries))
	}
	if entries[0].Condition != domain.ConditionOnSucceeded {
		t.Fatalf("expected condition OnSucceeded, got %s", entries[0].Condition)
	}

	// Remove and verify
	err = s.RemoveContinuations(ctx, parentID)
	if err != nil {
		t.Fatalf("RemoveContinuations: %v", err)
	}
	entries, _ = s.GetContinuations(ctx, parentID)
	if len(entries) != 0 {
		t.Fatalf("expected 0 continuations after removal, got %d", len(entries))
	}
}

// TestStorage_AcquireLock verifies locking and releasing.
func TestStorage_AcquireLock(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	lock, err := s.AcquireLock(ctx, "resource-a", 10*time.Second)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if lock == nil {
		t.Fatal("AcquireLock returned nil lock")
	}

	// Second acquire on same resource should fail
	_, err = s.AcquireLock(ctx, "resource-a", 10*time.Second)
	if err == nil {
		t.Fatal("expected second acquire to fail, got nil")
	}

	// Release
	err = lock.Release(ctx)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Now should be acquirable again
	lock2, err := s.AcquireLock(ctx, "resource-a", 10*time.Second)
	if err != nil {
		t.Fatalf("AcquireLock after release: %v", err)
	}
	_ = lock2.Release(ctx)
}

// TestStorage_RemoveExpired verifies cleanup of old terminal jobs.
func TestStorage_RemoveExpired(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	id, _ := s.Enqueue(ctx, "default", &domain.Job{Name: "cleanable"})
	s.Dequeue(ctx, []string{"default"}, 2*time.Second)
	s.ApplyState(ctx, id, domain.StateProcessing, &domain.JobState{
		Name:      domain.StateSucceeded,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	})

	// RemoveExpired with cutoff in the past
	deleted, err := s.RemoveExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("RemoveExpired: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	_, err = s.GetJob(ctx, id)
	if err == nil {
		t.Fatal("expected ErrNotFound after RemoveExpired")
	}
}

// TestStorage_Queues verifies queue enumeration.
func TestStorage_Queues(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	s.Enqueue(ctx, "alpha", &domain.Job{Name: "a"})
	s.Enqueue(ctx, "beta", &domain.Job{Name: "b"})
	s.Enqueue(ctx, "alpha", &domain.Job{Name: "c"})

	queues, err := s.GetQueues(ctx)
	if err != nil {
		t.Fatalf("GetQueues: %v", err)
	}
	if len(queues) != 2 {
		t.Fatalf("expected 2 queues, got %d", len(queues))
	}

	alphaLen, _ := s.GetQueueLength(ctx, "alpha")
	if alphaLen != 2 {
		t.Fatalf("expected alpha queue length 2, got %d", alphaLen)
	}

	betaLen, _ := s.GetQueueLength(ctx, "beta")
	if betaLen != 1 {
		t.Fatalf("expected beta queue length 1, got %d", betaLen)
	}
}

// TestStorage_JobHeartbeat verifies that HeartbeatJob updates the
// progress timestamp and GetOrphanedJobs correctly identifies stale jobs.
func TestStorage_JobHeartbeat(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	// Enqueue and dequeue a job
	s.Enqueue(ctx, "default", &domain.Job{Name: "long_running"})
	ticket, _ := s.Dequeue(ctx, []string{"default"}, 2*time.Second)

	// Job should have initial progress
	orphans, err := s.GetOrphanedJobs(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("GetOrphanedJobs: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("expected 0 orphaned jobs (freshly dequeued), got %d", len(orphans))
	}

	// Send a heartbeat (simulates worker saying "still alive")
	err = s.HeartbeatJob(ctx, ticket.JobID)
	if err != nil {
		t.Fatalf("HeartbeatJob: %v", err)
	}

	// Job should still NOT be orphaned (we just heartbeated)
	orphans, _ = s.GetOrphanedJobs(ctx, 1*time.Hour)
	if len(orphans) != 0 {
		t.Fatalf("expected 0 orphaned jobs after heartbeat, got %d", len(orphans))
	}

	// Now use a very short stale age — job should appear orphaned
	// Because the heartbeat is fresh but the stale age is 0
	orphans, _ = s.GetOrphanedJobs(ctx, 0) // 0 = any job in progress is stale
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphaned job with 0 stale age, got %d", len(orphans))
	}
	if orphans[0].JobID != ticket.JobID {
		t.Fatalf("expected orphan jobID %s, got %s", ticket.JobID, orphans[0].JobID)
	}
}

// TestStorage_JobHeartbeatNotFound verifies HeartbeatJob on unknown domain.
func TestStorage_JobHeartbeatNotFound(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	err := s.HeartbeatJob(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected ErrNotFound for unknown job")
	}
}
