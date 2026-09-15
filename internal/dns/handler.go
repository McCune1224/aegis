// Package dns answers DNS queries from the filter engine's verdicts.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	mdns "github.com/miekg/dns"

	"aegis/internal/filter"
)

// blockTTL is how long a client may cache a blocked answer. It stays short so
// that unblocking a name takes effect without waiting out a long cache.
const blockTTL = 60

// Resolver answers a query that the filter allowed.
type Resolver interface {
	Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error)
}

// Decider answers a query for the client at one address. The runtime implements
// it, and it reads one snapshot per call, so the rule set and the identity table
// behind an answer always come from the same generation. Splitting these into
// two dependencies would let a query pair a new rule set with an old selector
// table during a reload.
type Decider interface {
	Decide(name filter.Domain, address netip.Addr) filter.Verdict
}

// Config is what a Handler needs to answer queries.
type Config struct {
	Decider  Decider
	Upstream Resolver
}

// Handler answers one DNS message. It holds no mutable state, so one Handler
// serves every worker.
type Handler struct {
	decider  Decider
	upstream Resolver
}

// NewHandler checks the config and returns a Handler.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.Decider == nil {
		return nil, errors.New("dns: Config.Decider is required")
	}
	if cfg.Upstream == nil {
		return nil, errors.New("dns: Config.Upstream is required")
	}
	return &Handler{decider: cfg.Decider, upstream: cfg.Upstream}, nil
}

// Handle answers one query for the client at address. A blocked name never
// reaches the upstream. A name the engine allows is forwarded, and an upstream
// failure becomes SERVFAIL with the error returned so the caller can log what
// went wrong.
func (h *Handler) Handle(ctx context.Context, req *mdns.Msg, address netip.Addr) (*mdns.Msg, error) {
	if len(req.Question) != 1 {
		return reply(req, mdns.RcodeFormatError), nil
	}

	question := req.Question[0]
	name, err := filter.ParseDomain(question.Name)
	if err != nil {
		// A name we cannot parse cannot match a rule, so it is none of ours.
		return h.forward(ctx, req)
	}

	verdict := h.decider.Decide(name, address)
	if verdict.Action == filter.ActionBlock {
		return blocked(req, question, verdict.Policy), nil
	}
	return h.forward(ctx, req)
}

func (h *Handler) forward(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	resp, err := h.upstream.Resolve(ctx, req)
	if err != nil {
		return reply(req, mdns.RcodeServerFailure), fmt.Errorf("dns: upstream: %w", err)
	}
	resp.RecursionAvailable = true
	return resp, nil
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
			Hdr: mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: blockTTL},
			A:   address.AsSlice(),
		})
	case question.Qtype == mdns.TypeAAAA && address.Is6():
		resp.Answer = append(resp.Answer, &mdns.AAAA{
			Hdr:  mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeAAAA, Class: mdns.ClassINET, Ttl: blockTTL},
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
