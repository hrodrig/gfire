package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/version"
)

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.Version,
		"commit":  version.Commit,
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}
	_, err := s.store.GetQueues(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type enqueueRequest struct {
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
	Queue    string          `json:"queue"`
	RetryMax int             `json:"retry_max"`
	Timeout  string          `json:"timeout"`
}

func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	var req enqueueRequest
	if err := decodeJSON(r, s.cfg.Server.MaxBodySize, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	queue := req.Queue
	if queue == "" {
		queue = "default"
	}
	args := []byte("{}")
	if len(req.Args) > 0 {
		args = append([]byte(nil), req.Args...)
	}
	job := &domain.Job{Name: req.Name, Args: args, Queue: queue, RetryMax: req.RetryMax}
	if req.Timeout != "" {
		d, err := time.ParseDuration(req.Timeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid timeout")
			return
		}
		job.Timeout = d
	}

	// B5-010: Idempotency-Key header for client retry deduplication.
	idemKey := r.Header.Get("Idempotency-Key")
	job.IdempotencyKey = idemKey

	var id string
	var err error
	if idemKey != "" {
		id, _, err = s.store.EnqueueIdempotent(r.Context(), queue, job)
	} else {
		id, err = s.store.Enqueue(r.Context(), queue, job)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"job_id": id,
		"status": "enqueued",
		"queue":  queue,
	})
}

type batchEnqueueRequest struct {
	Jobs []enqueueRequest `json:"jobs"`
}

func (s *Server) handleBatchEnqueue(w http.ResponseWriter, r *http.Request) {
	var req batchEnqueueRequest
	if err := decodeJSON(r, s.cfg.Server.MaxBodySize, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Jobs) == 0 {
		writeError(w, http.StatusBadRequest, "jobs array is required and must not be empty")
		return
	}

	type acceptedEntry struct {
		JobID string `json:"job_id"`
		Name  string `json:"name"`
		Queue string `json:"queue"`
	}
	type rejectedEntry struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
		Index  int    `json:"index"`
	}

	var accepted []acceptedEntry
	var rejected []rejectedEntry

	// B5-010: batch Idempotency-Key → each job gets key:index.
	batchIdemKey := r.Header.Get("Idempotency-Key")

	for i, jr := range req.Jobs {
		if jr.Name == "" {
			rejected = append(rejected, rejectedEntry{Name: jr.Name, Reason: "name is required", Index: i})
			continue
		}
		queue := jr.Queue
		if queue == "" {
			queue = "default"
		}
		args := []byte("{}")
		if len(jr.Args) > 0 {
			args = append([]byte(nil), jr.Args...)
		}
		job := &domain.Job{Name: jr.Name, Args: args, Queue: queue, RetryMax: jr.RetryMax}
		if jr.Timeout != "" {
			d, err := time.ParseDuration(jr.Timeout)
			if err != nil {
				rejected = append(rejected, rejectedEntry{Name: jr.Name, Reason: "invalid timeout: " + jr.Timeout, Index: i})
				continue
			}
			job.Timeout = d
		}

		// Set per-job idempotency key from batch header.
		if batchIdemKey != "" {
			job.IdempotencyKey = fmt.Sprintf("%s:%d", batchIdemKey, i)
		}

		var id string
		var enqErr error
		if job.IdempotencyKey != "" {
			id, _, enqErr = s.store.EnqueueIdempotent(r.Context(), queue, job)
		} else {
			id, enqErr = s.store.Enqueue(r.Context(), queue, job)
		}
		if enqErr != nil {
			rejected = append(rejected, rejectedEntry{Name: jr.Name, Reason: "enqueue failed: " + enqErr.Error(), Index: i})
			continue
		}
		accepted = append(accepted, acceptedEntry{JobID: id, Name: jr.Name, Queue: queue})
	}

	status := http.StatusCreated
	if len(accepted) == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{
		"accepted": accepted,
		"rejected": rejected,
		"total":    len(req.Jobs),
	})
}

type scheduleRequest struct {
	enqueueRequest
	EnqueueAt time.Time `json:"enqueue_at"`
}

func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := decodeJSON(r, s.cfg.Server.MaxBodySize, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.EnqueueAt.IsZero() {
		writeError(w, http.StatusBadRequest, "enqueue_at is required")
		return
	}
	queue := req.Queue
	if queue == "" {
		queue = "default"
	}
	args := []byte("{}")
	if len(req.Args) > 0 {
		args = append([]byte(nil), req.Args...)
	}
	job := &domain.Job{Name: req.Name, Args: args, Queue: queue, RetryMax: req.RetryMax}
	id, err := s.store.AddScheduled(r.Context(), req.EnqueueAt, job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"job_id":     id,
		"status":     "scheduled",
		"enqueue_at": req.EnqueueAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	jw, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, jobResponse(jw))
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	jobs, err := s.store.GetJobsByState(r.Context(), state, offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]any, 0, len(jobs))
	for _, jw := range jobs {
		out = append(out, jobResponse(jw))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleRequeue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Requeue(r.Context(), id, "manual requeue"); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enqueued"})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeError(w, http.StatusNotImplemented, "engine not available")
		return
	}
	id := r.PathValue("id")
	if err := s.engine.CancelJob(id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteJob(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type continueRequest struct {
	ChildName  string          `json:"child_name"`
	ChildArgs  json.RawMessage `json:"child_args"`
	ChildQueue string          `json:"child_queue"`
	Condition  string          `json:"condition"`
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	parentID := r.PathValue("id")
	var req continueRequest
	if err := decodeJSON(r, s.cfg.Server.MaxBodySize, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ChildName == "" {
		writeError(w, http.StatusBadRequest, "child_name is required")
		return
	}
	cond := req.Condition
	if cond == "" {
		cond = domain.ConditionOnSucceeded
	}
	args := []byte("{}")
	if len(req.ChildArgs) > 0 {
		args = append([]byte(nil), req.ChildArgs...)
	}
	entry := &domain.ContinuationEntry{
		ChildName:  req.ChildName,
		ChildArgs:  args,
		ChildQueue: req.ChildQueue,
		Condition:  cond,
		CreatedAt:  time.Now(),
	}
	if err := s.store.AddContinuation(r.Context(), parentID, entry); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

func jobResponse(jw *domain.JobWithStates) map[string]any {
	states := make([]map[string]any, 0, len(jw.States))
	for _, st := range jw.States {
		states = append(states, map[string]any{
			"name":       st.Name,
			"reason":     st.Reason,
			"data":       st.Data,
			"created_at": st.CreatedAt,
		})
	}
	var args any
	_ = json.Unmarshal(jw.Job.Args, &args)
	var result any
	if len(jw.Job.Result) > 0 {
		_ = json.Unmarshal(jw.Job.Result, &result)
	}
	return map[string]any{
		"job": map[string]any{
			"id":         jw.Job.ID,
			"name":       jw.Job.Name,
			"args":       args,
			"queue":      jw.Job.Queue,
			"retry_max":  jw.Job.RetryMax,
			"timeout":    jw.Job.Timeout.String(),
			"created_at": jw.Job.CreatedAt,
			"result":     result,
		},
		"states":        states,
		"current_state": jw.CurrentState(),
	}
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return def
	}
	return n
}
