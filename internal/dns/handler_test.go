package dns_test

import (
	"context"
	"errors"
	"net/netip"
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

func mustDomain(name string) filter.Domain {
	domain, err := filter.ParseDomain(name)
	if err != nil {
		panic(err)
	}
	return domain
}

func engineFor(t *testing.T, specs ...filter.RuleSpec) *filter.Engine {
	t.Helper()
	set, err := filter.Compile(specs)
	require.NoError(t, err)
	engine := filter.New()
	engine.Publish(set)
	return engine
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
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: upstream,
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA))
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeNameError, got.Rcode)
	require.Empty(t, got.Answer)
	require.Zero(t, upstream.calls)

	allowed := query("example.com.", mdns.TypeA)
	upstream.answer = upstreamA(allowed, "203.0.113.10")
	forwarded, err := handler.Handle(context.Background(), allowed)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, mdns.RcodeSuccess, forwarded.Rcode)
}

func TestHandleAnswersTheNullAddressForABlockedName(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: &stubResolver{},
		Mode:     dns.NullAddress,
	})
	require.NoError(t, err)

	v4, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA))
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, v4.Rcode)
	require.Len(t, v4.Answer, 1)
	require.Equal(t, netip.IPv4Unspecified().AsSlice(), []byte(v4.Answer[0].(*mdns.A).A))

	v6, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeAAAA))
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, v6.Rcode)
	require.Len(t, v6.Answer, 1)
	require.Equal(t, netip.IPv6Unspecified().AsSlice(), []byte(v6.Answer[0].(*mdns.AAAA).AAAA))
}

func TestHandleAnswersAConfiguredAddressForABlockedName(t *testing.T) {
	custom := netip.MustParseAddr("192.0.2.7")
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: &stubResolver{},
		Mode:     dns.CustomAddress,
		Custom:   custom,
	})
	require.NoError(t, err)

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA))

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
	require.Len(t, got.Answer, 1)
	require.Equal(t, custom.AsSlice(), []byte(got.Answer[0].(*mdns.A).A))
}

func TestHandleAnswersRefusedForABlockedName(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: &stubResolver{},
		Mode:     dns.Refused,
	})
	require.NoError(t, err)

	got, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA))

	require.NoError(t, err)
	require.Equal(t, mdns.RcodeRefused, got.Rcode)
}

func TestHandleForwardsAnAllowedChildOfABlockedName(t *testing.T) {
	upstream := &stubResolver{}
	engine := engineFor(t,
		blockedAds(),
		filter.RuleSpec{
			ID:     "allow-news",
			Source: filter.Source{ID: "local", Name: "Local"},
			Kind:   filter.MatchSubdomains,
			Domain: mustDomain("news.ads.example.com"),
			Action: filter.ActionAllow,
		},
	)
	handler, err := dns.NewHandler(dns.Config{Engine: engine, Upstream: upstream, Mode: dns.NXDomain})
	require.NoError(t, err)

	stillBlocked, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA))
	require.NoError(t, err)
	require.Equal(t, mdns.RcodeNameError, stillBlocked.Rcode)

	req := query("news.ads.example.com.", mdns.TypeA)
	upstream.answer = upstreamA(req, "203.0.113.12")
	exempt, err := handler.Handle(context.Background(), req)

	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, mdns.RcodeSuccess, exempt.Rcode)
	require.Len(t, exempt.Answer, 1)
}

func TestHandleForwardsANameItCannotParse(t *testing.T) {
	upstream := &stubResolver{}
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: upstream,
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)
	req := query(".", mdns.TypeNS)
	resp := new(mdns.Msg)
	resp.SetReply(req)
	upstream.answer = resp

	got, err := handler.Handle(context.Background(), req)

	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, mdns.RcodeSuccess, got.Rcode)
}

func TestHandleAnswersServfailAndReportsWhenTheUpstreamFails(t *testing.T) {
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t),
		Upstream: &stubResolver{err: errors.New("no route to host")},
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	got, err := handler.Handle(context.Background(), query("example.com.", mdns.TypeA))

	require.Error(t, err)
	require.Contains(t, err.Error(), "no route to host")
	require.Equal(t, mdns.RcodeServerFailure, got.Rcode)
}

func TestNewHandlerRejectsAnIncompleteConfig(t *testing.T) {
	engine := engineFor(t)
	upstream := &stubResolver{}
	custom := netip.MustParseAddr("192.0.2.7")

	cases := map[string]dns.Config{
		"no engine":                    {Upstream: upstream},
		"no upstream":                  {Engine: engine},
		"custom mode needs an address": {Engine: engine, Upstream: upstream, Mode: dns.CustomAddress},
		"an address needs custom mode": {Engine: engine, Upstream: upstream, Mode: dns.NXDomain, Custom: custom},
		"unknown mode":                 {Engine: engine, Upstream: upstream, Mode: dns.BlockingMode(99)},
	}
	for name, cfg := range cases {
		_, err := dns.NewHandler(cfg)
		require.Error(t, err, "case=%q", name)
	}
}

func TestParseBlockingModeRoundTripsWithString(t *testing.T) {
	for _, name := range []string{"nxdomain", "null-address", "custom-address", "refused"} {
		mode, err := dns.ParseBlockingMode(name)
		require.NoError(t, err, "name=%q", name)
		require.Equal(t, name, mode.String())
	}
}

func TestParseBlockingModeRejectsAnUnknownName(t *testing.T) {
	_, err := dns.ParseBlockingMode("drop")

	require.Error(t, err)
}

func TestHandleSetsRecursionAvailableOnEveryAnswer(t *testing.T) {
	upstream := &stubResolver{}
	handler, err := dns.NewHandler(dns.Config{
		Engine:   engineFor(t, blockedAds()),
		Upstream: upstream,
		Mode:     dns.NXDomain,
	})
	require.NoError(t, err)

	blocked, err := handler.Handle(context.Background(), query("ads.example.com.", mdns.TypeA))
	require.NoError(t, err)
	require.True(t, blocked.RecursionAvailable, "a blocked answer must tell the client we recurse")

	req := query("example.com.", mdns.TypeA)
	upstream.answer = upstreamA(req, "203.0.113.50")
	allowed, err := handler.Handle(context.Background(), req)
	require.NoError(t, err)
	require.True(t, allowed.RecursionAvailable, "a forwarded answer must tell the client we recurse")
}
