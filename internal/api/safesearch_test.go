package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

func TestSafeSearchRewritesOnlyTheProfileThatEnabledIt(t *testing.T) {
	h := startHarness(t)
	stub := startStub(t, "203.0.113.10")
	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+stub.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/safesearch", `{"engines":["google"]}`)
	require.Equal(t, http.StatusOK, status, body)

	got := askFrom(t, "127.0.0.2", "www.google.com.", h.dnsAddress)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 2)
	cname, ok := got.Answer[0].(*mdns.CNAME)
	require.True(t, ok, "the first answer should be the safe host, got %T", got.Answer[0])
	require.Equal(t, "forcesafesearch.google.com.", cname.Target)
	_, ok = got.Answer[1].(*mdns.A)
	require.True(t, ok, "the safe host's address should follow the CNAME")

	plain := askFrom(t, "127.0.0.1", "www.google.com.", h.dnsAddress)
	require.Equal(t, mdns.RcodeSuccess, plain.Rcode)
	require.Len(t, plain.Answer, 1)
	_, ok = plain.Answer[0].(*mdns.A)
	require.True(t, ok, "a profile without safe search answers the name itself")

	logged := waitForLog(t, h, "name=www.google.com&client=127.0.0.2")
	require.Contains(t, logged, `"client":"127.0.0.2"`)
	require.Contains(t, logged, `"rule":"forcesafesearch.google.com"`)
}

func TestSafeSearchListCarriesTheCatalogAndWhoEnforcesEachEngine(t *testing.T) {
	h := startHarness(t)
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/safesearch", `{"engines":["google","youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/safesearch", "")
	require.Equal(t, http.StatusOK, status, body)

	var listing struct {
		Engines []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			RuleCount int      `json:"rule_count"`
			Profiles  []string `json:"profiles"`
		} `json:"engines"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &listing))
	require.Len(t, listing.Engines, 9)

	byID := map[string][]string{}
	for _, engine := range listing.Engines {
		byID[engine.ID] = engine.Profiles
	}
	require.Equal(t, []string{"kids"}, byID["google"])
	require.Equal(t, []string{"kids"}, byID["youtube"])
	require.Equal(t, []string{}, byID["bing"])

	for _, engine := range listing.Engines {
		if engine.ID == "google" {
			require.Equal(t, "Google", engine.Name)
			require.Greater(t, engine.RuleCount, 100)
		}
	}
}

func TestSafesearchRejectsAnUnknownEngineOrProfile(t *testing.T) {
	h := startHarness(t)
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/safesearch", `{"engines":["altavista"]}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "altavista")

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/ghosts/safesearch", `{"engines":["google"]}`)
	require.Equal(t, http.StatusNotFound, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/profiles/kids/safesearch", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"engines":[]`)
}

func TestDisablingSafeSearchStopsRewritingWithoutARestart(t *testing.T) {
	h := startHarness(t)
	stub := startStub(t, "203.0.113.10")
	status, body := h.do(t, http.MethodPut, "/api/v1/upstreams/primary", `{"url":"`+stub.address+`","enabled":true}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"default","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/default/safesearch", `{"engines":["google"]}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Len(t, askFrom(t, "127.0.0.2", "www.google.com.", h.dnsAddress).Answer, 2)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/default/safesearch", `{"engines":[]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Len(t, askFrom(t, "127.0.0.2", "www.google.com.", h.dnsAddress).Answer, 1)
}

// waitForLog polls the query log endpoint until the filter matches, because
// the writer flushes on a timer rather than per query.
func waitForLog(t *testing.T, h *harness, filter string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		require.Less(t, time.Now().UnixNano(), deadline.UnixNano(), "the query never reached the log")
		time.Sleep(20 * time.Millisecond)
		status, got := h.do(t, http.MethodGet, "/api/v1/queries?"+filter, "")
		require.Equal(t, http.StatusOK, status, got)
		if got != "{\"queries\":[]}\n" {
			return got
		}
	}
}
