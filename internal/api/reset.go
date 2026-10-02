package api

import (
	"fmt"
	"net/http"
)

// resetConfirmation is the word a wipe body has to carry, so a stray POST
// cannot empty an operator's database.
const resetConfirmation = "reset"

// resetRequest is the confirmation the wipe arrives with. The body is the
// answer: a set the client states rather than a patch it applies.
type resetRequest struct {
	Confirm string `json:"confirm"`
}

// resetResponse reports one wipe as the rows each named area gave up, so a
// caller learns what moved instead of inferring it from empty lists.
type resetResponse struct {
	Status      string `json:"status"`
	Clients     int    `json:"clients"`
	Discoveries int    `json:"discoveries"`
	Queries     int    `json:"queries"`
	Services    int    `json:"services"`
	Settings    int    `json:"settings"`
}

// postReset wipes the areas a full reset names and republishes the resolver,
// so the live filter drops the enablements the wipe removed in the same call
// that removes them. It runs under the mutation lock, because a wipe is the
// largest configuration change there is.
func (s *Server) postReset(w http.ResponseWriter, r *http.Request) {
	request, err := decodeJSON[resetRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	if request.Confirm != resetConfirmation {
		writeError(w, badRequest{fmt.Errorf("reset: confirmation %q is required, send {\"confirm\":\"%s\"}",
			resetConfirmation, resetConfirmation)})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	wipe, err := s.store.Reset(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(r.Context()); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, resetResponse{
		Status:      "reset",
		Clients:     wipe.Clients,
		Discoveries: wipe.Discoveries,
		Queries:     wipe.Queries,
		Services:    wipe.Services,
		Settings:    wipe.Settings,
	})
}
