package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Column widths. The name column is what is left over.
const (
	clockWidth   = 8
	verdictWidth = 7
	clientWidth  = 16
	// minName is the narrowest name column worth a column of its own. Below it
	// the row keeps the name and drops the columns left of it.
	minName = 16
	// minWidth is the narrowest terminal the screens render into.
	minWidth = 24
	// headerWidth is the width at which the full column heading fits.
	headerWidth = clockWidth + 2 + verdictWidth + 2 + clientWidth + 2 + len("NAME")
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	blockStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// list is the window of rows both screens render, newest first, with the totals
// each screen reports. The newest end is the end the operator reads.
type list struct {
	rows    []Query
	limit   int
	total   int
	blocked int
}

// add records one decision. The window keeps its newest limit rows; the totals
// count every decision the screen has seen.
func (l *list) add(query Query) {
	l.total++
	if query.Blocked() {
		l.blocked++
	}
	l.rows = append([]Query{query}, l.rows...)
	if l.limit > 0 && len(l.rows) > l.limit {
		l.rows = l.rows[:l.limit]
	}
}

// replace swaps the window for a freshly read one, which is newest first the
// way the API serves the log.
func (l *list) replace(rows []Query) {
	l.rows = rows
	l.total = len(rows)
	l.blocked = 0
	for _, query := range rows {
		if query.Blocked() {
			l.blocked++
		}
	}
}

// summary is the title's tail: how much is on screen and how much of it was
// blocked.
func (l *list) summary(unit string) string {
	return fmt.Sprintf("%d %s · %d blocked", l.total, unit, l.blocked)
}

// frame renders a screen: the title, the column headings, and the rows newest
// first, clipped to the height the terminal gave us.
func frame(title string, rows []Query, width, height int) string {
	if width < minWidth {
		width = minWidth
	}
	if height < 1 {
		height = 1
	}

	lines := []string{titleStyle.Render(title)}
	if height > 1 {
		lines = append(lines, headerRow(width))
	}
	for _, query := range rows {
		if len(lines) >= height {
			break
		}
		line := queryRow(query, width)
		if query.Blocked() {
			line = blockStyle.Render(line)
		}
		lines = append(lines, line)
	}
	if len(rows) == 0 && len(lines) < height {
		lines = append(lines, "waiting for queries")
	}
	return strings.Join(lines, "\n")
}

// headerRow names the columns the width has room for.
func headerRow(width int) string {
	if width >= headerWidth {
		return pad("TIME", clockWidth) + "  " + pad("VERDICT", verdictWidth) + "  " + pad("CLIENT", clientWidth) + "  NAME"
	}
	if budget := width - clockWidth - 2 - verdictWidth - 2; budget >= minName {
		return pad("TIME", clockWidth) + "  " + pad("VERDICT", verdictWidth) + "  NAME"
	}
	return pad("TIME", clockWidth) + "  NAME"
}

// queryRow renders one decision. The name survives longest: the client column
// goes first and then the verdict, because a row without its name says nothing
// about what was asked.
func queryRow(query Query, width int) string {
	// The decoders put the query in the daemon's clock, so the row shows the
	// same wall time the operator's query log does.
	clock := query.Time.Format("15:04:05")

	if budget := width - clockWidth - 2 - verdictWidth - 2 - clientWidth - 2; budget >= minName {
		return clock + "  " + pad(query.Verdict, verdictWidth) + "  " + pad(query.Client, clientWidth) + "  " + nameAndRule(query, budget)
	}
	if budget := width - clockWidth - 2 - verdictWidth - 2; budget >= minName {
		return clock + "  " + pad(query.Verdict, verdictWidth) + "  " + nameAndRule(query, budget)
	}
	return clock + "  " + clip(query.Name, width-clockWidth-2)
}

// nameAndRule pads the name so the rule, when there is room for it, ends at the
// line's right edge. The name comes first: if it does not fit alongside the
// rule, the rule is the thing that goes.
func nameAndRule(query Query, budget int) string {
	suffix := ""
	if query.Rule != "" {
		suffix = "  " + query.Rule
	}
	if room := budget - lipgloss.Width(suffix); room >= minName && lipgloss.Width(query.Name) <= room {
		return pad(query.Name, room) + suffix
	}
	return clip(query.Name, budget)
}

// pad widens s to n columns with spaces.
func pad(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if width := lipgloss.Width(s); width < n {
		return s + strings.Repeat(" ", n-width)
	}
	return clip(s, n)
}

// clip shortens s to at most n columns, which the ellipsis counts against.
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}
