// Package upstream holds the resolvers aegis forwards allowed queries to:
// several configured resolvers, plain UDP and TCP, DNS over TLS and HTTPS,
// health scoring, and failover.
package upstream

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Spec is one configured resolver. Name is the URL as the operator wrote it,
// for logs and status; URL is the parsed form every transport reads.
type Spec struct {
	Name string
	URL  *url.URL
}

// defaultPorts fill in the port an operator left off, per scheme.
var defaultPorts = map[string]string{
	"udp":   "53",
	"tcp":   "53",
	"tls":   "853",
	"https": "443",
}

// Parse reads one resolver from its URL form. A bare host, with or without a
// port, is plain UDP, which is what the upstream flag has always taken.
func Parse(raw string) (Spec, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Spec{}, fmt.Errorf("upstream: the resolver is empty")
	}
	if !strings.Contains(text, "://") {
		text = "udp://" + text
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return Spec{}, fmt.Errorf("upstream: %q: %w", raw, err)
	}
	switch parsed.Scheme {
	case "udp", "tcp", "tls", "https":
	case "http":
		return Spec{}, fmt.Errorf("upstream: %q: http is not a DNS transport, use udp://, tcp://, tls://, or https://", raw)
	default:
		return Spec{}, fmt.Errorf("upstream: %q: unknown scheme %q", raw, parsed.Scheme)
	}
	if parsed.User != nil {
		return Spec{}, fmt.Errorf("upstream: %q: userinfo is not part of a resolver", raw)
	}
	host := parsed.Hostname()
	if host == "" {
		return Spec{}, fmt.Errorf("upstream: %q: the resolver host is missing", raw)
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		if parsed.Scheme != "https" {
			return Spec{}, fmt.Errorf("upstream: %q: only https carries a path", raw)
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return Spec{}, fmt.Errorf("upstream: %q: a query or fragment has no meaning on a resolver", raw)
		}
	}
	if port := parsed.Port(); port == "" {
		parsed.Host = net.JoinHostPort(host, defaultPorts[parsed.Scheme])
	} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return Spec{}, fmt.Errorf("upstream: %q: bad port %q", raw, port)
	}
	return Spec{Name: raw, URL: parsed}, nil
}

// address is the host:port a DNS transport dials.
func (s Spec) address() string {
	return net.JoinHostPort(s.URL.Hostname(), s.URL.Port())
}
