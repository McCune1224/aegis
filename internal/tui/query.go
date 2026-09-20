// Package tui renders the operator screens. Each screen is a bubbletea model
// over data fetched from a running aegis over its own HTTP API, so the screens
// never touch the database the daemon has open.
package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Query is one decision as the screens and the JSON twins show it. The live
// stream and the recorded log both decode into it, so one renderer serves both
// screens.
type Query struct {
	Time    time.Time `json:"time"`
	Client  string    `json:"client"`
	Name    string    `json:"name"`
	Type    string    `json:"type,omitempty"`
	Verdict string    `json:"verdict"`
	Rule    string    `json:"rule,omitempty"`
}

// Blocked reports whether the decision denied the query.
func (q Query) Blocked() bool { return q.Verdict == "block" }

// streamEvent is the live decision wire. The API's stream is its own boundary
// type, with the address and the action under different names than the log.
type streamEvent struct {
	Time    time.Time `json:"time"`
	Address string    `json:"address"`
	Name    string    `json:"name"`
	Action  string    `json:"action"`
	Rule    struct {
		ID string `json:"id"`
	} `json:"rule"`
}

func (event streamEvent) query() Query {
	return Query{
		Time:    event.Time,
		Client:  event.Address,
		Name:    event.Name,
		Verdict: event.Action,
		Rule:    event.Rule.ID,
	}
}

// logRow is one recorded query on the wire, with its time in milliseconds since
// the Unix epoch.
type logRow struct {
	Time    int64  `json:"time"`
	Client  string `json:"client"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Verdict string `json:"verdict"`
	Rule    string `json:"rule"`
}

func (row logRow) query() Query {
	return Query{
		Time:    time.UnixMilli(row.Time),
		Client:  row.Client,
		Name:    row.Name,
		Type:    row.Type,
		Verdict: row.Verdict,
		Rule:    row.Rule,
	}
}

// Stream subscribes to the live decision stream at baseURL. The channel carries
// one decision per query and closes when the response ends, which is also how
// the screen learns the daemon went away.
func Stream(ctx context.Context, baseURL string) (<-chan Query, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL(baseURL, "/api/v1/stream/queries"), nil)
	if err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()
		return nil, fmt.Errorf("tui: the stream answered %s", response.Status)
	}

	events := make(chan Query)
	go func() {
		defer close(events)
		defer func() { _ = response.Body.Close() }()

		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(nil, 1<<20)
		for scanner.Scan() {
			payload, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var event streamEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}
			select {
			case events <- event.query():
			case <-ctx.Done():
				return
			}
		}
	}()

	return events, nil
}

// Log reads the newest recorded queries at baseURL, newest first, which is the
// order the API serves them in.
func Log(ctx context.Context, baseURL string, limit int) ([]Query, error) {
	url := fmt.Sprintf("%s?limit=%d", apiURL(baseURL, "/api/v1/queries"), limit)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tui: the query log answered %s", response.Status)
	}

	var body struct {
		Queries []logRow `json:"queries"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}

	rows := make([]Query, 0, len(body.Queries))
	for _, row := range body.Queries {
		rows = append(rows, row.query())
	}
	return rows, nil
}

// StreamJSON writes one JSON object per live decision to w until the stream
// ends. A piped process reads the decisions without a terminal.
func StreamJSON(ctx context.Context, w io.Writer, baseURL string) error {
	events, err := Stream(ctx, baseURL)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	for query := range events {
		if err := encoder.Encode(query); err != nil {
			return fmt.Errorf("tui: %w", err)
		}
	}
	return nil
}

// LogFollow writes recorded queries to w as JSON lines until ctx is done. It
// starts with the newest limit rows, prints them oldest first the way tail
// does, then polls every period for rows it has not printed.
func LogFollow(ctx context.Context, w io.Writer, baseURL string, limit int, period time.Duration) error {
	encoder := json.NewEncoder(w)
	var last *Query

	print := func(rows []Query) error {
		for _, query := range fresh(rows, last) {
			if err := encoder.Encode(query); err != nil {
				return fmt.Errorf("tui: %w", err)
			}
			printed := query
			last = &printed
		}
		return nil
	}

	for {
		rows, err := Log(ctx, baseURL, limit)
		if err != nil {
			// A cancelled context is how the caller stops the loop.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		if err := print(rows); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(period):
		}
	}
}

// fresh returns the rows newer than the last one printed, oldest first. The
// cursor is found by value rather than by time, because two queries can share a
// millisecond and a row that fell out of the window must not be printed twice.
func fresh(rows []Query, last *Query) []Query {
	if last == nil {
		return oldestFirst(rows)
	}
	for i, row := range rows {
		if same(row, *last) {
			return oldestFirst(rows[:i])
		}
	}
	return nil
}

// oldestFirst reverses a newest-first window.
func oldestFirst(rows []Query) []Query {
	out := make([]Query, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, rows[i])
	}
	return out
}

// same reports whether two rows describe the same decision. The times are
// compared as instants, because two decodes of one timestamp can carry
// different fixed-zone locations.
func same(a, b Query) bool {
	return a.Time.Equal(b.Time) && a.Client == b.Client && a.Name == b.Name &&
		a.Type == b.Type && a.Verdict == b.Verdict && a.Rule == b.Rule
}

// apiURL joins a path onto the API's base URL.
func apiURL(baseURL, path string) string {
	return strings.TrimSuffix(baseURL, "/") + path
}
