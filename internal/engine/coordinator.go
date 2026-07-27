package engine

import (
	"time"
)

func (e *Engine) coordinatorLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.cfg.ServerHeartbeatEvery)
	defer ticker.Stop()

	// stale sweep runs less frequently — heartbeat interval × 5.
	staleSweepEvery := e.cfg.ServerHeartbeatEvery * 5
	if staleSweepEvery < time.Second {
		staleSweepEvery = time.Second
	}
	staleTicker := time.NewTicker(staleSweepEvery)
	defer staleTicker.Stop()

	for {
		select {
		case <-e.runCtx.Done():
			return
		case <-ticker.C:
			e.coordinatorTick()
		case <-staleTicker.C:
			e.staleServerSweep()
		}
	}
}

func (e *Engine) coordinatorTick() {
	ctx := e.runCtx
	if err := e.storage.Heartbeat(ctx, e.cfg.ServerID, e.cfg.ServerHeartbeatTTL); err != nil {
		e.logger.Debug("server heartbeat", "err", err)
	}
	orphans, err := e.storage.GetOrphanedJobs(ctx, e.cfg.OrphanJobStaleAge)
	if err != nil {
		e.logger.Warn("orphan scan failed", "err", err)
		return
	}
	for _, t := range orphans {
		if err := e.storage.Requeue(ctx, t.JobID, "orphan recovery"); err != nil {
			e.logger.Warn("orphan requeue failed", "job_id", t.JobID, "err", err)
		}
	}
}

// staleServerSweep detects and cleans up dead server entries.
// Servers whose heartbeat is older than ServerHeartbeatTTL are marked stale.
// Servers stale for 3× TTL are unregistered.
func (e *Engine) staleServerSweep() {
	ctx := e.runCtx
	servers, err := e.storage.GetServers(ctx)
	if err != nil {
		e.logger.Warn("stale server sweep: get servers", "err", err)
		return
	}

	now := time.Now()
	staleThreshold := e.cfg.ServerHeartbeatTTL
	unregisterThreshold := staleThreshold * 3

	for _, sv := range servers {
		age := now.Sub(sv.LastHeartbeat)

		if age > unregisterThreshold {
			e.logger.Info("unregistering stale server",
				"server_id", sv.ID,
				"last_heartbeat", sv.LastHeartbeat.Format(time.RFC3339),
				"age", age.Round(time.Second),
			)
			if err := e.storage.UnregisterServer(ctx, sv.ID); err != nil {
				e.logger.Warn("unregister server", "server_id", sv.ID, "err", err)
			}
			continue
		}

		if age > staleThreshold {
			e.logger.Warn("stale server detected",
				"server_id", sv.ID,
				"last_heartbeat", sv.LastHeartbeat.Format(time.RFC3339),
				"age", age.Round(time.Second),
			)
		}
	}
}

func (e *Engine) cleanupLoop() {
	defer e.wg.Done()
	if e.cfg.CleanupInterval <= 0 {
		<-e.runCtx.Done()
		return
	}
	ticker := time.NewTicker(e.cfg.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.runCtx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-e.cfg.JobRetention)
			if _, err := e.storage.RemoveExpired(e.runCtx, cutoff); err != nil {
				e.logger.Warn("cleanup failed", "err", err)
			}
		}
	}
}
