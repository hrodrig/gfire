package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hrodrig/gfire/internal/app"
	"github.com/hrodrig/gfire/internal/config"
)

func TestWriteStartupBanner(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Queues = []string{"default", "critical"}
	cfg.Handlers = []config.HandlerEntry{{Name: "echo", Cmd: "/bin/echo"}}

	var buf bytes.Buffer
	app.WriteStartupBanner(&buf, &cfg, "test-node")
	out := buf.String()

	for _, want := range []string{
		"gfire ",
		"| listen ",
		"| storage memory",
		"| workers 4",
		"| queues default,critical",
		"| handlers echo",
		"| auth off",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q:\n%s", want, out)
		}
	}
}

func TestBaseURLBindAll(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 9090
	if got := app.BaseURL(&cfg); got != "http://127.0.0.1:9090" {
		t.Fatalf("BaseURL = %q, want localhost origin", got)
	}
}
