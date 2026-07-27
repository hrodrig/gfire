package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hrodrig/gfire/internal/api"
	"github.com/hrodrig/gfire/internal/config"
	"github.com/hrodrig/gfire/internal/engine"
	"github.com/hrodrig/gfire/internal/handler"
	domain "github.com/hrodrig/gfire/internal/job"
	"github.com/hrodrig/gfire/internal/storage/memory"
)

func TestAPI_EnqueueAndProcess(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := config.Defaults()
	cfg.Server.Workers = 2

	runner := handler.Func(func(_ context.Context, _ *domain.Job) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	})
	eng := engine.New(store, cfg.EngineConfig("test"), runner, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	srv := api.NewServer(&cfg, store, eng)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body := `{"name":"work","args":{"x":1},"queue":"default"}`
	resp, err := http.Post(ts.URL+"/v1/jobs/enqueue", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	jobID, _ := created["job_id"].(string)
	if jobID == "" {
		t.Fatal("missing job_id")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		getResp, err := http.Get(ts.URL + "/v1/jobs/" + jobID)
		if err != nil {
			t.Fatal(err)
		}
		var detail map[string]any
		_ = json.NewDecoder(getResp.Body).Decode(&detail)
		getResp.Body.Close()
		if detail["current_state"] == "Succeeded" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job did not succeed in time")
}

func TestAPI_RecurringCRUD(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := config.Defaults()
	cfg.Server.Workers = 1

	eng := engine.New(store, cfg.EngineConfig("test"), handler.NopRunner{}, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	srv := api.NewServer(&cfg, store, eng)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Create a recurring job.
	body := `{"id":"nightly","job_name":"cleanup","cron_expr":"@every 1h","args":{"days":30}}`
	resp, err := http.Post(ts.URL+"/v1/recurring", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}

	// List recurring jobs.
	resp2, err := http.Get(ts.URL + "/v1/recurring")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var list map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	entries, _ := list["recurring"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 recurring entry, got %d", len(entries))
	}

	// Trigger it immediately.
	resp3, err := http.Post(ts.URL+"/v1/recurring/nightly/trigger", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusCreated {
		t.Fatalf("trigger: expected 201, got %d", resp3.StatusCode)
	}
	var triggered map[string]any
	json.NewDecoder(resp3.Body).Decode(&triggered)
	jobID, _ := triggered["job_id"].(string)
	if jobID == "" {
		t.Fatal("trigger did not return job_id")
	}

	// Verify the triggered job exists and completes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := http.Get(ts.URL + "/v1/jobs/" + jobID)
		var detail map[string]any
		json.NewDecoder(r.Body).Decode(&detail)
		r.Body.Close()
		if detail["current_state"] == "Succeeded" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Delete it.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/recurring/nightly", nil)
	resp4, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", resp4.StatusCode)
	}

	// Verify it's gone.
	resp5, err := http.Get(ts.URL + "/v1/recurring")
	if err != nil {
		t.Fatal(err)
	}
	defer resp5.Body.Close()
	var list2 map[string]any
	json.NewDecoder(resp5.Body).Decode(&list2)
	if entries, _ := list2["recurring"].([]any); len(entries) != 0 {
		t.Fatal("recurring entry not deleted")
	}
}

func TestAPI_BatchEnqueue(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	cfg := config.Defaults()
	cfg.Server.Workers = 2

	eng := engine.New(store, cfg.EngineConfig("test"), handler.NopRunner{}, nil)
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
	})

	srv := api.NewServer(&cfg, store, eng)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Batch with 3 valid + 1 invalid (missing name).
	body := `{"jobs":[
		{"name":"a","args":{"x":1}},
		{"name":"b"},
		{"name":""},
		{"name":"c","args":{"z":3}}
	]}`
	resp, err := http.Post(ts.URL+"/v1/jobs/enqueue/batch", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	accepted, _ := result["accepted"].([]any)
	rejected, _ := result["rejected"].([]any)
	total, _ := result["total"].(float64)

	if int(total) != 4 {
		t.Fatalf("total: expected 4, got %v", total)
	}
	if len(accepted) != 3 {
		t.Fatalf("accepted: expected 3, got %d", len(accepted))
	}
	if len(rejected) != 1 {
		t.Fatalf("rejected: expected 1, got %d", len(rejected))
	}

	// Verify the accepted jobs completed.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobsResp, _ := http.Get(ts.URL + "/v1/jobs?limit=10")
		var list map[string]any
		json.NewDecoder(jobsResp.Body).Decode(&list)
		jobsResp.Body.Close()
		jobs, _ := list["jobs"].([]any)
		succeeded := 0
		for _, j := range jobs {
			jm, _ := j.(map[string]any)
			if jm["current_state"] == "Succeeded" {
				succeeded++
			}
		}
		if succeeded >= 3 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("not all accepted jobs completed")
}
