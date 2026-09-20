package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// refreshPeriod is how often the recorded-log screen re-reads the log.
const refreshPeriod = time.Second

// Tail is the recorded-log screen. It re-reads the newest window on a timer
// instead of holding the database the daemon has open.
type Tail struct {
	fetch  func() ([]Query, error)
	window list
	width  int
	height int
	err    error
	period time.Duration
}

// tailRows carries one successful read into the model.
type tailRows []Query

// tailTick asks for the next read.
type tailTick struct{}

// NewTail returns the log screen over a read function, which the command wires
// to the API and the tests wire to a canned window.
func NewTail(fetch func() ([]Query, error)) *Tail {
	return &Tail{fetch: fetch, window: list{limit: windowLimit}, period: refreshPeriod}
}

func (t *Tail) Init() tea.Cmd { return t.read() }

func (t *Tail) read() tea.Cmd {
	return func() tea.Msg {
		rows, err := t.fetch()
		if err != nil {
			return err
		}
		return tailRows(rows)
	}
}

func (t *Tail) tick() tea.Cmd {
	return tea.Tick(t.period, func(time.Time) tea.Msg { return tailTick{} })
}

func (t *Tail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height = msg.Width, msg.Height
	case tailRows:
		t.window.replace(msg)
		t.err = nil
		return t, t.tick()
	case error:
		t.err = msg
		return t, t.tick()
	case tailTick:
		return t, t.read()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return t, tea.Quit
		}
	}
	return t, nil
}

func (t *Tail) View() tea.View {
	title := "aegis query tail · " + t.window.summary("shown")
	if t.err != nil {
		title = "aegis query tail · " + t.err.Error()
	}
	view := tea.NewView(frame(title, t.window.rows, t.width, t.height))
	view.AltScreen = true
	view.WindowTitle = "aegis query tail"
	return view
}
