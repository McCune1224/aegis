package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type loginRequest struct {
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, badRequest{fmt.Errorf("invalid JSON: %w", err)})
		return
	}
	if !s.auth.CheckPassword(request.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
		return
	}

	csrf, err := s.auth.Begin(w)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"csrf": csrf})
}

func (s *Server) sessionStatus(w http.ResponseWriter, r *http.Request) {
	found, ok := s.auth.session(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf": found.csrf})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.End(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// requireAuth refuses a request with no live session before the handler sees it.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Authenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next(w, r)
	}
}

// requireCSRF refuses a state-changing request that does not echo the session's
// CSRF token. It runs after requireAuth, so the session exists by then.
func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.ValidCSRF(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing or invalid CSRF token"})
			return
		}
		next(w, r)
	}
}
