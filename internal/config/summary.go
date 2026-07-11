package config

import (
	"fmt"
	"net/url"
	"strings"
)

// StorageSummary returns a log-safe storage backend description.
func (c *Config) StorageSummary() string {
	cfg := c.Storage
	backend := cfg.Backend
	if backend == "" {
		backend = "memory"
	}
	switch backend {
	case "memory":
		return "memory"
	case "postgres", "postgresql":
		return "postgres (" + redactDSN(cfg.Postgres.DSN) + ")"
	case "redis":
		return "redis (" + redisAddrSummary(cfg) + ")"
	case "valkey":
		return "valkey (" + redisAddrSummary(cfg) + ")"
	default:
		return backend
	}
}

// HandlersSummary describes configured job handlers for startup logs.
func (c *Config) HandlersSummary() string {
	if len(c.Handlers) == 0 {
		return "echo (dev default)"
	}
	names := make([]string, 0, len(c.Handlers))
	for _, h := range c.Handlers {
		if h.Name != "" {
			names = append(names, h.Name)
		}
	}
	if len(names) == 0 {
		return "echo (dev default)"
	}
	return strings.Join(names, ", ")
}

func redisAddrSummary(cfg StorageConfig) string {
	addr := cfg.Redis.Addr
	if addr == "" {
		addr = "localhost:6379"
	}
	if cfg.Redis.Password != "" {
		addr += " auth=***"
	}
	if cfg.Redis.DB != 0 {
		addr += fmt.Sprintf(" db=%d", cfg.Redis.DB)
	}
	return addr
}

func redactDSN(dsn string) string {
	if dsn == "" {
		return "(default)"
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return "[configured]"
	}
	if u.User != nil {
		user := u.User.Username()
		u.User = url.UserPassword(user, "****")
	}
	u.RawQuery = ""
	return u.String()
}
