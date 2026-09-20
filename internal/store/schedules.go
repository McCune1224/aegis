package store

import (
	"context"
	"encoding/json"
	"fmt"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// SaveSchedule inserts one named set of windows, replacing what the name held.
func (s *Store) SaveSchedule(ctx context.Context, schedule filter.ScheduleSpec) error {
	windows, err := json.Marshal(schedule.Windows)
	if err != nil {
		return fmt.Errorf("store: schedule %s: %w", schedule.Name, err)
	}
	if _, err := s.queries.SaveSchedule(ctx, storedb.SaveScheduleParams{
		Name:     schedule.Name,
		Priority: int64(schedule.Priority),
		Windows:  string(windows),
	}); err != nil {
		return fmt.Errorf("store: save schedule %s: %w", schedule.Name, err)
	}
	return nil
}

// Schedules returns every named schedule, ordered by name.
func (s *Store) Schedules(ctx context.Context) ([]filter.ScheduleSpec, error) {
	rows, err := s.queries.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: schedules: %w", err)
	}
	schedules := make([]filter.ScheduleSpec, 0, len(rows))
	for _, row := range rows {
		var windows []filter.Window
		if err := json.Unmarshal([]byte(row.Windows), &windows); err != nil {
			return nil, fmt.Errorf("store: schedule %s: %w", row.Name, err)
		}
		schedules = append(schedules, filter.ScheduleSpec{
			Name:     row.Name,
			Priority: int(row.Priority),
			Windows:  windows,
		})
	}
	return schedules, nil
}

// ScheduleExists reports whether the store holds one named schedule.
func (s *Store) ScheduleExists(ctx context.Context, name string) (bool, error) {
	found, err := s.queries.ScheduleExists(ctx, name)
	if err != nil {
		return false, fmt.Errorf("store: schedule %s: %w", name, err)
	}
	return found != 0, nil
}

// DeleteSchedule removes one named schedule. Rules that name it fail the next
// reload, which is the loud outcome; the rules API refuses the deletion of a
// schedule rules still name.
func (s *Store) DeleteSchedule(ctx context.Context, name string) error {
	if err := s.queries.DeleteSchedule(ctx, name); err != nil {
		return fmt.Errorf("store: delete schedule %s: %w", name, err)
	}
	return nil
}
