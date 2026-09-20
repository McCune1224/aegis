package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// Top is the live query screen. It renders each decision as it arrives off the
// daemon's stream and never reads the log.
type Top struct {
	events  <-chan Query
	window  list
	width   int
	height  int
	paused  bool
	skipped int
	ended   bool
}

// windowLimit is how many rows the live screen holds. Older rows fall off the
// bottom; the header keeps counting them.
const windowLimit = 500

// NewTop returns the live screen over a decision channel. A nil channel renders
// only what arrives as a message, which is how the tests drive it.
func NewTop(events <-chan Query) *Top {
	return &Top{events: events, window: list{limit: windowLimit}}
}

// streamEnded says the daemon closed the stream, which is not an error the
// operator can act on but is worth showing.
type streamEnded struct{}

func (t *Top) Init() tea.Cmd { return t.wait() }

// wait reads the next decision. Every update that handles one asks for the
// next, so the model follows the channel for as long as it lives.
func (t *Top) wait() tea.Cmd {
	if t.events == nil {
		return nil
	}
	return func() tea.Msg {
		query, ok := <-t.events
		if !ok {
			return streamEnded{}
		}
		return query
	}
}

func (t *Top) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height = msg.Width, msg.Height
	case Query:
		if t.paused {
			t.skipped++
		} else {
			t.window.add(msg)
		}
		return t, t.wait()
	case streamEnded:
		t.ended = true
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return t, tea.Quit
		case "space":
			t.paused = !t.paused
		}
	}
	return t, nil
}

func (t *Top) View() tea.View {
	title := "aegis top · " + t.window.summary("queries")
	if t.paused {
		title += " · paused"
		if t.skipped > 0 {
			title += " · " + strconv.Itoa(t.skipped) + " skipped"
		}
	}
	if t.ended {
		title += " · stream ended"
	}
	view := tea.NewView(frame(title, t.window.rows, t.width, t.height))
	view.AltScreen = true
	view.WindowTitle = "aegis top"
	return view
}
