package dns

import (
	"context"
	"fmt"

	mdns "github.com/miekg/dns"
)

// Forwarder sends a query the filter allowed to one upstream resolver.
type Forwarder struct {
	upstream string
	udp      *mdns.Client
	tcp      *mdns.Client
}

// NewForwarder returns a Forwarder that sends queries to upstream, given as a
// host and port such as 9.9.9.9:53.
func NewForwarder(upstream string) *Forwarder {
	return &Forwarder{
		upstream: upstream,
		udp:      &mdns.Client{Net: "udp"},
		tcp:      &mdns.Client{Net: "tcp"},
	}
}

// Resolve sends req upstream and returns the answer.
func (f *Forwarder) Resolve(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	resp, _, err := f.udp.ExchangeContext(ctx, req, f.upstream)
	if err != nil {
		return nil, err
	}
	if !resp.Truncated {
		return resp, nil
	}

	// A truncated answer lacks the records the client asked for, so ask again
	// over a transport that can carry them.
	full, _, err := f.tcp.ExchangeContext(ctx, req, f.upstream)
	if err != nil {
		return nil, fmt.Errorf("dns: tcp retry after truncation: %w", err)
	}
	return full, nil
}
