package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
	"github.com/jackc/pgx/v5"
)

func (s *Storage) Enqueue(ctx context.Context, queue string, job *domain.Job) (string, error) {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if queue == "" {
		queue = "default"
	}
	job.Queue = queue
	if job.RetryMax == 0 {
		job.RetryMax = 10
	}
	if job.Args == nil {
		job.Args = []byte("[]")
	}
	now := time.Now().UTC()
	job.CreatedAt = now

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("enqueue begin: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.jobs (id, name, args, queue, state, retry_max, timeout_ms, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $8)`,
		job.ID, job.Name, job.Args, job.Queue, domain.StateEnqueued, job.RetryMax, timeoutMS(job.Timeout), now,
	)
	if err != nil {
		return "", fmt.Errorf("enqueue insert job: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.job_states (job_id, name, created_at)
		VALUES ($1, $2, $3)`,
		job.ID, domain.StateEnqueued, now,
	)
	if err != nil {
		return "", fmt.Errorf("enqueue insert state: %w", err)
	}

	if err := bumpCounter(ctx, tx, "enqueued", 1); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("enqueue commit: %w", err)
	}
	return job.ID, nil
}

func (s *Storage) Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*domain.JobTicket, error) {
	if len(queues) == 0 {
		return nil, serrors.ErrQueueEmpty
	}

	deadline := time.Now().Add(timeout)
	poll := 50 * time.Millisecond

	for {
		ticket, err := s.tryDequeue(ctx, queues)
		if err == nil {
			return ticket, nil
		}
		if !errors.Is(err, serrors.ErrQueueEmpty) {
			return nil, err
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, serrors.ErrQueueEmpty
		}
		wait := poll
		if wait > remaining {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (s *Storage) tryDequeue(ctx context.Context, queues []string) (*domain.JobTicket, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("dequeue begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var jobID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM gfire.jobs
		WHERE state = $1 AND queue = ANY($2)
		ORDER BY queue_token
		FOR UPDATE SKIP LOCKED
		LIMIT 1`,
		domain.StateEnqueued, queues,
	).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, serrors.ErrQueueEmpty
	}
	if err != nil {
		return nil, fmt.Errorf("dequeue select: %w", err)
	}

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE gfire.jobs
		SET state = $1, progress_at = $2, updated_at = $2
		WHERE id = $3`,
		domain.StateProcessing, now, jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("dequeue update: %w", err)
	}

	stateData, _ := json.Marshal(map[string]string{"server_id": s.serverID})
	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.job_states (job_id, name, data, created_at)
		VALUES ($1, $2, $3::jsonb, $4)`,
		jobID, domain.StateProcessing, stateData, now,
	)
	if err != nil {
		return nil, fmt.Errorf("dequeue state: %w", err)
	}

	if err := bumpCounter(ctx, tx, "dequeued", 1); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("dequeue commit: %w", err)
	}

	return &domain.JobTicket{
		JobID: jobID,
		Token: fmt.Sprintf("tok-%s-%d", jobID, now.UnixNano()),
	}, nil
}

func (s *Storage) Requeue(ctx context.Context, jobID string, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("requeue begin: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE gfire.jobs
		SET state = $1, progress_at = NULL, updated_at = $2
		WHERE id = $3`,
		domain.StateEnqueued, now, jobID,
	)
	if err != nil {
		return fmt.Errorf("requeue update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return serrors.ErrNotFound
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.job_states (job_id, name, reason, created_at)
		VALUES ($1, $2, $3, $4)`,
		jobID, domain.StateEnqueued, reason, now,
	)
	if err != nil {
		return fmt.Errorf("requeue state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("requeue commit: %w", err)
	}
	return nil
}

func (s *Storage) ApplyState(ctx context.Context, jobID string, expectedCurrent string, newState *domain.JobState) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("applystate begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var current string
	err = tx.QueryRow(ctx, `SELECT state FROM gfire.jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return serrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("applystate select: %w", err)
	}
	if current != expectedCurrent {
		return serrors.ErrStateConflict
	}

	now := newState.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
		newState.CreatedAt = now
	}

	data, err := json.Marshal(newState.Data)
	if err != nil {
		return fmt.Errorf("applystate marshal data: %w", err)
	}
	if newState.Data == nil {
		data = []byte("{}")
	}

	clearProgress := newState.Name != domain.StateProcessing
	_, err = tx.Exec(ctx, `
		UPDATE gfire.jobs
		SET state = $1,
		    progress_at = CASE WHEN $2 THEN NULL ELSE progress_at END,
		    updated_at = $3
		WHERE id = $4`,
		newState.Name, clearProgress, now, jobID,
	)
	if err != nil {
		return fmt.Errorf("applystate update: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.job_states (job_id, name, reason, data, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5)`,
		jobID, newState.Name, nullIfEmpty(newState.Reason), data, now,
	)
	if err != nil {
		return fmt.Errorf("applystate insert state: %w", err)
	}

	switch newState.Name {
	case domain.StateSucceeded:
		if err := bumpCounter(ctx, tx, "succeeded", 1); err != nil {
			return err
		}
	case domain.StateFailed:
		if err := bumpCounter(ctx, tx, "failed", 1); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("applystate commit: %w", err)
	}
	return nil
}

func (s *Storage) HeartbeatJob(ctx context.Context, jobID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE gfire.jobs SET progress_at = $1, updated_at = $1 WHERE id = $2`,
		time.Now().UTC(), jobID,
	)
	if err != nil {
		return fmt.Errorf("heartbeat job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return serrors.ErrNotFound
	}
	return nil
}

func (s *Storage) GetOrphanedJobs(ctx context.Context, staleAge time.Duration) ([]*domain.JobTicket, error) {
	cutoff := time.Now().UTC().Add(-staleAge)
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM gfire.jobs
		WHERE state = $1 AND progress_at IS NOT NULL AND progress_at < $2`,
		domain.StateProcessing, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("orphaned jobs: %w", err)
	}
	defer rows.Close()

	var tickets []*domain.JobTicket
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		tickets = append(tickets, &domain.JobTicket{JobID: id, Token: "orphan-" + id})
	}
	return tickets, rows.Err()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
