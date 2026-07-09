package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
	"github.com/jackc/pgx/v5"
)

func (s *Storage) GetJob(ctx context.Context, jobID string) (*domain.JobWithStates, error) {
	j, err := s.scanJob(ctx, `
		SELECT id, name, args, queue, retry_max, timeout_ms, created_at
		FROM gfire.jobs WHERE id = $1`, jobID)
	if err != nil {
		return nil, err
	}

	states, err := s.loadStates(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return &domain.JobWithStates{Job: j, States: states}, nil
}

func (s *Storage) GetJobsByState(ctx context.Context, state string, offset, limit int) ([]*domain.JobWithStates, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows pgx.Rows
	var err error
	if state == "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id, name, args, queue, retry_max, timeout_ms, created_at
			FROM gfire.jobs
			ORDER BY created_at DESC
			OFFSET $1 LIMIT $2`, offset, limit)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, name, args, queue, retry_max, timeout_ms, created_at
			FROM gfire.jobs
			WHERE state = $1
			ORDER BY created_at DESC
			OFFSET $2 LIMIT $3`, state, offset, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("jobs by state: %w", err)
	}
	defer rows.Close()

	var result []*domain.JobWithStates
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		states, err := s.loadStates(ctx, j.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, &domain.JobWithStates{Job: j, States: states})
	}
	return result, rows.Err()
}

func (s *Storage) GetQueueLength(ctx context.Context, queue string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM gfire.jobs WHERE queue = $1 AND state = $2`,
		queue, domain.StateEnqueued,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("queue length: %w", err)
	}
	return n, nil
}

func (s *Storage) GetQueues(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT queue FROM gfire.jobs ORDER BY queue`)
	if err != nil {
		return nil, fmt.Errorf("get queues: %w", err)
	}
	defer rows.Close()

	var queues []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return nil, err
		}
		queues = append(queues, q)
	}
	return queues, rows.Err()
}

func (s *Storage) scanJob(ctx context.Context, query string, args ...any) (*domain.Job, error) {
	row := s.pool.QueryRow(ctx, query, args...)
	j, err := scanJobFromRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, serrors.ErrNotFound
	}
	return j, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanJobFromRow(row scannable) (*domain.Job, error) {
	var j domain.Job
	var args []byte
	var timeoutMSVal int64
	err := row.Scan(&j.ID, &j.Name, &args, &j.Queue, &j.RetryMax, &timeoutMSVal, &j.CreatedAt)
	if err != nil {
		return nil, err
	}
	j.Args = args
	j.Timeout = durationFromMS(timeoutMSVal)
	return &j, nil
}

func scanJobRow(rows pgx.Rows) (*domain.Job, error) {
	return scanJobFromRow(rows)
}

func (s *Storage) loadStates(ctx context.Context, jobID string) ([]*domain.JobState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, COALESCE(reason, ''), data, created_at
		FROM gfire.job_states
		WHERE job_id = $1
		ORDER BY created_at ASC, id ASC`, jobID)
	if err != nil {
		return nil, fmt.Errorf("load states: %w", err)
	}
	defer rows.Close()

	var states []*domain.JobState
	for rows.Next() {
		var st domain.JobState
		var data []byte
		if err := rows.Scan(&st.Name, &st.Reason, &data, &st.CreatedAt); err != nil {
			return nil, err
		}
		if len(data) > 0 && string(data) != "{}" && string(data) != "null" {
			_ = json.Unmarshal(data, &st.Data)
		}
		states = append(states, &st)
	}
	return states, rows.Err()
}
