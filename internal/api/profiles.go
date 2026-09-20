package api

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// profileRequest is one profile as it arrives over HTTP. A nil field means
// inherit, which is why absent and empty are different.
type profileRequest struct {
	Extends *string `json:"extends"`
	Mode    *string `json:"mode"`
	Custom  *string `json:"custom"`
}

// profileResponse is one profile as it leaves over HTTP.
type profileResponse struct {
	Name    string  `json:"name"`
	Extends string  `json:"extends,omitempty"`
	Mode    *string `json:"mode,omitempty"`
	Custom  *string `json:"custom,omitempty"`
}

func (r profileRequest) spec(id filter.ProfileID) (filter.ProfileSpec, error) {
	spec := filter.ProfileSpec{ID: id}
	if r.Extends != nil {
		spec.Extends = filter.ProfileID(*r.Extends)
	}
	if r.Mode != nil {
		mode, err := filter.ParseBlockingMode(*r.Mode)
		if err != nil {
			return filter.ProfileSpec{}, fmt.Errorf("mode: %w", err)
		}
		spec.Mode = &mode
	}
	if r.Custom != nil {
		address, err := netip.ParseAddr(*r.Custom)
		if err != nil {
			return filter.ProfileSpec{}, fmt.Errorf("custom: %w", err)
		}
		spec.Custom = &address
	}
	return spec, nil
}

func profileResponseFrom(spec filter.ProfileSpec) profileResponse {
	response := profileResponse{Name: string(spec.ID), Extends: string(spec.Extends)}
	if spec.Mode != nil {
		mode := spec.Mode.String()
		response.Mode = &mode
	}
	if spec.Custom != nil {
		custom := spec.Custom.String()
		response.Custom = &custom
	}
	return response
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	profiles := make([]profileResponse, 0, len(cfg.Profiles))
	for _, spec := range cfg.Profiles {
		profiles = append(profiles, profileResponseFrom(spec))
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	id := filter.ProfileID(r.PathValue("name"))
	for _, spec := range cfg.Profiles {
		if spec.ID == id {
			writeJSON(w, http.StatusOK, profileResponseFrom(spec))
			return
		}
	}
	writeError(w, errNotFound)
}

func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	id := filter.ProfileID(r.PathValue("name"))
	request, err := decodeJSON[profileRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	spec, err := request.spec(id)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	err = s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.Profiles {
				if cfg.Profiles[i].ID == id {
					cfg.Profiles[i] = spec
					return nil
				}
			}
			cfg.Profiles = append(cfg.Profiles, spec)
			return nil
		},
		func(ctx context.Context) error { return s.store.SaveProfile(ctx, spec) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profileResponseFrom(spec))
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id := filter.ProfileID(r.PathValue("name"))
	err := s.apply(r.Context(),
		func(cfg *store.Config) error {
			for i := range cfg.Profiles {
				if cfg.Profiles[i].ID == id {
					cfg.Profiles = append(cfg.Profiles[:i], cfg.Profiles[i+1:]...)
					return nil
				}
			}
			return errNotFound
		},
		func(ctx context.Context) error { return s.store.DeleteProfile(ctx, id) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
