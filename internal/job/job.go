package job

import "time"

// Job is the unit of work. It carries the job name, serialized arguments,
// and metadata. Jobs are created via the HTTP API and stored by the Storage backend.
type Job struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Args      []byte        `json:"args"`
	Queue     string        `json:"queue"`
	RetryMax  int           `json:"retry_max,omitempty"`
	Timeout   time.Duration `json:"timeout,omitempty"` // max execution time (0 = config default)
	CreatedAt time.Time     `json:"created_at"`
}

// JobState represents an immutable node in the job's lifecycle.
// States are append-only — the full history is preserved for auditing.
type JobState struct {
	Name      string            `json:"name"`
	Reason    string            `json:"reason,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// JobWithStates pairs a Job with its full state history.
type JobWithStates struct {
	Job    *Job
	States []*JobState
}

// CurrentState returns the most recent state name, or "" if no states.
func (j *JobWithStates) CurrentState() string {
	if len(j.States) == 0 {
		return ""
	}
	return j.States[len(j.States)-1].Name
}

// State constants for the job lifecycle.
const (
	StateEnqueued   = "Enqueued"
	StateProcessing = "Processing"
	StateSucceeded  = "Succeeded"
	StateFailed     = "Failed"
	StateScheduled  = "Scheduled"
	StateDeleted    = "Deleted"
	StateAwaiting   = "Awaiting"
)

// TerminalStates returns true if the state is terminal
// (no more transitions expected except manual intervention or cleanup).
var TerminalStates = map[string]bool{
	StateSucceeded: true,
	StateFailed:    true,
	StateDeleted:   true,
}

// ServerInfo represents a GFire node registered in the cluster.
type ServerInfo struct {
	ID            string    `json:"id"`
	StartedAt     time.Time `json:"started_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	WorkerCount   int       `json:"worker_count"`
	Queues        []string  `json:"queues"`
	Status        string    `json:"status"`
}

const (
	ServerStatusActive  = "active"
	ServerStatusStale   = "stale"
	ServerStatusRemoved = "removed"
)
