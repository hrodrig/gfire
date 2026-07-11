package app

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/hrodrig/gfire/internal/config"
	"github.com/hrodrig/gfire/internal/version"
)

// WriteStartupBanner prints a single-line summary to w (always shown, gghstats-style).
func WriteStartupBanner(w io.Writer, cfg *config.Config, serverID string) {
	if cfg == nil {
		return
	}
	fmt.Fprintf(w,
		"gfire %s | build %s | commit %s | platform %s/%s | listen %s | storage %s | workers %d | queues %s | handlers %s | auth %s\n",
		version.Version,
		version.BuildDate,
		version.Commit,
		runtime.GOOS, runtime.GOARCH,
		cfg.ListenAddr(),
		cfg.StorageSummary(),
		cfg.Server.Workers,
		strings.Join(cfg.Server.Queues, ","),
		cfg.HandlersSummary(),
		authLabel(cfg.Auth.Enabled),
	)
}

// LogStartup writes the banner and a structured summary before subsystems start.
func LogStartup(cfg *config.Config, serverID string) {
	WriteStartupBanner(os.Stderr, cfg, serverID)
	base := BaseURL(cfg)
	slog.Info("gfire starting",
		"version", version.Version,
		"commit", version.Commit,
		"server_id", serverID,
		"api", base+"/v1",
		"health", base+"/healthz",
		"storage", cfg.StorageSummary(),
		"workers", cfg.Server.Workers,
		"queues", cfg.Server.Queues,
		"handlers", cfg.HandlersSummary(),
		"auth", authLabel(cfg.Auth.Enabled),
		"metrics", cfg.Metrics.Enabled,
	)
}

// BaseURL returns a browser/curl-friendly HTTP origin (localhost when bind-all).
func BaseURL(cfg *config.Config) string {
	host := cfg.Server.Host
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://%s", joinHostPort(host, port))
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func authLabel(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}
