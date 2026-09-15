package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"aegis/internal/filter"
	"aegis/internal/store"
)

type defaultProfileRequest struct {
	Profile string `json:"profile"`
}

func (s *Server) getDefaultProfile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"profile": string(cfg.Default)})
}

func (s *Server) putDefaultProfile(w http.ResponseWriter, r *http.Request) {
	var request defaultProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, badRequest{fmt.Errorf("invalid JSON: %w", err)})
		return
	}
	id := filter.ProfileID(request.Profile)

	err := s.apply(r.Context(),
		func(cfg *store.Config) error {
			for _, profile := range cfg.Profiles {
				if profile.ID == id {
					cfg.Default = id
					return nil
				}
			}
			return badRequest{fmt.Errorf("profile %q is not defined", request.Profile)}
		},
		func(ctx context.Context) error { return s.store.SetDefaultProfile(ctx, id) },
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"profile": string(id)})
}
