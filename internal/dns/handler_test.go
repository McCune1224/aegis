package dns_test

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
	"aegis/internal/filter"
)

type stubResolver struct {
	answer *mdns.Msg
	err    error
	calls  int
}

func (s *stubResolver) Resolve(context.Context, *mdns.Msg) (*mdns.Msg, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.answer, nil
}

// clients stands in for identity resolution. An address missing from the map is
// unidentified, which takes the default profile.
type clients map[netip.Addr]filter.ClientKey

func (c clients) Key(address netip.Addr) filter.ClientKey { return c[address] }

func mustDomain(name string) filter.Domain {
	domain, err := filter.ParseDomain(name)
	if err != nil {
		panic(err)
	}
	return domain
}

func modePtr(mode filter.BlockingMode) *filter.BlockingMode { return &mode }

func customPtr(address netip.Addr) *netip.Addr {
	if !address.IsValid() {
		return nil
	}
	return &address
}

// defaultPolicy is what a test that only cares about rules gets.
var defaultPolicy = filter.Policy{Mode: filter.NXDomain}

// stubDecider pairs one rule set with one identity table, which is the shape the
// runtime holds behind a single pointer.
type stubDecider struct {
	set     *filter.RuleSet
	clients clients
}

func (d stubDecider) Decide(name filter.Domain, address netip.Addr) filter.Verdict {
	return d.set.Decide(name, d.clients.Key(address), address)
}

// deciderFor builds a decider whose default profile answers blocked names with
// policy and whose every client is unidentified.
func deciderFor(t *testing.T, policy filter.Policy, specs ...filter.RuleSpec) stubDecider {
	t.Helper()
	return stubDecider{set: compileSet(t, filter.Config{
		Rules: specs,
		Profiles: []filter.ProfileSpec{{
			ID:     "default",
			Mode:   &policy.Mode,
			Custom: customPtr(policy.Custom),
		}},
		Default: "default",
	})}
}

func compileSet(t *testing.T, cfg filter.Config) *filter.RuleSet {
	t.Helper()
	set, err := filter.Compile(cfg)
	require.NoError(t, err)
	return set
}

func handlerFor(t *testing.T, decider dns.Decider, upstream dns.Resolver) *dns.Handler {
	t.Helper()
	handler, err := dns.NewHandler(dns.Config{Decider: decider, Upstream: upstream})
	require.NoError(t, err)
	return handler
}

func blockedAds() filter.RuleSpec {
	return filter.RuleSpec{
		ID:     "block-ads",
		Source: filter.Source{ID: "hagezi", Name: "Hagezi"},
		Kind:   filter.MatchSubdomains,
		Domain: mustDomain("ads.example.com"),
		Action: filter.ActionBlock,
	}
}

func query(name string, qtype uint16) *mdns.Msg {
	return new(mdns.Msg).SetQuestion(name, qtype)
}

func upstreamA(req *mdns.Msg, address string) *mdns.Msg {
	resp := new(mdns.Msg)
	resp.SetReply(req)
	resp.Answer = append(resp.Answer, &mdns.A{
		Hdr: mdns.RR_Header{
			Name:   req.Question[0].Name,
			Rrtype: mdns.TypeA,
			Class:  mdns.ClassINET,
			Ttl:    300,
		},
		A: netip.MustParseAddr(address).AsSlice(),
	})
	return resp
}

func TestHandleAnswersNxDomainForABlockedNameAndStillForwardsTheOther(t *testing.T) {
	upstream := &stubResolver{}
	handler := handlerFor(t, deciderFor(t, defaultPolicy, blockedAds()), upstream)

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.Addr{})
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeNameError, got.Rcode)
	require.Empty(t, got.Answer)
	require.Zero(t, upstream.calls)

	allowed := query("example.com.", mdns.TypeA)
	upstream.answer = upstreamA(allowed, "203.0.113.10")
	forwarded, err := handler.Handle(context.Background(), allowed, netip.Addr{})
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, mdns.RcodeSuccess, forwarded.Rcode)
}

func TestHandleAnswersTheNullAddressForABlockedName(t *testing.T) {
	handler := handlerFor(t,
		deciderFor(t, filter.Policy{Mode: filter.NullAddress}, blockedAds()),
		&stubResolver{})

	v4, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.Addr{})
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, v4.Rcode)
	require.Len(t, v4.Answer, 1)
	require.Equal(t, netip.IPv4Unspecified().AsSlice(), []byte(v4.Answer[0].(*mdns.A).A))

	v6, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeAAAA), netip.Addr{})
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, v6.Rcode)
	require.Len(t, v6.Answer, 1)
	require.Equal(t, netip.IPv6Unspecified().AsSlice(), []byte(v6.Answer[0].(*mdns.AAAA).AAAA))
}

func TestHandleAnswersAConfiguredAddressForABlockedName(t *testing.T) {
	custom := netip.MustParseAddr("192.0.2.7")
	handler := handlerFor(t,
		deciderFor(t, filter.Policy{Mode: filter.CustomAddress, Custom: custom}, blockedAds()),
		&stubResolver{})

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, custom.AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
}

func TestHandleAnswersRefusedForABlockedName(t *testing.T) {
	handler := handlerFor(t,
		deciderFor(t, filter.Policy{Mode: filter.Refused}, blockedAds()),
		&stubResolver{})

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeRefused, got.Rcode)
}

func TestHandleUsesThePolicyOfTheClientThatAsked(t *testing.T) {
	tablet := netip.MustParseAddr("10.9.9.2")
	laptop := netip.MustParseAddr("10.9.9.3")

	set := compileSet(t, filter.Config{
		Rules: []filter.RuleSpec{blockedAds()},
		Profiles: []filter.ProfileSpec{
			{ID: "strict", Mode: modePtr(filter.Refused)},
			{ID: "loose", Mode: modePtr(filter.NullAddress)},
		},
		Clients: []filter.ClientSpec{
			{Key: "tablet", Profile: "strict"},
			{Key: "laptop", Profile: "loose"},
		},
		Default: "loose",
	})

	handler := handlerFor(t, stubDecider{set: set, clients: clients{tablet: "tablet", laptop: "laptop"}}, &stubResolver{})

	fromTablet, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), tablet)
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeRefused, fromTablet.Rcode)

	fromLaptop, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), laptop)
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, fromLaptop.Rcode)
	require.Len(t, fromLaptop.Answer, 1)
	require.Equal(t, netip.IPv4Unspecified().AsSlice(), []byte(fromLaptop.Answer[0].(*mdns.A).A))
}

func TestHandleUsesTheDefaultPolicyForAnUnidentifiedClient(t *testing.T) {
	set := compileSet(t, filter.Config{
		Rules: []filter.RuleSpec{blockedAds()},
		Profiles: []filter.ProfileSpec{
			{ID: "strict", Mode: modePtr(filter.Refused)},
			{ID: "default", Mode: modePtr(filter.NullAddress)},
		},
		Clients: []filter.ClientSpec{{Key: "tablet", Profile: "strict"}},
		Default: "default",
	})

	handler := handlerFor(t, stubDecider{set: set, clients: clients{netip.MustParseAddr("10.9.9.2"): "tablet"}}, &stubResolver{})

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.MustParseAddr("10.9.9.9"))

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, netip.IPv4Unspecified().AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
}

func TestHandleForwardsAnAllowedChildOfABlockedName(t *testing.T) {
	upstream := &stubResolver{}
	decider := deciderFor(t, defaultPolicy,
		blockedAds(),
		filter.RuleSpec{
			ID:     "allow-news",
			Source: filter.Source{ID: "local", Name: "Local"},
			Kind:   filter.MatchSubdomains,
			Domain: mustDomain("news.ads.example.com"),
			Action: filter.ActionAllow,
		},
	)
	handler := handlerFor(t, decider, upstream)

	stillBlocked, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.Addr{})
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeNameError, stillBlocked.Rcode)

	req := query("news.ads.example.com.", mdns.TypeA)
	upstream.answer = upstreamA(req, "203.0.113.12")
	exempt, err := handler.Handle(context.Background(), req, netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, mdns.RcodeSuccess, exempt.Rcode)
	require.Len(t, exempt.Answer, 1)
}

func TestHandleForwardsANameItCannotParse(t *testing.T) {
	upstream := &stubResolver{}
	handler := handlerFor(t, deciderFor(t, defaultPolicy, blockedAds()), upstream)
	req := query(".", mdns.TypeNS)
	resp := new(mdns.Msg)
	resp.SetReply(req)
	upstream.answer = resp

	got, err := handler.Handle(context.Background(), req, netip.Addr{})

	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
}

func TestHandleAnswersServfailAndReportsWhenTheUpstreamFails(t *testing.T) {
	handler := handlerFor(t, deciderFor(t, defaultPolicy), &stubResolver{err: errors.New("no route to host")})

	got, err := handler.Handle(context.Background(), query("example.com.", mdns.TypeA), netip.Addr{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "no route to host")
	require.Equal(t, mdns.RcodeServerFailure, got.Rcode)
}

func TestHandleSetsRecursionAvailableOnEveryAnswer(t *testing.T) {
	upstream := &stubResolver{}
	handler := handlerFor(t, deciderFor(t, defaultPolicy, blockedAds()), upstream)

	blocked, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.Addr{})
	require.NoError(t, err)
	require.True(t, blocked.RecursionAvailable, "a blocked answer must tell the client we recurse")

	req := query("example.com.", mdns.TypeA)
	upstream.answer = upstreamA(req, "203.0.113.50")
	allowed, err := handler.Handle(context.Background(), req, netip.Addr{})
	require.NoError(t, err)
	require.True(t, allowed.RecursionAvailable, "a forwarded answer must tell the client we recurse")
}

func TestHandlePublishesEachDecisionToTheObserver(t *testing.T) {
	observer := &captureObserver{}
	second := &captureObserver{}
	upstream := &stubResolver{}
	handler, err := dns.NewHandler(dns.Config{
		Decider:   deciderFor(t, defaultPolicy, blockedAds()),
		Upstream:  upstream,
		Observers: []dns.Observer{observer, second},
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA), netip.MustParseAddr("10.9.9.2"))
	require.NoError(t, err)

	allowed := query("example.com.", mdns.TypeA)
	upstream.answer = upstreamA(allowed, "203.0.113.60")
	_, err = handler.Handle(context.Background(), allowed, netip.MustParseAddr("10.9.9.3"))
	require.NoError(t, err)

	require.Len(t, observer.decisions, 2)
	blocked := observer.decisions[0]
	require.Equal(t, filter.ActionBlock, blocked.Action)
	require.Equal(t, "ads.example.com", blocked.Name.String())
	require.Equal(t, netip.MustParseAddr("10.9.9.2"), blocked.Address)
	require.Equal(t, "A", blocked.Type)
	require.NotNil(t, blocked.Match)
	require.Equal(t, "block-ads", blocked.Match.RuleID)

	require.Equal(t, filter.ActionAllow, observer.decisions[1].Action)
	require.Nil(t, observer.decisions[1].Match)

	require.Len(t, second.decisions, 2, "every observer sees every decision")
	require.Equal(t, "A", second.decisions[0].Type)
}

type captureObserver struct {
	mu        sync.Mutex
	decisions []dns.Decision
}

func (c *captureObserver) Observe(decision dns.Decision) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decisions = append(c.decisions, decision)
}

func TestNewHandlerRejectsAnIncompleteConfig(t *testing.T) {
	decider := deciderFor(t, defaultPolicy)
	upstream := &stubResolver{}

	cases := map[string]dns.Config{
		"no decider":  {Upstream: upstream},
		"no upstream": {Decider: decider},
	}
	for name, cfg := range cases {
		_, err := dns.NewHandler(cfg)
		require.Error(t, err, "case=%q", name)
	}
}
