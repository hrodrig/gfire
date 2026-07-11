package engine

import (
	"time"
)

func (e *Engine) coordinatorLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.cfg.ServerHeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-e.runCtx.Done():
			return
		case <-ticker.C:
			e.coordinatorTick()
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
