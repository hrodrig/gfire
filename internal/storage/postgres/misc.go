package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
	"github.com/jackc/pgx/v5"
)

func bumpCounter(ctx context.Context, tx pgx.Tx, key string, delta int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO gfire.counters (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET
			value = gfire.counters.value + EXCLUDED.value,
			updated_at = now()`,
		key, delta,
	)
	if err != nil {
		return fmt.Errorf("bump counter %s: %w", key, err)
	}
	return nil
}

func (s *Storage) IncrementCounter(ctx context.Context, key string, delta int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gfire.counters (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET
			value = gfire.counters.value + EXCLUDED.value,
			updated_at = now()`,
		key, delta,
	)
	if err != nil {
		return fmt.Errorf("increment counter: %w", err)
	}
	return nil
}

func (s *Storage) GetCounter(ctx context.Context, key string) (int64, error) {
	var v int64
	err := s.pool.QueryRow(ctx, `SELECT value FROM gfire.counters WHERE key = $1`, key).Scan(&v)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get counter: %w", err)
	}
	return v, nil
}

func (s *Storage) GetAllCounters(ctx context.Context, skip, limit int) (map[string]int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT key, value FROM gfire.counters
		ORDER BY key
		OFFSET $1 LIMIT $2`, skip, limit)
	if err != nil {
		return nil, fmt.Errorf("get all counters: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

type pgLock struct {
	resource string
	ownerID  string
	store    *Storage
}

func (l *pgLock) Release(ctx context.Context) error {
	tag, err := l.store.pool.Exec(ctx, `
		DELETE FROM gfire.locks WHERE resource = $1 AND owner_id = $2`,
		l.resource, l.ownerID,
	)
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return serrors.ErrLockNotHeld
	}
	return nil
}

func (s *Storage) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (domain.Lock, error) {
	ownerID := uuid.NewString()
	expires := time.Now().UTC().Add(ttl)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Drop expired lock if present
	_, _ = tx.Exec(ctx, `DELETE FROM gfire.locks WHERE resource = $1 AND expires_at < now()`, resource)

	tag, err := tx.Exec(ctx, `
		INSERT INTO gfire.locks (resource, owner_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (resource) DO NOTHING`,
		resource, ownerID, expires,
	)
	if err != nil {
		return nil, fmt.Errorf("acquire lock insert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, serrors.ErrLockNotHeld
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("acquire lock commit: %w", err)
	}
	return &pgLock{resource: resource, ownerID: ownerID, store: s}, nil
}

func (s *Storage) RemoveExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM gfire.jobs
		WHERE state = ANY($1) AND updated_at < $2`,
		[]string{domain.StateSucceeded, domain.StateFailed, domain.StateDeleted},
		cutoff.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("remove expired: %w", err)
	}
	return tag.RowsAffected(), nil
}
