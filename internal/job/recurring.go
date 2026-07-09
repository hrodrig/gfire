package job

import "time"

// RecurringJobEntry defines a cron-based recurring job.
// Entries are persisted in storage and survive server restarts.
type RecurringJobEntry struct {
	ID        string     `json:"id"`
	JobName   string     `json:"job_name"`
	Args      []byte     `json:"args"`
	Queue     string     `json:"queue"`
	CronExpr  string     `json:"cron_expr"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	NextRun   *time.Time `json:"next_run,omitempty"`
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
