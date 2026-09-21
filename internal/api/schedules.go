package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"aegis/internal/filter"
)

// scheduleWindow is one recurring range as it arrives over HTTP. Days are
// time.Weekday numbers, Sunday first, and the times read as HH:MM.
type scheduleWindow struct {
	Days  []time.Weekday `json:"days"`
	Start string         `json:"start"`
	End   string         `json:"end"`
}

// scheduleRequest is one schedule as it arrives over HTTP.
type scheduleRequest struct {
	Priority *int              `json:"priority"`
	Windows  *[]scheduleWindow `json:"windows"`
}

// scheduleResponse is one schedule as it leaves over HTTP.
type scheduleResponse struct {
	Name     string           `json:"name"`
	Priority int              `json:"priority"`
	Windows  []scheduleWindow `json:"windows"`
}

func scheduleResponseFrom(schedule filter.ScheduleSpec) scheduleResponse {
	windows := make([]scheduleWindow, 0, len(schedule.Windows))
	for _, window := range schedule.Windows {
		windows = append(windows, scheduleWindow{
			Days:  window.Days,
			Start: filter.MinuteOfDay(window.Start),
			End:   filter.MinuteOfDay(window.End),
		})
	}
	return scheduleResponse{Name: schedule.Name, Priority: schedule.Priority, Windows: windows}
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.store.Schedules(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]scheduleResponse, 0, len(schedules))
	for _, schedule := range schedules {
		response = append(response, scheduleResponseFrom(schedule))
	}
	writeJSON(w, http.StatusOK, response)
}

// putSchedule creates one schedule or replaces what its name held, so the
// windows of a running server change on the next reload.
func (s *Server) putSchedule(w http.ResponseWriter, r *http.Request) {
	request, err := decodeJSON[scheduleRequest](r)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeError(w, badRequest{errors.New("a schedule needs a name")})
		return
	}
	if request.Priority == nil {
		writeError(w, badRequest{errors.New("a schedule needs a priority")})
		return
	}
	if request.Windows == nil {
		writeError(w, badRequest{errors.New("a schedule needs windows")})
		return
	}

	schedule, err := parseSchedule(name, *request.Priority, *request.Windows)
	if err != nil {
		writeError(w, badRequest{err})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.SaveSchedule(r.Context(), schedule); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(r.Context()); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, scheduleResponseFrom(schedule))
}

// parseSchedule turns a create request into the typed schedule the store
// keeps, refusing one whose windows the engine would reject.
func parseSchedule(name string, priority int, windows []scheduleWindow) (filter.ScheduleSpec, error) {
	spec := filter.ScheduleSpec{Name: name, Priority: priority}
	for _, window := range windows {
		start, err := filter.ParseMinuteOfDay(window.Start)
		if err != nil {
			return filter.ScheduleSpec{}, err
		}
		end, err := filter.ParseMinuteOfDay(window.End)
		if err != nil {
			return filter.ScheduleSpec{}, err
		}
		for _, day := range window.Days {
			if day < time.Sunday || day > time.Saturday {
				return filter.ScheduleSpec{}, fmt.Errorf("filter: a window names a day %d the week does not have", day)
			}
		}
		spec.Windows = append(spec.Windows, filter.Window{Days: window.Days, Start: start, End: end})
	}
	if err := filter.ValidateSchedule(spec); err != nil {
		return filter.ScheduleSpec{}, err
	}
	return spec, nil
}

// deleteSchedule removes one schedule. Rules and focus windows still naming it
// would fail every later reload, so the deletion is refused with the names
// instead.
func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()

	rules, err := s.store.Rules(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	var users []string
	for _, rule := range rules {
		if rule.Schedule == name {
			users = append(users, fmt.Sprintf("rule %d", rule.ID))
		}
	}
	windows, err := s.store.FocusWindows(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	for _, window := range windows {
		if window.Schedule == name {
			users = append(users, fmt.Sprintf("focus window %q", window.Name))
		}
	}
	if len(users) > 0 {
		writeError(w, badRequest{fmt.Errorf("schedule %s is still named by %v", name, users)})
		return
	}

	if err := s.store.DeleteSchedule(ctx, name); err != nil {
		writeError(w, err)
		return
	}
	if err := s.reloader.Reload(ctx); err != nil {
		writeError(w, fmt.Errorf("api: reload: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
