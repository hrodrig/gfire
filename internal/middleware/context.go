package middleware

import (
	"context"
	"log/slog"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/storage"
)

// JobContext carries per-job execution state through the middleware chain.
type JobContext struct {
	context.Context
	Job       *domain.Job
	Attempt   int
	StartedAt time.Time
	Logger    *slog.Logger
	Storage   storage.Storage
	Items     map[string]any
}

// NewJobContext builds a JobContext for pipeline execution.
func NewJobContext(ctx context.Context, job *domain.Job, attempt int, store storage.Storage, logger *slog.Logger) *JobContext {
	if logger == nil {
		logger = slog.Default()
	}
	return &JobContext{
		Context:   ctx,
		Job:       job,
		Attempt:   attempt,
		StartedAt: time.Now(),
		Logger:    logger,
		Storage:   store,
		Items:     make(map[string]any),
	}
}
