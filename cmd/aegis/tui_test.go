package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/tui"
)

// safeBuffer is a bytes.Buffer the screen commands write from their own
// goroutines while the test reads.
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

// startAegis runs serve on an ephemeral DNS and API port over a temp database,
// and returns the two addresses once the API answers.
func startAegis(t *testing.T) (dnsAddress, apiAddress string) {
	t.Helper()
	dnsAddress = freeAddress(t)
	apiAddress = freeTCPAddress(t)

	ctx, cancel := context.WithCancel(t.Context())
	serve := newRootCmd()
	serve.SetArgs([]string{
		"serve",
		"--dns-address", dnsAddress,
		"--api-address", apiAddress,
		"--upstream", startUpstream(t, "203.0.113.50"),
		"--db", filepath.Join(t.TempDir(), "aegis.db"),
		"--log-level", "error",
	})
	serve.SetContext(ctx)

	served := make(chan error, 1)
	go func() { served <- serve.Execute() }()

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+apiAddress+"/api/v1/status", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 25*time.Millisecond, "serve never served the API")

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-served)
	})
	return dnsAddress, apiAddress
}

// runScreenCommand runs a screen command in the background with its output
// captured, and returns the buffer and a stop function that waits for it.
func runScreenCommand(t *testing.T, args ...string) (*safeBuffer, func() error) {
	t.Helper()
	out := &safeBuffer{}

	ctx, cancel := context.WithCancel(t.Context())
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	return out, func() error {
		cancel()
		return <-done
	}
}

func TestTopCommandStreamsDecisionsAsJSONLines(t *testing.T) {
	dnsAddress, apiAddress := startAegis(t)
	out, stop := runScreenCommand(t, "top", "--api-address", apiAddress, "--json")
	defer func() { require.NoError(t, stop()) }()

	// The stream has to be attached before a decision can reach it, so keep
	// asking with fresh names until one shows up on the wire.
	var asked string
	require.Eventually(t, func() bool {
		asked = fmt.Sprintf("top%d.example.com.", time.Now().UnixNano())
		ask(t, dnsAddress, asked)
		return bytes.Contains(out.Bytes(), []byte(strings.TrimSuffix(asked, ".")))
	}, 5*time.Second, 25*time.Millisecond, "the screen never printed a decision")

	// The condition matched the decision it just asked for, and the stream is
	// ordered, so that decision is the last line.
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	var got tui.Query
	require.NoError(t, json.Unmarshal(lines[len(lines)-1], &got))
	require.Equal(t, strings.TrimSuffix(asked, "."), got.Name)
	require.Equal(t, "allow", got.Verdict)
}

func TestQueryTailCommandPrintsRecordedRowsAsJSONLines(t *testing.T) {
	dnsAddress, apiAddress := startAegis(t)
	ask(t, dnsAddress, "tail.example.com.")

	var out safeBuffer
	require.Eventually(t, func() bool {
		out.body.Reset()
		cmd := newRootCmd()
		cmd.SetArgs([]string{"query", "tail", "--api-address", apiAddress, "--json"})
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil {
			return false
		}
		return bytes.Contains(out.Bytes(), []byte("tail.example.com"))
	}, 5*time.Second, 100*time.Millisecond, "the tail never read the recorded query")

	var got tui.Query
	line := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))[0]
	require.NoError(t, json.Unmarshal(line, &got))
	require.Equal(t, "tail.example.com", got.Name)
	require.Equal(t, "allow", got.Verdict)
}

func TestQueryTailRefusesToFollowWithoutJSON(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"query", "tail", "--follow", "--api-address", "127.0.0.1:1"})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--follow needs --json")
}

func TestScreensSaySoWhenNothingIsListening(t *testing.T) {
	// The port is closed, so both screens fail before opening a terminal.
	out := &safeBuffer{}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"top", "--api-address", "127.0.0.1:1", "--json"})
	cmd.SetOut(out)
	require.Error(t, cmd.Execute())
}
