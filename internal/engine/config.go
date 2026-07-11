package engine

import (
	"runtime"
	"time"
)

// Config controls the worker pool and job execution defaults.
type Config struct {
	ServerID             string
	Workers              int
	Queues               []string
	QueueLimits          map[string]int
	DequeueTimeout       time.Duration
	DefaultTimeout       time.Duration
	ShutdownTimeout      time.Duration
	JobHeartbeatInterval time.Duration
	SchedulerInterval    time.Duration
	SchedulerBatchSize   int
	ServerHeartbeatTTL   time.Duration
}

// DefaultConfig returns sensible defaults for local development.
func DefaultConfig() Config {
	workers := runtime.NumCPU() * 2
	if workers < 2 {
		workers = 2
	}
	return Config{
		ServerID:             "local",
		Workers:              workers,
		Queues:               []string{"default"},
		QueueLimits:          map[string]int{},
		DequeueTimeout:       2 * time.Second,
		DefaultTimeout:       30 * time.Second,
		ShutdownTimeout:      30 * time.Second,
		JobHeartbeatInterval: 60 * time.Second,
		SchedulerInterval:    time.Second,
		SchedulerBatchSize:   100,
		ServerHeartbeatTTL:   30 * time.Second,
	}
}

func (c *Config) normalize() {
	if c.ServerID == "" {
		c.ServerID = "local"
	}
	if c.Workers <= 0 {
		c.Workers = DefaultConfig().Workers
	}
	if len(c.Queues) == 0 {
		c.Queues = []string{"default"}
	}
	if c.DequeueTimeout <= 0 {
		c.DequeueTimeout = 2 * time.Second
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.JobHeartbeatInterval <= 0 {
		c.JobHeartbeatInterval = 60 * time.Second
	}
	if c.SchedulerInterval <= 0 {
		c.SchedulerInterval = time.Second
	}
	if c.SchedulerBatchSize <= 0 {
		c.SchedulerBatchSize = 100
	}
	if c.ServerHeartbeatTTL <= 0 {
		c.ServerHeartbeatTTL = 30 * time.Second
	}
}
