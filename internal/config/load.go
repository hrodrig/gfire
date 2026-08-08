package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// bindEnv registers nested keys so GFIRE_* env overrides reach Unmarshal.
// Viper AutomaticEnv alone does not populate nested mapstructure fields.
func bindEnv(v *viper.Viper) {
	keys := []string{
		"server.host",
		"server.port",
		"server.workers",
		"server.server_id",
		"server.dequeue_timeout",
		"server.shutdown_timeout",
		"server.max_body_size",
		"server.default_timeout",
		"heartbeat.interval",
		"heartbeat.timeout",
		"heartbeat.orphan_timeout",
		"scheduler.interval",
		"scheduler.batch_size",
		"cleanup.interval",
		"cleanup.job_retention",
		"storage.backend",
		"storage.redis.addr",
		"storage.redis.password",
		"storage.redis.db",
		"storage.postgres.dsn",
		"storage.postgres.max_conns",
		"storage.postgres.min_conns",
		"logging.level",
		"logging.format",
		"metrics.enabled",
		"auth.enabled",
		"auth.token",
	}
	for _, k := range keys {
		_ = v.BindEnv(k)
	}
}

// Load reads configuration from file path and GFIRE_* environment variables.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	v := viper.New()
	v.SetEnvPrefix("GFIRE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	bindEnv(v)

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else if p := os.Getenv("GFIRE_CONFIG"); p != "" {
		v.SetConfigFile(p)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read GFIRE_CONFIG: %w", err)
		}
	}

	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.QueueLimits == nil {
		cfg.QueueLimits = map[string]int{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}
