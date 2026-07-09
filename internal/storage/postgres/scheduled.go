package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hrodrig/gfire/internal/job"
)

func (s *Storage) AddScheduled(ctx context.Context, enqueueAt time.Time, job *domain.Job) (string, error) {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.Queue == "" {
		job.Queue = "default"
	}
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
		return "", fmt.Errorf("add scheduled begin: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.jobs (id, name, args, queue, state, retry_max, timeout_ms, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $8)`,
		job.ID, job.Name, job.Args, job.Queue, domain.StateScheduled, job.RetryMax, timeoutMS(job.Timeout), now,
	)
	if err != nil {
		return "", fmt.Errorf("add scheduled job: %w", err)
	}

	data, _ := json.Marshal(map[string]string{"enqueue_at": enqueueAt.UTC().Format(time.RFC3339)})
	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.job_states (job_id, name, data, created_at)
		VALUES ($1, $2, $3::jsonb, $4)`,
		job.ID, domain.StateScheduled, data, now,
	)
	if err != nil {
		return "", fmt.Errorf("add scheduled state: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.scheduled_jobs (id, job_id, enqueue_at, created_at)
		VALUES ($1, $2, $3, $4)`,
		job.ID, job.ID, enqueueAt.UTC(), now,
	)
	if err != nil {
		return "", fmt.Errorf("add scheduled row: %w", err)
	}

	if err := bumpCounter(ctx, tx, "scheduled", 1); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("add scheduled commit: %w", err)
	}
	return job.ID, nil
}

func (s *Storage) GetDueScheduled(ctx context.Context, now time.Time, batchSize int) ([]*domain.JobTicket, error) {
	if batchSize <= 0 {
		batchSize = 100
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("due scheduled begin: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT job_id FROM gfire.scheduled_jobs
		WHERE enqueue_at <= $1
		ORDER BY enqueue_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2`,
		now.UTC(), batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("due scheduled select: %w", err)
	}

	var jobIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		jobIDs = append(jobIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tickets := make([]*domain.JobTicket, 0, len(jobIDs))
	ts := time.Now().UTC()
	for _, jobID := range jobIDs {
		_, err = tx.Exec(ctx, `DELETE FROM gfire.scheduled_jobs WHERE job_id = $1`, jobID)
		if err != nil {
			return nil, fmt.Errorf("due scheduled delete: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE gfire.jobs SET state = $1, updated_at = $2 WHERE id = $3`,
			domain.StateEnqueued, ts, jobID,
		)
		if err != nil {
			return nil, fmt.Errorf("due scheduled update: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO gfire.job_states (job_id, name, reason, created_at)
			VALUES ($1, $2, $3, $4)`,
			jobID, domain.StateEnqueued, "scheduled", ts,
		)
		if err != nil {
			return nil, fmt.Errorf("due scheduled state: %w", err)
		}
		if err := bumpCounter(ctx, tx, "scheduled", -1); err != nil {
			return nil, err
		}
		if err := bumpCounter(ctx, tx, "enqueued", 1); err != nil {
			return nil, err
		}
		tickets = append(tickets, &domain.JobTicket{JobID: jobID, Token: "sched-" + jobID})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("due scheduled commit: %w", err)
	}
	return tickets, nil
}

func (s *Storage) RemoveScheduled(ctx context.Context, jobID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM gfire.scheduled_jobs WHERE job_id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("remove scheduled: %w", err)
	}
	return nil
}

func (s *Storage) UpsertRecurring(ctx context.Context, entry *domain.RecurringJobEntry) error {
	now := time.Now().UTC()
	entry.UpdatedAt = now
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.Args == nil {
		entry.Args = []byte("[]")
	}
	if entry.Queue == "" {
		entry.Queue = "default"
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO gfire.recurring_jobs
			(id, job_name, args, queue, cron_expr, last_run, next_run, enabled, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			job_name = EXCLUDED.job_name,
			args = EXCLUDED.args,
			queue = EXCLUDED.queue,
			cron_expr = EXCLUDED.cron_expr,
			last_run = EXCLUDED.last_run,
			next_run = EXCLUDED.next_run,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at`,
		entry.ID, entry.JobName, entry.Args, entry.Queue, entry.CronExpr,
		entry.LastRun, entry.NextRun, entry.Enabled, entry.CreatedAt, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert recurring: %w", err)
	}
	return nil
}

func (s *Storage) RemoveRecurring(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM gfire.recurring_jobs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("remove recurring: %w", err)
	}
	return nil
}

func (s *Storage) GetRecurringJobs(ctx context.Context) ([]*domain.RecurringJobEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, job_name, args, queue, cron_expr, last_run, next_run, enabled, created_at, updated_at
		FROM gfire.recurring_jobs
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("get recurring: %w", err)
	}
	defer rows.Close()

	var result []*domain.RecurringJobEntry
	for rows.Next() {
		var e domain.RecurringJobEntry
		var args []byte
		if err := rows.Scan(
			&e.ID, &e.JobName, &args, &e.Queue, &e.CronExpr,
			&e.LastRun, &e.NextRun, &e.Enabled, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		e.Args = args
		result = append(result, &e)
	}
	return result, rows.Err()
}

func (s *Storage) AddContinuation(ctx context.Context, parentID string, entry *domain.ContinuationEntry) error {
	if entry.ChildArgs == nil {
		entry.ChildArgs = []byte("[]")
	}
	if entry.ChildQueue == "" {
		entry.ChildQueue = "default"
	}
	entry.CreatedAt = time.Now().UTC()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO gfire.continuations (parent_id, child_name, child_args, child_queue, condition, created_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6)`,
		parentID, entry.ChildName, entry.ChildArgs, entry.ChildQueue, entry.Condition, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("add continuation: %w", err)
	}
	return nil
}

func (s *Storage) GetContinuations(ctx context.Context, parentID string) ([]*domain.ContinuationEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT child_name, child_args, child_queue, condition, created_at
		FROM gfire.continuations
		WHERE parent_id = $1
		ORDER BY id`, parentID)
	if err != nil {
		return nil, fmt.Errorf("get continuations: %w", err)
	}
	defer rows.Close()

	var result []*domain.ContinuationEntry
	for rows.Next() {
		var e domain.ContinuationEntry
		var args []byte
		if err := rows.Scan(&e.ChildName, &args, &e.ChildQueue, &e.Condition, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ChildArgs = args
		result = append(result, &e)
	}
	return result, rows.Err()
}

func (s *Storage) RemoveContinuations(ctx context.Context, parentID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM gfire.continuations WHERE parent_id = $1`, parentID)
	if err != nil {
		return fmt.Errorf("remove continuations: %w", err)
	}
	return nil
}
