// Package api serves the configuration over HTTP so the web app can change what
// the resolver runs without a restart.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"aegis/internal/metrics"
	"aegis/internal/store"
	"aegis/internal/upstream"
)

// Reloader rebuilds the running resolver from the store. Runtime implements it,
// and the API calls it after every successful write so a change is live.
type Reloader interface {
	Reload(ctx context.Context) error
}

// SourceRefresher fetches the enabled blocklist sources and republishes the
// resolver with their rules. Runtime's SourceSync implements it, and the API
// calls it after every source write.
type SourceRefresher interface {
	RefreshSources(ctx context.Context) error
}

// SourcePreviewer previews what a refresh would change without applying it and
// refreshes one source on demand. Runtime's SourceSync implements both.
type SourcePreviewer interface {
	PreviewSource(ctx context.Context, name string) (added, removed []string, notModified bool, err error)
	RefreshOne(ctx context.Context, name string) error
}

// ServiceRefresher fetches the blocked-services catalog and republishes the
// resolver with what it holds. Runtime's ServiceSync implements it, and the API
// refreshes on demand so a new service reaches an operator without a restart.
type ServiceRefresher interface {
	RefreshCatalog(ctx context.Context) error
}

// Config is what Start needs.
type Config struct {
	Store    *store.Store
	Reloader Reloader
	Sources  SourceRefresher
	Preview  SourcePreviewer
	// Catalog is optional: a server without it still serves the stored
	// catalog, and only the refresh endpoint is unavailable.
	Catalog ServiceRefresher
	Hub     *Hub
	Files   fs.FS
	Address string
	Logger  *slog.Logger
	Metrics *metrics.Metrics
	// Upstreams is the running resolver switch; nil leaves the upstream rows
	// without live health.
	Upstreams *upstream.Switch
}

// Server is the HTTP control plane. It holds its own listener, separate from
// the DNS socket.
type Server struct {
	store     *store.Store
	reloader  Reloader
	sources   SourceRefresher
	preview   SourcePreviewer
	catalog   ServiceRefresher
	hub       *Hub
	files     fs.FS
	metrics   *metrics.Metrics
	upstreams *upstream.Switch
	http      *http.Server
	listener  net.Listener

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
	if cfg.Sources == nil {
		return nil, errors.New("api: Config.Sources is required")
	}
	if cfg.Preview == nil {
		return nil, errors.New("api: Config.Preview is required")
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

	s := &Server{store: cfg.Store, reloader: cfg.Reloader, sources: cfg.Sources, preview: cfg.Preview, catalog: cfg.Catalog, hub: cfg.Hub, files: cfg.Files, metrics: cfg.Metrics, upstreams: cfg.Upstreams, listener: listener}
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
	mux.HandleFunc("GET /api/v1/status", s.status)
	if s.metrics != nil {
		mux.Handle("GET /metrics", s.metrics.Handler())
	}
	mux.HandleFunc("GET /api/v1/profiles", s.listProfiles)
	mux.HandleFunc("GET /api/v1/default-profile", s.getDefaultProfile)
	mux.HandleFunc("PUT /api/v1/default-profile", s.putDefaultProfile)
	mux.HandleFunc("PUT /api/v1/profiles/{name}", s.putProfile)
	mux.HandleFunc("GET /api/v1/profiles/{name}", s.getProfile)
	mux.HandleFunc("DELETE /api/v1/profiles/{name}", s.deleteProfile)
	mux.HandleFunc("GET /api/v1/clients", s.listClients)
	mux.HandleFunc("PUT /api/v1/clients/{name}", s.putClient)
	mux.HandleFunc("GET /api/v1/clients/{name}", s.getClient)
	mux.HandleFunc("DELETE /api/v1/clients/{name}", s.deleteClient)
	mux.HandleFunc("GET /api/v1/sources", s.listSources)
	mux.HandleFunc("GET /api/v1/sources/catalog", s.getCatalog)
	mux.HandleFunc("PUT /api/v1/sources/{name}", s.putSource)
	mux.HandleFunc("GET /api/v1/sources/{name}", s.getSource)
	mux.HandleFunc("DELETE /api/v1/sources/{name}", s.deleteSource)
	mux.HandleFunc("POST /api/v1/sources/{name}/refresh", s.refreshSource)
	mux.HandleFunc("GET /api/v1/sources/{name}/preview", s.previewSource)
	mux.HandleFunc("GET /api/v1/leases", s.listLeases)
	mux.HandleFunc("GET /api/v1/discoveries", s.listDiscoveries)
	mux.HandleFunc("DELETE /api/v1/discoveries/{mac}", s.deleteDiscovery)
	mux.HandleFunc("GET /api/v1/rules", s.listRules)
	mux.HandleFunc("POST /api/v1/rules", s.postRule)
	mux.HandleFunc("PUT /api/v1/rules/{id}", s.putRule)
	mux.HandleFunc("DELETE /api/v1/rules/{id}", s.deleteRule)
	mux.HandleFunc("GET /api/v1/schedules", s.listSchedules)
	mux.HandleFunc("PUT /api/v1/schedules/{name}", s.putSchedule)
	mux.HandleFunc("DELETE /api/v1/schedules/{name}", s.deleteSchedule)
	mux.HandleFunc("GET /api/v1/rewrites", s.listRewrites)
	mux.HandleFunc("PUT /api/v1/rewrites/{pattern...}", s.putRewrite)
	mux.HandleFunc("DELETE /api/v1/rewrites/{pattern...}", s.deleteRewrite)
	mux.HandleFunc("GET /api/v1/upstreams", s.listUpstreams)
	mux.HandleFunc("PUT /api/v1/upstreams/{name}", s.putUpstream)
	mux.HandleFunc("DELETE /api/v1/upstreams/{name}", s.deleteUpstream)
	mux.HandleFunc("GET /api/v1/access", s.getAccess)
	mux.HandleFunc("PUT /api/v1/access", s.putAccess)
	mux.HandleFunc("GET /api/v1/services", s.listServices)
	mux.HandleFunc("POST /api/v1/services/refresh", s.refreshServices)
	mux.HandleFunc("GET /api/v1/profiles/{name}/services", s.getProfileServices)
	mux.HandleFunc("PUT /api/v1/profiles/{name}/services", s.putProfileServices)
	mux.HandleFunc("GET /api/v1/safesearch", s.listSafesearch)
	mux.HandleFunc("GET /api/v1/profiles/{name}/safesearch", s.getProfileSafesearch)
	mux.HandleFunc("PUT /api/v1/profiles/{name}/safesearch", s.putProfileSafesearch)
	mux.HandleFunc("GET /api/v1/routes", s.listRoutes)
	mux.HandleFunc("POST /api/v1/routes", s.postRoute)
	mux.HandleFunc("PUT /api/v1/routes/{id}", s.putRoute)
	mux.HandleFunc("DELETE /api/v1/routes/{id}", s.deleteRoute)
	mux.HandleFunc("GET /api/v1/stream/queries", s.streamQueries)
	mux.HandleFunc("GET /api/v1/queries", s.listQueries)
	mux.HandleFunc("POST /api/v1/reload", s.reload)
	if s.files != nil {
		mux.Handle("GET /", http.FileServerFS(s.files))
	}
	return mux
}

// refreshSource fetches one source now and republishes the resolver.
func (s *Server) refreshSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.preview.RefreshOne(r.Context(), name); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed", "source": name})
}

// previewSource reports the domains a refresh would add and remove, without
// applying anything.
func (s *Server) previewSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	added, removed, notModified, err := s.preview.PreviewSource(r.Context(), name)
	if err != nil {
		writeError(w, err)
		return
	}
	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name, "added": added, "removed": removed, "notModified": notModified,
	})
}

// reload republishes the resolver from the current inputs: the stored
// configuration, the blocklist files, and the enabled sources. The operator
// calls it after editing a list file outside the API.
func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if err := s.reloader.Reload(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
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

// conflict marks a write the stored records contradict, so it becomes a 409
// rather than a 500.
type conflict struct{ err error }

func (e conflict) Error() string { return e.err.Error() }
func (e conflict) Unwrap() error { return e.err }

// decodeJSON reads one request body into the type its handler expects. A body
// that is not JSON is the client's to fix, so callers answer it as a bad request.
func decodeJSON[T any](r *http.Request) (T, error) {
	var value T
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		return value, fmt.Errorf("invalid JSON: %w", err)
	}
	return value, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var bad badRequest
	var clash conflict
	switch {
	case errors.Is(err, errNotFound):
		status = http.StatusNotFound
	case errors.As(err, &bad):
		status = http.StatusBadRequest
	case errors.As(err, &clash):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
