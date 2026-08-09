package engine

import (
	"context"
	"errors"
	"log/slog"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	serrors "github.com/hrodrig/gfire/internal/storage/errors"
	"github.com/robfig/cron/v3"
)

const (
	// recurringLockPrefix is the distributed-lock resource prefix for recurring jobs.
	recurringLockPrefix = "lock:recurring:"

	// recurringLockTTL is how long a recurring lock is held.
	// Must be longer than the max Enqueue time, shorter than the tick interval.
	recurringLockTTL = 30 * time.Second

	// recurringReloadInterval is how often the manager re-reads
	// definitions from storage to pick up new/removed entries.
	recurringReloadInterval = 60 * time.Second
)

type recurringManager struct {
	store    domainStorage
	serverID string
	logger   *slog.Logger

	cron    *cron.Cron
	entries map[string]cron.EntryID // entry definition ID → cron entry ID
}

// domainStorage is the subset of storage.Storage needed by the recurring manager.
type domainStorage interface {
	GetRecurringJobs(ctx context.Context) ([]*domain.RecurringJobEntry, error)
	Enqueue(ctx context.Context, queue string, job *domain.Job) (id string, err error)
	AcquireLock(ctx context.Context, resource string, ttl time.Duration) (domain.Lock, error)
	UpdateRecurringLastRun(ctx context.Context, id string, lastRun, nextRun time.Time) error
}

func newRecurringManager(store domainStorage, serverID string, logger *slog.Logger) *recurringManager {
	return &recurringManager{
		store:    store,
		serverID: serverID,
		logger:   logger,
		cron: cron.New(cron.WithSeconds(),
			cron.WithLocation(time.UTC),
			cron.WithLogger(cron.VerbosePrintfLogger(loggerWriter{logger})),
		),
		entries: make(map[string]cron.EntryID),
	}
}

func (e *Engine) recurringLoop() {
	defer e.wg.Done()

	m := newRecurringManager(e.storage, e.cfg.ServerID, e.logger)
	if err := m.loadAll(e.runCtx); err != nil {
		e.logger.Warn("recurring initial load", "err", err)
	}

	m.cron.Start()

	reloadTicker := time.NewTicker(recurringReloadInterval)
	defer reloadTicker.Stop()

	for {
		select {
		case <-e.runCtx.Done():
			m.cron.Stop()
			return
		case <-reloadTicker.C:
			if err := m.reload(e.runCtx); err != nil {
				e.logger.Warn("recurring reload", "err", err)
			}
		}
	}
}

// loadAll reads all enabled recurring definitions and schedules them.
func (m *recurringManager) loadAll(ctx context.Context) error {
	entries, err := m.store.GetRecurringJobs(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		m.schedule(entry)
	}
	return nil
}

// reload reconciles storage state with the cron scheduler.
func (m *recurringManager) reload(ctx context.Context) error {
	entries, err := m.store.GetRecurringJobs(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[entry.ID] = true
		if !entry.Enabled {
			m.unschedule(entry.ID)
			continue
		}
		if _, exists := m.entries[entry.ID]; !exists {
			m.schedule(entry)
		}
	}
	// Remove entries no longer in storage.
	for id := range m.entries {
		if !seen[id] {
			m.unschedule(id)
		}
	}
	return nil
}

func (m *recurringManager) schedule(entry *domain.RecurringJobEntry) {
	id, err := m.cron.AddFunc(entry.CronExpr, m.fireFunc(entry))
	if err != nil {
		m.logger.Warn("recurring schedule failed", "id", entry.ID, "expr", entry.CronExpr, "err", err)
		return
	}
	m.entries[entry.ID] = id
	m.logger.Info("recurring scheduled", "id", entry.ID, "job", entry.JobName, "expr", entry.CronExpr)
}

func (m *recurringManager) unschedule(definitionID string) {
	if cronID, ok := m.entries[definitionID]; ok {
		m.cron.Remove(cronID)
		delete(m.entries, definitionID)
		m.logger.Info("recurring unscheduled", "id", definitionID)
	}
}

// fireFunc returns the cron callback for a recurring entry.
// It acquires a distributed lock so only one node enqueues per tick.
func (m *recurringManager) fireFunc(entry *domain.RecurringJobEntry) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		lockResource := recurringLockPrefix + entry.ID
		lock, err := m.store.AcquireLock(ctx, lockResource, recurringLockTTL)
		if err != nil {
			// Lock held by another node — expected, not an error.
			if !errors.Is(err, serrors.ErrLockNotHeld) {
				m.logger.Debug("recurring lock skip", "id", entry.ID, "err", err)
			}
			return
		}
		defer func() {
			if err := lock.Release(ctx); err != nil {
				m.logger.Debug("recurring lock release", "id", entry.ID, "err", err)
			}
		}()

		queue := entry.Queue
		if queue == "" {
			queue = "default"
		}
		args := entry.Args
		if len(args) == 0 {
			args = []byte("{}")
		}

		job := &domain.Job{
			Name:  entry.JobName,
			Args:  args,
			Queue: queue,
		}

		jobID, err := m.store.Enqueue(ctx, queue, job)
		if err != nil {
			m.logger.Error("recurring enqueue failed", "id", entry.ID, "job", entry.JobName, "err", err)
			return
		}

		now := time.Now().UTC()
		nextRun, nextErr := domain.NextRecurringRun(entry.CronExpr, now)
		if nextErr != nil {
			m.logger.Warn("recurring next_run parse", "id", entry.ID, "err", nextErr)
			nextRun = now
		}
		if err := m.store.UpdateRecurringLastRun(ctx, entry.ID, now, nextRun); err != nil {
			m.logger.Warn("recurring last_run update", "id", entry.ID, "err", err)
		}
		m.logger.Info("recurring fired", "id", entry.ID, "job", entry.JobName, "job_id", jobID)
	}
}

// loggerWriter adapts slog.Logger to io.Writer for robfig/cron debug output.
type loggerWriter struct {
	logger *slog.Logger
}

func (w loggerWriter) Printf(format string, args ...interface{}) {
	w.logger.Debug("cron", "msg", args[0])
}

func (w loggerWriter) Write(p []byte) (int, error) {
	w.logger.Debug("cron", "msg", string(p))
	return len(p), nil
}
