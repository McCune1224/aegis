package store_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func tabletRecord() store.Client {
	return store.Client{
		Key:       "tablet",
		Profile:   "default",
		Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")},
		MACs:      hardwareTablet(),
		Prefixes:  []netip.Prefix{netip.MustParsePrefix("10.9.8.0/24")},
	}
}

func hardwareTablet() []net.HardwareAddr {
	mac, err := net.ParseMAC("aa:bb:cc:dd:ee:f0")
	if err != nil {
		panic(err)
	}
	return []net.HardwareAddr{mac}
}

func TestSaveClientRefusesASelectorAnotherClientHolds(t *testing.T) {
	t.Run("saving the holder again keeps its own selectors", func(t *testing.T) {
		s := open(t)
		require.NoError(t, s.SaveClient(t.Context(), tabletRecord()))
		require.NoError(t, s.SaveClient(t.Context(), tabletRecord()))
	})

	for _, tc := range []struct {
		name     string
		pick     func(store.Client) store.Client
		selector string
	}{
		{"address", func(c store.Client) store.Client {
			c.Addresses = tabletRecord().Addresses
			c.MACs = nil
			c.Prefixes = nil
			return c
		}, "10.9.9.2"},
		{"hardware address", func(c store.Client) store.Client {
			c.MACs = hardwareTablet()
			c.Addresses = nil
			c.Prefixes = nil
			return c
		}, "aa:bb:cc:dd:ee:f0"},
		{"prefix", func(c store.Client) store.Client {
			c.Prefixes = tabletRecord().Prefixes
			c.Addresses = nil
			c.MACs = nil
			return c
		}, "10.9.8.0/24"},
	} {
		t.Run("a "+tc.name+" the holder keeps", func(t *testing.T) {
			s := open(t)
			require.NoError(t, s.SaveClient(t.Context(), tabletRecord()))

			phone := tc.pick(store.Client{Key: "phone", Profile: "default"})
			err := s.SaveClient(t.Context(), phone)
			require.ErrorContains(t, err, "tablet")
			require.ErrorContains(t, err, "phone")
			require.ErrorContains(t, err, tc.selector)

			held, err := s.Load(t.Context())
			require.NoError(t, err)
			require.Equal(t, []store.Client{tabletRecord()}, held.Clients, "the refused write changed the holders")
		})
	}
}

func TestApplyDocumentMovesASelectorBothClientsName(t *testing.T) {
	ctx := t.Context()
	s := open(t)
	require.NoError(t, s.SaveClient(ctx, tabletRecord()))

	document, err := store.ParseDocument([]byte(`{"version":1,"clients":[
		{"key":"tablet","profile":"default"},
		{"key":"phone","profile":"default","addresses":["10.9.9.2"]}
	]}`))
	require.NoError(t, err)
	require.NoError(t, s.ApplyDocument(ctx, document))

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, []store.Client{
		{Key: "phone", Profile: "default", Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.2")}},
		{Key: "tablet", Profile: "default"},
	}, cfg.Clients)
}

func TestApplyDocumentRefusesASelectorAnUnnamedClientHolds(t *testing.T) {
	ctx := t.Context()
	s := open(t)
	require.NoError(t, s.SaveClient(ctx, tabletRecord()))
	before, err := s.ReadDocument(ctx)
	require.NoError(t, err)

	document, err := store.ParseDocument([]byte(`{"version":1,"clients":[
		{"key":"phone","profile":"default","addresses":["10.9.9.2"]}
	]}`))
	require.NoError(t, err)
	err = s.ApplyDocument(ctx, document)
	require.ErrorContains(t, err, "tablet")
	require.ErrorContains(t, err, "phone")

	after, err := s.ReadDocument(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "the refused import changed the stored configuration")

	cfg, err := s.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}
