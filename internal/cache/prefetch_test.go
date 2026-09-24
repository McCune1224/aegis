package cache_test

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/cache"
)

// prefetchStub is an upstream for the prefetch tests. The call count is
// atomic because the background refresh rings it from its own goroutine.
// arrived carries one ring per call, dropped when full, so a test waits for
// a call to start instead of sleeping for it. routes records the route each
// call came in with.
type prefetchStub struct {
	calls  atomic.Int64
	routes struct {
		sync.Mutex
		byCall map[int64]string
	}
	arrived chan struct{}
	behave  func(n int64, req *mdns.Msg) (*mdns.Msg, error)
}

func newPrefetchStub() *prefetchStub {
	s := &prefetchStub{arrived: make(chan struct{}, 16)}
	s.routes.byCall = map[int64]string{}
	return s
}

func (s *prefetchStub) saw() int { return int(s.calls.Load()) }

func (s *prefetchStub) routeOf(n int64) string {
	s.routes.Lock()
	defer s.routes.Unlock()
	return s.routes.byCall[n]
}

func (s *prefetchStub) Resolve(_ context.Context, req *mdns.Msg, route string) (*mdns.Msg, error) {
	n := s.calls.Add(1)
	s.routes.Lock()
	s.routes.byCall[n] = route
	s.routes.Unlock()
	select {
	case s.arrived <- struct{}{}:
	default:
	}
	if s.behave != nil {
		return s.behave(n, req)
	}
	return aAnswer(req, int(n), 300), nil
}

func (s *prefetchStub) waitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-s.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("the query never reached the upstream")
	}
}

// drainArrived drops the rings of calls the test already accounted for, so a
// later waitArrived waits for the next one.
func (s *prefetchStub) drainArrived() {
	for {
		select {
		case <-s.arrived:
			continue
		default:
			return
		}
	}
}

// waitArrivedNothing proves the opposite: that no call starts.
func (s *prefetchStub) waitArrivedNothing(t *testing.T) {
	t.Helper()
	select {
	case <-s.arrived:
		t.Fatal("an upstream exchange started that must not")
	case <-time.After(250 * time.Millisecond):
	}
}

func newPrefetchResolver(t *testing.T, upstream cache.Upstream, tune func(*cache.Config)) *cache.Cache {
	return newResolver(t, upstream, func(cfg *cache.Config) {
		cfg.Prefetch = true
		if tune != nil {
			tune(cfg)
		}
	})
}

func answeredAddress(t *testing.T, msg *mdns.Msg) string {
	t.Helper()
	require.Len(t, msg.Answer, 1)
	a, ok := msg.Answer[0].(*mdns.A)
	require.True(t, ok)
	return netip.AddrFrom4([4]byte{a.A[0], a.A[1], a.A[2], a.A[3]}).String()
}

func TestStatsCountsHitsMissesAndEvictions(t *testing.T) {
	upstream := &stubUpstream{}
	resolver := newResolver(t, upstream, func(cfg *cache.Config) { cfg.MaxEntries = 2 })
	ctx := context.Background()

	_, err := resolver.Resolve(ctx, query("a.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(ctx, query("b.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(ctx, query("a.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(ctx, query("c.example.net.", mdns.TypeA), "")
	require.NoError(t, err)
	_, err = resolver.Resolve(ctx, query("a.example.net.", mdns.TypeA), "")
	require.NoError(t, err)

	twoQuestions := new(mdns.Msg)
	twoQuestions.Question = append(twoQuestions.Question,
		mdns.Question{Name: "x.example.net.", Qtype: mdns.TypeA, Qclass: mdns.ClassINET},
		mdns.Question{Name: "y.example.net.", Qtype: mdns.TypeA, Qclass: mdns.ClassINET},
	)
	_, err = resolver.Resolve(ctx, twoQuestions, "")
	require.NoError(t, err)

	stats := resolver.Stats()
	require.Equal(t, uint64(2), stats.Hits)
	require.Equal(t, uint64(3), stats.Misses, "the passthrough query is no cache miss")
	require.Equal(t, uint64(1), stats.Evictions, "c evicted b")
	require.Equal(t, uint64(0), stats.Prefetches)
}

func TestPrefetchRefreshesAPopularNameBeforeItExpires(t *testing.T) {
	stub := newPrefetchStub()
	fake := newClock()
	resolver := newPrefetchResolver(t, stub, func(cfg *cache.Config) { cfg.Now = fake.Now })
	ctx := context.Background()
	name := query("popular.example.net.", mdns.TypeA)

	// The entry lives 300 seconds from now.
	first, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.1", answeredAddress(t, first))
	require.Equal(t, 1, stub.saw())
	stub.drainArrived()

	// Asked with just over a fifth of its life left, the entry is still
	// served without arming a refresh: 61 seconds remain against a 60-second
	// prefetch window.
	fake.Advance(239 * time.Second)
	early, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.1", answeredAddress(t, early))
	require.Equal(t, 1, stub.saw(), "the prefetch window is not open yet")

	// Asked with a fifth of its life left, the entry is served from memory
	// while the refresh runs behind it.
	fake.Advance(2 * time.Second)
	near, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.1", answeredAddress(t, near), "the client never waits for the refresh")
	require.Equal(t, 1, stub.saw())
	stub.waitArrived(t)

	// Once the refresh lands, the entry answers with the new address.
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := resolver.Resolve(ctx, name, "")
		require.NoError(t, err)
		if answeredAddress(t, resp) == "203.0.113.2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the prefetch never landed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, 2, stub.saw(), "no second refresh may start")

	// Past the original expiry the refreshed entry answers with no upstream
	// exchange: this is the round trip the popular name never pays. The ask
	// lands between the original expiry (300) and the refreshed entry's own
	// prefetch window (which opens 240 before its expiry).
	fake.Advance(200 * time.Second)
	late, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.2", answeredAddress(t, late))
	require.Equal(t, 2, stub.saw())

	stats := resolver.Stats()
	require.Equal(t, uint64(1), stats.Misses)
	require.GreaterOrEqual(t, stats.Hits, uint64(3))
	require.Equal(t, uint64(1), stats.Prefetches)
}

func TestPrefetchDoesNotStartTwiceWhileOneRefreshIsInFlight(t *testing.T) {
	release := make(chan struct{})
	stub := newPrefetchStub()
	stub.behave = func(n int64, req *mdns.Msg) (*mdns.Msg, error) {
		if n == 1 {
			return aAnswer(req, 1, 300), nil
		}
		<-release
		return aAnswer(req, 2, 300), nil
	}
	fake := newClock()
	resolver := newPrefetchResolver(t, stub, func(cfg *cache.Config) { cfg.Now = fake.Now })
	ctx := context.Background()
	name := query("held.example.net.", mdns.TypeA)

	_, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, 1, stub.saw())
	stub.drainArrived()

	fake.Advance(241 * time.Second)
	for i := 0; i < 3; i++ {
		resp, err := resolver.Resolve(ctx, name, "")
		require.NoError(t, err)
		require.Equal(t, "203.0.113.1", answeredAddress(t, resp))
	}
	stub.waitArrived(t)

	stats := resolver.Stats()
	require.Equal(t, uint64(1), stats.Prefetches, "one refresh in flight arms no second one")
	require.Equal(t, 2, stub.saw())

	// The parked refresh lands at t=250, so the entry lives to 550: past the
	// original 300 the entry would have died at. The final ask sits between
	// that original expiry and the refreshed entry's own window at 490.
	fake.Advance(9 * time.Second)
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := resolver.Resolve(ctx, name, "")
		require.NoError(t, err)
		if answeredAddress(t, resp) == "203.0.113.2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the parked refresh never landed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	fake.Advance(210 * time.Second)
	late, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.2", answeredAddress(t, late))
	require.Equal(t, 2, stub.saw())
	stats = resolver.Stats()
	require.Equal(t, uint64(1), stats.Prefetches)
}

func TestPrefetchKeepsTheOldEntryWhenTheRefreshFails(t *testing.T) {
	stub := newPrefetchStub()
	stub.behave = func(n int64, req *mdns.Msg) (*mdns.Msg, error) {
		if n == 1 {
			return aAnswer(req, 1, 300), nil
		}
		return nil, errors.New("upstream down")
	}
	fake := newClock()
	resolver := newPrefetchResolver(t, stub, func(cfg *cache.Config) { cfg.Now = fake.Now })
	ctx := context.Background()
	name := query("flaky.example.net.", mdns.TypeA)

	_, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	stub.drainArrived()

	fake.Advance(241 * time.Second)
	near, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.1", answeredAddress(t, near))
	stub.waitArrived(t)

	// The failed refresh leaves the stored entry alone until its own expiry.
	// The next ask inside the window rearms the refresh: one try per ask.
	fake.Advance(58 * time.Second)
	still, err := resolver.Resolve(ctx, name, "")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.1", answeredAddress(t, still))
	stub.waitArrived(t)
	require.Equal(t, uint64(2), resolver.Stats().Prefetches)

	// Past that expiry the query goes upstream again and meets the failure:
	// the client's own ask, plus the two background refreshes.
	fake.Advance(2 * time.Second)
	_, err = resolver.Resolve(ctx, name, "")
	require.Error(t, err)
	require.Equal(t, 4, stub.saw())

	stats := resolver.Stats()
	require.Equal(t, uint64(2), stats.Misses)
	require.Equal(t, uint64(2), stats.Hits)
	require.Equal(t, uint64(2), stats.Prefetches)
}

func TestPrefetchRefreshesWithTheRouteTheEntryWasFetchedWith(t *testing.T) {
	stub := newPrefetchStub()
	fake := newClock()
	resolver := newPrefetchResolver(t, stub, func(cfg *cache.Config) { cfg.Now = fake.Now })
	ctx := context.Background()
	name := query("routed.example.net.", mdns.TypeA)

	_, err := resolver.Resolve(ctx, name, "internal")
	require.NoError(t, err)
	require.Equal(t, 1, stub.saw())
	require.Equal(t, "internal", stub.routeOf(1))
	stub.drainArrived()

	fake.Advance(241 * time.Second)
	_, err = resolver.Resolve(ctx, name, "internal")
	require.NoError(t, err)
	stub.waitArrived(t)

	deadline := time.Now().Add(2 * time.Second)
	for stub.saw() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the prefetch never landed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, "internal", stub.routeOf(2),
		"the refresh re-resolves with the route the entry was fetched with")
}

func TestPrefetchDisabledKeepsTheUpstreamQuiet(t *testing.T) {
	stub := newPrefetchStub()
	fake := newClock()
	resolver := newResolver(t, stub, func(cfg *cache.Config) {
		cfg.Now = fake.Now
		cfg.Prefetch = false
	})
	ctx := context.Background()
	const name = "quiet.example.net."

	_, err := resolver.Resolve(ctx, query(name, mdns.TypeA), "")
	require.NoError(t, err)
	stub.drainArrived()

	fake.Advance(241 * time.Second)
	_, err = resolver.Resolve(ctx, query(name, mdns.TypeA), "")
	require.NoError(t, err)
	stub.waitArrivedNothing(t)
	require.Equal(t, 1, stub.saw())
}
