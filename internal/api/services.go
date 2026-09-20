package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// serviceResponse is one catalog entry as it leaves over HTTP, with the
// profiles that block it.
type serviceResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Group     string   `json:"group"`
	RuleCount int      `json:"rule_count"`
	Profiles  []string `json:"profiles"`
}

type servicesResponse struct {
	Services  []serviceResponse `json:"services"`
	Groups    []string          `json:"groups"`
	FetchedAt string            `json:"fetched_at,omitempty"`
}

type profileServicesRequest struct {
	Services []string `json:"services"`
}

type profileServicesResponse struct {
	Profile  string   `json:"profile"`
	Services []string `json:"services"`
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	byProfile := make(map[string][]string, len(cfg.ProfileServices))
	for _, enable := range cfg.ProfileServices {
		byProfile[enable.Service] = append(byProfile[enable.Service], string(enable.Profile))
	}

	response := servicesResponse{Services: make([]serviceResponse, 0, len(cfg.Services)), Groups: []string{}}
	groups := make(map[string]bool, len(cfg.Services))
	var newest time.Time
	for _, row := range cfg.Services {
		profiles := byProfile[row.ID]
		if profiles == nil {
			profiles = []string{}
		}
		sort.Strings(profiles)
		response.Services = append(response.Services, serviceResponse{
			ID:        row.ID,
			Name:      row.Name,
			Group:     row.Group,
			RuleCount: len(row.Rules),
			Profiles:  profiles,
		})
		if row.Group != "" {
			groups[row.Group] = true
		}
		if row.FetchedAt.After(newest) {
			newest = row.FetchedAt
		}
	}
	for group := range groups {
		response.Groups = append(response.Groups, group)
	}
	sort.Strings(response.Groups)
	if !newest.IsZero() {
		response.FetchedAt = newest.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getProfileServices(w http.ResponseWriter, r *http.Request) {
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
	for _, enable := range cfg.ProfileServices {
		if enable.Profile == id {
			set = append(set, enable.Service)
		}
	}
	writeJSON(w, http.StatusOK, profileServicesResponse{Profile: string(id), Services: set})
}

// putProfileServices replaces the whole set of services a profile blocks. The
// body is the answer, not a patch, so a service left out is unblocked even
// when the catalog has moved on since the operator last looked.
func (s *Server) putProfileServices(w http.ResponseWriter, r *http.Request) {
	id := filter.ProfileID(r.PathValue("name"))
	var request profileServicesRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, badRequest{fmt.Errorf("invalid JSON: %w", err)})
		return
	}

	set, err := normalizeServiceSet(request.Services)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	err = s.apply(r.Context(),
		func(cfg *store.Config) error {
			if !profileExists(*cfg, id) {
				return errNotFound
			}
			known := make(map[string]bool, len(cfg.Services))
			for _, row := range cfg.Services {
				known[row.ID] = true
			}
			for _, service := range set {
				if !known[service] {
					return badRequest{fmt.Errorf("service %q is not in the catalog", service)}
				}
			}

			enables := make([]store.ProfileService, 0, len(cfg.ProfileServices)+len(set))
			for _, enable := range cfg.ProfileServices {
				if enable.Profile != id {
					enables = append(enables, enable)
				}
			}
			for _, service := range set {
				enables = append(enables, store.ProfileService{Profile: id, Service: service})
			}
			cfg.ProfileServices = enables
			return nil
		},
		func(ctx context.Context) error { return s.store.SetProfileServices(ctx, id, set) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profileServicesResponse{Profile: string(id), Services: set})
}

// refreshServices fetches the catalog now and republishes, so an operator sees
// a service the catalog gained without waiting for the background refresh.
func (s *Server) refreshServices(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, badRequest{fmt.Errorf("the services catalog is not wired")})
		return
	}
	if err := s.catalog.RefreshCatalog(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

// normalizeServiceSet trims, dedupes, and sorts the requested set, so the
// stored set has one spelling and the rules it compiles to keep one order.
func normalizeServiceSet(raw []string) ([]string, error) {
	seen := make(map[string]bool, len(raw))
	set := make([]string, 0, len(raw))
	for _, candidate := range raw {
		service := strings.TrimSpace(candidate)
		if service == "" {
			return nil, fmt.Errorf("a service id cannot be empty")
		}
		if seen[service] {
			continue
		}
		seen[service] = true
		set = append(set, service)
	}
	sort.Strings(set)
	return set, nil
}

func profileExists(cfg store.Config, id filter.ProfileID) bool {
	for _, spec := range cfg.Profiles {
		if spec.ID == id {
			return true
		}
	}
	return false
}
