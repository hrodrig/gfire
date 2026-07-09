package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
)

// finalizeDequeueScript atomically marks a job Processing after BRPOP.
var finalizeDequeueScript = goredis.NewScript(`
local job_key = KEYS[1]
local states_key = KEYS[2]
local processing_key = KEYS[3]
local state_old = KEYS[4]
local state_new = KEYS[5]
local counter_key = KEYS[6]

if redis.call('EXISTS', job_key) == 0 then
  return -1
end

redis.call('HSET', job_key, 'state', 'Processing', 'progress_at', ARGV[2], 'updated_at', ARGV[2])
redis.call('RPUSH', states_key, ARGV[3])
redis.call('HSET', processing_key, ARGV[1], ARGV[4])
redis.call('SREM', state_old, ARGV[1])
redis.call('SADD', state_new, ARGV[1])
redis.call('INCRBY', counter_key, 1)
return 1
`)

// applyStateScript atomically transitions job state.
var applyStateScript = goredis.NewScript(`
local job_key = KEYS[1]
local states_key = KEYS[2]
local processing_key = KEYS[3]
local state_old = KEYS[4]
local state_new = KEYS[5]
local counter_key = KEYS[6]

local current = redis.call('HGET', job_key, 'state')
if not current then
  return -1
end
if current ~= ARGV[1] then
  return -2
end

redis.call('HSET', job_key, 'state', ARGV[2], 'updated_at', ARGV[3])
if ARGV[4] == '1' then
  redis.call('HDEL', job_key, 'progress_at')
end
redis.call('RPUSH', states_key, ARGV[5])
redis.call('SREM', state_old, ARGV[6])
redis.call('SADD', state_new, ARGV[6])
if ARGV[1] == 'Processing' and ARGV[2] ~= 'Processing' then
  redis.call('HDEL', processing_key, ARGV[6])
end
if ARGV[7] ~= '' then
  redis.call('INCRBY', counter_key, 1)
end
return 1
`)

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

	st := &domain.JobState{Name: domain.StateEnqueued, CreatedAt: now}
	stateJSON, err := marshalState(st)
	if err != nil {
		return "", err
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, jobKey(job.ID), jobHashFields(job, domain.StateEnqueued, time.Time{}))
	pipe.RPush(ctx, jobStatesKey(job.ID), stateJSON)
	pipe.LPush(ctx, queueKey(queue), job.ID)
	pipe.SAdd(ctx, queuesIndexKey, queue)
	pipe.SAdd(ctx, stateIndexKey(domain.StateEnqueued), job.ID)
	pipe.IncrBy(ctx, counterKey("enqueued"), 1)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("enqueue: %w", err)
	}
	return job.ID, nil
}

func (s *Storage) Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*domain.JobTicket, error) {
	if len(queues) == 0 {
		return nil, serrors.ErrQueueEmpty
	}

	keys := make([]string, len(queues))
	for i, q := range queues {
		keys[i] = queueKey(q)
	}

	for {
		result, err := s.client.BRPop(ctx, timeout, keys...).Result()
		if errors.Is(err, goredis.Nil) {
			return nil, serrors.ErrQueueEmpty
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("dequeue brpop: %w", err)
		}
		if len(result) < 2 {
			continue
		}
		jobID := result[1]

		now := time.Now().UTC()
		token := fmt.Sprintf("tok-%s-%d", jobID, now.UnixNano())
		st := &domain.JobState{
			Name:      domain.StateProcessing,
			CreatedAt: now,
			Data:      map[string]string{"server_id": s.serverID},
		}
		stateJSON, err := marshalState(st)
		if err != nil {
			return nil, err
		}

		processingVal := s.serverID + "|" + token + "|" + strconvUnix(now)
		res, err := finalizeDequeueScript.Run(ctx, s.client,
			[]string{
				jobKey(jobID),
				jobStatesKey(jobID),
				processingKey,
				stateIndexKey(domain.StateEnqueued),
				stateIndexKey(domain.StateProcessing),
				counterKey("dequeued"),
			},
			jobID, now.Format(time.RFC3339Nano), stateJSON, processingVal,
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("dequeue finalize: %w", err)
		}
		if res == -1 {
			continue
		}
		return &domain.JobTicket{JobID: jobID, Token: token}, nil
	}
}

func strconvUnix(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixNano())
}

func (s *Storage) Requeue(ctx context.Context, jobID string, reason string) error {
	exists, err := s.client.Exists(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("requeue exists: %w", err)
	}
	if exists == 0 {
		return serrors.ErrNotFound
	}

	fields, err := s.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("requeue hgetall: %w", err)
	}
	queue := fields["queue"]
	if queue == "" {
		queue = "default"
	}

	now := time.Now().UTC()
	st := &domain.JobState{Name: domain.StateEnqueued, Reason: reason, CreatedAt: now}
	stateJSON, err := marshalState(st)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, jobKey(jobID), map[string]string{
		"state":      domain.StateEnqueued,
		"updated_at": now.Format(time.RFC3339Nano),
	})
	pipe.HDel(ctx, jobKey(jobID), "progress_at")
	pipe.RPush(ctx, jobStatesKey(jobID), stateJSON)
	pipe.LPush(ctx, queueKey(queue), jobID)
	pipe.HDel(ctx, processingKey, jobID)
	pipe.SRem(ctx, stateIndexKey(domain.StateProcessing), jobID)
	pipe.SAdd(ctx, stateIndexKey(domain.StateEnqueued), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("requeue: %w", err)
	}
	return nil
}

func (s *Storage) ApplyState(ctx context.Context, jobID string, expectedCurrent string, newState *domain.JobState) error {
	now := newState.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
		newState.CreatedAt = now
	}
	stateJSON, err := marshalState(newState)
	if err != nil {
		return err
	}

	clearProgress := "0"
	if newState.Name != domain.StateProcessing {
		clearProgress = "1"
	}

	counterName := ""
	switch newState.Name {
	case domain.StateSucceeded:
		counterName = "succeeded"
	case domain.StateFailed:
		counterName = "failed"
	}

	res, err := applyStateScript.Run(ctx, s.client,
		[]string{
			jobKey(jobID),
			jobStatesKey(jobID),
			processingKey,
			stateIndexKey(expectedCurrent),
			stateIndexKey(newState.Name),
			counterKey(counterName),
		},
		expectedCurrent, newState.Name, now.Format(time.RFC3339Nano),
		clearProgress, stateJSON, jobID, counterName,
	).Int64()
	if err != nil {
		return fmt.Errorf("applystate: %w", err)
	}
	switch res {
	case -1:
		return serrors.ErrNotFound
	case -2:
		return serrors.ErrStateConflict
	}
	return nil
}

func (s *Storage) HeartbeatJob(ctx context.Context, jobID string) error {
	exists, err := s.client.Exists(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("heartbeat exists: %w", err)
	}
	if exists == 0 {
		return serrors.ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.client.HSet(ctx, jobKey(jobID), "progress_at", now).Err(); err != nil {
		return fmt.Errorf("heartbeat job: %w", err)
	}
	if err := s.client.HSet(ctx, processingKey, jobID, s.serverID+"|heartbeat|"+now).Err(); err != nil {
		return fmt.Errorf("heartbeat processing: %w", err)
	}
	return nil
}

func (s *Storage) GetOrphanedJobs(ctx context.Context, staleAge time.Duration) ([]*domain.JobTicket, error) {
	cutoff := time.Now().UTC().Add(-staleAge)
	members, err := s.client.SMembers(ctx, stateIndexKey(domain.StateProcessing)).Result()
	if err != nil {
		return nil, fmt.Errorf("orphaned smembers: %w", err)
	}

	var tickets []*domain.JobTicket
	for _, jobID := range members {
		progressRaw, err := s.client.HGet(ctx, jobKey(jobID), "progress_at").Result()
		if errors.Is(err, goredis.Nil) || progressRaw == "" {
			tickets = append(tickets, &domain.JobTicket{JobID: jobID, Token: "orphan-" + jobID})
			continue
		}
		progressAt, err := time.Parse(time.RFC3339Nano, progressRaw)
		if err != nil || progressAt.Before(cutoff) {
			tickets = append(tickets, &domain.JobTicket{JobID: jobID, Token: "orphan-" + jobID})
		}
	}
	return tickets, nil
}

func (s *Storage) GetJob(ctx context.Context, jobID string) (*domain.JobWithStates, error) {
	fields, err := s.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	if len(fields) == 0 {
		return nil, serrors.ErrNotFound
	}
	job, _, _, err := jobFromHash(fields)
	if err != nil {
		return nil, err
	}
	states, err := s.loadStates(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return &domain.JobWithStates{Job: job, States: states}, nil
}

func (s *Storage) GetJobsByState(ctx context.Context, state string, offset, limit int) ([]*domain.JobWithStates, error) {
	if limit <= 0 {
		limit = 100
	}

	var jobIDs []string
	var err error
	if state == "" {
		// Union of all state index sets is expensive; scan Enqueued+Processing+terminal sets.
		for _, st := range []string{
			domain.StateEnqueued, domain.StateProcessing, domain.StateScheduled,
			domain.StateSucceeded, domain.StateFailed, domain.StateDeleted,
		} {
			ids, e := s.client.SMembers(ctx, stateIndexKey(st)).Result()
			if e != nil {
				return nil, fmt.Errorf("jobs by state smembers: %w", e)
			}
			jobIDs = append(jobIDs, ids...)
		}
	} else {
		jobIDs, err = s.client.SMembers(ctx, stateIndexKey(state)).Result()
		if err != nil {
			return nil, fmt.Errorf("jobs by state: %w", err)
		}
	}

	if offset >= len(jobIDs) {
		return nil, nil
	}
	jobIDs = jobIDs[offset:]
	if len(jobIDs) > limit {
		jobIDs = jobIDs[:limit]
	}

	result := make([]*domain.JobWithStates, 0, len(jobIDs))
	for _, id := range jobIDs {
		jws, err := s.GetJob(ctx, id)
		if errors.Is(err, serrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if state != "" && jws.CurrentState() != state {
			continue
		}
		result = append(result, jws)
	}
	return result, nil
}

func (s *Storage) GetQueueLength(ctx context.Context, queue string) (int64, error) {
	n, err := s.client.LLen(ctx, queueKey(queue)).Result()
	if err != nil {
		return 0, fmt.Errorf("queue length: %w", err)
	}
	return n, nil
}

func (s *Storage) GetQueues(ctx context.Context) ([]string, error) {
	queues, err := s.client.SMembers(ctx, queuesIndexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("get queues: %w", err)
	}
	return queues, nil
}

func (s *Storage) loadStates(ctx context.Context, jobID string) ([]*domain.JobState, error) {
	raw, err := s.client.LRange(ctx, jobStatesKey(jobID), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("load states: %w", err)
	}
	states := make([]*domain.JobState, 0, len(raw))
	for _, r := range raw {
		st, err := unmarshalState(r)
		if err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	return states, nil
}
