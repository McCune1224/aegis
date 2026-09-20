package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"aegis/internal/filter"
	"aegis/internal/safesearch"
	"aegis/internal/store"
)

// safesearchEngineResponse is one engine as it leaves over HTTP, with the
// profiles that enforce it.
type safesearchEngineResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	RuleCount int      `json:"rule_count"`
	Profiles  []string `json:"profiles"`
}

type safesearchResponse struct {
	Engines []safesearchEngineResponse `json:"engines"`
}

type profileSafesearchRequest struct {
	Engines []string `json:"engines"`
}

type profileSafesearchResponse struct {
	Profile string   `json:"profile"`
	Engines []string `json:"engines"`
}

func (s *Server) listSafesearch(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	byProfile := make(map[string][]string, len(cfg.Safesearch))
	for _, enable := range cfg.Safesearch {
		byProfile[string(enable.Engine)] = append(byProfile[string(enable.Engine)], string(enable.Profile))
	}

	catalog := safesearch.Catalog()
	response := safesearchResponse{Engines: make([]safesearchEngineResponse, 0, len(catalog))}
	for _, engine := range catalog {
		profiles := byProfile[string(engine.ID)]
		if profiles == nil {
			profiles = []string{}
		}
		sort.Strings(profiles)
		response.Engines = append(response.Engines, safesearchEngineResponse{
			ID:        string(engine.ID),
			Name:      engine.Name,
			RuleCount: len(engine.Rules),
			Profiles:  profiles,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getProfileSafesearch(w http.ResponseWriter, r *http.Request) {
	id := filter.ProfileID(r.PathValue("name"))
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if !profileExists(cfg, id) {
		writeError(w, errNotFound)
		return
	}

	set := make([]string, 0)
	for _, enable := range cfg.Safesearch {
		if enable.Profile == id {
			set = append(set, string(enable.Engine))
		}
	}
	writeJSON(w, http.StatusOK, profileSafesearchResponse{Profile: string(id), Engines: set})
}

// putProfileSafesearch replaces the whole set of engines one profile enforces.
// The body is the answer, not a patch, so an engine left out stops rewriting
// even though the catalog still holds it.
func (s *Server) putProfileSafesearch(w http.ResponseWriter, r *http.Request) {
	id := filter.ProfileID(r.PathValue("name"))
	request, err := decodeJSON[profileSafesearchRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	set, err := normalizeIDSet(request.Engines, "engine")
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	err = s.apply(r.Context(),
		func(cfg *store.Config) error {
			if !profileExists(*cfg, id) {
				return errNotFound
			}
			known := make(map[string]bool)
			for _, engine := range safesearch.Catalog() {
				known[string(engine.ID)] = true
			}
			for _, engine := range set {
				if !known[engine] {
					return badRequest{fmt.Errorf("engine %q is not in the catalog", engine)}
				}
			}

			enables := make([]store.ProfileSafesearch, 0, len(cfg.Safesearch)+len(set))
			for _, enable := range cfg.Safesearch {
				if enable.Profile != id {
					enables = append(enables, enable)
				}
			}
			for _, engine := range set {
				enables = append(enables, store.ProfileSafesearch{Profile: id, Engine: safesearch.EngineID(engine)})
			}
			cfg.Safesearch = enables
			return nil
		},
		func(ctx context.Context) error {
			engines := make([]safesearch.EngineID, 0, len(set))
			for _, engine := range set {
				engines = append(engines, safesearch.EngineID(engine))
			}
			return s.store.SetProfileSafesearch(ctx, id, engines)
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profileSafesearchResponse{Profile: string(id), Engines: set})
}
