package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResetRefusesWithoutYes keeps the destructive call behind a flag the
// operator has to type, and proves it never reaches a server.
func TestResetRefusesWithoutYes(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, `{"status":"reset"}`)
	}))
	t.Cleanup(server.Close)

	root := newRootCmd()
	root.SetArgs([]string{"reset", "--api-address", strings.TrimPrefix(server.URL, "http://")})
	err := root.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "--yes")
	require.False(t, called, "the reset must not reach a server without --yes")
}

// TestResetAsksTheRunningServer is the whole job of the subcommand: one
// confirmed POST at the running server, and its answer on stdout.
func TestResetAsksTheRunningServer(t *testing.T) {
	var method, path, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		payload, _ := io.ReadAll(r.Body)
		body = string(payload)
		_, _ = io.WriteString(w, `{"status":"reset","clients":3,"discoveries":1,"queries":1,"services":2,"settings":3}`)
	}))
	t.Cleanup(server.Close)

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"reset", "--api-address", strings.TrimPrefix(server.URL, "http://"), "--yes"})

	require.NoError(t, root.Execute())
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/api/v1/reset", path)
	require.JSONEq(t, `{"confirm":"reset"}`, body)
	require.Contains(t, out.String(), `"clients":3`)
	require.Contains(t, out.String(), `"services":2`)
}

// TestResetNamesARefusedServer makes a server that will not wipe say why, so
// the operator reads the reason instead of a bare transport error.
func TestResetNamesARefusedServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"reset: confirmation must be \"reset\""}`)
	}))
	t.Cleanup(server.Close)

	root := newRootCmd()
	root.SetArgs([]string{"reset", "--api-address", strings.TrimPrefix(server.URL, "http://"), "--yes"})
	err := root.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "reset")
}
