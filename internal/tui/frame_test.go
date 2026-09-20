package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ansi matches the escapes lipgloss writes, which say nothing about the text a
// row contains.
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// at turns a wall clock into a time on the day the fixtures live on.
func at(clock string) time.Time {
	parsed, err := time.Parse("15:04:05", clock)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 9, 20, parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.UTC)
}

// rows is newest first, the order the frame takes.
func rows() []Query {
	return []Query{
		{Time: at("15:04:06"), Client: "10.0.0.6", Name: "second.example.com", Type: "AAAA", Verdict: "block", Rule: "ads"},
		{Time: at("15:04:05"), Client: "10.0.0.5", Name: "first.example.com", Type: "A", Verdict: "allow"},
	}
}

func TestFramePutsTheNewestRowFirst(t *testing.T) {
	view := frame("aegis top", rows(), 100, 24)

	newest := strings.Index(view, "second.example.com")
	oldest := strings.Index(view, "first.example.com")
	require.NotEqual(t, -1, newest)
	require.NotEqual(t, -1, oldest)
	require.Less(t, newest, oldest, "the newest query belongs at the top")
	require.Contains(t, view, "15:04:05")
	require.Contains(t, view, "10.0.0.5")
	require.Contains(t, view, "ads")
}

func TestFrameCarriesTheHeaderAsTheFirstLine(t *testing.T) {
	view := frame("aegis top · 2 queries · 1 blocked", rows(), 100, 24)
	require.Equal(t, "aegis top · 2 queries · 1 blocked", plain(strings.Split(view, "\n")[0]))
}

func TestFrameStopsAtTheHeightItIsGiven(t *testing.T) {
	// Newest first, which is the order the frame is documented to take.
	many := make([]Query, 0, 50)
	for i := 49; i >= 0; i-- {
		many = append(many, Query{
			Time:    at(fmt.Sprintf("15:04:%02d", i%60)),
			Client:  "10.0.0.5",
			Name:    fmt.Sprintf("host%02d.example.com", i),
			Verdict: "allow",
		})
	}

	view := frame("aegis top", many, 100, 5)
	require.Len(t, strings.Split(view, "\n"), 5, "two header lines and three rows fit")
	require.Contains(t, view, "host49.example.com", "the newest rows are the ones that survive")
	require.NotContains(t, plain(view), "host00.example.com")
}

func TestFrameSaysItIsWaitingBeforeAnyQueryArrives(t *testing.T) {
	view := frame("aegis query tail", nil, 100, 24)
	require.Contains(t, view, "waiting for queries")
}

func TestFrameKeepsTheNameWhenTheTerminalIsNarrow(t *testing.T) {
	view := frame("aegis top", rows(), 40, 24)
	require.Contains(t, view, "second.example.com", "the name survives the narrowest column drop")
	require.NotContains(t, view, "10.0.0.6", "the client column is the first one to go")
}

func TestFrameCutsANameThatWouldBreakTheLine(t *testing.T) {
	long := []Query{{Time: at("15:04:05"), Client: "10.0.0.5", Name: strings.Repeat("x", 200), Verdict: "allow"}}
	view := frame("aegis top", long, 60, 24)
	for _, line := range strings.Split(plain(view), "\n") {
		require.LessOrEqual(t, len([]rune(line)), 60, "a row never exceeds the width: %q", line)
	}
}

func TestListKeepsTheNewestRowsAndCountsEveryOne(t *testing.T) {
	var window list
	window.limit = 2
	for i := range 5 {
		window.add(Query{Name: fmt.Sprintf("host%d.example.com", i), Verdict: "block"})
	}

	require.Len(t, window.rows, 2)
	require.Equal(t, "host4.example.com", window.rows[0].Name)
	require.Equal(t, "host3.example.com", window.rows[1].Name)
	require.Equal(t, 5, window.total)
	require.Equal(t, 5, window.blocked)
}
