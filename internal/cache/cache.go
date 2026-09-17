// Package cache answers a repeated allowed query from memory instead of the
// upstream resolver. It sits between the DNS handler and the forwarder, so it
// only ever sees names the filter allowed.
package cache

import (
	"container/list"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
)

// Defaults hold the cache to answers that go stale within an hour and keep a
// zero-TTL answer from hammering the upstream on every query.
const (
	DefaultMinTTL     = 5 * time.Second
	DefaultMaxTTL     = time.Hour
	DefaultMaxEntries = 4096
)

// Upstream answers a query on behalf of the cache. It is the same shape the
// DNS handler calls a Resolver, declared here so this package does not import
// the server.
type Upstream interface {
	Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error)
}

// Config is what a Cache needs. Zero bounds fall to the defaults, so wiring
// the cache in takes only an Upstream.
type Config struct {
	Upstream   Upstream
	MaxEntries int
	MinTTL     time.Duration
	MaxTTL     time.Duration
	// Now names the wall clock, so a test can state the moment it asks about.
	Now func() time.Time
}

// key identifies one cached answer. DNS names are case-insensitive, so the
// name is stored lowercased. The class and type are part of the slot: the
// same name in IN and CH is a different question.
type key struct {
	name   string
	qclass uint16
	qtype  uint16
}

// entry is one stored upstream answer. resp is a private template whose ID and
// question are rewritten on every hit. ttl is the clamped TTL the entry lives
// for, not the upstream's raw value.
type entry struct {
	k       key
	resp    *mdns.Msg
	ttl     uint32
	stored  time.Time
	expires time.Time
}

// Cache is an LRU of upstream answers bounded by MaxEntries. One mutex covers
// the table and the LRU order, so the hit path is a map lookup and a list
// move.
type Cache struct {
	upstream Upstream
	now      func() time.Time
	minTTL   time.Duration
	maxTTL   time.Duration
	bound    int

	mu    sync.Mutex
	items map[key]*list.Element
	lru   *list.List
}

// New checks the config and returns a Cache.
func New(cfg Config) (*Cache, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("cache: Config.Upstream is required")
	}
	if cfg.MinTTL < 0 || cfg.MaxTTL < 0 || cfg.MaxEntries < 0 {
		return nil, errors.New("cache: Config bounds are negative")
	}
	if cfg.MaxTTL != 0 && cfg.MinTTL > cfg.MaxTTL {
		return nil, errors.New("cache: Config.MinTTL is past MaxTTL")
	}
	minTTL, maxTTL := cfg.MinTTL, cfg.MaxTTL
	if minTTL == 0 {
		minTTL = DefaultMinTTL
	}
	if maxTTL == 0 {
		maxTTL = DefaultMaxTTL
	}
	bound := cfg.MaxEntries
	if bound == 0 {
		bound = DefaultMaxEntries
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Cache{
		upstream: cfg.Upstream,
		now:      now,
		minTTL:   minTTL,
		maxTTL:   maxTTL,
		bound:    bound,
		items:    make(map[key]*list.Element, bound),
		lru:      list.New(),
	}, nil
}

// Resolve answers req from the cache when it holds a live entry for the
// question's name and type, and asks the upstream once otherwise. A cacheable
// answer is stored under the clamped TTL. An error response is never stored,
// so the next query retries the upstream.
func (c *Cache) Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	if len(req.Question) != 1 {
		return c.upstream.Resolve(ctx, req)
	}
	question := req.Question[0]
	k := key{name: strings.ToLower(question.Name), qclass: question.Qclass, qtype: question.Qtype}

	c.mu.Lock()
	if el, ok := c.items[k]; ok {
		e := el.Value.(*entry)
		now := c.now()
		if now.Before(e.expires) {
			c.lru.MoveToFront(el)
			c.mu.Unlock()
			return e.reply(req, now), nil
		}
		c.lru.Remove(el)
		delete(c.items, k)
	}
	c.mu.Unlock()

	resp, err := c.upstream.Resolve(ctx, req)
	if err != nil {
		return nil, err
	}
	if e := storable(resp, k, c.minTTL, c.maxTTL, c.now()); e != nil {
		c.mu.Lock()
		c.replace(k, e)
		c.mu.Unlock()
	}
	return resp, nil
}

// replace inserts the entry at the front of the LRU and evicts from the back
// while the table is over its bound. An entry replacing itself frees its old
// element first, so the bound still holds.
func (c *Cache) replace(k key, e *entry) {
	if el, ok := c.items[k]; ok {
		c.lru.Remove(el)
		delete(c.items, k)
	}
	c.items[k] = c.lru.PushFront(e)
	for c.lru.Len() > c.bound {
		oldest := c.lru.Back()
		if oldest == nil {
			return
		}
		delete(c.items, oldest.Value.(*entry).k)
		c.lru.Remove(oldest)
	}
}

// storable reads the TTL guidance out of an upstream answer and returns the
// entry to cache under the requesting slot k, or nil when the answer has to
// be asked for again. The slot comes from the request, not the response, so a
// broken upstream that echoes another name cannot move the entry out from
// under its LRU element. A positive answer lives for the shortest answer TTL.
// A negative answer lives for the SOA's negative TTL (RFC 2308), the shorter
// of the SOA header TTL and the SOA minimum. Guidance of zero is real
// guidance and takes the floor; an answer with no guidance at all (no answer
// records and no SOA), including every rcode that is not a success or a name
// error, returns nil.
func storable(resp *mdns.Msg, k key, minTTL, maxTTL time.Duration, now time.Time) *entry {
	var ttl time.Duration
	var guided bool
	switch resp.Rcode {
	case mdns.RcodeSuccess:
		if len(resp.Answer) > 0 {
			ttl, guided = shortestTTL(resp.Answer), true
		} else {
			ttl, guided = negativeTTL(resp.Ns)
		}
	case mdns.RcodeNameError:
		ttl, guided = negativeTTL(resp.Ns)
	default:
		return nil
	}
	if !guided {
		return nil
	}
	if ttl < minTTL {
		ttl = minTTL
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}

	stored := resp.Copy()
	// The extra section carries this upstream's EDNS state, which has no
	// meaning on a replay to another client.
	stored.Extra = nil
	seconds := uint32(ttl / time.Second)
	setTTL(stored.Answer, seconds)
	setTTL(stored.Ns, seconds)
	for _, rr := range stored.Ns {
		if soa, ok := rr.(*mdns.SOA); ok {
			// A stub caching this answer for itself takes the negative TTL
			// from Minttl, so it moves with the clamp.
			soa.Minttl = seconds
		}
	}
	return &entry{
		k:       k,
		resp:    stored,
		ttl:     seconds,
		stored:  now,
		expires: now.Add(ttl),
	}
}

func setTTL(rrs []mdns.RR, ttl uint32) {
	for _, rr := range rrs {
		rr.Header().Ttl = ttl
	}
}

func shortestTTL(rrs []mdns.RR) time.Duration {
	shortest := time.Duration(0)
	for _, rr := range rrs {
		ttl := time.Duration(rr.Header().Ttl) * time.Second
		if shortest == 0 || ttl < shortest {
			shortest = ttl
		}
	}
	return shortest
}

func negativeTTL(authority []mdns.RR) (time.Duration, bool) {
	for _, rr := range authority {
		soa, ok := rr.(*mdns.SOA)
		if !ok {
			continue
		}
		header := time.Duration(soa.Hdr.Ttl) * time.Second
		minimum := time.Duration(soa.Minttl) * time.Second
		if minimum < header {
			return minimum, true
		}
		return header, true
	}
	return 0, false
}

// reply builds the answer for one client from the stored template: the
// incoming ID and question, the stored rcode and sections, and every TTL
// decayed by the entry's age. A hit is only served before expires, so a
// decayed TTL never reaches zero.
func (e *entry) reply(req *mdns.Msg, now time.Time) *mdns.Msg {
	out := e.resp.Copy()
	out.SetRcode(req, e.resp.Rcode)
	age := uint32(0)
	if elapsed := now.Sub(e.stored); elapsed > 0 {
		age = uint32(elapsed / time.Second)
	}
	decay := func(rrs []mdns.RR) {
		for _, rr := range rrs {
			rr.Header().Ttl -= age
		}
	}
	decay(out.Answer)
	decay(out.Ns)
	return out
}
