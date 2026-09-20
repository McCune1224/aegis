package tui_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/tui"
)

// streamServer answers one SSE event per line it is given and then ends the
// response, which is how the live stream looks to a client that walks away.
func streamServer(t *testing.T, events ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("the test server cannot flush")
			return
		}
		for _, event := range events {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				return
			}
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestStreamDecodesTheLiveDecisionWire(t *testing.T) {
	server := streamServer(t,
		`{"time":"2026-09-20T15:04:05Z","address":"10.0.0.5","name":"ads.example.com","action":"block","rule":{"id":"ads","source":"ads","pattern":"ads.example.com"}}`,
		`{"time":"2026-09-20T15:04:06Z","address":"10.0.0.6","name":"cdn.example.com","action":"allow"}`,
	)

	events, err := tui.Stream(t.Context(), server.URL)
	require.NoError(t, err)

	first, ok := <-events
	require.True(t, ok)
	require.Equal(t, "ads.example.com", first.Name)
	require.Equal(t, "10.0.0.5", first.Client)
	require.Equal(t, "block", first.Verdict)
	require.Equal(t, "ads", first.Rule)
	require.True(t, first.Blocked())
	require.Equal(t, "2026-09-20T15:04:05Z", first.Time.UTC().Format(time.RFC3339))

	second, ok := <-events
	require.True(t, ok)
	require.Equal(t, "cdn.example.com", second.Name)
	require.Equal(t, "allow", second.Verdict)
	require.Empty(t, second.Rule)
	require.False(t, second.Blocked())

	_, ok = <-events
	require.False(t, ok, "the channel closes when the stream ends")
}

func TestLogDecodesRecordedRowsAndAsksForTheLimit(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprint(w, `{"queries":[{"time":1789881845000,"client":"10.0.0.5","name":"ads.example.com","type":"A","verdict":"block","rule":"ads"}]}`)
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	rows, err := tui.Log(t.Context(), server.URL, 25)
	require.NoError(t, err)
	require.Equal(t, "25", asked)
	require.Len(t, rows, 1)
	require.Equal(t, "ads.example.com", rows[0].Name)
	require.Equal(t, "10.0.0.5", rows[0].Client)
	require.Equal(t, "A", rows[0].Type)
	require.Equal(t, "block", rows[0].Verdict)
	require.Equal(t, "ads", rows[0].Rule)
	require.Equal(t, int64(1789881845000), rows[0].Time.UnixMilli())
}

func TestStreamJSONWritesOneObjectPerDecision(t *testing.T) {
	server := streamServer(t,
		`{"time":"2026-09-20T15:04:05Z","address":"10.0.0.5","name":"ads.example.com","action":"block","rule":{"id":"ads"}}`,
		`{"time":"2026-09-20T15:04:06Z","address":"10.0.0.6","name":"cdn.example.com","action":"allow"}`,
	)

	var out bytes.Buffer
	require.NoError(t, tui.StreamJSON(t.Context(), &out, server.URL))

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)

	var first tui.Query
	require.NoError(t, json.Unmarshal(lines[0], &first))
	require.Equal(t, "ads.example.com", first.Name)
	require.Equal(t, "block", first.Verdict)
	require.Equal(t, "ads", first.Rule)

	var second tui.Query
	require.NoError(t, json.Unmarshal(lines[1], &second))
	require.Equal(t, "cdn.example.com", second.Name)
	require.Equal(t, "allow", second.Verdict)
}

func TestLogFollowPrintsOnlyQueriesItHasNotPrinted(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		// Newest first, the way the API serves the log.
		switch calls {
		case 1:
			_, err := fmt.Fprint(w, `{"queries":[{"time":300,"client":"10.0.0.5","name":"c.example.com","type":"A","verdict":"allow"},{"time":200,"client":"10.0.0.5","name":"b.example.com","type":"A","verdict":"allow"}]}`)
			require.NoError(t, err)
		default:
			_, err := fmt.Fprint(w, `{"queries":[{"time":400,"client":"10.0.0.5","name":"d.example.com","type":"A","verdict":"block"},{"time":300,"client":"10.0.0.5","name":"c.example.com","type":"A","verdict":"allow"},{"time":200,"client":"10.0.0.5","name":"b.example.com","type":"A","verdict":"allow"}]}`)
			require.NoError(t, err)
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// follow writes from its own goroutine while the test polls, so the buffer
	// has to be safe to read across them.
	var out safeBuffer
	done := make(chan error, 1)
	go func() { done <- tui.LogFollow(ctx, &out, server.URL, 10, 5*time.Millisecond) }()

	require.Eventually(t, func() bool {
		return bytes.Count(out.Bytes(), []byte("\n")) >= 3
	}, 5*time.Second, 5*time.Millisecond, "follow never printed the third query")
	cancel()
	require.NoError(t, <-done)

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	require.Len(t, lines, 3)
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		var got tui.Query
		require.NoError(t, json.Unmarshal(line, &got))
		names = append(names, got.Name)
	}
	require.Equal(t, []string{"b.example.com", "c.example.com", "d.example.com"}, names, "follow prints oldest first and never repeats")
}

// safeBuffer is a bytes.Buffer the follow loop can write while the test reads.
type safeBuffer struct {
	mu   sync.Mutex
	body bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.Write(p)
}

func (b *safeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.body.Bytes())
}
