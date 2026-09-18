package dns_test

import (
	"context"
	"net/netip"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
	"aegis/internal/filter"
	"aegis/internal/rewrite"
)

// stubRewriter delegates to a rewrite table, the seam the runtime fills with
// one snapshot generation.
type stubRewriter struct {
	table *rewrite.Table
}

func (r stubRewriter) Lookup(name filter.Domain) (rewrite.Record, bool) {
	return r.table.Lookup(name)
}

func (r stubRewriter) Reverse(address netip.Addr) (filter.Domain, bool) {
	return r.table.Reverse(address)
}

func rewriterFor(t *testing.T, entries ...[2]string) stubRewriter {
	t.Helper()
	records := make([]rewrite.Record, 0, len(entries))
	for _, entry := range entries {
		record, err := rewrite.Parse(entry[0], entry[1])
		require.NoError(t, err)
		records = append(records, record)
	}
	return stubRewriter{table: rewrite.New(records)}
}

func handlerWithRewriter(t *testing.T, decider dns.Decider, upstream dns.Resolver, rewriter dns.Rewriter) *dns.Handler {
	t.Helper()
	handler, err := dns.NewHandler(dns.Config{Decider: decider, Upstream: upstream, Rewriter: rewriter})
	require.NoError(t, err)
	return handler
}

func cnameOf(t *testing.T, rr mdns.RR) (string, string) {
	t.Helper()
	cname, ok := rr.(*mdns.CNAME)
	require.True(t, ok, "expected a CNAME record, got %T", rr)
	return cname.Hdr.Name, cname.Target
}

func TestHandleAnswersAnAddressRewriteLocallyWithoutTheUpstream(t *testing.T) {
	upstream := &stubResolver{}
	rewriter := rewriterFor(t, [2]string{"home.local", "192.0.2.77"})
	handler := handlerWithRewriter(t, deciderFor(t, defaultPolicy, blockedAds()), upstream, rewriter)

	got, err := handler.Handle(context.Background(), query("home.local.", mdns.TypeA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.MustParseAddr("192.0.2.77").AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
	require.Zero(t, upstream.calls, "an address rewrite is answered without the upstream")
}

func TestHandleAnswersAnAddressRewriteEvenWhenARuleBlocksTheName(t *testing.T) {
	blockedHome := filter.RuleSpec{
		ID:     "block-home",
		Source: filter.Source{ID: "custom", Name: "Custom"},
		Kind:   filter.MatchSubdomains,
		Domain: mustDomain("home.local"),
		Action: filter.ActionBlock,
	}
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy, blockedHome),
		&stubResolver{},
		rewriterFor(t, [2]string{"home.local", "192.0.2.77"}),
	)

	got, err := handler.Handle(context.Background(), query("home.local.", mdns.TypeA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, "192.0.2.77", netip.AddrFrom4([4]byte(got.Answer[0].(*mdns.A).A)).String())
}

func TestHandleAnswersEmptySuccessWhenTheRewriteFamilyDiffers(t *testing.T) {
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy),
		&stubResolver{},
		rewriterFor(t, [2]string{"home.local", "192.0.2.77"}),
	)

	got, err := handler.Handle(context.Background(), query("home.local.", mdns.TypeAAAA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Empty(t, got.Answer)
}

func TestHandleRewritesWildcardSubdomainsButNotTheApex(t *testing.T) {
	upstream := &stubResolver{}
	upstream.answer = upstreamA(query("home.local.", mdns.TypeA), "203.0.113.10")
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy),
		upstream,
		rewriterFor(t, [2]string{"*.home.local", "192.0.2.78"}),
	)

	got, err := handler.Handle(context.Background(), query("a.b.home.local.", mdns.TypeA), netip.Addr{})
	require.NoError(t, err)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.MustParseAddr("192.0.2.78").AsSlice(), []byte(got.Answer[0].(*mdns.A).A))

	apex := query("home.local.", mdns.TypeA)
	_, err = handler.Handle(context.Background(), apex, netip.Addr{})
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls, "the apex is not matched by its wildcard")
}

func TestHandleFollowsANameRewriteAndStillFiltersTheTarget(t *testing.T) {
	blockedHome := filter.RuleSpec{
		ID:     "block-home",
		Source: filter.Source{ID: "custom", Name: "Custom"},
		Kind:   filter.MatchSubdomains,
		Domain: mustDomain("home.local"),
		Action: filter.ActionBlock,
	}
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy, blockedHome),
		&stubResolver{},
		rewriterFor(t, [2]string{"alias.local", "home.local"}),
	)

	got, err := handler.Handle(context.Background(), query("alias.local.", mdns.TypeA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeNameError, got.Rcode, "the rewritten target is still matched against the rules")
	require.Empty(t, got.Answer)
}

func TestHandleFollowsANameRewriteAndForwardsTheTarget(t *testing.T) {
	upstream := &stubResolver{}
	upstream.answer = upstreamA(query("home.local.", mdns.TypeA), "203.0.113.99")
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy),
		upstream,
		rewriterFor(t, [2]string{"alias.local", "home.local"}),
	)

	got, err := handler.Handle(context.Background(), query("alias.local.", mdns.TypeA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 2)
	name, target := cnameOf(t, got.Answer[0])
	require.Equal(t, "alias.local.", name)
	require.Equal(t, "home.local.", target)
	require.Equal(t, netip.MustParseAddr("203.0.113.99").AsSlice(), []byte(got.Answer[1].(*mdns.A).A))
	require.Equal(t, "home.local.", upstream.asked, "the upstream is asked about the target name")
	require.Equal(t, "alias.local.", got.Question[0].Name, "the answer keeps the client's question")
}

func TestHandleAnswersPTRFromAnAddressRewrite(t *testing.T) {
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy),
		&stubResolver{},
		rewriterFor(t, [2]string{"home.local", "192.0.2.77"}),
	)

	got, err := handler.Handle(context.Background(), query("77.2.0.192.in-addr.arpa.", mdns.TypePTR), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, "home.local.", got.Answer[0].(*mdns.PTR).Ptr)
}

func TestHandleRefusesARewriteLoop(t *testing.T) {
	handler := handlerWithRewriter(t,
		deciderFor(t, defaultPolicy),
		&stubResolver{},
		rewriterFor(t, [2]string{"a.local", "b.local"}, [2]string{"b.local", "a.local"}),
	)

	_, err := handler.Handle(context.Background(), query("a.local.", mdns.TypeA), netip.Addr{})

	require.Error(t, err)
}

func TestHandleObservesRewrittenQueries(t *testing.T) {
	observer := &captureObserver{}
	upstream := &stubResolver{}
	handler, err := dns.NewHandler(dns.Config{
		Decider:   deciderFor(t, defaultPolicy),
		Upstream:  upstream,
		Rewriter:  rewriterFor(t, [2]string{"home.local", "192.0.2.77"}),
		Observers: []dns.Observer{observer},
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), query("home.local.", mdns.TypeA), netip.Addr{})
	require.NoError(t, err)

	require.Len(t, observer.decisions, 1)
	require.Equal(t, "192.0.2.77", observer.decisions[0].Rewritten)
}
