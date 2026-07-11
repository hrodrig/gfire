package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
)

func (s *Storage) ScheduleRetry(ctx context.Context, jobID string, enqueueAt time.Time) error {
	fields, err := s.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("schedule retry get: %w", err)
	}
	if len(fields) == 0 {
		return serrors.ErrNotFound
	}
	if fields["state"] != domain.StateFailed {
		return serrors.ErrStateConflict
	}

	now := time.Now().UTC()
	st := &domain.JobState{
		Name:      domain.StateScheduled,
		CreatedAt: now,
		Data:      map[string]string{"enqueue_at": enqueueAt.UTC().Format(time.RFC3339)},
	}
	stateJSON, err := marshalState(st)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, jobKey(jobID), map[string]string{
		"state":      domain.StateScheduled,
		"updated_at": now.Format(time.RFC3339Nano),
	})
	pipe.RPush(ctx, jobStatesKey(jobID), stateJSON)
	pipe.ZAdd(ctx, scheduledKey, goredis.Z{Score: float64(enqueueAt.UTC().Unix()), Member: jobID})
	pipe.SRem(ctx, stateIndexKey(domain.StateFailed), jobID)
	pipe.SAdd(ctx, stateIndexKey(domain.StateScheduled), jobID)
	pipe.IncrBy(ctx, counterKey("scheduled"), 1)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("schedule retry: %w", err)
	}
	return nil
}

func (s *Storage) SetJobResult(ctx context.Context, jobID string, result []byte) error {
	if len(result) > 65536 {
		result = result[:65536]
	}
	exists, err := s.client.Exists(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("set result exists: %w", err)
	}
	if exists == 0 {
		return serrors.ErrNotFound
	}
	if err := s.client.HSet(ctx, jobKey(jobID), "result", string(result)).Err(); err != nil {
		return fmt.Errorf("set result: %w", err)
	}
	return nil
}
