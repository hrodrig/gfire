package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Load reads configuration from file path and GFIRE_* environment variables.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	v := viper.New()
	v.SetEnvPrefix("GFIRE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

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
	return &cfg, nil
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}
