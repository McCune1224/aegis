// Package api serves the configuration over HTTP so the web app can change what
// the resolver runs without a restart.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"aegis/internal/store"
)

// Reloader rebuilds the running resolver from the store. Runtime implements it,
// and the API calls it after every successful write so a change is live.
type Reloader interface {
	Reload(ctx context.Context) error
}

// Config is what Start needs.
type Config struct {
	Store    *store.Store
	Reloader Reloader
	Hub      *Hub
	Address  string
	Logger   *slog.Logger
}

// Server is the HTTP control plane. It holds its own listener, separate from
// the DNS socket.
type Server struct {
	store    *store.Store
	reloader Reloader
	hub      *Hub
	http     *http.Server
	listener net.Listener

	// mu serializes a mutation's validate, write, and reload, so two requests
	// cannot both validate against a configuration that never saw the other and
	// then leave a pair of records that fail to compile.
	mu sync.Mutex
}

// Start binds address and begins serving. A caller that passes port 0 reads the
// chosen port from Addr as soon as Start returns.
func Start(cfg Config) (*Server, error) {
	if cfg.Store == nil {
		return nil, errors.New("api: Config.Store is required")
	}
	if cfg.Reloader == nil {
		return nil, errors.New("api: Config.Reloader is required")
	}
	if cfg.Address == "" {
		return nil, errors.New("api: Config.Address is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("api: listen %s: %w", cfg.Address, err)
	}

	s := &Server{store: cfg.Store, reloader: cfg.Reloader, hub: cfg.Hub, listener: listener}
	s.http = &http.Server{
		Handler:  s.routes(),
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	go func() {
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("api: listener stopped", "error", err)
		}
	}()
	return s, nil
}

// Addr is the address the listener bound.
func (s *Server) Addr() net.Addr { return s.listener.Addr() }

// Shutdown stops accepting and waits for in-flight requests.
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/profiles", s.listProfiles)
	mux.HandleFunc("PUT /api/v1/profiles/{name}", s.putProfile)
	mux.HandleFunc("GET /api/v1/profiles/{name}", s.getProfile)
	mux.HandleFunc("DELETE /api/v1/profiles/{name}", s.deleteProfile)
	mux.HandleFunc("GET /api/v1/clients", s.listClients)
	mux.HandleFunc("PUT /api/v1/clients/{name}", s.putClient)
	mux.HandleFunc("GET /api/v1/clients/{name}", s.getClient)
	mux.HandleFunc("DELETE /api/v1/clients/{name}", s.deleteClient)
	mux.HandleFunc("GET /api/v1/stream/queries", s.streamQueries)
	return mux
}

// apply runs one configuration change as validate, write, then reload. A change
// that would not compile is refused before it reaches the database, so the
// stored configuration is always one the runtime accepted.
func (s *Server) apply(ctx context.Context, mutate func(*store.Config) error, commit func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	if err := mutate(&cfg); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return badRequest{err}
	}
	if err := commit(ctx); err != nil {
		return err
	}
	if err := s.reloader.Reload(ctx); err != nil {
		return fmt.Errorf("api: reload: %w", err)
	}
	return nil
}

var errNotFound = errors.New("api: not found")

// badRequest marks an error the client can fix, so it becomes a 400 rather than
// a 500.
type badRequest struct{ err error }

func (e badRequest) Error() string { return e.err.Error() }
func (e badRequest) Unwrap() error { return e.err }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var bad badRequest
	switch {
	case errors.Is(err, errNotFound):
		status = http.StatusNotFound
	case errors.As(err, &bad):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
