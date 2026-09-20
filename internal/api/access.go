package api

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"aegis/internal/store"
)

// accessRequest is the two client sets as they arrive over HTTP. An absent
// list is an empty one, so a PUT replaces both sets.
type accessRequest struct {
	Allowed    []string `json:"allowed"`
	Disallowed []string `json:"disallowed"`
}

// accessResponse is the two client sets as they leave over HTTP. Every entry
// is the masked form the listener gate matches against.
type accessResponse struct {
	Allowed    []string `json:"allowed"`
	Disallowed []string `json:"disallowed"`
}

func parsePrefixes(rows []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(rows))
	for _, row := range rows {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(row))
		if err != nil {
			return nil, fmt.Errorf("access entry %q: %w", row, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func prefixText(prefixes []netip.Prefix) []string {
	text := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		text = append(text, prefix.String())
	}
	return text
}

func (s *Server) getAccess(w http.ResponseWriter, r *http.Request) {
	allowed, disallowed, err := s.store.Access(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accessResponse{Allowed: prefixText(allowed), Disallowed: prefixText(disallowed)})
}

// putAccess replaces both client sets atomically, so the listener's gate
// changes as one generation on the reload that follows the write.
func (s *Server) putAccess(w http.ResponseWriter, r *http.Request) {
	request, err := decodeJSON[accessRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	allowed, err := parsePrefixes(request.Allowed)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	disallowed, err := parsePrefixes(request.Disallowed)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	ctx := r.Context()
	if err := s.apply(ctx, func(cfg *store.Config) error {
		cfg.Allowed = allowed
		cfg.Disallowed = disallowed
		return nil
	}, func(ctx context.Context) error {
		return s.store.SetAccess(ctx, allowed, disallowed)
	}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accessResponse{Allowed: prefixText(allowed), Disallowed: prefixText(disallowed)})
}
