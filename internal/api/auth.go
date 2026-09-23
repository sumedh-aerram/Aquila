package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

const apiTokenHeader = "X-Aquila-Token"

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/v1/traces" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.apiAuthorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func publicPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/version":
		return true
	default:
		return false
	}
}

func (s *Server) apiAuthorized(r *http.Request) bool {
	want := s.cfg.Server.Token
	if want == "" {
		return true
	}
	got := r.Header.Get(apiTokenHeader)
	if got == "" {
		got = bearerToken(r.Header.Get("Authorization"))
	}
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func bearerToken(h string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
