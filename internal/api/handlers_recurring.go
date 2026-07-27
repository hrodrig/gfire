package api

import (
	"encoding/json"
	"net/http"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
)

// handleListRecurring returns all recurring job definitions.
func (s *Server) handleListRecurring(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.GetRecurringJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []*domain.RecurringJobEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recurring": entries})
}

type recurringRequest struct {
	ID       string          `json:"id"`
	JobName  string          `json:"job_name"`
	Args     json.RawMessage `json:"args"`
	Queue    string          `json:"queue"`
	CronExpr string          `json:"cron_expr"`
	Enabled  *bool           `json:"enabled,omitempty"`
}

func (s *Server) handleCreateRecurring(w http.ResponseWriter, r *http.Request) {
	var req recurringRequest
	if err := decodeJSON(r, s.cfg.Server.MaxBodySize, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == "" || req.JobName == "" || req.CronExpr == "" {
		writeError(w, http.StatusBadRequest, "id, job_name, and cron_expr are required")
		return
	}
	queue := req.Queue
	if queue == "" {
		queue = "default"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	args := []byte("{}")
	if len(req.Args) > 0 {
		args = append([]byte(nil), req.Args...)
	}

	entry := &domain.RecurringJobEntry{
		ID:       req.ID,
		JobName:  req.JobName,
		Args:     args,
		Queue:    queue,
		CronExpr: req.CronExpr,
		Enabled:  enabled,
	}
	entry.CreatedAt = time.Now()
	entry.UpdatedAt = entry.CreatedAt

	if err := s.store.UpsertRecurring(r.Context(), entry); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        entry.ID,
		"job_name":  entry.JobName,
		"cron_expr": entry.CronExpr,
		"enabled":   entry.Enabled,
	})
}

func (s *Server) handleDeleteRecurring(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := s.store.RemoveRecurring(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleTriggerRecurring(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	entries, err := s.store.GetRecurringJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var entry *domain.RecurringJobEntry
	for _, e := range entries {
		if e.ID == id {
			entry = e
			break
		}
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "recurring job not found")
		return
	}

	queue := entry.Queue
	if queue == "" {
		queue = "default"
	}
	args := entry.Args
	if len(args) == 0 {
		args = []byte("{}")
	}

	job := &domain.Job{
		Name:  entry.JobName,
		Args:  args,
		Queue: queue,
	}

	jobID, err := s.store.Enqueue(r.Context(), queue, job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"recurring_id": id,
		"job_id":       jobID,
		"status":       "triggered",
	})
}
