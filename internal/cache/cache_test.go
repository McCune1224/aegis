package cache_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/cache"
)

// The time lives in an atomic because the prefetch refresh reads the clock
// from its own goroutine while the test advances it.
type clock struct{ nanos atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.nanos.Store(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC).UnixNano())
	return c
}

func (c *clock) Now() time.Time { return time.Unix(0, c.nanos.Load()).UTC() }

func (c *clock) Advance(d time.Duration) { c.nanos.Add(int64(d)) }

// stubUpstream counts Resolve calls. With no answer set it replies to each A
// query with a distinct address, so a cached answer is distinguishable from a
// fresh fetch even if the counter alone would not show it. The counter is
// atomic because concurrent cold misses call it from several goroutines.
type stubUpstream struct {
	calls  atomic.Int64
	answer func(req *mdns.Msg) *mdns.Msg
	err    error
}

func (s *stubUpstream) saw() int { return int(s.calls.Load()) }

func (s *stubUpstream) Resolve(_ context.Context, req *mdns.Msg, _ string) (*mdns.Msg, error) {
	n := int(s.calls.Add(1))
	if s.err != nil {
		return nil, s.err
	}
	if s.answer != nil {
		return s.answer(req), nil
	}
	switch req.Question[0].Qtype {
	case mdns.TypeAAAA:
		return aaaaAnswer(req, n), nil
	default:
		return aAnswer(req, n, 300), nil
	}
}

func query(name string, qtype uint16) *mdns.Msg {
	req := new(mdns.Msg).SetQuestion(name, qtype)
	req.Id = 4242
	return req
}

func aAnswer(req *mdns.Msg, n int, ttl uint32) *mdns.Msg {
	resp := new(mdns.Msg)
	resp.SetReply(req)
	resp.Answer = append(resp.Answer, &mdns.A{
		Hdr: mdns.RR_Header{Name: req.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: ttl},
		A:   netip.MustParseAddr(fmt.Sprintf("203.0.113.%d", n)).AsSlice(),
	})
	return resp
}

func aaaaAnswer(req *mdns.Msg, n int) *mdns.Msg {
	resp := new(mdns.Msg)
	resp.SetReply(req)
	resp.Answer = append(resp.Answer, &mdns.AAAA{
		Hdr:  mdns.RR_Header{Name: req.Question[0].Name, Rrtype: mdns.TypeAAAA, Class: mdns.ClassINET, Ttl: 300},
		AAAA: netip.MustParseAddr(fmt.Sprintf("2001:db8::%d", n)).AsSlice(),
	})
	return resp
}

// nxAnswer is the negative answer RFC 2308 describes: an NXDOMAIN whose SOA
// carries the negative TTL, here 30 seconds inside a 60 second header.
func nxAnswer(req *mdns.Msg) *mdns.Msg {
	resp := new(mdns.Msg)
	resp.SetRcode(req, mdns.RcodeNameError)
	resp.Ns = append(resp.Ns, &mdns.SOA{
		Hdr:    mdns.RR_Header{Name: "example.net.", Rrtype: mdns.TypeSOA, Class: mdns.ClassINET, Ttl: 60},
		Ns:     "ns.example.net.",
		Mbox:   "hostmaster.example.net.",
		Serial: 1,
		Minttl: 30,
	})
	return resp
}

func servfailAnswer(req *mdns.Msg) *mdns.Msg {
	resp := new(mdns.Msg)
	return resp.SetRcode(req, mdns.RcodeServerFailure)
}

func requireA(t *testing.T, msg *mdns.Msg, want string) {
	t.Helper()
	require.Len(t, msg.Answer, 1)
	a, ok := msg.Answer[0].(*mdns.A)
	require.True(t, ok)
	require.Equal(t, want, netip.AddrFrom4([4]byte{a.A[0], a.A[1], a.A[2], a.A[3]}).String())
}

func newResolver(t *testing.T, upstream cache.Upstream, tune func(*cache.Config)) *cache.Cache {
	t.Helper()
	cfg := cache.Config{Upstream: upstream, Now: newClock().Now}
	if tune != nil {
		tune(&cfg)
	}
	resolver, err := cache.New(cfg)
	require.NoError(t, err)
	return resolver
}

func TestResolveAnswersARepeatFromTheCacheAndTheUpstreamSeesOneRequest(t *testing.T) {
	upstream := &stubUpstream{}
	resolver := newResolver(t, upstream, nil)

	first, err := resolver.Resolve(context.Background(), query("allowed.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw())
	requireA(t, first, "203.0.113.1")

	second, err := resolver.Resolve(context.Background(), query("allowed.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw(), "the repeat must come from the cache")
	requireA(t, second, "203.0.113.1")
	require.Equal(t, uint16(4242), second.Id)
	require.Equal(t, "allowed.example.net.", second.Question[0].Name)
	require.True(t, second.Response)
}

func TestResolveServesTheCachedNegativeAnswer(t *testing.T) {
	upstream := &stubUpstream{answer: nxAnswer}
	resolver := newResolver(t, upstream, nil)

	for i := 0; i < 2; i++ {
		resp, err := resolver.Resolve(context.Background(), query("gone.example.net.", mdns.TypeA), "")
		require.NoError(t, err)
		require.Equal(t, mdns.RcodeNameError, resp.Rcode)
		require.Len(t, resp.Ns, 1, "the SOA reaches the client so it can cache the negative too")
		_, isSOA := resp.Ns[0].(*mdns.SOA)
		require.True(t, isSOA)
	}
	require.Equal(t, 1, upstream.saw(), "the negative answer must be cached")
}

func TestResolveDecaysTheCachedTtlByAge(t *testing.T) {
	upstream := &stubUpstream{}
	fake := newClock()
	resolver := newResolver(t, upstream, func(cfg *cache.Config) { cfg.Now = fake.Now })

	_, err := resolver.Resolve(context.Background(), query("aged.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw())

	fake.Advance(40 * time.Second)
	aged, err := resolver.Resolve(context.Background(), query("aged.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw())
	require.Equal(t, uint32(260), aged.Answer[0].Header().Ttl)

	fake.Advance(300 * time.Second)
	_, err = resolver.Resolve(context.Background(), query("aged.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 2, upstream.saw(), "an expired entry asks upstream again")
}

func TestResolveClampsTheTtlToTheFloor(t *testing.T) {
	upstream := &stubUpstream{answer: func(req *mdns.Msg) *mdns.Msg { return aAnswer(req, 1, 1) }}
	fake := newClock()
	resolver := newResolver(t, upstream, func(cfg *cache.Config) {
		cfg.Now = fake.Now
		cfg.MinTTL = 5 * time.Second
	})

	_, err := resolver.Resolve(context.Background(), query("churn.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw())

	fake.Advance(2 * time.Second)
	early, err := resolver.Resolve(context.Background(), query("churn.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw(), "the floor holds a one second answer for five seconds")
	require.Equal(t, uint32(3), early.Answer[0].Header().Ttl)

	fake.Advance(4 * time.Second)
	_, err = resolver.Resolve(context.Background(), query("churn.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 2, upstream.saw(), "expiry follows the clamped TTL, not the upstream one")
}

func TestResolveClampsTheTtlToTheCeiling(t *testing.T) {
	upstream := &stubUpstream{answer: func(req *mdns.Msg) *mdns.Msg { return aAnswer(req, 1, 86400) }}
	fake := newClock()
	resolver := newResolver(t, upstream, func(cfg *cache.Config) {
		cfg.Now = fake.Now
		cfg.MaxTTL = time.Hour
	})

	_, err := resolver.Resolve(context.Background(), query("stable.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw())

	fake.Advance(3599 * time.Second)
	late, err := resolver.Resolve(context.Background(), query("stable.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw())
	require.Equal(t, uint32(1), late.Answer[0].Header().Ttl)

	fake.Advance(2 * time.Second)
	_, err = resolver.Resolve(context.Background(), query("stable.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 2, upstream.saw(), "a day-long TTL still expires at the ceiling")
}

func TestResolveNeverCachesServfail(t *testing.T) {
	upstream := &stubUpstream{answer: servfailAnswer}
	resolver := newResolver(t, upstream, nil)

	for i := 0; i < 2; i++ {
		resp, err := resolver.Resolve(context.Background(), query("down.example.net.", mdns.TypeA), "")
		require.NoError(t, err)
		require.Equal(t, mdns.RcodeServerFailure, resp.Rcode)
	}
	require.Equal(t, 2, upstream.saw())
}

func TestResolveNeverCachesAnError(t *testing.T) {
	upstream := &stubUpstream{err: errors.New("upstream unreachable")}
	resolver := newResolver(t, upstream, nil)

	_, err := resolver.Resolve(context.Background(), query("dark.example.net.", mdns.TypeA), "")
	require.Error(t, err)
	require.Equal(t, 1, upstream.saw())

	_, err = resolver.Resolve(context.Background(), query("dark.example.net.", mdns.TypeA), "")
	require.Error(t, err)
	require.Equal(t, 2, upstream.saw())
}

func TestResolveKeysOnNameAndTypeCaseInsensitively(t *testing.T) {
	upstream := &stubUpstream{}
	resolver := newResolver(t, upstream, nil)

	_, err := resolver.Resolve(context.Background(), query("Mixed.Case.Example.NET.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), query("mixed.case.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw(), "the name key ignores case")

	_, err = resolver.Resolve(context.Background(), query("mixed.case.example.net.", mdns.TypeAAAA), "")
	require.NoError(t, err)
	require.Equal(t, 2, upstream.saw(), "each record type holds its own entry")
}

func TestResolveEvictsTheLeastRecentlyUsedEntry(t *testing.T) {
	upstream := &stubUpstream{}
	resolver := newResolver(t, upstream, func(cfg *cache.Config) { cfg.MaxEntries = 2 })

	_, err := resolver.Resolve(context.Background(), query("a.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), query("b.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), query("a.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 2, upstream.saw(), "a was touched, so a stays")

	_, err = resolver.Resolve(context.Background(), query("c.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 3, upstream.saw())

	_, err = resolver.Resolve(context.Background(), query("a.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 3, upstream.saw(), "a survived the eviction")

	_, err = resolver.Resolve(context.Background(), query("b.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 4, upstream.saw(), "b was the least recently used and was evicted")
}

func TestConcurrentResolveSharesEntries(t *testing.T) {
	upstream := &stubUpstream{}
	resolver := newResolver(t, upstream, nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				resp, err := resolver.Resolve(ctx, query(fmt.Sprintf("host%d.example.net.", i%3), mdns.TypeA), "")
				require.NoError(t, err)
				a, ok := resp.Answer[0].(*mdns.A)
				require.True(t, ok)
				address := netip.AddrFrom4([4]byte{a.A[0], a.A[1], a.A[2], a.A[3]})
				require.True(t, netip.MustParsePrefix("203.0.113.0/24").Contains(address),
					"every answer comes from the stub's block")
			}
		}()
	}
	wg.Wait()
	require.Greater(t, upstream.saw(), 0)
}

func TestResolveCachesAZeroTTLAnswerAtTheFloor(t *testing.T) {
	upstream := &stubUpstream{answer: func(req *mdns.Msg) *mdns.Msg { return aAnswer(req, 1, 0) }}
	resolver := newResolver(t, upstream, nil)

	_, err := resolver.Resolve(context.Background(), query("zero.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), query("zero.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.saw(), "the floor holds a zero-TTL answer for five seconds")
}

func TestResolveDoesNotCacheAnEmptyAnswerWithoutASOA(t *testing.T) {
	upstream := &stubUpstream{answer: func(req *mdns.Msg) *mdns.Msg {
		resp := new(mdns.Msg)
		return resp.SetReply(req)
	}}
	resolver := newResolver(t, upstream, nil)

	for i := 0; i < 2; i++ {
		_, err := resolver.Resolve(context.Background(), query("nodata.example.net.", mdns.TypeA), "")
		require.NoError(t, err)
	}
	require.Equal(t, 2, upstream.saw(), "no answer records and no SOA is no guidance, so nothing is cached")
}

func TestResolveKeysOnClass(t *testing.T) {
	upstream := &stubUpstream{}
	resolver := newResolver(t, upstream, nil)

	ch := query("chaos.example.net.", mdns.TypeA)
	ch.Question[0].Qclass = mdns.ClassCHAOS

	_, err := resolver.Resolve(context.Background(), query("chaos.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), ch, "")
	require.NoError(t, err)
	require.Equal(t, 2, upstream.saw(), "each class holds its own entry")
}

func TestResolveKeepsTheSlotWhenTheUpstreamEchoesAnotherName(t *testing.T) {
	upstream := &stubUpstream{answer: func(req *mdns.Msg) *mdns.Msg {
		resp := aAnswer(req, 1, 300)
		resp.Question[0].Name = "wrong.example.net."
		return resp
	}}
	resolver := newResolver(t, upstream, func(cfg *cache.Config) { cfg.MaxEntries = 1 })

	_, err := resolver.Resolve(context.Background(), query("right.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), query("other.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), query("right.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	require.Equal(t, 3, upstream.saw(), "the evicted slot must not survive under its request key")
}

func TestNewRejectsAConfigWithoutAnUpstream(t *testing.T) {
	_, err := cache.New(cache.Config{})
	require.Error(t, err)

	_, err = cache.New(cache.Config{Upstream: &stubUpstream{}, MinTTL: time.Hour, MaxTTL: time.Minute})
	require.Error(t, err)
}
