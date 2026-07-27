// Package memory provides an in-memory Storage implementation for development and testing.
//
// WARNING: This backend is NOT durable. Data is lost on restart.
// Use Redis/ValKey or PostgreSQL for production.
package memory

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
)

// Storage is an in-memory implementation of storage.Storage.
// All operations are safe for concurrent use.
type Storage struct {
	mu sync.RWMutex

	jobs        map[string]*domain.Job
	jobStates   map[string][]*domain.JobState
	queues      map[string]*list.List
	queueTokens map[string]int64

	scheduled  map[string]time.Time
	processing map[string]string
	progress   map[string]time.Time // jobID → last progress heartbeat

	recurring     map[string]*domain.RecurringJobEntry
	continuations map[string][]*domain.ContinuationEntry
	servers       map[string]*domain.ServerInfo
	counters      map[string]int64
	locks         map[string]*memoryLock
	dequeueCh     chan struct{}
}

// New creates a new empty in-memory Storage.
func New() *Storage {
	return &Storage{
		jobs:          make(map[string]*domain.Job),
		jobStates:     make(map[string][]*domain.JobState),
		queues:        make(map[string]*list.List),
		queueTokens:   make(map[string]int64),
		scheduled:     make(map[string]time.Time),
		processing:    make(map[string]string),
		progress:      make(map[string]time.Time),
		recurring:     make(map[string]*domain.RecurringJobEntry),
		continuations: make(map[string][]*domain.ContinuationEntry),
		servers:       make(map[string]*domain.ServerInfo),
		counters:      make(map[string]int64),
		locks:         make(map[string]*memoryLock),
		dequeueCh:     make(chan struct{}, 1),
	}
}

// ──────────────────────────────────────────────────────
// Queues & Job Dispatch
// ──────────────────────────────────────────────────────

func (s *Storage) Enqueue(ctx context.Context, queue string, job *domain.Job) (string, error) {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	job.CreatedAt = time.Now()
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.RetryMax == 0 {
		job.RetryMax = 10
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[job.ID] = job
	s.jobStates[job.ID] = []*domain.JobState{{
		Name:      domain.StateEnqueued,
		CreatedAt: time.Now(),
	}}
	s.queueTokens[job.ID] = time.Now().UnixNano()

	if s.queues[queue] == nil {
		s.queues[queue] = list.New()
	}
	s.queues[queue].PushBack(job.ID)

	s.counters["enqueued"]++

	select {
	case s.dequeueCh <- struct{}{}:
	default:
	}
	return job.ID, nil
}

func (s *Storage) Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*domain.JobTicket, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		s.mu.Lock()
		for _, q := range queues {
			l := s.queues[q]
			if l == nil || l.Len() == 0 {
				continue
			}
			e := l.Front()
			jobID := e.Value.(string)
			l.Remove(e)

			job, ok := s.jobs[jobID]
			if !ok {
				s.mu.Unlock()
				continue
			}

			s.processing[jobID] = "local"
			s.progress[jobID] = time.Now()
			s.jobStates[jobID] = append(s.jobStates[jobID], &domain.JobState{
				Name:      domain.StateProcessing,
				CreatedAt: time.Now(),
				Data:      map[string]string{"server_id": "local"},
			})
			s.counters["dequeued"]++
			token := fmt.Sprintf("tok-%s-%d", jobID, time.Now().UnixNano())
			s.mu.Unlock()
			return &domain.JobTicket{JobID: job.ID, Token: token}, nil
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, serrors.ErrQueueEmpty
		case <-s.dequeueCh:
		case <-ticker.C:
		}
	}
}

func (s *Storage) Requeue(ctx context.Context, jobID string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[jobID]; !ok {
		return serrors.ErrNotFound
	}

	// B7-006: reject Requeue on terminal states.
	states := s.jobStates[jobID]
	if len(states) > 0 {
		current := states[len(states)-1].Name
		if domain.IrreversibleStates[current] {
			return serrors.ErrTerminalState
		}
	}

	delete(s.processing, jobID)
	s.jobStates[jobID] = append(s.jobStates[jobID], &domain.JobState{
		Name:   domain.StateEnqueued,
		Reason: reason, CreatedAt: time.Now(),
	})

	job := s.jobs[jobID]
	if s.queues[job.Queue] == nil {
		s.queues[job.Queue] = list.New()
	}
	s.queues[job.Queue].PushBack(job.ID)

	select {
	case s.dequeueCh <- struct{}{}:
	default:
	}
	return nil
}

// ──────────────────────────────────────────────────────
// State Machine
// ──────────────────────────────────────────────────────

func (s *Storage) ApplyState(ctx context.Context, jobID string, expectedCurrent string, newState *domain.JobState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, ok := s.jobStates[jobID]
	if !ok {
		return serrors.ErrNotFound
	}

	current := domain.StateEnqueued
	if len(states) > 0 {
		current = states[len(states)-1].Name
	}
	if current != expectedCurrent {
		return serrors.ErrStateConflict
	}

	if newState.CreatedAt.IsZero() {
		newState.CreatedAt = time.Now()
	}
	s.jobStates[jobID] = append(states, newState)

	if expectedCurrent == domain.StateProcessing && newState.Name != domain.StateProcessing {
		delete(s.processing, jobID)
	}

	switch newState.Name {
	case domain.StateSucceeded:
		s.counters["succeeded"]++
	case domain.StateFailed:
		s.counters["failed"]++
	case domain.StateDead:
		s.counters["dead"]++
	}
	return nil
}

func (s *Storage) ScheduleRetry(ctx context.Context, jobID string, enqueueAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, ok := s.jobStates[jobID]
	if !ok {
		return serrors.ErrNotFound
	}
	current := states[len(states)-1].Name
	if current != domain.StateFailed {
		return serrors.ErrStateConflict
	}

	s.jobStates[jobID] = append(states, &domain.JobState{
		Name:      domain.StateScheduled,
		Data:      map[string]string{"enqueue_at": enqueueAt.Format(time.RFC3339)},
		CreatedAt: time.Now(),
	})
	s.scheduled[jobID] = enqueueAt
	s.counters["scheduled"]++
	return nil
}

func (s *Storage) SetJobResult(ctx context.Context, jobID string, result []byte) error {
	if len(result) > 65536 {
		result = result[:65536]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return serrors.ErrNotFound
	}
	job.Result = append([]byte(nil), result...)
	return nil
}

func (s *Storage) HeartbeatJob(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[jobID]; !ok {
		return serrors.ErrNotFound
	}
	s.progress[jobID] = time.Now()
	return nil
}

func (s *Storage) GetOrphanedJobs(ctx context.Context, staleAge time.Duration) ([]*domain.JobTicket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-staleAge)
	var tickets []*domain.JobTicket
	for jobID, lastProgress := range s.progress {
		if lastProgress.Before(cutoff) {
			tickets = append(tickets, &domain.JobTicket{JobID: jobID, Token: "orphan-" + jobID})
		}
	}
	return tickets, nil
}

func (s *Storage) GetJob(ctx context.Context, jobID string) (*domain.JobWithStates, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return nil, serrors.ErrNotFound
	}
	states := s.jobStates[jobID]
	statesCopy := make([]*domain.JobState, len(states))
	copy(statesCopy, states)
	return &domain.JobWithStates{Job: job, States: statesCopy}, nil
}

func (s *Storage) GetJobsByState(ctx context.Context, state string, offset, limit int) ([]*domain.JobWithStates, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*domain.JobWithStates
	for _, job := range s.jobs {
		states := s.jobStates[job.ID]
		current := ""
		if len(states) > 0 {
			current = states[len(states)-1].Name
		}
		if state == "" || current == state {
			statesCopy := make([]*domain.JobState, len(states))
			copy(statesCopy, states)
			result = append(result, &domain.JobWithStates{Job: job, States: statesCopy})
		}
	}
	if offset >= len(result) {
		return nil, nil
	}
	result = result[offset:]
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// ──────────────────────────────────────────────────────
// Queue Metadata
// ──────────────────────────────────────────────────────

func (s *Storage) GetQueueLength(ctx context.Context, queue string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l := s.queues[queue]
	if l == nil {
		return 0, nil
	}
	return int64(l.Len()), nil
}

func (s *Storage) GetQueues(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var queues []string
	for q := range s.queues {
		queues = append(queues, q)
	}
	return queues, nil
}

// ──────────────────────────────────────────────────────
// Server Registry
// ──────────────────────────────────────────────────────

func (s *Storage) RegisterServer(ctx context.Context, server *domain.ServerInfo, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	server.LastHeartbeat = time.Now()
	if server.Status == "" {
		server.Status = domain.ServerStatusActive
	}
	s.servers[server.ID] = server
	return nil
}

func (s *Storage) UnregisterServer(ctx context.Context, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.servers, serverID)
	return nil
}

func (s *Storage) Heartbeat(ctx context.Context, serverID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.servers[serverID]
	if !ok {
		return serrors.ErrNotFound
	}
	sv.LastHeartbeat = time.Now()
	sv.Status = domain.ServerStatusActive
	return nil
}

func (s *Storage) GetServers(ctx context.Context) ([]*domain.ServerInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.ServerInfo
	for _, sv := range s.servers {
		cp := *sv
		result = append(result, &cp)
	}
	return result, nil
}

// ──────────────────────────────────────────────────────
// Scheduled Jobs
// ──────────────────────────────────────────────────────

func (s *Storage) AddScheduled(ctx context.Context, enqueueAt time.Time, job *domain.Job) (string, error) {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	job.CreatedAt = time.Now()
	if job.Queue == "" {
		job.Queue = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	s.jobStates[job.ID] = []*domain.JobState{{
		Name: domain.StateScheduled, CreatedAt: time.Now(),
		Data: map[string]string{"enqueue_at": enqueueAt.Format(time.RFC3339)},
	}}
	s.scheduled[job.ID] = enqueueAt
	s.counters["scheduled"]++
	return job.ID, nil
}

func (s *Storage) GetDueScheduled(ctx context.Context, now time.Time, batchSize int) ([]*domain.JobTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []string
	for jobID, enqueueAt := range s.scheduled {
		if !enqueueAt.After(now) {
			due = append(due, jobID)
		}
		if len(due) >= batchSize {
			break
		}
	}

	tickets := make([]*domain.JobTicket, 0, len(due))
	for _, jobID := range due {
		delete(s.scheduled, jobID)
		s.jobStates[jobID] = append(s.jobStates[jobID], &domain.JobState{
			Name: domain.StateEnqueued, Reason: "scheduled", CreatedAt: time.Now(),
		})
		job := s.jobs[jobID]
		if s.queues[job.Queue] == nil {
			s.queues[job.Queue] = list.New()
		}
		s.queues[job.Queue].PushBack(jobID)
		s.counters["scheduled"]--
		s.counters["enqueued"]++
		tickets = append(tickets, &domain.JobTicket{JobID: jobID, Token: "sched-" + jobID})
	}

	if len(tickets) > 0 {
		select {
		case s.dequeueCh <- struct{}{}:
		default:
		}
	}
	return tickets, nil
}

func (s *Storage) RemoveScheduled(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.scheduled, jobID)
	return nil
}

// ──────────────────────────────────────────────────────
// Recurring Jobs
// ──────────────────────────────────────────────────────

func (s *Storage) UpsertRecurring(ctx context.Context, entry *domain.RecurringJobEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entry.UpdatedAt = now
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	s.recurring[entry.ID] = entry
	return nil
}

func (s *Storage) RemoveRecurring(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recurring, id)
	return nil
}

func (s *Storage) GetRecurringJobs(ctx context.Context) ([]*domain.RecurringJobEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.RecurringJobEntry
	for _, entry := range s.recurring {
		cp := *entry
		result = append(result, &cp)
	}
	return result, nil
}

// ──────────────────────────────────────────────────────
// Continuations
// ──────────────────────────────────────────────────────

func (s *Storage) AddContinuation(ctx context.Context, parentID string, entry *domain.ContinuationEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.CreatedAt = time.Now()
	s.continuations[parentID] = append(s.continuations[parentID], entry)
	return nil
}

func (s *Storage) GetContinuations(ctx context.Context, parentID string) ([]*domain.ContinuationEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.continuations[parentID]
	if entries == nil {
		return nil, nil
	}
	result := make([]*domain.ContinuationEntry, len(entries))
	copy(result, entries)
	return result, nil
}

func (s *Storage) RemoveContinuations(ctx context.Context, parentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.continuations, parentID)
	return nil
}

// ──────────────────────────────────────────────────────
// Counters
// ──────────────────────────────────────────────────────

func (s *Storage) IncrementCounter(ctx context.Context, key string, delta int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key] += delta
	return nil
}

func (s *Storage) GetCounter(ctx context.Context, key string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.counters[key], nil
}

func (s *Storage) GetAllCounters(ctx context.Context, skip, limit int) (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]int64, len(s.counters))
	for k, v := range s.counters {
		result[k] = v
	}
	return result, nil
}

// ──────────────────────────────────────────────────────
// Distributed Lock
// ──────────────────────────────────────────────────────

type memoryLock struct {
	resource  string
	ownerID   string
	expiresAt time.Time
	store     *Storage
	released  bool
}

func (l *memoryLock) Release(ctx context.Context) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if l.released {
		return serrors.ErrLockNotHeld
	}
	existing, ok := l.store.locks[l.resource]
	if !ok || existing.ownerID != l.ownerID {
		return serrors.ErrLockNotHeld
	}
	delete(l.store.locks, l.resource)
	l.released = true
	return nil
}

func (s *Storage) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (domain.Lock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.locks[resource]
	if ok && time.Now().Before(existing.expiresAt) {
		return nil, serrors.ErrLockNotHeld
	}
	lock := &memoryLock{
		resource:  resource,
		ownerID:   uuid.NewString(),
		expiresAt: time.Now().Add(ttl),
		store:     s,
	}
	s.locks[resource] = lock
	return lock, nil
}

// ──────────────────────────────────────────────────────
// Maintenance
// ──────────────────────────────────────────────────────

func (s *Storage) RemoveExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var deleted int64
	for jobID, states := range s.jobStates {
		if len(states) == 0 {
			continue
		}
		lastState := states[len(states)-1]
		if isTerminal(lastState.Name) && lastState.CreatedAt.Before(cutoff) {
			delete(s.jobs, jobID)
			delete(s.jobStates, jobID)
			delete(s.queueTokens, jobID)
			delete(s.processing, jobID)
			delete(s.progress, jobID)
			delete(s.scheduled, jobID)
			delete(s.continuations, jobID)
			deleted++
		}
	}
	return deleted, nil
}

func isTerminal(state string) bool {
	return state == domain.StateSucceeded ||
		state == domain.StateFailed ||
		state == domain.StateDeleted
}

// DeleteJob marks a job as Deleted (soft-delete, B5-014).
func (s *Storage) DeleteJob(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, ok := s.jobStates[jobID]
	if !ok {
		return serrors.ErrNotFound
	}
	current := states[len(states)-1].Name
	if domain.IrreversibleStates[current] {
		return serrors.ErrTerminalState
	}

	s.jobStates[jobID] = append(states, &domain.JobState{
		Name:      domain.StateDeleted,
		CreatedAt: time.Now(),
	})
	delete(s.processing, jobID)
	delete(s.progress, jobID)
	return nil
}

func (s *Storage) Close() error { return nil }
