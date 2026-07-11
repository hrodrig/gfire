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
