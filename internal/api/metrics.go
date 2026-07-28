package api

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder

	// Counters from storage.
	counters, _ := s.store.GetAllCounters(r.Context(), 0, 100)
	for name, val := range counters {
		fmt.Fprintf(&b, "# HELP gfire_jobs_%s_total Total jobs %s.\n", name, name)
		fmt.Fprintf(&b, "# TYPE gfire_jobs_%s_total counter\n", name)
		fmt.Fprintf(&b, "gfire_jobs_%s_total %d\n", name, val)
	}

	// Queue depths.
	queues, err := s.store.GetQueues(r.Context())
	if err == nil {
		for _, q := range queues {
			n, _ := s.store.GetQueueLength(r.Context(), q)
			fmt.Fprintf(&b, "# HELP gfire_queue_depth Number of enqueued jobs per queue.\n")
			fmt.Fprintf(&b, "# TYPE gfire_queue_depth gauge\n")
			fmt.Fprintf(&b, "gfire_queue_depth{queue=\"%s\"} %d\n", q, n)
		}
	}

	// Server count.
	servers, err := s.store.GetServers(r.Context())
	if err == nil {
		active := 0
		for _, sv := range servers {
			if sv.Status == "active" {
				active++
			}
		}
		fmt.Fprintf(&b, "# HELP gfire_servers Number of registered servers.\n")
		fmt.Fprintf(&b, "# TYPE gfire_servers gauge\n")
		fmt.Fprintf(&b, "gfire_servers{status=\"active\"} %d\n", active)
		fmt.Fprintf(&b, "gfire_servers{status=\"total\"} %d\n", len(servers))
	}

	// Active workers on this engine.
	if s.engine != nil {
		n := s.engine.ActiveCount()
		fmt.Fprintf(&b, "# HELP gfire_workers_active In-flight jobs on this node.\n")
		fmt.Fprintf(&b, "# TYPE gfire_workers_active gauge\n")
		fmt.Fprintf(&b, "gfire_workers_active %d\n", n)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(b.String()))
}
