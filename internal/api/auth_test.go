package api_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/api"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

func anonymousClient() *http.Client { return &http.Client{} }

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func TestMutatingRoutesRefuseAnUnauthenticatedRequest(t *testing.T) {
	h := startHarness(t)

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, "/api/v1/profiles/kids", `{"mode":"refused"}`},
		{http.MethodDelete, "/api/v1/profiles/kids", ""},
		{http.MethodPut, "/api/v1/clients/tablet", `{"profile":"default"}`},
		{http.MethodDelete, "/api/v1/clients/tablet", ""},
	} {
		req, err := http.NewRequestWithContext(t.Context(), request.method, h.apiURL+request.path, strings.NewReader(request.body))
		require.NoError(t, err)
		resp, err := anonymousClient().Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s %s", request.method, request.path)
	}
}

func TestReadingConfigurationNeedsASession(t *testing.T) {
	h := startHarness(t)

	resp := get(t, anonymousClient(), h.apiURL+"/api/v1/profiles")

	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestLoginRejectsTheWrongPassword(t *testing.T) {
	h := startHarness(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.apiURL+"/api/v1/session", strings.NewReader(`{"password":"wrong"}`))
	require.NoError(t, err)
	resp, err := anonymousClient().Do(req)

	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAMutationWithoutTheCSRFTokenIsRefused(t *testing.T) {
	h := startHarness(t)

	// h.do always attaches the token, so send this one through the logged-in
	// client without a header.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, h.apiURL+"/api/v1/profiles/kids", strings.NewReader(`{"mode":"refused"}`))
	require.NoError(t, err)
	resp, err := h.client.Do(req)

	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestLogoutEndsTheSession(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodDelete, "/api/v1/session", "")
	require.Equal(t, http.StatusNoContent, status, body)

	h.csrf = ""
	status, body = h.do(t, http.MethodGet, "/api/v1/profiles", "")
	require.Equal(t, http.StatusUnauthorized, status, body)
}

func TestTheAPIRefusesToBindBeyondLoopbackWithoutOptIn(t *testing.T) {
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = api.Start(api.Config{
		Store:    database,
		Reloader: runtime.New(database, nil),
		Auth:     api.NewAuth("unused", false),
		Address:  "0.0.0.0:0",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "loopback")
}

func TestTheAPIServesTLSWithASelfSignedCertificate(t *testing.T) {
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	hash, err := api.HashPassword("secret")
	require.NoError(t, err)
	server, err := api.Start(api.Config{
		Store:      database,
		Reloader:   runtime.New(database, nil),
		Auth:       api.NewAuth(hash, true),
		Address:    "127.0.0.1:0",
		SelfSigned: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp := get(t, client, "https://"+server.Addr().String()+"/api/v1/session")

	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPasswordHashRoundTrips(t *testing.T) {
	hash, err := api.HashPassword("correct horse battery staple")
	require.NoError(t, err)

	require.True(t, api.NewAuth(hash, false).CheckPassword("correct horse battery staple"))
	require.False(t, api.NewAuth(hash, false).CheckPassword("wrong"))
}
