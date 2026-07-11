// Package handler runs job handlers (subprocess cmd or in-process func for tests).
package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
)

// MaxResultBytes is the maximum handler stdout stored as Job.Result.
const MaxResultBytes = 64 * 1024

// Runner executes a job handler.
type Runner interface {
	Run(ctx context.Context, job *domain.Job) (stdout []byte, err error)
}

// Func is an in-process handler for tests and embedded use.
type Func func(ctx context.Context, job *domain.Job) ([]byte, error)

// Run implements Runner.
func (f Func) Run(ctx context.Context, job *domain.Job) ([]byte, error) {
	return f(ctx, job)
}

// Registry maps job names to external command paths.
type Registry struct {
	cmds map[string]string
}

// NewRegistry builds a handler registry from name → cmd path.
func NewRegistry(entries map[string]string) *Registry {
	cmds := make(map[string]string, len(entries))
	for k, v := range entries {
		cmds[k] = v
	}
	return &Registry{cmds: cmds}
}

// Run executes the subprocess handler for job.Name.
func (r *Registry) Run(ctx context.Context, job *domain.Job) ([]byte, error) {
	cmdPath, ok := r.cmds[job.Name]
	if !ok {
		return nil, fmt.Errorf("unknown handler: %s", job.Name)
	}
	return runSubprocess(ctx, cmdPath, job.Args)
}

func runSubprocess(ctx context.Context, cmdPath string, args []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cmdPath)
	cmd.Stdin = bytes.NewReader(args)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start handler: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
		return nil, ctx.Err()
	case err := <-done:
		out := trimResult(stdout.Bytes())
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return out, fmt.Errorf("%w: %s", err, msg)
			}
			return out, err
		}
		return out, nil
	}
}

func trimResult(b []byte) []byte {
	if len(b) <= MaxResultBytes {
		return b
	}
	return b[:MaxResultBytes]
}

// NopRunner succeeds without output (jobs with no configured handler in dev).
type NopRunner struct{}

func (NopRunner) Run(_ context.Context, _ *domain.Job) ([]byte, error) {
	return nil, nil
}

// EchoRunner returns args unchanged (testing).
type EchoRunner struct{}

func (EchoRunner) Run(_ context.Context, job *domain.Job) ([]byte, error) {
	if job.Args == nil {
		return []byte("{}"), nil
	}
	return trimResult(bytes.Clone(job.Args)), nil
}

// ErrUnknownHandler is returned when Cancel targets a job not being processed.
var ErrUnknownHandler = errors.New("unknown handler")

// WriteArgsFile is a test helper that writes args to a temp file path in env.
func WriteArgsFile(args []byte) (string, error) {
	f, err := os.CreateTemp("", "gfire-args-*.json")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, bytes.NewReader(args)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
