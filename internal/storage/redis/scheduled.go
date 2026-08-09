package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
)

// promoteScheduledScript atomically moves due scheduled jobs to enqueued.
var promoteScheduledScript = goredis.NewScript(`
local scheduled_key = KEYS[1]
local now = tonumber(ARGV[1])
local batch = tonumber(ARGV[2])
local ids = redis.call('ZRANGEBYSCORE', scheduled_key, '-inf', now, 'LIMIT', 0, batch)
local result = {}
for _, job_id in ipairs(ids) do
  redis.call('ZREM', scheduled_key, job_id)
  table.insert(result, job_id)
end
return result
`)

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

	st := &domain.JobState{
		Name:      domain.StateScheduled,
		CreatedAt: now,
		Data:      map[string]string{"enqueue_at": enqueueAt.UTC().Format(time.RFC3339)},
	}
	stateJSON, err := marshalState(st)
	if err != nil {
		return "", err
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, jobKey(job.ID), jobHashFields(job, domain.StateScheduled, time.Time{}))
	pipe.RPush(ctx, jobStatesKey(job.ID), stateJSON)
	pipe.ZAdd(ctx, scheduledKey, goredis.Z{Score: float64(enqueueAt.UTC().Unix()), Member: job.ID})
	pipe.SAdd(ctx, stateIndexKey(domain.StateScheduled), job.ID)
	pipe.IncrBy(ctx, counterKey("scheduled"), 1)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("add scheduled: %w", err)
	}
	return job.ID, nil
}

func (s *Storage) GetDueScheduled(ctx context.Context, now time.Time, batchSize int) ([]*domain.JobTicket, error) {
	if batchSize <= 0 {
		batchSize = 100
	}

	raw, err := promoteScheduledScript.Run(ctx, s.client,
		[]string{scheduledKey},
		now.UTC().Unix(), batchSize,
	).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("due scheduled: %w", err)
	}

	tickets := make([]*domain.JobTicket, 0, len(raw))
	ts := time.Now().UTC()
	for _, jobID := range raw {
		fields, err := s.client.HGetAll(ctx, jobKey(jobID)).Result()
		if err != nil || len(fields) == 0 {
			continue
		}
		queue := fields["queue"]
		if queue == "" {
			queue = "default"
		}

		st := &domain.JobState{Name: domain.StateEnqueued, Reason: "scheduled", CreatedAt: ts}
		stateJSON, err := marshalState(st)
		if err != nil {
			return nil, err
		}

		pipe := s.client.Pipeline()
		pipe.HSet(ctx, jobKey(jobID), map[string]string{
			"state":      domain.StateEnqueued,
			"updated_at": ts.Format(time.RFC3339Nano),
		})
		pipe.RPush(ctx, jobStatesKey(jobID), stateJSON)
		pipe.LPush(ctx, queueKey(queue), jobID)
		pipe.SAdd(ctx, queuesIndexKey, queue)
		pipe.SRem(ctx, stateIndexKey(domain.StateScheduled), jobID)
		pipe.SAdd(ctx, stateIndexKey(domain.StateEnqueued), jobID)
		pipe.IncrBy(ctx, counterKey("scheduled"), -1)
		pipe.IncrBy(ctx, counterKey("enqueued"), 1)
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, fmt.Errorf("promote scheduled %s: %w", jobID, err)
		}
		tickets = append(tickets, &domain.JobTicket{JobID: jobID, Token: "sched-" + jobID})
	}
	return tickets, nil
}

func (s *Storage) RemoveScheduled(ctx context.Context, jobID string) error {
	if err := s.client.ZRem(ctx, scheduledKey, jobID).Err(); err != nil {
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
	raw, err := marshalRecurring(entry)
	if err != nil {
		return err
	}
	if err := s.client.HSet(ctx, recurringKey, entry.ID, raw).Err(); err != nil {
		return fmt.Errorf("upsert recurring: %w", err)
	}
	return nil
}

func (s *Storage) RemoveRecurring(ctx context.Context, id string) error {
	if err := s.client.HDel(ctx, recurringKey, id).Err(); err != nil {
		return fmt.Errorf("remove recurring: %w", err)
	}
	return nil
}

func (s *Storage) GetRecurringJobs(ctx context.Context) ([]*domain.RecurringJobEntry, error) {
	raw, err := s.client.HGetAll(ctx, recurringKey).Result()
	if err != nil {
		return nil, fmt.Errorf("get recurring: %w", err)
	}
	result := make([]*domain.RecurringJobEntry, 0, len(raw))
	for _, v := range raw {
		e, err := unmarshalRecurring(v)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (s *Storage) UpdateRecurringLastRun(ctx context.Context, id string, lastRun, nextRun time.Time) error {
	raw, err := s.client.HGet(ctx, recurringKey, id).Result()
	if err == goredis.Nil {
		return serrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update recurring last_run get: %w", err)
	}
	entry, err := unmarshalRecurring(raw)
	if err != nil {
		return err
	}
	lr, nr := lastRun.UTC(), nextRun.UTC()
	entry.LastRun = &lr
	entry.NextRun = &nr
	entry.UpdatedAt = time.Now().UTC()
	encoded, err := marshalRecurring(entry)
	if err != nil {
		return err
	}
	if err := s.client.HSet(ctx, recurringKey, id, encoded).Err(); err != nil {
		return fmt.Errorf("update recurring last_run: %w", err)
	}
	return nil
}

func (s *Storage) AddContinuation(ctx context.Context, parentID string, entry *domain.ContinuationEntry) error {
	if entry.ChildArgs == nil {
		entry.ChildArgs = []byte("[]")
	}
	if entry.ChildQueue == "" {
		entry.ChildQueue = "default"
	}
	entry.CreatedAt = time.Now().UTC()
	entryID := uuid.NewString()
	raw, err := marshalContinuation(entry)
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.SAdd(ctx, continuationsKey(parentID), entryID)
	pipe.Set(ctx, continuationEntryKey(parentID, entryID), raw, 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("add continuation: %w", err)
	}
	return nil
}

func (s *Storage) GetContinuations(ctx context.Context, parentID string) ([]*domain.ContinuationEntry, error) {
	ids, err := s.client.SMembers(ctx, continuationsKey(parentID)).Result()
	if err != nil {
		return nil, fmt.Errorf("get continuations: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	result := make([]*domain.ContinuationEntry, 0, len(ids))
	for _, id := range ids {
		raw, err := s.client.Get(ctx, continuationEntryKey(parentID, id)).Result()
		if err != nil {
			if err == goredis.Nil {
				continue
			}
			return nil, err
		}
		e, err := unmarshalContinuation(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (s *Storage) RemoveContinuations(ctx context.Context, parentID string) error {
	ids, err := s.client.SMembers(ctx, continuationsKey(parentID)).Result()
	if err != nil {
		return fmt.Errorf("remove continuations list: %w", err)
	}
	pipe := s.client.Pipeline()
	pipe.Del(ctx, continuationsKey(parentID))
	for _, id := range ids {
		pipe.Del(ctx, continuationEntryKey(parentID, id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("remove continuations: %w", err)
	}
	return nil
}
