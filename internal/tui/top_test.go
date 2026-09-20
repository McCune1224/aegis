package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	tea "charm.land/bubbletea/v2"
)

// press is the message bubbletea delivers for one keystroke.
func press(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text}
}

func resized(width int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: width, Height: 24}
}

func send(t *testing.T, model tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	next, _ := model.Update(msg)
	require.NotNil(t, next)
	return next
}

func TestTopRendersTheLiveStreamNewestFirst(t *testing.T) {
	top := NewTop(nil)
	top.Update(resized(100))

	top = send(t, top, Query{Time: at("15:04:05"), Client: "10.0.0.5", Name: "first.example.com", Verdict: "allow"}).(*Top)
	top = send(t, top, Query{Time: at("15:04:06"), Client: "10.0.0.6", Name: "second.example.com", Verdict: "block", Rule: "ads"}).(*Top)

	view := top.View().Content
	require.Contains(t, view, "aegis top · 2 queries · 1 blocked")
	require.Less(t, indexOf(view, "second.example.com"), indexOf(view, "first.example.com"))
}

func TestTopRelaysOutWhenTheTerminalResizes(t *testing.T) {
	top := NewTop(nil)
	top.Update(resized(100))
	top = send(t, top, Query{Time: at("15:04:06"), Client: "10.0.0.6", Name: "second.example.com", Verdict: "allow"}).(*Top)
	require.Contains(t, top.View().Content, "10.0.0.6")

	top = send(t, top, resized(40)).(*Top)
	require.Contains(t, top.View().Content, "second.example.com")
	require.NotContains(t, top.View().Content, "10.0.0.6", "the client column is what makes way")
}

func TestTopStopsTheScreenWhileItIsPaused(t *testing.T) {
	top := NewTop(nil)
	top.Update(resized(100))
	top = send(t, top, Query{Name: "before.example.com", Verdict: "allow"}).(*Top)

	top = send(t, top, press(tea.KeySpace, " ")).(*Top)
	require.Contains(t, top.View().Content, "paused")

	top = send(t, top, Query{Name: "while-paused.example.com", Verdict: "block"}).(*Top)
	view := top.View().Content
	require.Contains(t, view, "before.example.com")
	require.NotContains(t, view, "while-paused.example.com")
	require.Contains(t, view, "1 skipped")

	top = send(t, top, press(tea.KeySpace, " ")).(*Top)
	require.NotContains(t, top.View().Content, "paused")

	top = send(t, top, Query{Name: "after.example.com", Verdict: "allow"}).(*Top)
	require.Contains(t, top.View().Content, "after.example.com")
}

func TestTopQuitsOnQAndOnControlC(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{press('q', "q"), {Code: 'c', Mod: tea.ModCtrl}} {
		model, cmd := NewTop(nil).Update(key)
		require.NotNil(t, model)
		require.NotNil(t, cmd, "the key %q should quit", key.String())
		require.IsType(t, tea.QuitMsg{}, cmd())
	}
}

func TestTopFollowsTheStreamWhenItIsGivenOne(t *testing.T) {
	events := make(chan Query, 1)
	events <- Query{Name: "streamed.example.com", Verdict: "block"}

	top := NewTop(events)
	top.Update(resized(100))

	cmd := top.Init()
	require.NotNil(t, cmd, "the model asks for the first decision")
	msg := cmd()
	query, ok := msg.(Query)
	require.True(t, ok, "the command yields a query, got %T", msg)
	require.Equal(t, "streamed.example.com", query.Name)

	top = send(t, top, query).(*Top)
	require.Contains(t, top.View().Content, "streamed.example.com")
}

func TestTopSaysWhenTheStreamEnds(t *testing.T) {
	events := make(chan Query)
	close(events)

	top := NewTop(events)
	cmd := top.Init()
	msg := cmd()
	top = send(t, top, msg).(*Top)
	require.Contains(t, top.View().Content, "stream ended")
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestTailReplacesItsWindowOnEveryRefresh(t *testing.T) {
	fetches := [][]Query{
		{{Time: at("15:04:05"), Client: "10.0.0.5", Name: "first.example.com", Verdict: "allow"}},
		{
			{Time: at("15:04:05"), Client: "10.0.0.5", Name: "first.example.com", Verdict: "allow"},
			{Time: at("15:04:06"), Client: "10.0.0.6", Name: "second.example.com", Verdict: "block", Rule: "ads"},
		},
	}
	calls := 0
	tail := NewTail(func() ([]Query, error) {
		rows := fetches[calls]
		calls++
		return rows, nil
	})
	tail.Update(resized(100))

	tail = send(t, tail, tailRows(fetches[0])).(*Tail)
	view := tail.View().Content
	require.Contains(t, view, "aegis query tail · 1 shown · 0 blocked")
	require.NotContains(t, view, "second.example.com")

	tail = send(t, tail, tailRows(fetches[1])).(*Tail)
	view = tail.View().Content
	require.Contains(t, view, "aegis query tail · 2 shown · 1 blocked")
	require.Contains(t, view, "second.example.com")
}

func TestTailReportsAFetchItCouldNotMake(t *testing.T) {
	tail := NewTail(func() ([]Query, error) { return nil, errNoAPI })
	view := send(t, tail, errNoAPI).(*Tail).View().Content
	require.Contains(t, view, "no aegis api")
}

var errNoAPI = errText("no aegis api at that address")

type errText string

func (e errText) Error() string { return string(e) }
