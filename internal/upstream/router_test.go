package upstream_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/upstream"
)

func TestRouterNamesTheFirstMatchingRoute(t *testing.T) {
	router := upstream.NewRouter([]upstream.RouteSpec{
		{ID: 1, Domain: "target.example", Upstream: "by-domain"},
		{ID: 2, Client: "tablet", Upstream: "by-client"},
		{ID: 3, Upstream: "catch-all"},
	})

	require.Equal(t, "by-client", router.Lookup("tablet", "target.example"),
		"a client-scoped route outranks a domain-scoped one")
	require.Equal(t, "by-domain", router.Lookup("laptop", "target.example"))
	require.Equal(t, "catch-all", router.Lookup("laptop", "unrelated.example"))
}

func TestRouterMatchesTheEntryDomainAndEverythingUnderIt(t *testing.T) {
	router := upstream.NewRouter([]upstream.RouteSpec{
		{ID: 1, Domain: "Target.Example", Upstream: "routed"},
	})

	require.Equal(t, "routed", router.Lookup("", "target.example"))
	require.Equal(t, "routed", router.Lookup("", "host.target.example"))
	require.Equal(t, "", router.Lookup("", "notarget.example"),
		"a suffix only matches on a label boundary")
	require.Equal(t, "", router.Lookup("", "example"))
}

func TestRouterPrefersTheDeeperDomain(t *testing.T) {
	router := upstream.NewRouter([]upstream.RouteSpec{
		{ID: 1, Domain: "example", Upstream: "shallow"},
		{ID: 2, Domain: "target.example", Upstream: "deep"},
	})

	require.Equal(t, "deep", router.Lookup("", "host.target.example"))
	require.Equal(t, "shallow", router.Lookup("", "other.example"))
}

func TestRouterBreaksTiesByRowOrder(t *testing.T) {
	router := upstream.NewRouter([]upstream.RouteSpec{
		{ID: 5, Domain: "target.example", Upstream: "later"},
		{ID: 3, Domain: "target.example", Upstream: "earlier"},
	})

	require.Equal(t, "earlier", router.Lookup("", "target.example"))
}
