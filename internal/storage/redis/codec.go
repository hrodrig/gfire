package redis

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
)

func timeoutMS(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return d.Milliseconds()
}

func durationFromMS(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func marshalState(st *domain.JobState) (string, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}
	return string(b), nil
}

func unmarshalState(raw string) (*domain.JobState, error) {
	var st domain.JobState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &st, nil
}

func jobHashFields(job *domain.Job, state string, progressAt time.Time) map[string]string {
	fields := map[string]string{
		"id":         job.ID,
		"name":       job.Name,
		"args":       string(job.Args),
		"queue":      job.Queue,
		"retry_max":  strconv.Itoa(job.RetryMax),
		"timeout_ms": strconv.FormatInt(timeoutMS(job.Timeout), 10),
		"created_at": job.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"state":      state,
	}
	if !progressAt.IsZero() {
		fields["progress_at"] = progressAt.UTC().Format(time.RFC3339Nano)
	}
	return fields
}

func jobFromHash(fields map[string]string) (*domain.Job, string, time.Time, error) {
	id := fields["id"]
	if id == "" {
		return nil, "", time.Time{}, fmt.Errorf("job hash missing id")
	}
	retryMax, _ := strconv.Atoi(fields["retry_max"])
	timeoutMSVal, _ := strconv.ParseInt(fields["timeout_ms"], 10, 64)
	createdAt, _ := time.Parse(time.RFC3339Nano, fields["created_at"])
	progressAt, _ := time.Parse(time.RFC3339Nano, fields["progress_at"])
	j := &domain.Job{
		ID:        id,
		Name:      fields["name"],
		Args:      []byte(fields["args"]),
		Queue:     fields["queue"],
		RetryMax:  retryMax,
		Timeout:   durationFromMS(timeoutMSVal),
		CreatedAt: createdAt,
	}
	return j, fields["state"], progressAt, nil
}

func marshalServer(sv *domain.ServerInfo) (string, error) {
	b, err := json.Marshal(sv)
	if err != nil {
		return "", fmt.Errorf("marshal server: %w", err)
	}
	return string(b), nil
}

func unmarshalServer(raw string) (*domain.ServerInfo, error) {
	var sv domain.ServerInfo
	if err := json.Unmarshal([]byte(raw), &sv); err != nil {
		return nil, fmt.Errorf("unmarshal server: %w", err)
	}
	return &sv, nil
}

func marshalRecurring(e *domain.RecurringJobEntry) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("marshal recurring: %w", err)
	}
	return string(b), nil
}

func unmarshalRecurring(raw string) (*domain.RecurringJobEntry, error) {
	var e domain.RecurringJobEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return nil, fmt.Errorf("unmarshal recurring: %w", err)
	}
	return &e, nil
}

func marshalContinuation(e *domain.ContinuationEntry) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("marshal continuation: %w", err)
	}
	return string(b), nil
}

func unmarshalContinuation(raw string) (*domain.ContinuationEntry, error) {
	var e domain.ContinuationEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return nil, fmt.Errorf("unmarshal continuation: %w", err)
	}
	return &e, nil
}
