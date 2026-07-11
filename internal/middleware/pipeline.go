package middleware

import (
	"github.com/hrodrig/gfire/internal/handler"
	domain "github.com/hrodrig/gfire/internal/job"
)

// HandlerFunc is the innermost job executor.
type HandlerFunc func(ctx *JobContext) (result []byte, err error)

// MiddlewareFunc wraps job execution (net/http style).
type MiddlewareFunc func(ctx *JobContext, next HandlerFunc) HandlerFunc

// Pipeline chains middleware around a handler Runner.
type Pipeline struct {
	middlewares []MiddlewareFunc
	runner      handler.Runner
}

// NewPipeline builds a pipeline with middleware applied outermost-first.
func NewPipeline(runner handler.Runner, middlewares ...MiddlewareFunc) *Pipeline {
	return &Pipeline{middlewares: middlewares, runner: runner}
}

// Execute runs the full chain and returns handler stdout + error.
func (p *Pipeline) Execute(ctx *JobContext) (result []byte, err error) {
	var h HandlerFunc = func(c *JobContext) ([]byte, error) {
		return p.runner.Run(c.Context, c.Job)
	}
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		mw := p.middlewares[i]
		next := h
		h = func(c *JobContext) ([]byte, error) {
			return mw(c, next)(c)
		}
	}
	return h(ctx)
}

// AttemptCount counts prior Failed states (retry attempts so far).
func AttemptCount(states []*domain.JobState) int {
	n := 0
	for _, st := range states {
		if st.Name == domain.StateFailed {
			n++
		}
	}
	return n
}
