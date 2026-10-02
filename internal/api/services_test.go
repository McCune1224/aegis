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
// client it should not. A third client shares the kids profile with tablet, so
// a test can tell a client-scoped enablement from a profile-scoped one.
func placeClients(t *testing.T, h *harness) {
	t.Helper()
	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/laptop", `{"profile":"default","addresses":["127.0.0.3"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/console", `{"profile":"kids","addresses":["127.0.0.4"]}`)
	require.Equal(t, http.StatusOK, status, body)
}

func TestEnablingAServiceBlocksOnlyTheClientThatEnabledIt(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/clients/tablet/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.2", "youtube.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeServerFailure, askFrom(t, "127.0.0.3", "youtube.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeServerFailure, askFrom(t, "127.0.0.4", "youtube.com.", h.dnsAddress).Rcode)

	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet/services", `{"services":[]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeServerFailure, askFrom(t, "127.0.0.2", "youtube.com.", h.dnsAddress).Rcode)
}

func TestClientServicesAddToWhatTheProfileBlocks(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet/services", `{"services":["4chan"]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.2", "youtube.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.2", "4chan.org.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.4", "youtube.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeServerFailure, askFrom(t, "127.0.0.4", "4chan.org.", h.dnsAddress).Rcode)
}

func TestReadingBackAClientServicesReturnsTheWholeSet(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/clients/tablet/services", `{"services":["youtube","4chan"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/clients/tablet/services", "")
	require.Equal(t, http.StatusOK, status, body)
	var set struct {
		Client   string   `json:"client"`
		Services []string `json:"services"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &set))
	require.Equal(t, "tablet", set.Client)
	require.Equal(t, []string{"4chan", "youtube"}, set.Services)
}

func TestClientServicesRefuseAnUnknownClientOrService(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/clients/tablet/services", `{"services":["tiktok"]}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "tiktok")

	status, body = h.do(t, http.MethodPut, "/api/v1/clients/ghosts/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusNotFound, status, body)

	status, body = h.do(t, http.MethodGet, "/api/v1/clients/tablet/services", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"services":[]`)
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

func TestDeletingAClientDropsItsServiceEnablements(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/clients/tablet/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/clients/tablet", "")
	require.Equal(t, http.StatusNoContent, status, body)

	// A new client reusing the name starts with an empty set, so the delete
	// cascaded the stored rows instead of only relaxing the validation.
	status, body = h.do(t, http.MethodPut, "/api/v1/clients/tablet", `{"profile":"kids","addresses":["127.0.0.2"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodGet, "/api/v1/clients/tablet/services", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"services":[]`)
}

func TestDeletingAProfileDropsItsServiceEnablements(t *testing.T) {
	h := startHarness(t)
	seedCatalog(t, h)
	placeClients(t, h)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/kids/services", `{"services":["youtube"]}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids/safesearch", `{"engines":["google"]}`)
	require.Equal(t, http.StatusOK, status, body)

	// Clients hold the profile with a restrict, so they leave first.
	status, body = h.do(t, http.MethodDelete, "/api/v1/clients/tablet", "")
	require.Equal(t, http.StatusNoContent, status, body)
	status, body = h.do(t, http.MethodDelete, "/api/v1/clients/console", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodDelete, "/api/v1/profiles/kids", "")
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = h.do(t, http.MethodPut, "/api/v1/profiles/kids", `{}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = h.do(t, http.MethodGet, "/api/v1/profiles/kids/services", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"services":[]`)
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

// redditCatalog is the Reddit entry as AdGuard publishes it, copied from
// HostlistsRegistry's services.json, so the save path is exercised in the
// dialect a running instance really fetches rather than in a hand-written row.
const redditCatalog = `{"blocked_services":[{"id":"reddit","name":"Reddit","group":"social_network","rules":["||reddit.com^","||redditstatic.com^","||redditmail.com^","||redditmedia.com^","||redd.it^"]}],"groups":[{"id":"social_network"}]}`

// TestSavingAServiceBlocksTheDomainsOfItsCatalogRules is the regression net
// for the save path: a store with no enablements, one PUT through the endpoint
// the console saves through, and a real query on the harness's ephemeral DNS
// port. The control queries before the save prove the same names are let
// through until it lands, so this test fails exactly when a saved enable stops
// reaching the live filter.
func TestSavingAServiceBlocksTheDomainsOfItsCatalogRules(t *testing.T) {
	h := startHarness(t)

	catalog, err := services.ParseCatalog([]byte(redditCatalog))
	require.NoError(t, err)
	require.NoError(t, h.database.SaveCatalog(t.Context(), time.Now().UTC(), catalog.Services))

	// Control: nothing blocks these names yet, so both leave for upstream, and
	// a dead upstream answers with a server failure rather than the sinkhole's
	// name error.
	require.NotEqual(t, mdns.RcodeNameError, askFrom(t, "127.0.0.1", "reddit.com.", h.dnsAddress).Rcode)
	require.NotEqual(t, mdns.RcodeNameError, askFrom(t, "127.0.0.1", "redd.it.", h.dnsAddress).Rcode)

	status, body := h.do(t, http.MethodPut, "/api/v1/profiles/default/services", `{"services":["reddit"]}`)
	require.Equal(t, http.StatusOK, status, body)

	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.1", "reddit.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.1", "www.reddit.com.", h.dnsAddress).Rcode)
	require.Equal(t, mdns.RcodeNameError, askFrom(t, "127.0.0.1", "redd.it.", h.dnsAddress).Rcode)
	// A name no rule in the catalog covers still leaves, so the block came from
	// the save and not from something the harness does to every query.
	require.NotEqual(t, mdns.RcodeNameError, askFrom(t, "127.0.0.1", "gitlab.com.", h.dnsAddress).Rcode)
}
