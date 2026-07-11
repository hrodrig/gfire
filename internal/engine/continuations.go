package engine

import (
	"context"
	"encoding/json"

	domain "github.com/hrodrig/gfire/internal/job"
)

func (e *Engine) fireContinuations(ctx context.Context, parentID, terminalState string) {
	entries, err := e.storage.GetContinuations(ctx, parentID)
	if err != nil || len(entries) == 0 {
		return
	}
	parent, err := e.storage.GetJob(ctx, parentID)
	if err != nil {
		e.logger.Warn("continuation parent lookup failed", "job_id", parentID, "err", err)
		return
	}
	for _, entry := range entries {
		if !continuationMatches(terminalState, entry.Condition) {
			continue
		}
		args := mergeContinuationArgs(parent.Job.Result, entry.ChildArgs)
		queue := entry.ChildQueue
		if queue == "" {
			queue = "default"
		}
		child := &domain.Job{
			Name:  entry.ChildName,
			Args:  args,
			Queue: queue,
		}
		if _, err := e.storage.Enqueue(ctx, queue, child); err != nil {
			e.logger.Warn("continuation enqueue failed", "parent", parentID, "child", entry.ChildName, "err", err)
		}
	}
}

func continuationMatches(terminalState, condition string) bool {
	switch condition {
	case domain.ConditionOnSucceeded:
		return terminalState == domain.StateSucceeded
	case domain.ConditionOnFailed:
		return terminalState == domain.StateFailed || terminalState == domain.StateDead
	case domain.ConditionOnAny:
		return true
	default:
		return false
	}
}

func mergeContinuationArgs(parentResult, childArgs []byte) []byte {
	if len(parentResult) == 0 {
		return append([]byte(nil), childArgs...)
	}
	var base map[string]any
	if len(childArgs) > 0 {
		_ = json.Unmarshal(childArgs, &base)
	}
	if base == nil {
		base = map[string]any{}
	}
	var pr any
	if json.Unmarshal(parentResult, &pr) == nil {
		base["_parent_result"] = pr
	}
	out, err := json.Marshal(base)
	if err != nil {
		return append([]byte(nil), childArgs...)
	}
	return out
}
