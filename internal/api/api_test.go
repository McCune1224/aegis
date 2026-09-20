package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/api"
	"aegis/internal/blocklist"
	"aegis/internal/dns"
	"aegis/internal/querylog"
	"aegis/internal/runtime"
	"aegis/internal/store"
)

// deadResolver never answers: the harness exercises the API surface, so no
// query should reach an upstream.
type deadResolver struct{}

func (deadResolver) Resolve(_ context.Context, _ *mdns.Msg) (*mdns.Msg, error) {
	return nil, errors.New("the api harness has no resolver")
}

type harness struct {
	apiURL     string
	dnsAddress string
	hub        *api.Hub
	listPath   string
	log        *querylog.QueryLog
}

// startHarness runs the store, runtime, DNS server, and API in process, the way
// serve wires them, so a mutation travels the whole path to a wire answer.
func startHarness(t *testing.T) *harness {
	t.Helper()
	return startHarnessWithClock(t, nil)
}

func startHarnessWithClock(t *testing.T, now func() time.Time) *harness {
	t.Helper()
	ctx := t.Context()

	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, database.SaveUpstream(ctx, store.Upstream{Name: deadUpstream, URL: deadUpstream, Enabled: true}))

	lists := []runtime.ListFile{listFileWith(t, "ads.example.com\n")}
	var options []runtime.Option
	if now != nil {
		options = append(options, runtime.WithClock(now))
	}
	rt := runtime.New(database, lists, quietLogger(), options...)
	require.NoError(t, rt.Reload(ctx))
	sync := runtime.NewSourceSync(database, blocklist.NewFetcher(2*time.Second), rt, quietLogger())

	hub := api.NewHub(nil)
	log := querylog.New(database, quietLogger())
	handler, err := dns.NewHandler(dns.Config{
		Decider:   rt,
		Upstream:  deadResolver{},
		Rewriter:  rt,
		Observers: []dns.Observer{hub, log},
	})
	require.NoError(t, err)
	dnsServer, err := dns.Start(dns.ServerConfig{Handler: handler, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = dnsServer.Shutdown(context.Background()) })

	apiServer, err := api.Start(api.Config{Store: database, Reloader: rt, Sources: sync, Preview: sync, Hub: hub, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = apiServer.Shutdown(context.Background()) })

	return &harness{
		apiURL:     "http://" + apiServer.Addr().String(),
		dnsAddress: dnsServer.UDPAddr().String(),
		hub:        hub,
		listPath:   lists[0].Path,
		log:        log,
	}
}

func (h *harness) do(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, h.apiURL+path, strings.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(payload)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func listFileWith(t *testing.T, content string) runtime.ListFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "list.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return runtime.ListFile{Path: path, Format: blocklist.FormatHosts}
}

// listedName is the name the harness's list server blocks, and the name every
// DNS-path test in this package asks for.
const listedName = "tracker.example.net"

// deadUpstream is the row the harness seeds, so the runtime's reload always
// has an enabled resolver. Nothing listens there, so a query the filter lets
// through fails the way it did when the harness wired a dead resolver in.
const deadUpstream = "127.0.0.1:1"

// queryFor asks the harness DNS server for the listed name from the loopback
// address the policy defaults to.
func queryFor(t *testing.T, server string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(listedName+".", mdns.TypeA), server)
	require.NoError(t, err)
	return resp
}

// queryFrom asks from a chosen source address, so the server sees the client
// the policy is keyed on.
func queryFrom(t *testing.T, local, server string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{
		Net:     "udp",
		Timeout: 2 * time.Second,
		Dialer:  &net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP(local)}},
	}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion("ads.example.com.", mdns.TypeA), server)
	require.NoError(t, err)
	return resp
}

type profileJSON struct {
	Name    string  `json:"name"`
	Extends string  `json:"extends"`
	Mode    *string `json:"mode"`
	Custom  *string `json:"custom"`
}

type clientJSON struct {
	Name      string   `json:"name"`
	Profile   string   `json:"profile"`
	Notes     string   `json:"notes"`
	Addresses []string `json:"addresses"`
	Prefixes  []string `json:"prefixes"`
}

func TestAProfileAndClientCreatedOverHTTPGovernARealQuery(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"refused"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeRefused, queryFrom(t, "127.0.0.2", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)
}

func TestReadingBackAProfileAndAClientReturnsWhatWasStored(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"extends":"default","mode":"refused"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","notes":"the spare one","addresses":["10.9.9.2"],"prefixes":["10.9.8.0/24"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/profiles/kids", "")
	require.Equal(t, http.StatusOK, status, body)
	var profile profileJSON
	require.NoError(t, json.Unmarshal([]byte(body), &profile))
	require.Equal(t, "kids", profile.Name)
	require.Equal(t, "default", profile.Extends)
	require.NotNil(t, profile.Mode)
	require.Equal(t, "refused", *profile.Mode)

	status, body = h.do(t, http.MethodGet, "/api/v1/clients/tablet", "")
	require.Equal(t, http.StatusOK, status, body)
	var client clientJSON
	require.NoError(t, json.Unmarshal([]byte(body), &client))
	require.Equal(t, "tablet", client.Name)
	require.Equal(t, "kids", client.Profile)
	require.Equal(t, "the spare one", client.Notes)
	require.Equal(t, []string{"10.9.9.2"}, client.Addresses)
	require.Equal(t, []string{"10.9.8.0/24"}, client.Prefixes)

	status, body = h.do(t, http.MethodGet, "/api/v1/clients", "")
	require.Equal(t, http.StatusOK, status, body)
	var clients []clientJSON
	require.NoError(t, json.Unmarshal([]byte(body), &clients))
	require.Len(t, clients, 1)
}

func TestMutationsRejectBadInputWithFourHundred(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		want string
	}{
		{"unknown mode", "/api/v1/profiles/kids", `{"mode":"drop"}`, "mode"},
		{"custom mode without an address", "/api/v1/profiles/kids", `{"mode":"custom-address"}`, "custom-address"},
		{"address with a non-custom mode", "/api/v1/profiles/kids", `{"mode":"refused","custom":"10.0.0.1"}`, "refused"},
		{"bad address", "/api/v1/clients/tablet", `{"profile":"default","addresses":["nope"]}`, "addresses"},
		{"bad prefix", "/api/v1/clients/tablet", `{"profile":"default","prefixes":["10.0.0.0/33"]}`, "prefixes"},
		{"unknown profile", "/api/v1/clients/tablet", `{"profile":"ghost"}`, "ghost"},
		{"wrong field type", "/api/v1/profiles/kids", `{"mode":5}`, "mode"},
		{"malformed json", "/api/v1/profiles/kids", `{`, "invalid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := startHarness(t)

			status, body := h.do(t, http.MethodPut, tc.path, tc.body)

			require.Equal(t, http.StatusBadRequest, status, body)
			require.Contains(t, body, tc.want)
		})
	}
}

func TestAProfileCycleIsRejectedAndLeavesTheStoreAlone(t *testing.T) {
	h := startHarness(t)
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/b", `{}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/a", `{"extends":"b"}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/b", `{"extends":"a"}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "extends itself")

	status, body = h.do(t, http.MethodGet, "/api/v1/profiles/b", "")
	require.Equal(t, http.StatusOK, status, body)
	var profile profileJSON
	require.NoError(t, json.Unmarshal([]byte(body), &profile))
	require.Empty(t, profile.Extends)
}

func TestTwoClientsCannotClaimOneAddress(t *testing.T) {
	h := startHarness(t)
	status, body := h.do(t, http.MethodPut, "/api/v1/clients/a", `{"profile":"default","addresses":["10.9.9.2"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/clients/b", `{"profile":"default","addresses":["10.9.9.2"]}`)

	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "10.9.9.2")
	var apiError struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &apiError))
	require.Contains(t, apiError.Error, `"a"`)
	require.Contains(t, apiError.Error, `"b"`)
}

func TestARejectedMutationDoesNotChangeWhatTheServerAnswers(t *testing.T) {
	h := startHarness(t)
	h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"refused"}`)
	h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, mdns.RcodeRefused, queryFrom(t, "127.0.0.2", h.dnsAddress).Rcode)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"drop"}`)
	require.Equal(t, http.StatusBadRequest, status, body)

	require.Equal(t, mdns.RcodeRefused, queryFrom(t, "127.0.0.2", h.dnsAddress).Rcode)
}

func TestDeletingAClientReturnsItsAddressToTheDefaultProfile(t *testing.T) {
	h := startHarness(t)
	h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"refused"}`)
	h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, mdns.RcodeRefused, queryFrom(t, "127.0.0.2", h.dnsAddress).Rcode)

	status, body := h.do(t, http.MethodDelete, "/api/v1/clients/tablet", "")

	require.Equal(t, http.StatusNoContent, status, body)
	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.2", h.dnsAddress).Rcode)
}

func TestDeletingTheDefaultProfileIsRefused(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodDelete, "/api/v1/profiles/default", "")

	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "default")
}

func TestTheQueryStreamCarriesADecisionFromARealQuery(t *testing.T) {
	h := startHarness(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.apiURL+"/api/v1/stream/queries", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)

	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line, "data: "), "frame was %q", line)
	var event decisionEventJSON
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event))
	require.Equal(t, "ads.example.com", event.Name)
	require.Equal(t, "block", event.Action)
	require.NotNil(t, event.Rule)
	require.Equal(t, "ads.example.com", event.Rule.Pattern)
}

func TestAStalledConsumerCannotBlockAQuery(t *testing.T) {
	h := startHarness(t)
	decisions, cancel := h.hub.Subscribe()
	defer cancel()

	for range cap(decisions) + 1 {
		h.hub.Observe(dns.Decision{})
	}
	before := h.hub.Dropped()
	require.Greater(t, before, uint64(0), "a full subscriber must drop, not block")

	got := queryFrom(t, "127.0.0.1", h.dnsAddress)

	require.Equal(t, mdns.RcodeNameError, got.Rcode)
	require.Greater(t, h.hub.Dropped(), before, "the query's own decision dropped while the consumer stalled")
}

type decisionEventJSON struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Rule   *struct {
		ID      string `json:"id"`
		Pattern string `json:"pattern"`
	} `json:"rule"`
}

func TestStatusReportsTheUpstreamAndRuleCount(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodGet, "/api/v1/status", "")

	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, deadUpstream)
	require.Contains(t, body, `"rules":1`)
}

func TestTheDefaultProfileCanBeChangedOverHTTP(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{"mode":"refused"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/default-profile", `{"profile":"kids"}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeRefused, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)

	status, body = h.do(t, http.MethodGet, "/api/v1/default-profile", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, "kids")
}

func TestTheDefaultProfileMustExist(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodPut, "/api/v1/default-profile", `{"profile":"ghost"}`)

	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "ghost")
}

func TestUnknownProfilesAndClientsReturnNotFound(t *testing.T) {
	h := startHarness(t)

	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/profiles/ghost"},
		{http.MethodDelete, "/api/v1/profiles/ghost"},
		{http.MethodGet, "/api/v1/clients/ghost"},
		{http.MethodDelete, "/api/v1/clients/ghost"},
	} {
		status, body := h.do(t, request.method, request.path, "")
		require.Equal(t, http.StatusNotFound, status, "%s %s: %s", request.method, request.path, body)
	}
}

func TestAReloadEndpointPicksUpBlocklistEdits(t *testing.T) {
	h := startHarness(t)

	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)

	require.NoError(t, os.WriteFile(h.listPath, []byte("other.example.com\n"), 0o600))
	status, body := h.do(t, http.MethodPost, "/api/v1/reload", "")
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeServerFailure, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)
}

func TestAQueriedNameAppearsInTheQueryLogEndpoint(t *testing.T) {
	h := startHarness(t)

	require.Equal(t, mdns.RcodeNameError, queryFrom(t, "127.0.0.1", h.dnsAddress).Rcode)

	var body string
	deadline := time.Now().Add(5 * time.Second)
	for {
		require.Less(t, time.Now().UnixNano(), deadline.UnixNano(), "the query never reached the log")
		time.Sleep(20 * time.Millisecond)
		status, got := h.do(t, http.MethodGet, "/api/v1/queries?name=ads.example.com&verdict=block", "")
		require.Equal(t, http.StatusOK, status, got)
		if got != "{\"queries\":[]}\n" {
			body = got
			break
		}
	}
	require.Contains(t, body, "ads.example.com")
	require.Contains(t, body, "127.0.0.1")
}
