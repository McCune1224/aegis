package api_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/store"
)

func TestLeasesAndDiscoveriesReachTheDashboard(t *testing.T) {
	h := startHarness(t)
	ctx := t.Context()

	require.NoError(t, h.database.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))
	require.NoError(t, h.database.SaveClient(ctx, store.Client{Key: "tablet", Profile: "kids"}))
	require.NoError(t, h.database.SaveLease(ctx, store.Lease{
		Address:  netip.MustParseAddr("10.9.9.20"),
		MAC:      mustMAC(t, "AA:BB:CC:DD:EE:01"),
		Client:   "tablet",
		Hostname: "tablet",
		Expires:  time.Now().Add(time.Hour),
	}))
	require.NoError(t, h.database.SaveLease(ctx, store.Lease{
		Address: netip.MustParseAddr("10.9.9.21"),
		MAC:     mustMAC(t, "aa:bb:cc:dd:ee:02"),
		Expires: time.Now().Add(-time.Minute),
	}))
	require.NoError(t, h.database.RecordDiscovery(ctx, store.Discovery{
		MAC:      mustMAC(t, "aa:bb:cc:dd:ee:09"),
		Address:  netip.MustParseAddr("10.9.9.30"),
		Hostname: "phone",
		First:    time.Now().Add(-time.Hour),
		Last:     time.Now(),
	}))

	status, body := h.do(t, http.MethodGet, "/api/v1/leases", "")
	require.Equal(t, http.StatusOK, status)
	var leases struct {
		Leases []map[string]any `json:"leases"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &leases))
	require.Len(t, leases.Leases, 1, "an expired lease is not offered to the dashboard")
	require.Equal(t, "10.9.9.20", leases.Leases[0]["address"])
	require.Equal(t, "aa:bb:cc:dd:ee:01", leases.Leases[0]["mac"])
	require.Equal(t, "tablet", leases.Leases[0]["client"])

	status, body = h.do(t, http.MethodGet, "/api/v1/discoveries", "")
	require.Equal(t, http.StatusOK, status)
	var discoveries struct {
		Discoveries []map[string]any `json:"discoveries"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &discoveries))
	require.Len(t, discoveries.Discoveries, 1)
	require.Equal(t, "aa:bb:cc:dd:ee:09", discoveries.Discoveries[0]["mac"])
	require.Equal(t, "10.9.9.30", discoveries.Discoveries[0]["address"])
	require.Equal(t, "phone", discoveries.Discoveries[0]["hostname"])
}

func TestDismissingADiscoveryRemovesThePrompt(t *testing.T) {
	h := startHarness(t)
	require.NoError(t, h.database.RecordDiscovery(t.Context(), store.Discovery{
		MAC:     mustMAC(t, "aa:bb:cc:dd:ee:09"),
		Address: netip.MustParseAddr("10.9.9.30"),
		Last:    time.Now(),
	}))

	status, _ := h.do(t, http.MethodDelete, "/api/v1/discoveries/aa:bb:cc:dd:ee:09", "")
	require.Equal(t, http.StatusNoContent, status)

	_, body := h.do(t, http.MethodGet, "/api/v1/discoveries", "")
	var discoveries struct {
		Discoveries []map[string]any `json:"discoveries"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &discoveries))
	require.Empty(t, discoveries.Discoveries)
}

func TestADiscoveryWithABadHardwareAddressIsRefused(t *testing.T) {
	h := startHarness(t)
	status, _ := h.do(t, http.MethodDelete, "/api/v1/discoveries/not-a-mac", "")
	require.Equal(t, http.StatusBadRequest, status)
}

func TestAClientCarriesItsHardwareAddressThroughTheAPI(t *testing.T) {
	h := startHarness(t)
	ctx := t.Context()
	require.NoError(t, h.database.SaveProfile(ctx, filter.ProfileSpec{ID: "kids"}))

	status, body := h.do(t, http.MethodPut, "/api/v1/clients/tablet",
		`{"profile":"kids","addresses":[],"macs":["AA:BB:CC:DD:EE:01"],"prefixes":[]}`)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"aa:bb:cc:dd:ee:01"`)

	cfg, err := h.database.Load(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Clients, 1)
	require.Equal(t, []net.HardwareAddr{mustMAC(t, "aa:bb:cc:dd:ee:01")}, cfg.Clients[0].MACs)
}

func mustMAC(t *testing.T, raw string) net.HardwareAddr {
	t.Helper()
	parsed, err := net.ParseMAC(raw)
	require.NoError(t, err)
	return parsed
}
