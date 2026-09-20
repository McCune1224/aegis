package api_test

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/services"
)

// seedCatalog writes a two-service catalog straight into the store and asks
// the API to reload, which is the same path a catalog refresh takes.
func seedCatalog(t *testing.T, h *harness) {
	t.Helper()
	err := h.database.SaveCatalog(t.Context(), time.Now().UTC(), []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^", "||youtu.be^"}},
		{ID: "4chan", Name: "4chan", Group: "social_network", Rules: []string{"||4chan.org^"}},
	})
	require.NoError(t, err)
	status, body := h.do(t, http.MethodPost, "/api/v1/reload", "")
	require.Equal(t, http.StatusOK, status, body)
}

// askFrom asks for any name from a chosen source address, so a test can state
// which client is asking.
func askFrom(t *testing.T, local, name, server string) *mdns.Msg {
	t.Helper()
	client := &mdns.Client{
		Net:     "udp",
		Timeout: 2 * time.Second,
		Dialer:  &net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP(local)}},
	}
	resp, _, err := client.Exchange(new(mdns.Msg).SetQuestion(name, mdns.TypeA), server)
	require.NoError(t, err)
	return resp
}

// placeClients puts one client on the kids profile and one on the default
// profile, so a service enabled for kids has a client it should reach and a
// client it should not.
func placeClients(t *testing.T, h *harness) {
	t.Helper()
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/laptop", `{"profile":"default","addresses":["127.0.0.3"]}`)
	require.Equal(t, http.StatusOK, status, body)
}

func TestEnablingAServiceBlocksOnlyTheProfileThatEnabledIt(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.2", "youtube.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.2", "youtu.be.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeServerFailure, askFrom(t, "127.0.0.3", "youtube.com.", h.dnsAddress).Rcode)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":[]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeServerFailure, askFrom(t, "127.0.0.2", "youtube.com.", h.dnsAddress).Rcode)
}

func TestReadingBackAProfileServicesReturnsTheWholeSet(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube","4chan"]}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"youtube"`)
	require.Contains(t, body, `"4chan"`)

	status, body = h.do(t, http.MethodGet, "/api/v1/profiles/kids/services", "")
	require.Equal(t, http.StatusOK, status, body)
	var set struct {
		Profile  string   `json:"profile"`
		Services []string `json:"services"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &set))
	require.Equal(t, "kids", set.Profile)
	require.Equal(t, []string{"4chan", "youtube"}, set.Services)
}

func TestServicesListCarriesTheCatalogAndWhoBlocksEachOne(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)
	h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)

	status, body := h.do(t, http.MethodGet, "/api/v1/services", "")
	require.Equal(t, http.StatusOK, status, body)

	var listing struct {
		Services []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Group     string   `json:"group"`
			RuleCount int      `json:"rule_count"`
			Profiles  []string `json:"profiles"`
		} `json:"services"`
		Groups []string `json:"groups"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &listing))
	require.Len(t, listing.Services, 2)

	first := listing.Services[0]
	require.Equal(t, "4chan", first.ID)
	require.Equal(t, []string{}, first.Profiles)

	second := listing.Services[1]
	require.Equal(t, "youtube", second.ID)
	require.Equal(t, "YouTube", second.Name)
	require.Equal(t, "streaming", second.Group)
	require.Equal(t, 2, second.RuleCount)
	require.Equal(t, []string{"kids"}, second.Profiles)
	require.Equal(t, []string{"social_network", "streaming"}, listing.Groups)
}

func TestRefreshingTheCatalogOverHTTPMakesAServiceBlockable(t *testing.T) {
	h := startHarness(t)
	placeClients(t, h)

	status, body := h.do(t, http.MethodGet, "/api/v1/services", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"services":[]`)

	status, body = h.do(t, http.MethodPost, "/api/v1/services/refresh", "")
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/services", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"id":"youtube"`)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.2", "youtube.com.", h.dnsAddress).Rcode)
}

func TestEnablingAnUnknownServiceOrProfileIsRefused(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["tiktok"]}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "tiktok")

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/ghosts/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusNotFound, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/profiles/kids/services", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"services":[]`)
}
