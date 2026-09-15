package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// ruleRequest is one rule as it arrives over HTTP. A nil field means keep what
// is stored, which is what makes a one-field body an action toggle.
type ruleRequest struct {
	Domain *string `json:"domain"`
	Kind   *string `json:"kind"`
	Action *string `json:"action"`
	Notes  *string `json:"notes"`
}

// ruleResponse is one rule as it leaves over HTTP.
type ruleResponse struct {
	ID      int64  `json:"id"`
	Domain  string `json:"domain"`
	Kind    string `json:"kind"`
	Action  string `json:"action"`
	Notes   string `json:"notes,omitempty"`
	Created string `json:"created,omitempty"`
}

func ruleResponseFrom(rule store.Rule) ruleResponse {
	response := ruleResponse{
		ID:     rule.ID,
		Domain: rule.Domain.String(),
		Kind:   rule.Kind.String(),
		Action: rule.Action.String(),
		Notes:  rule.Notes,
	}
	if !rule.Created.IsZero() {
		response.Created = rule.Created.Format(time.RFC3339)
	}
	return response
}

func decodeRule(r *http.Request) (ruleRequest, error) {
	var request ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return ruleRequest{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return request, nil
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.store.Rules(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]ruleResponse, 0, len(rules))
	for _, rule := range rules {
		response = append(response, ruleResponseFrom(rule))
	}
	writeJSON(w, http.StatusOK, response)
}

// postRule creates one rule and reloads, so the next query already answers from
// it. Every field a rule needs is required here, because a new rule with a
// missing field has nothing to inherit.
func (s *Server) postRule(w http.ResponseWriter, r *http.Request) {
	request, err := decodeRule(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	domain, kind, action, err := parseRuleFields(request)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rule := store.Rule{Domain: domain, Kind: kind, Action: action, Created: time.Now().UTC()}
	if request.Notes != nil {
		rule.Notes = *request.Notes
	}
	id, err := s.store.SaveRule(ctx, rule)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}

	stored, err := s.lookupRule(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ruleResponseFrom(stored))
}

// putRule patches one stored rule. A body that names a field changes that
// field; the rest of the stored rule survives.
func (s *Server) putRule(w http.ResponseWriter, r *http.Request) {
	request, err := decodeRule(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	id, err := parseRuleID(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rule, err := s.lookupRule(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if request.Domain != nil || request.Kind != nil || request.Action != nil {
		// A patch that changes any field must still leave a whole rule, so the
		// missing pieces come from the stored one before parsing.
		if request.Domain != nil {
			rule.Domain, err = filter.ParseDomain(*request.Domain)
		}
		if err == nil && request.Kind != nil {
			rule.Kind, err = filter.ParseMatchKind(*request.Kind)
		}
		if err == nil && request.Action != nil {
			rule.Action, err = filter.ParseAction(*request.Action)
		}
		if err != nil {
			writeError(w, badRequest{err})
			return
		}
	}
	if request.Notes != nil {
		rule.Notes = *request.Notes
	}

	if _, err := s.store.SaveRule(ctx, rule); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}

	stored, err := s.lookupRule(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleResponseFrom(stored))
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseRuleID(r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	if _, err := s.lookupRule(ctx, id); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.DeleteRule(ctx, id); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseRuleFields turns a create request into the typed values the store
// stores, refusing a rule that is missing a required field.
func parseRuleFields(request ruleRequest) (filter.Domain, filter.MatchKind, filter.Action, error) {
	if request.Domain == nil {
		return filter.Domain{}, 0, 0, errors.New("a rule needs a domain")
	}
	if request.Kind == nil {
		return filter.Domain{}, 0, 0, errors.New("a rule needs a kind")
	}
	if request.Action == nil {
		return filter.Domain{}, 0, 0, errors.New("a rule needs an action")
	}
	domain, err := filter.ParseDomain(*request.Domain)
	if err != nil {
		return filter.Domain{}, 0, 0, err
	}
	kind, err := filter.ParseMatchKind(*request.Kind)
	if err != nil {
		return filter.Domain{}, 0, 0, err
	}
	action, err := filter.ParseAction(*request.Action)
	if err != nil {
		return filter.Domain{}, 0, 0, err
	}
	return domain, kind, action, nil
}

func parseRuleID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errors.New("a rule id must be a number")
	}
	return id, nil
}

func (s *Server) lookupRule(ctx context.Context, id int64) (store.Rule, error) {
	rules, err := s.store.Rules(ctx)
	if err != nil {
		return store.Rule{}, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return store.Rule{}, errNotFound
}
