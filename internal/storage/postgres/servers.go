package postgres

import (
	"context"
	"fmt"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
)

func (s *Storage) RegisterServer(ctx context.Context, server *domain.ServerInfo, ttl time.Duration) error {
	_ = ttl // TTL enforced by coordinator via HeartbeatTimeout, not DB expiry
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

	_, err := s.pool.Exec(ctx, `
		INSERT INTO gfire.servers (id, started_at, last_heartbeat, worker_count, queues, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			last_heartbeat = EXCLUDED.last_heartbeat,
			worker_count = EXCLUDED.worker_count,
			queues = EXCLUDED.queues,
			status = EXCLUDED.status`,
		server.ID, server.StartedAt, server.LastHeartbeat, server.WorkerCount, server.Queues, server.Status,
	)
	if err != nil {
		return fmt.Errorf("register server: %w", err)
	}
	return nil
}

func (s *Storage) UnregisterServer(ctx context.Context, serverID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM gfire.servers WHERE id = $1`, serverID)
	if err != nil {
		return fmt.Errorf("unregister server: %w", err)
	}
	return nil
}

func (s *Storage) Heartbeat(ctx context.Context, serverID string, ttl time.Duration) error {
	_ = ttl
	tag, err := s.pool.Exec(ctx, `
		UPDATE gfire.servers
		SET last_heartbeat = $1, status = $2
		WHERE id = $3`,
		time.Now().UTC(), domain.ServerStatusActive, serverID,
	)
	if err != nil {
		return fmt.Errorf("server heartbeat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return serrors.ErrNotFound
	}
	return nil
}

func (s *Storage) GetServers(ctx context.Context) ([]*domain.ServerInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, started_at, last_heartbeat, worker_count, queues, status
		FROM gfire.servers
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("get servers: %w", err)
	}
	defer rows.Close()

	var result []*domain.ServerInfo
	for rows.Next() {
		var sv domain.ServerInfo
		if err := rows.Scan(&sv.ID, &sv.StartedAt, &sv.LastHeartbeat, &sv.WorkerCount, &sv.Queues, &sv.Status); err != nil {
			return nil, err
		}
		result = append(result, &sv)
	}
	return result, rows.Err()
}
