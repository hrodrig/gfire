package job

import (
	"context"
	"time"
)

// Lock represents a distributed mutex. It must be Released to unlock.
type Lock interface {
	Release(ctx context.Context) error
}

// JobTicket wraps a dequeued job with its concurrency token.
// The Token is used for optimistic state transitions (ApplyState).
type JobTicket struct {
	JobID string
	Token string
}

// ContinuationEntry defines a conditional child job.
// When the parent job reaches a terminal state matching Condition,
// the child job is enqueued automatically.
type ContinuationEntry struct {
	ChildName  string    `json:"child_name"`
	ChildArgs  []byte    `json:"child_args"`
	ChildQueue string    `json:"child_queue"`
	Condition  string    `json:"condition"`
	CreatedAt  time.Time `json:"created_at"`
}

// Condition constants for continuations.
const (
	ConditionOnSucceeded = "OnSucceeded"
	ConditionOnFailed    = "OnFailed"
	ConditionOnAny       = "OnAny"
)
