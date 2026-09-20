// Package dns answers DNS queries from the filter engine's verdicts.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	mdns "github.com/miekg/dns"

	"aegis/internal/filter"
	"aegis/internal/rewrite"
)

// localAnswerTTL is how long a client may cache an answer aegis built
// itself, a blocked name or a rewrite. It stays short so that unblocking a
// name or removing a rewrite takes effect without waiting out a long cache.
const localAnswerTTL = 60

// Resolver answers a query that the filter allowed. Route names the upstream
// route the query must take, empty when the pool may choose.
type Resolver interface {
	Resolve(ctx context.Context, req *mdns.Msg, route string) (*mdns.Msg, error)
}

// Decider answers a query for the client at one address. The runtime implements
// it, and it reads one snapshot per call, so the rule set and the identity table
// behind an answer always come from the same generation. Splitting these into
// two dependencies would let a query pair a new rule set with an old selector
// table during a reload.
type Decider interface {
	Decide(name filter.Domain, address netip.Addr) filter.Verdict
}

// Observer receives every decision the handler makes. It runs on the resolver's
// worker, so an implementation must not block. A consumer that cannot keep up
// drops rather than waits.
type Observer interface {
	Observe(Decision)
}

// RateLimiter decides whether the client at one address may spend the
// resolver's time. The handler asks it on the allowed path only, so a blocked
// query never pays for it and a refused query never reaches the cache.
type RateLimiter interface {
	Allow(address netip.Addr) bool
}

// Gate decides whether the client at one address may ask at all. It carries
// the listener's stored client sets: a disallowed client is refused before any
// processing, and a non-empty allowed set serves only the clients it lists.
// A nil Gate allows everyone.
type Gate interface {
	Allows(address netip.Addr) bool
}

// Rewriter answers one query from the configured rewrite table, the seam the
// runtime fills with one snapshot generation. Lookup maps a name to its
// record; Reverse maps an address back to the name that pins it.
type Rewriter interface {
	Lookup(name filter.Domain) (rewrite.Record, bool)
	Reverse(address netip.Addr) (filter.Domain, bool)
}

// maxRewriteHops bounds how many name rewrites one query may follow, so a
// configuration loop fails loudly instead of spinning.
const maxRewriteHops = 8

// Config is what a Handler needs to answer queries. Every observer sees every
// decision, so the stream and the query log can consume them independently.
type Config struct {
	Decider   Decider
	Upstream  Resolver
	Rewriter  Rewriter
	Observers []Observer
	Limiter   RateLimiter
	Gate      Gate
}

// Decision is one resolved query, as the live stream and the query log consume
// it. The handler publishes it for a blocked name and for an allowed one, so
// the stream shows the whole pipeline rather than only what it stopped. Type is
// the question's record type as its mnemonic, such as A or AAAA. Rewritten
// names the rewrite target when a rewrite took part, empty otherwise.
type Decision struct {
	Time      time.Time
	Address   netip.Addr
	Name      filter.Domain
	Type      string
	Action    filter.Action
	Match     *filter.Provenance
	Rewritten string
}

// Handler answers one DNS message. It holds no mutable state, so one Handler
// serves every worker.
type Handler struct {
	decider   Decider
	upstream  Resolver
	rewriter  Rewriter
	observers []Observer
	limiter   RateLimiter
	gate      Gate
}

// NewHandler checks the config and returns a Handler.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.Decider == nil {
		return nil, errors.New("dns: Config.Decider is required")
	}
	if cfg.Upstream == nil {
		return nil, errors.New("dns: Config.Upstream is required")
	}
	return &Handler{decider: cfg.Decider, upstream: cfg.Upstream, rewriter: cfg.Rewriter, observers: cfg.Observers, limiter: cfg.Limiter, gate: cfg.Gate}, nil
}

// Handle answers one query for the client at address. A name with a rewrite
// is answered or followed before the filter sees it: an address rewrite is
// answered locally, and a name rewrite becomes a CNAME whose target is
// filtered and forwarded in the query's place.
func (h *Handler) Handle(ctx context.Context, req *mdns.Msg, address netip.Addr) (*mdns.Msg, error) {
	if h.gate != nil && !h.gate.Allows(address) {
		return reply(req, mdns.RcodeRefused), nil
	}
	if len(req.Question) != 1 {
		return reply(req, mdns.RcodeFormatError), nil
	}

	question := req.Question[0]
	name, err := filter.ParseDomain(question.Name)
	if err != nil {
		// A name we cannot parse cannot match a rule, so it is none of ours.
		return h.forward(ctx, req, question, address, nil, "")
	}

	if h.rewriter != nil && question.Qtype == mdns.TypePTR {
		if arpa, ok := rewrite.ParseReverse(name); ok {
			if host, ok := h.rewriter.Reverse(arpa); ok {
				h.publish(question, address, name, filter.Verdict{Action: filter.ActionRewrite}, host.String())
				return ptrAnswer(req, question, host), nil
			}
		}
	}

	target := name
	rewritten := ""
	var chain []mdns.RR
	if h.rewriter != nil {
		var pinned *rewrite.Record
		for range maxRewriteHops {
			record, ok := h.rewriter.Lookup(target)
			if !ok {
				break
			}
			if rewritten == "" {
				rewritten = rewrite.TargetText(record)
			}
			if record.Addr.IsValid() {
				pinned = &record
				break
			}
			chain = append(chain, cnameRecord(target, record.CName))
			target = record.CName
		}
		if pinned != nil {
			h.publish(question, address, name, filter.Verdict{Action: filter.ActionRewrite}, rewritten)
			return addressAnswer(req, question, pinned.Addr), nil
		}
		if _, looping := h.rewriter.Lookup(target); looping {
			return reply(req, mdns.RcodeServerFailure), fmt.Errorf("dns: rewrite loop from %s", name)
		}
	}

	verdict := h.decider.Decide(target, address)
	h.publish(question, address, name, verdict, rewritten)
	if verdict.Action == filter.ActionBlock {
		return blocked(req, question, verdict.Policy), nil
	}

	ask := question
	if target.String() != name.String() {
		ask = mdns.Question{Name: mdns.Fqdn(target.String()), Qtype: question.Qtype, Qclass: question.Qclass}
	}
	return h.forward(ctx, req, ask, address, chain, verdict.Route)
}

func (h *Handler) forward(ctx context.Context, req *mdns.Msg, ask mdns.Question, address netip.Addr, chain []mdns.RR, route string) (*mdns.Msg, error) {
	if h.limiter != nil && !h.limiter.Allow(address) {
		return reply(req, mdns.RcodeRefused), nil
	}
	outbound := req
	if ask.Name != req.Question[0].Name {
		outbound = req.Copy()
		outbound.Question = []mdns.Question{ask}
	}
	resp, err := h.upstream.Resolve(ctx, outbound, route)
	if err != nil {
		return reply(req, mdns.RcodeServerFailure), fmt.Errorf("dns: upstream: %w", err)
	}
	resp.RecursionAvailable = true
	if len(chain) > 0 {
		resp.Question = req.Question
		resp.Answer = append(chain, resp.Answer...)
	}
	return resp, nil
}

// publish sends one decision to every observer. Rewritten is empty unless a
// rewrite took part in the answer.
func (h *Handler) publish(question mdns.Question, address netip.Addr, name filter.Domain, verdict filter.Verdict, rewritten string) {
	if len(h.observers) == 0 {
		return
	}
	decision := Decision{
		Time:      time.Now(),
		Address:   address,
		Name:      name,
		Type:      mdns.TypeToString[question.Qtype],
		Action:    verdict.Action,
		Match:     verdict.Match,
		Rewritten: rewritten,
	}
	for _, observer := range h.observers {
		observer.Observe(decision)
	}
}

func blocked(req *mdns.Msg, question mdns.Question, policy filter.Policy) *mdns.Msg {
	switch policy.Mode {
	case filter.NXDomain:
		return reply(req, mdns.RcodeNameError)
	case filter.Refused:
		return reply(req, mdns.RcodeRefused)
	case filter.NullAddress:
		return addressAnswer(req, question, nullAddressFor(question.Qtype))
	case filter.CustomAddress:
		return addressAnswer(req, question, policy.Custom)
	}
	return reply(req, mdns.RcodeNameError)
}

func reply(req *mdns.Msg, rcode int) *mdns.Msg {
	resp := new(mdns.Msg)
	resp.SetRcode(req, rcode)
	// A client reads this bit to decide whether to keep using us or fall back to
	// another resolver, so it belongs on the answers we build ourselves and not
	// only on the ones we pass through from upstream.
	resp.RecursionAvailable = true
	return resp
}

// addressAnswer points a blocked name at an address. A query whose type does
// not match the address family gets an empty success rather than a false
// answer, so a client asking for AAAA of a name answered with IPv4 sees no
// record instead of a wrong one.
func addressAnswer(req *mdns.Msg, question mdns.Question, address netip.Addr) *mdns.Msg {
	resp := reply(req, mdns.RcodeSuccess)
	switch {
	case question.Qtype == mdns.TypeA && address.Is4():
		resp.Answer = append(resp.Answer, &mdns.A{
			Hdr: mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: localAnswerTTL},
			A:   address.AsSlice(),
		})
	case question.Qtype == mdns.TypeAAAA && address.Is6():
		resp.Answer = append(resp.Answer, &mdns.AAAA{
			Hdr:  mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeAAAA, Class: mdns.ClassINET, Ttl: localAnswerTTL},
			AAAA: address.AsSlice(),
		})
	}
	return resp
}

func nullAddressFor(qtype uint16) netip.Addr {
	if qtype == mdns.TypeAAAA {
		return netip.IPv6Unspecified()
	}
	return netip.IPv4Unspecified()
}

// ptrAnswer points one reverse name at the host that pins its address.
func ptrAnswer(req *mdns.Msg, question mdns.Question, host filter.Domain) *mdns.Msg {
	resp := reply(req, mdns.RcodeSuccess)
	resp.Answer = append(resp.Answer, &mdns.PTR{
		Hdr: mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypePTR, Class: mdns.ClassINET, Ttl: localAnswerTTL},
		Ptr: mdns.Fqdn(host.String()),
	})
	return resp
}

// cnameRecord hops one name to the next along a rewrite chain.
func cnameRecord(name filter.Domain, target filter.Domain) mdns.RR {
	return &mdns.CNAME{
		Hdr:    mdns.RR_Header{Name: mdns.Fqdn(name.String()), Rrtype: mdns.TypeCNAME, Class: mdns.ClassINET, Ttl: localAnswerTTL},
		Target: mdns.Fqdn(target.String()),
	}
}
