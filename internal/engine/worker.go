package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/middleware"
)

func (e *Engine) workerLoop(_ int) {
	defer e.wg.Done()
	for {
		select {
		case <-e.runCtx.Done():
			return
		default:
		}
		queues := e.queuesUnderLimit()
		if len(queues) == 0 {
			// B7-004: exponential backoff when all queues are at concurrency limit.
			backoff := 100 * time.Millisecond
			for {
				select {
				case <-e.runCtx.Done():
					return
				case <-time.After(backoff):
				}
				if len(e.queuesUnderLimit()) > 0 {
					break
				}
				backoff *= 2
				if backoff > 2*time.Second {
					backoff = 2 * time.Second
				}
			}
			continue
		}

		ticket, err := e.storage.Dequeue(e.runCtx, queues, e.cfg.DequeueTimeout)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			if isQueueEmpty(err) {
				continue
			}
			e.logger.Warn("dequeue failed", "err", err)
			continue
		}

		e.processTicket(ticket)
	}
}

func (e *Engine) processTicket(ticket *domain.JobTicket) {
	// Detached ctx: ticket already dequeued; load job even when engine is stopping (B7-003).
	jw, err := e.storage.GetJob(context.Background(), ticket.JobID)
	if err != nil {
		e.logger.Warn("get job failed", "job_id", ticket.JobID, "err", err)
		return
	}
	job := jw.Job
	e.incActive(job.Queue)
	defer e.decActive(job.Queue)

	attempt := middleware.AttemptCount(jw.States)
	timeout := e.jobTimeout(job)
	jobCtx, cancel := context.WithTimeout(e.runCtx, timeout)
	defer cancel()

	e.registerCancel(job.ID, cancel)
	defer e.unregisterCancel(job.ID)

	heartbeatDone := make(chan struct{})
	go e.jobHeartbeat(jobCtx, job.ID, heartbeatDone)
	defer close(heartbeatDone)

	mctx := middleware.NewJobContext(jobCtx, job, attempt, e.storage, e.logger)
	result, execErr := e.pipeline.Execute(mctx)

	var terminal string
	if execErr == nil {
		var err error
		terminal, err = e.finalizeSuccess(jobCtx, job.ID, result)
		if err != nil {
			e.logger.Error("apply succeeded", "job_id", job.ID, "err", err)
			return
		}
	} else {
		var err error
		terminal, err = e.finalizeFailure(jobCtx, job, attempt, execErr)
		if err != nil {
			e.logger.Error("finalize failure", "job_id", job.ID, "err", err)
			return
		}
	}
	if terminal != "" {
		e.fireContinuations(context.Background(), job.ID, terminal)
	}
}

func (e *Engine) jobHeartbeat(ctx context.Context, jobID string, done <-chan struct{}) {
	ticker := time.NewTicker(e.cfg.JobHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := e.storage.HeartbeatJob(context.Background(), jobID); err != nil {
				e.logger.Debug("job heartbeat", "job_id", jobID, "err", err)
			}
		}
	}
}

// WaitUntilIdle blocks until no jobs are in-flight on this engine (tests).
func (e *Engine) WaitUntilIdle(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		e.activeMu.Lock()
		busy := 0
		for _, n := range e.active {
			busy += n
		}
		e.activeMu.Unlock()
		if busy == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait idle: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
