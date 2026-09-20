package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/filter"
	"aegis/internal/store"
)

// ruleRequest is one rule as it arrives over HTTP. A nil field means keep what
// is stored, which is what makes a one-field body an action toggle. Schedule
// and client are optional: a rule that names a schedule is only active while
// that schedule covers the query minute, and a rule that names a client only
// answers for that identity.
type ruleRequest struct {
	Domain   *string `json:"domain"`
	Kind     *string `json:"kind"`
	Action   *string `json:"action"`
	Schedule *string `json:"schedule"`
	Client   *string `json:"client"`
	Notes    *string `json:"notes"`
}

// ruleResponse is one rule as it leaves over HTTP.
type ruleResponse struct {
	ID       int64  `json:"id"`
	Domain   string `json:"domain"`
	Kind     string `json:"kind"`
	Action   string `json:"action"`
	Schedule string `json:"schedule,omitempty"`
	Client   string `json:"client,omitempty"`
	Notes    string `json:"notes,omitempty"`
	Created  string `json:"created,omitempty"`
}

func ruleResponseFrom(rule store.Rule) ruleResponse {
	response := ruleResponse{
		ID:       rule.ID,
		Domain:   rule.Value(),
		Kind:     rule.Kind.String(),
		Action:   rule.Action.String(),
		Schedule: rule.Schedule,
		Client:   string(rule.Client),
		Notes:    rule.Notes,
	}
	if !rule.Created.IsZero() {
		response.Created = rule.Created.Format(time.RFC3339)
	}
	return response
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
	request, err := decodeJSON[ruleRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	rule, err := parseRuleFields(request)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	rule.Created = time.Now().UTC()
	if request.Notes != nil {
		rule.Notes = *request.Notes
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	if rule.Schedule != "" {
		if err := s.scheduleExists(ctx, rule.Schedule); err != nil {
			writeError(w, badRequest{err})
			return
		}
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

	stored, found, err := s.store.RuleByID(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errNotFound)
		return
	}
	writeJSON(w, http.StatusCreated, ruleResponseFrom(stored))
}

// putRule patches one stored rule. A body that names a field changes that
// field; the rest of the stored rule survives.
func (s *Server) putRule(w http.ResponseWriter, r *http.Request) {
	request, err := decodeJSON[ruleRequest](r)
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

	rule, found, err := s.store.RuleByID(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errNotFound)
		return
	}
	if request.Domain != nil || request.Kind != nil || request.Action != nil || request.Schedule != nil || request.Client != nil {
		// A patch that changes any field must still leave a whole rule, so the
		// missing pieces come from the stored one and the value is reparsed
		// under the kind the rule will carry.
		kind := rule.Kind
		value := rule.Value()
		if request.Kind != nil {
			if kind, err = filter.ParseMatchKind(*request.Kind); err != nil {
				writeError(w, badRequest{err})
				return
			}
		}
		if request.Domain != nil {
			value = *request.Domain
		}
		if err = rule.SetValue(kind, value); err != nil {
			writeError(w, badRequest{err})
			return
		}
		if request.Action != nil {
			if rule.Action, err = filter.ParseAction(*request.Action); err != nil {
				writeError(w, badRequest{err})
				return
			}
		}
	}
	if request.Schedule != nil {
		rule.Schedule = strings.TrimSpace(*request.Schedule)
	}
	if request.Client != nil {
		rule.Client = filter.ClientKey(strings.TrimSpace(*request.Client))
	}
	if rule.Schedule != "" {
		if err := s.scheduleExists(ctx, rule.Schedule); err != nil {
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

	stored, found, err := s.store.RuleByID(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errNotFound)
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

	deleted, err := s.store.DeleteRule(ctx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !deleted {
		writeError(w, errNotFound)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseRuleFields turns a create request into the typed rule the store keeps,
// refusing a rule that is missing a required field.
func parseRuleFields(request ruleRequest) (store.Rule, error) {
	if request.Domain == nil {
		return store.Rule{}, errors.New("a rule needs a domain")
	}
	if request.Kind == nil {
		return store.Rule{}, errors.New("a rule needs a kind")
	}
	if request.Action == nil {
		return store.Rule{}, errors.New("a rule needs an action")
	}
	kind, err := filter.ParseMatchKind(*request.Kind)
	if err != nil {
		return store.Rule{}, err
	}
	action, err := filter.ParseAction(*request.Action)
	if err != nil {
		return store.Rule{}, err
	}
	rule := store.Rule{Kind: kind, Action: action}
	if err := rule.SetValue(kind, *request.Domain); err != nil {
		return store.Rule{}, err
	}
	if request.Schedule != nil {
		rule.Schedule = strings.TrimSpace(*request.Schedule)
	}
	if request.Client != nil {
		rule.Client = filter.ClientKey(strings.TrimSpace(*request.Client))
	}
	return rule, nil
}

// scheduleExists refuses a rule that names a schedule the store does not hold,
// because a rule whose schedule never exists would fail every reload.
func (s *Server) scheduleExists(ctx context.Context, name string) error {
	found, err := s.store.ScheduleExists(ctx, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("schedule %q does not exist", name)
	}
	return nil
}

func parseRuleID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errors.New("a rule id must be a number")
	}
	return id, nil
}
