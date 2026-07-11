package api

import (
	"net/http"
)

func (s *Server) handleListQueues(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.GetQueues(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		depth, _ := s.store.GetQueueLength(r.Context(), name)
		out = append(out, map[string]any{"name": name, "depth": depth})
	}
	writeJSON(w, http.StatusOK, map[string]any{"queues": out})
}

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	depth, err := s.store.GetQueueLength(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":  name,
		"depth": depth,
	})
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.GetServers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}
