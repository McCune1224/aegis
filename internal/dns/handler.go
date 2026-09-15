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

// BlockingMode is how the server answers a name that a rule blocked.
type BlockingMode uint8

const (
	NXDomain BlockingMode = iota
	NullAddress
	CustomAddress
	Refused
)

var blockingModeNames = [...]string{
	NXDomain:      "nxdomain",
	NullAddress:   "null-address",
	CustomAddress: "custom-address",
	Refused:       "refused",
}

func (m BlockingMode) String() string {
	if int(m) >= len(blockingModeNames) {
		return fmt.Sprintf("mode(%d)", uint8(m))
	}
	return blockingModeNames[m]
}

// ParseBlockingMode turns a configured name into a BlockingMode. The names
// table is the single source, so a new mode is one row and both directions
// keep working.
func ParseBlockingMode(name string) (BlockingMode, error) {
	for mode, candidate := range blockingModeNames {
		if candidate == name {
			return BlockingMode(mode), nil
		}
	}
	return 0, fmt.Errorf("dns: unknown blocking mode %q", name)
}

// blockTTL is how long a client may cache a blocked answer. It stays short so
// that unblocking a name takes effect without waiting out a long cache.
const blockTTL = 60

// Resolver answers a query that the filter allowed.
type Resolver interface {
	Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error)
}

// Config is what a Handler needs to answer queries.
type Config struct {
	Engine   *filter.Engine
	Upstream Resolver
	Mode     BlockingMode
	Custom   netip.Addr
}

// Handler answers one DNS message. It holds no mutable state, so one Handler
// serves every worker.
type Handler struct {
	engine   *filter.Engine
	upstream Resolver
	mode     BlockingMode
	custom   netip.Addr
}

// NewHandler checks the config and returns a Handler. It refuses CustomAddress
// without an address and refuses an address that no other mode can use, so the
// pairing holds for the Handler's whole life instead of being rechecked per
// query.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.Engine == nil {
		return nil, errors.New("dns: Config.Engine is required")
	}
	if cfg.Upstream == nil {
		return nil, errors.New("dns: Config.Upstream is required")
	}
	if int(cfg.Mode) >= len(blockingModeNames) {
		return nil, fmt.Errorf("dns: unknown blocking mode %d", cfg.Mode)
	}
	if cfg.Mode == CustomAddress {
		if !cfg.Custom.IsValid() {
			return nil, errors.New("dns: blocking mode custom-address needs a valid address")
		}
	} else if cfg.Custom.IsValid() {
		return nil, fmt.Errorf("dns: blocking mode %s does not take an address", cfg.Mode)
	}

	return &Handler{
		engine:   cfg.Engine,
		upstream: cfg.Upstream,
		mode:     cfg.Mode,
		custom:   cfg.Custom,
	}, nil
}

// Handle answers one query. A blocked name never reaches the upstream. A name
// the engine allows is forwarded, and an upstream failure becomes SERVFAIL with
// the error returned so the caller can log what went wrong.
func (h *Handler) Handle(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	if len(req.Question) != 1 {
		return reply(req, mdns.RcodeFormatError), nil
	}

	question := req.Question[0]
	name, err := filter.ParseDomain(question.Name)
	if err != nil {
		// A name we cannot parse cannot match a rule, so it is none of ours.
		return h.forward(ctx, req)
	}

	if verdict := h.engine.Decide(name); verdict.Action == filter.ActionBlock {
		return h.blocked(req, question), nil
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

func (h *Handler) blocked(req *mdns.Msg, question mdns.Question) *mdns.Msg {
	switch h.mode {
	case NXDomain:
		return reply(req, mdns.RcodeNameError)
	case Refused:
		return reply(req, mdns.RcodeRefused)
	case NullAddress:
		return addressAnswer(req, question, nullAddressFor(question.Qtype))
	case CustomAddress:
		return addressAnswer(req, question, h.custom)
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
