package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"

	mdns "github.com/miekg/dns"
)

// maxDNSMessage bounds one response body. A DNS message stops at 64KiB on
// every transport that carries one.
const maxDNSMessage = 1 << 16

// transport exchanges one message with one resolver. Implementations must be
// safe for concurrent use, which the miekg clients and net/http are.
type transport interface {
	Exchange(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error)
}

// newTransport builds the transport one parsed resolver speaks. tlsConfig is
// a base the DoT and DoH transports clone; it is nil in production, where
// the system trust roots are right, and tests install their own.
func newTransport(spec Spec, tlsConfig *tls.Config) transport {
	switch spec.URL.Scheme {
	case "tcp":
		return &plainTransport{primary: &mdns.Client{Net: "tcp"}, address: spec.address()}
	case "tls":
		client := &mdns.Client{Net: "tcp-tls"}
		if tlsConfig != nil {
			client.TLSConfig = tlsConfig.Clone()
			client.TLSConfig.ServerName = spec.URL.Hostname()
		}
		return &plainTransport{primary: client, address: spec.address()}
	case "https":
		roundTripper := http.DefaultTransport.(*http.Transport).Clone()
		if tlsConfig != nil {
			roundTripper.TLSClientConfig = tlsConfig.Clone()
		}
		return &dohTransport{endpoint: spec.URL, client: &http.Client{Transport: roundTripper}}
	default:
		return &plainTransport{
			primary: &mdns.Client{Net: "udp"},
			retry:   &mdns.Client{Net: "tcp"},
			address: spec.address(),
		}
	}
}

// plainTransport speaks one connectionless hop. Over UDP a truncated answer
// lacks the records the client asked for, so the same upstream is asked again
// over TCP; over TLS the stream never truncates and the retry client stays
// nil.
type plainTransport struct {
	primary *mdns.Client
	retry   *mdns.Client
	address string
}

func (t *plainTransport) Exchange(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	resp, _, err := t.primary.ExchangeContext(ctx, req, t.address)
	if err != nil {
		return nil, err
	}
	if !resp.Truncated || t.retry == nil {
		return resp, nil
	}
	full, _, err := t.retry.ExchangeContext(ctx, req, t.address)
	if err != nil {
		return nil, fmt.Errorf("upstream: tcp retry after truncation: %w", err)
	}
	return full, nil
}

// dohTransport speaks DNS over HTTPS, RFC 8484, posting the wire message.
type dohTransport struct {
	endpoint *url.URL
	client   *http.Client
}

func (t *dohTransport) Exchange(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	wire, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("upstream: doh: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint.String(), bytes.NewReader(wire))
	if err != nil {
		return nil, fmt.Errorf("upstream: doh: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream: doh: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDNSMessage))
	if err != nil {
		return nil, fmt.Errorf("upstream: doh: %w", err)
	}
	answer := new(mdns.Msg)
	if err := answer.Unpack(body); err != nil {
		return nil, fmt.Errorf("upstream: doh: %w", err)
	}
	return answer, nil
}
