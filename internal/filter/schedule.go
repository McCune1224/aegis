package filter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minutesPerDay  = 24 * 60
	minutesPerWeek = 7 * minutesPerDay
)

// Window is one recurring range of the week, in local wall-clock minutes of
// the day. End may be less than Start: the window then runs to midnight on
// each named day and resumes after midnight on the following day, so a
// 21:00-to-07:00 window means what the operator meant.
type Window struct {
	Days  []time.Weekday
	Start int
	End   int
}

// ScheduleSpec is a named set of recurring windows with a priority. When two
// schedules cover the same minute the higher priority wins, and a tie is a
// compile error, because an ambiguous minute would pick a winner the operator
// cannot debug.
type ScheduleSpec struct {
	Name     string
	Priority int
	Windows  []Window
}

// ParseMinuteOfDay turns "21:00" into the minute-of-day form a Window stores.
func ParseMinuteOfDay(raw string) (int, error) {
	hour, minute, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok {
		return 0, fmt.Errorf("filter: %q is not a time of day", raw)
	}
	h, err := strconv.Atoi(hour)
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("filter: %q is not a time of day", raw)
	}
	m, err := strconv.Atoi(minute)
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("filter: %q is not a time of day", raw)
	}
	return h*60 + m, nil
}

// compiledSchedule holds the rules one schedule activates, and scheduledRule
// is one of them: a candidate with its comparison baked in, the client it is
// scoped to, and the matcher compile built for it, so answering never
// branches on the kind.
type compiledSchedule struct {
	name  string
	rules []scheduledRule
}

type scheduledRule struct {
	candidate candidate
	client    ClientKey
	match     func(name string) bool
}

// compileSchedules validates every schedule and folds recurrence and overlap
// into one table: the winner for every minute of the week, keyed by wall
// clock. Daylight saving needs no special case, because the table is indexed
// by what the clock reads, so a minute that repeats answers the same way both
// times and a minute that never comes is simply never read.
func compileSchedules(specs []ScheduleSpec) ([]compiledSchedule, [minutesPerWeek]uint16, error) {
	byName := make(map[string]int, len(specs))
	compiled := make([]compiledSchedule, 0, len(specs))

	var winner [minutesPerWeek]uint16
	var bestPriority [minutesPerWeek]int

	for i, spec := range specs {
		if spec.Name == "" {
			return nil, winner, fmt.Errorf("filter: a schedule has no name")
		}
		if _, exists := byName[spec.Name]; exists {
			return nil, winner, fmt.Errorf("filter: schedule %q is defined twice", spec.Name)
		}
		if len(spec.Windows) == 0 {
			return nil, winner, fmt.Errorf("filter: schedule %q has no window", spec.Name)
		}
		for _, window := range spec.Windows {
			if err := validateWindow(window); err != nil {
				return nil, winner, fmt.Errorf("filter: schedule %q: %w", spec.Name, err)
			}
		}
		byName[spec.Name] = i
		compiled = append(compiled, compiledSchedule{name: spec.Name})

		// Fill the minutes this schedule claims. A higher priority takes a
		// minute away from an earlier schedule, and an equal priority from a
		// different schedule is refused instead of picked.
		for _, window := range spec.Windows {
			for _, day := range window.Days {
				for _, span := range windowSpans(window, day) {
					for minute := span.start; minute < span.end; minute++ {
						switch {
						case winner[minute] == 0 || bestPriority[minute] < spec.Priority:
							winner[minute] = uint16(i + 1)
							bestPriority[minute] = spec.Priority
						case bestPriority[minute] == spec.Priority && winner[minute] != uint16(i+1):
							return nil, winner, fmt.Errorf(
								"filter: schedules %q and %q both claim minute %d with priority %d",
								compiled[winner[minute]-1].name, spec.Name, minute, spec.Priority)
						}
					}
				}
			}
		}
	}
	return compiled, winner, nil
}

func validateWindow(window Window) error {
	if len(window.Days) == 0 {
		return fmt.Errorf("a window names no day")
	}
	for _, day := range window.Days {
		if day < time.Sunday || day > time.Saturday {
			return fmt.Errorf("a window names a day %d the week does not have", day)
		}
	}
	if window.Start < 0 || window.Start >= minutesPerDay || window.End < 0 || window.End >= minutesPerDay {
		return fmt.Errorf("a window names a minute outside the day")
	}
	if window.Start == window.End {
		return fmt.Errorf("a window from %d to %d covers no minute", window.Start, window.End)
	}
	return nil
}

type minuteSpan struct {
	start int
	end   int
}

// windowSpans lists the minute ranges one window covers on one day, as
// offsets within the week. A window that crosses midnight spends its first
// part on the named day and its second part on the following one.
func windowSpans(window Window, day time.Weekday) []minuteSpan {
	base := int(day) * minutesPerDay
	if window.Start < window.End {
		return []minuteSpan{{start: base + window.Start, end: base + window.End}}
	}
	next := (int(day) + 1) % 7
	return []minuteSpan{
		{start: base + window.Start, end: base + minutesPerDay},
		{start: next * minutesPerDay, end: next*minutesPerDay + window.End},
	}
}

func minuteOfWeek(now time.Time) int {
	return (int(now.Weekday())*24+now.Hour())*60 + now.Minute()
}
