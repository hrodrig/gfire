package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hrodrig/gfire/internal/handler"
	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/middleware"
	"github.com/hrodrig/gfire/internal/storage"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
)

// ErrNotRunning is returned when an operation requires a started engine.
var ErrNotRunning = errors.New("engine not running")

// ErrNotProcessing is returned when CancelJob targets a job not active on this engine.
var ErrNotProcessing = errors.New("job not processing on this engine")

// Engine runs the worker pool and promotes scheduled retries.
type Engine struct {
	cfg      Config
	storage  storage.Storage
	pipeline *middleware.Pipeline
	logger   *slog.Logger

	runCtx    context.Context
	runCancel context.CancelFunc
	wg        sync.WaitGroup

	activeMu sync.Mutex
	active   map[string]int // queue → in-flight count on this engine

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	startOnce sync.Once
	stopOnce  sync.Once
}

// New creates an Engine. runner executes jobs; pass handler.Func for tests.
func New(store storage.Storage, cfg Config, runner handler.Runner, logger *slog.Logger) *Engine {
	cfg.normalize()
	if logger == nil {
		logger = slog.Default()
	}
	pipe := middleware.NewPipeline(runner, middleware.PanicRecovery())
	return &Engine{
		cfg:      cfg,
		storage:  store,
		pipeline: pipe,
		logger:   logger,
		active:   make(map[string]int),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Start launches workers and the scheduled-job poller.
func (e *Engine) Start(ctx context.Context) error {
	var startErr error
	e.startOnce.Do(func() {
		e.runCtx, e.runCancel = context.WithCancel(ctx)
		sv := &domain.ServerInfo{
			ID:          e.cfg.ServerID,
			StartedAt:   time.Now(),
			WorkerCount: e.cfg.Workers,
			Queues:      append([]string(nil), e.cfg.Queues...),
			Status:      domain.ServerStatusActive,
		}
		if err := e.storage.RegisterServer(e.runCtx, sv, e.cfg.ServerHeartbeatTTL); err != nil {
			startErr = fmt.Errorf("register server: %w", err)
			e.runCancel()
			return
		}
		for i := 0; i < e.cfg.Workers; i++ {
			e.wg.Add(1)
			go e.workerLoop(i)
		}
		e.wg.Add(1)
		go e.schedulerLoop()
		e.logger.Info("engine started", "server_id", e.cfg.ServerID, "workers", e.cfg.Workers)
	})
	return startErr
}

// Stop drains workers and unregisters the server.
func (e *Engine) Stop(ctx context.Context) error {
	var stopErr error
	e.stopOnce.Do(func() {
		if e.runCancel != nil {
			e.runCancel()
		}
		done := make(chan struct{})
		go func() {
			e.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			stopErr = ctx.Err()
		}
		if err := e.storage.UnregisterServer(context.Background(), e.cfg.ServerID); err != nil {
			e.logger.Warn("unregister server", "err", err)
		}
		e.logger.Info("engine stopped", "server_id", e.cfg.ServerID)
	})
	return stopErr
}

// CancelJob cancels an in-flight job on this engine (B3-009).
func (e *Engine) CancelJob(jobID string) error {
	e.cancelMu.Lock()
	cancel, ok := e.cancels[jobID]
	e.cancelMu.Unlock()
	if !ok {
		return ErrNotProcessing
	}
	cancel()
	return nil
}

func (e *Engine) registerCancel(jobID string, cancel context.CancelFunc) {
	e.cancelMu.Lock()
	e.cancels[jobID] = cancel
	e.cancelMu.Unlock()
}

func (e *Engine) unregisterCancel(jobID string) {
	e.cancelMu.Lock()
	delete(e.cancels, jobID)
	e.cancelMu.Unlock()
}

func (e *Engine) incActive(queue string) {
	e.activeMu.Lock()
	e.active[queue]++
	e.activeMu.Unlock()
}

func (e *Engine) decActive(queue string) {
	e.activeMu.Lock()
	if e.active[queue] > 0 {
		e.active[queue]--
	}
	e.activeMu.Unlock()
}

func (e *Engine) queuesUnderLimit() []string {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	var out []string
	for _, q := range e.cfg.Queues {
		limit := e.cfg.QueueLimits[q]
		if limit <= 0 || e.active[q] < limit {
			out = append(out, q)
		}
	}
	return out
}

func (e *Engine) schedulerLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.cfg.SchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.runCtx.Done():
			return
		case <-ticker.C:
			e.promoteScheduled()
		}
	}
}

func (e *Engine) promoteScheduled() {
	tickets, err := e.storage.GetDueScheduled(e.runCtx, time.Now(), e.cfg.SchedulerBatchSize)
	if err != nil {
		e.logger.Warn("scheduled poll failed", "err", err)
		return
	}
	for range tickets {
		select {
		case <-e.runCtx.Done():
			return
		default:
		}
	}
}

func (e *Engine) jobTimeout(job *domain.Job) time.Duration {
	if job.Timeout > 0 {
		return job.Timeout
	}
	return e.cfg.DefaultTimeout
}

func (e *Engine) finalizeSuccess(ctx context.Context, jobID string, result []byte) error {
	if len(result) > 0 {
		if err := e.storage.SetJobResult(ctx, jobID, result); err != nil {
			return err
		}
	}
	return e.storage.ApplyState(ctx, jobID, domain.StateProcessing, &domain.JobState{
		Name: domain.StateSucceeded,
	})
}

func (e *Engine) finalizeFailure(ctx context.Context, job *domain.Job, attempt int, execErr error) error {
	reason := execErr.Error()
	if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
		return e.storage.ApplyState(ctx, job.ID, domain.StateProcessing, &domain.JobState{
			Name:   domain.StateCancelled,
			Reason: reason,
		})
	}

	if err := e.storage.ApplyState(ctx, job.ID, domain.StateProcessing, &domain.JobState{
		Name:   domain.StateFailed,
		Reason: reason,
		Data:   map[string]string{"attempt": fmt.Sprintf("%d", attempt+1)},
	}); err != nil {
		return err
	}

	max := effectiveRetryMax(job.RetryMax)
	if attempt+1 >= max {
		return e.storage.ApplyState(ctx, job.ID, domain.StateFailed, &domain.JobState{
			Name:   domain.StateDead,
			Reason: "retry exhausted",
		})
	}

	at := time.Now().Add(RetryDelay(attempt))
	return e.storage.ScheduleRetry(ctx, job.ID, at)
}

func isQueueEmpty(err error) bool {
	return errors.Is(err, serrors.ErrQueueEmpty)
}
