package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"aegis/internal/rewrite"
)

// rewriteRequest is one rewrite as it arrives over HTTP.
type rewriteRequest struct {
	Target *string `json:"target"`
}

// rewriteResponse is one rewrite as it leaves over HTTP.
type rewriteResponse struct {
	Pattern string `json:"pattern"`
	Target  string `json:"target"`
}

func rewriteResponseFrom(record rewrite.Record) rewriteResponse {
	return rewriteResponse{Pattern: record.Pattern, Target: rewrite.TargetText(record)}
}

func decodeRewrite(r *http.Request) (rewriteRequest, error) {
	var request rewriteRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return rewriteRequest{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return request, nil
}

func (s *Server) listRewrites(w http.ResponseWriter, r *http.Request) {
	records, err := s.store.Rewrites(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]rewriteResponse, 0, len(records))
	for _, record := range records {
		response = append(response, rewriteResponseFrom(record))
	}
	writeJSON(w, http.StatusOK, response)
}

// putRewrite creates one rewrite or replaces what its pattern held, so the
// answers of a running server change on the next reload.
func (s *Server) putRewrite(w http.ResponseWriter, r *http.Request) {
	request, err := decodeRewrite(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	pattern := strings.TrimSpace(r.PathValue("pattern"))
	if pattern == "" {
		writeError(w, badRequest{errors.New("a rewrite needs a pattern")})
		return
	}
	if request.Target == nil || strings.TrimSpace(*request.Target) == "" {
		writeError(w, badRequest{errors.New("a rewrite needs a target")})
		return
	}

	record, err := rewrite.Parse(pattern, *request.Target)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.SaveRewrite(r.Context(), record); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(r.Context()); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, rewriteResponseFrom(record))
}

// deleteRewrite removes one rewrite by its pattern.
func (s *Server) deleteRewrite(w http.ResponseWriter, r *http.Request) {
	pattern := r.PathValue("pattern")
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	if err := s.store.DeleteRewrite(ctx, pattern); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
