// Package errors defines sentinel errors returned by Storage implementations.
package errors

import "errors"

// Sentinel errors returned by Storage implementations.
var (
	// ErrNotFound is returned when a job or resource does not exist.
	ErrNotFound = errors.New("not found")

	// ErrStateConflict is returned when ApplyState fails because the
	// job is not in the expected current state (optimistic lock failure).
	ErrStateConflict = errors.New("state conflict")

	// ErrQueueEmpty is returned by Dequeue when no job is available
	// within the timeout.
	ErrQueueEmpty = errors.New("queue empty")

	// ErrAlreadyExists is returned when creating a resource that
	// already exists (e.g., duplicate recurring job ID).
	ErrAlreadyExists = errors.New("already exists")

	// ErrLockNotHeld is returned by Lock.Release when the lock was
	// lost or already released.
	ErrLockNotHeld = errors.New("lock not held")

	// ErrTerminalState is returned when an operation targets a job in a
	// terminal state (Requeue on Succeeded/Dead/Cancelled, etc.).
	ErrTerminalState = errors.New("terminal state")
)
