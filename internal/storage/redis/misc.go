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

// releaseLockScript releases a lock only if the owner matches.
var releaseLockScript = goredis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

func (s *Storage) RegisterServer(ctx context.Context, server *domain.ServerInfo, ttl time.Duration) error {
	_ = ttl
	now := time.Now().UTC()
	if server.StartedAt.IsZero() {
		server.StartedAt = now
	}
	server.LastHeartbeat = now
	if server.Status == "" {
		server.Status = domain.ServerStatusActive
	}
	if server.Queues == nil {
		server.Queues = []string{"default"}
	}
	raw, err := marshalServer(server)
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, serversKey, server.ID, raw)
	pipe.ZAdd(ctx, serverHBKey, goredis.Z{Score: float64(now.Unix()), Member: server.ID})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("register server: %w", err)
	}
	return nil
}

func (s *Storage) UnregisterServer(ctx context.Context, serverID string) error {
	pipe := s.client.Pipeline()
	pipe.HDel(ctx, serversKey, serverID)
	pipe.ZRem(ctx, serverHBKey, serverID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("unregister server: %w", err)
	}
	return nil
}

func (s *Storage) Heartbeat(ctx context.Context, serverID string, ttl time.Duration) error {
	_ = ttl
	raw, err := s.client.HGet(ctx, serversKey, serverID).Result()
	if err == goredis.Nil {
		return serrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("server heartbeat get: %w", err)
	}
	sv, err := unmarshalServer(raw)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	sv.LastHeartbeat = now
	sv.Status = domain.ServerStatusActive
	updated, err := marshalServer(sv)
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, serversKey, serverID, updated)
	pipe.ZAdd(ctx, serverHBKey, goredis.Z{Score: float64(now.Unix()), Member: serverID})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("server heartbeat: %w", err)
	}
	return nil
}

func (s *Storage) GetServers(ctx context.Context) ([]*domain.ServerInfo, error) {
	raw, err := s.client.HGetAll(ctx, serversKey).Result()
	if err != nil {
		return nil, fmt.Errorf("get servers: %w", err)
	}
	result := make([]*domain.ServerInfo, 0, len(raw))
	for _, v := range raw {
		sv, err := unmarshalServer(v)
		if err != nil {
			return nil, err
		}
		result = append(result, sv)
	}
	return result, nil
}

func (s *Storage) IncrementCounter(ctx context.Context, key string, delta int64) error {
	return s.bumpCounter(ctx, key, delta)
}

func (s *Storage) GetCounter(ctx context.Context, key string) (int64, error) {
	v, err := s.client.Get(ctx, counterKey(key)).Int64()
	if err == goredis.Nil {
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
	var cursor uint64
	result := make(map[string]int64)
	skipped := 0
	for {
		keys, next, err := s.client.Scan(ctx, cursor, counterKeyPrefix+"*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("scan counters: %w", err)
		}
		for _, k := range keys {
			name := k[len(counterKeyPrefix):]
			if skipped < skip {
				skipped++
				continue
			}
			if len(result) >= limit {
				return result, nil
			}
			v, err := s.client.Get(ctx, k).Int64()
			if err != nil && err != goredis.Nil {
				return nil, err
			}
			result[name] = v
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return result, nil
}

type redisLock struct {
	resource string
	ownerID  string
	client   goredis.UniversalClient
}

func (l *redisLock) Release(ctx context.Context) error {
	res, err := releaseLockScript.Run(ctx, l.client, []string{lockKey(l.resource)}, l.ownerID).Int64()
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	if res == 0 {
		return serrors.ErrLockNotHeld
	}
	return nil
}

func (s *Storage) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (domain.Lock, error) {
	ownerID := uuid.NewString()
	ok, err := s.client.SetNX(ctx, lockKey(resource), ownerID, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		return nil, serrors.ErrLockNotHeld
	}
	return &redisLock{resource: resource, ownerID: ownerID, client: s.client}, nil
}

func (s *Storage) RemoveExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	for _, st := range []string{domain.StateSucceeded, domain.StateFailed, domain.StateDeleted} {
		ids, err := s.client.SMembers(ctx, stateIndexKey(st)).Result()
		if err != nil {
			return deleted, fmt.Errorf("remove expired smembers: %w", err)
		}
		for _, jobID := range ids {
			updatedRaw, err := s.client.HGet(ctx, jobKey(jobID), "updated_at").Result()
			if err == goredis.Nil {
				continue
			}
			if err != nil {
				return deleted, err
			}
			updatedAt, err := time.Parse(time.RFC3339Nano, updatedRaw)
			if err != nil || !updatedAt.Before(cutoff.UTC()) {
				continue
			}
			if err := s.deleteJob(ctx, jobID, st); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}

func (s *Storage) deleteJob(ctx context.Context, jobID, state string) error {
	fields, _ := s.client.HGetAll(ctx, jobKey(jobID)).Result()
	queue := fields["queue"]

	pipe := s.client.Pipeline()
	pipe.Del(ctx, jobKey(jobID))
	pipe.Del(ctx, jobStatesKey(jobID))
	pipe.SRem(ctx, stateIndexKey(state), jobID)
	pipe.HDel(ctx, processingKey, jobID)
	pipe.ZRem(ctx, scheduledKey, jobID)
	if queue != "" {
		pipe.LRem(ctx, queueKey(queue), 0, jobID)
	}
	pipe.Del(ctx, continuationsKey(jobID))
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete job %s: %w", jobID, err)
	}
	return nil
}
