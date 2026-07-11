package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func maxBody(max int64, next http.Handler) http.Handler {
	if max <= 0 {
		max = 10 << 20
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}

func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		got := strings.TrimPrefix(h, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
