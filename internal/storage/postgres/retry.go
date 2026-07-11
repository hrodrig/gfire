package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
	"github.com/jackc/pgx/v5"
)

func (s *Storage) ScheduleRetry(ctx context.Context, jobID string, enqueueAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("schedule retry begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var current string
	err = tx.QueryRow(ctx, `SELECT state FROM gfire.jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return serrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("schedule retry select: %w", err)
	}
	if current != domain.StateFailed {
		return serrors.ErrStateConflict
	}

	now := time.Now().UTC()
	data, _ := json.Marshal(map[string]string{"enqueue_at": enqueueAt.UTC().Format(time.RFC3339)})
	_, err = tx.Exec(ctx, `
		UPDATE gfire.jobs SET state = $1, updated_at = $2 WHERE id = $3`,
		domain.StateScheduled, now, jobID,
	)
	if err != nil {
		return fmt.Errorf("schedule retry update: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.job_states (job_id, name, data, created_at)
		VALUES ($1, $2, $3::jsonb, $4)`,
		jobID, domain.StateScheduled, data, now,
	)
	if err != nil {
		return fmt.Errorf("schedule retry state: %w", err)
	}
	_, err = tx.Exec(ctx, `DELETE FROM gfire.scheduled_jobs WHERE job_id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("schedule retry delete: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO gfire.scheduled_jobs (id, job_id, enqueue_at, created_at)
		VALUES ($1, $2, $3, $4)`,
		jobID, jobID, enqueueAt.UTC(), now,
	)
	if err != nil {
		return fmt.Errorf("schedule retry row: %w", err)
	}
	if err := bumpCounter(ctx, tx, "scheduled", 1); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Storage) SetJobResult(ctx context.Context, jobID string, result []byte) error {
	if len(result) > 65536 {
		result = result[:65536]
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE gfire.jobs SET result = $1::jsonb, updated_at = now() WHERE id = $2`,
		result, jobID,
	)
	if err != nil {
		return fmt.Errorf("set job result: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return serrors.ErrNotFound
	}
	return nil
}
