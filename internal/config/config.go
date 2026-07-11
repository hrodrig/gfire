// Package config loads GFire YAML configuration and builds runtime dependencies.
package config

import (
	"time"

	"github.com/hrodrig/gfire/internal/engine"
	"github.com/hrodrig/gfire/internal/handler"
)

// Config is the root configuration document (gfire.yaml).
type Config struct {
	Server      ServerConfig    `mapstructure:"server"`
	Heartbeat   HeartbeatConfig `mapstructure:"heartbeat"`
	Scheduler   SchedulerConfig `mapstructure:"scheduler"`
	Cleanup     CleanupConfig   `mapstructure:"cleanup"`
	Storage     StorageConfig   `mapstructure:"storage"`
	Logging     LoggingConfig   `mapstructure:"logging"`
	Metrics     MetricsConfig   `mapstructure:"metrics"`
	Auth        AuthConfig      `mapstructure:"auth"`
	Handlers    []HandlerEntry  `mapstructure:"handlers"`
	QueueLimits map[string]int  `mapstructure:"queue_limits"`
}

type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Workers         int           `mapstructure:"workers"`
	Queues          []string      `mapstructure:"queues"`
	DequeueTimeout  time.Duration `mapstructure:"dequeue_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	MaxBodySize     int64         `mapstructure:"max_body_size"`
	DefaultTimeout  time.Duration `mapstructure:"default_timeout"`
	ServerID        string        `mapstructure:"server_id"`
}

type HeartbeatConfig struct {
	Interval      time.Duration `mapstructure:"interval"`
	Timeout       time.Duration `mapstructure:"timeout"`
	OrphanTimeout time.Duration `mapstructure:"orphan_timeout"`
}

type SchedulerConfig struct {
	Interval  time.Duration `mapstructure:"interval"`
	BatchSize int           `mapstructure:"batch_size"`
}

type CleanupConfig struct {
	Interval     time.Duration `mapstructure:"interval"`
	JobRetention time.Duration `mapstructure:"job_retention"`
}

type StorageConfig struct {
	Backend  string         `mapstructure:"backend"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type PostgresConfig struct {
	DSN      string `mapstructure:"dsn"`
	MaxConns int32  `mapstructure:"max_conns"`
	MinConns int32  `mapstructure:"min_conns"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type AuthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
}

type HandlerEntry struct {
	Name string `mapstructure:"name"`
	Cmd  string `mapstructure:"cmd"`
}

// Defaults returns a configuration with SPEC defaults.
func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			Workers:         4,
			Queues:          []string{"default"},
			DequeueTimeout:  5 * time.Second,
			ShutdownTimeout: 30 * time.Second,
			MaxBodySize:     10 << 20,
			DefaultTimeout:  30 * time.Second,
		},
		Heartbeat: HeartbeatConfig{
			Interval:      5 * time.Second,
			Timeout:       30 * time.Second,
			OrphanTimeout: 5 * time.Minute,
		},
		Scheduler: SchedulerConfig{
			Interval:  time.Second,
			BatchSize: 100,
		},
		Cleanup: CleanupConfig{
			Interval:     time.Hour,
			JobRetention: 24 * time.Hour,
		},
		Storage:     StorageConfig{Backend: "memory"},
		Logging:     LoggingConfig{Level: "info", Format: "text"},
		Metrics:     MetricsConfig{Enabled: true},
		QueueLimits: map[string]int{},
	}
}

// EngineConfig maps file config to engine runtime config.
func (c *Config) EngineConfig(serverID string) engine.Config {
	if serverID == "" {
		serverID = c.Server.ServerID
	}
	if serverID == "" {
		serverID = "local"
	}
	return engine.Config{
		ServerID:             serverID,
		Workers:              c.Server.Workers,
		Queues:               append([]string(nil), c.Server.Queues...),
		QueueLimits:          c.QueueLimits,
		DequeueTimeout:       c.Server.DequeueTimeout,
		DefaultTimeout:       c.Server.DefaultTimeout,
		ShutdownTimeout:      c.Server.ShutdownTimeout,
		JobHeartbeatInterval: 60 * time.Second,
		SchedulerInterval:    c.Scheduler.Interval,
		SchedulerBatchSize:   c.Scheduler.BatchSize,
		ServerHeartbeatTTL:   c.Heartbeat.Timeout,
		ServerHeartbeatEvery: c.Heartbeat.Interval,
		OrphanJobStaleAge:    c.Heartbeat.OrphanTimeout,
		CleanupInterval:      c.Cleanup.Interval,
		JobRetention:         c.Cleanup.JobRetention,
	}
}

func (c *Config) HandlerRegistry() *handler.Registry {
	m := make(map[string]string, len(c.Handlers))
	for _, h := range c.Handlers {
		if h.Name != "" && h.Cmd != "" {
			m[h.Name] = h.Cmd
		}
	}
	return handler.NewRegistry(m)
}

// ListenAddr returns host:port for the HTTP API.
func (c *Config) ListenAddr() string {
	host := c.Server.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := c.Server.Port
	if port == 0 {
		port = 8080
	}
	return joinHostPort(host, port)
}
