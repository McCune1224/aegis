package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/store"
)

// sourceRequest is one source as it arrives over HTTP. A nil field means keep
// what is stored, which is what makes a one-field body an enable toggle.
type sourceRequest struct {
	URL     *string `json:"url"`
	Format  *string `json:"format"`
	Enabled *bool   `json:"enabled"`
}

// sourceResponse is one source as it leaves over HTTP. LastFetch, LastError,
// and RuleCount are the fetch record, so the panel shows health without a
// second route.
type sourceResponse struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Format    string `json:"format"`
	Enabled   bool   `json:"enabled"`
	LastFetch string `json:"last_fetch,omitempty"`
	LastError string `json:"last_error,omitempty"`
	RuleCount int    `json:"rule_count"`
}

func sourceResponseFrom(source store.Source) sourceResponse {
	response := sourceResponse{
		Name:      source.Name,
		URL:       source.URL,
		Format:    source.Format.String(),
		Enabled:   source.Enabled,
		LastError: source.LastError,
		RuleCount: source.RuleCount,
	}
	if !source.LastFetch.IsZero() {
		response.LastFetch = source.LastFetch.Format(time.RFC3339)
	}
	return response
}

func decodeSource(r *http.Request) (sourceRequest, error) {
	var request sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return sourceRequest{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return request, nil
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.Sources(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]sourceResponse, 0, len(sources))
	for _, source := range sources {
		response = append(response, sourceResponseFrom(source))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request) {
	source, err := s.lookupSource(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceResponseFrom(source))
}

// putSource upserts one source and refreshes, so the response carries the fetch
// record of the change it just made. A body that names a field patches that
// field; the rest of the stored source survives.
func (s *Server) putSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, badRequest{errors.New("a source needs a name")})
		return
	}
	request, err := decodeSource(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	source, err := s.lookupSource(ctx, name)
	if errors.Is(err, errNotFound) {
		source = store.Source{Name: name, Enabled: true}
	} else if err != nil {
		writeError(w, err)
		return
	}

	if request.URL != nil {
		if err := validateSourceURL(*request.URL); err != nil {
			writeError(w, badRequest{err})
			return
		}
		// The old fetch record belongs to the old URL, so drop it rather than
		// risk pairing a stale body or ETag with the new one.
		if source.URL != *request.URL {
			source.ETag = ""
			source.Body = nil
			source.LastFetch = time.Time{}
			source.LastError = ""
			source.RuleCount = 0
		}
		source.URL = *request.URL
	}
	if request.Format != nil {
		format, err := blocklist.ParseFormat(*request.Format)
		if err != nil {
			writeError(w, badRequest{err})
			return
		}
		source.Format = format
	}
	if request.Enabled != nil {
		source.Enabled = *request.Enabled
	}
	if source.URL == "" {
		writeError(w, badRequest{errors.New("a source needs a url")})
		return
	}

	if err := s.store.SaveSource(ctx, source); err != nil {
		writeError(w, err)
		return
	}
	if err := s.sources.RefreshSources(ctx); err != nil {
		writeError(w, fmt.Errorf("refresh sources: %w", err))
		return
	}

	stored, err := s.lookupSource(ctx, name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceResponseFrom(stored))
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	if _, err := s.lookupSource(ctx, name); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.DeleteSource(ctx, name); err != nil {
		writeError(w, err)
		return
	}
	if err := s.sources.RefreshSources(ctx); err != nil {
		writeError(w, fmt.Errorf("refresh sources: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type catalogResponse struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Format string `json:"format"`
}

func (s *Server) getCatalog(w http.ResponseWriter, _ *http.Request) {
	response := make([]catalogResponse, 0, len(blocklist.Catalog))
	for _, entry := range blocklist.Catalog {
		response = append(response, catalogResponse{Name: entry.Name, URL: entry.URL, Format: entry.Format.String()})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) lookupSource(ctx context.Context, name string) (store.Source, error) {
	sources, err := s.store.Sources(ctx)
	if err != nil {
		return store.Source{}, err
	}
	for _, source := range sources {
		if source.Name == name {
			return source, nil
		}
	}
	return store.Source{}, errNotFound
}

func validateSourceURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url scheme %q is not http or https", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("url needs a host")
	}
	return nil
}
