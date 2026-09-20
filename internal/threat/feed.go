package threat

import (
	"bytes"
	"fmt"
	"strings"

	"aegis/internal/filter"
)

// DefaultKind is what an entry without a kind means.
const DefaultKind = "malware"

// FeedEntry is one line of a threat feed: a domain and the kind of threat it
// carries.
type FeedEntry struct {
	Domain filter.Domain
	Kind   string
}

// ParseFeed reads one feed body. Each line carries a domain and, optionally,
// the kind of threat; a blank kind means malware. Blank lines and # comments
// are skipped. Parsing is the boundary, so a domain the DNS cannot carry is an
// error rather than a row that silently never matches.
func ParseFeed(body []byte) ([]FeedEntry, error) {
	entries := make([]FeedEntry, 0, bytes.Count(body, []byte("\n")))
	for number, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		domain, err := filter.ParseDomain(fields[0])
		if err != nil {
			return nil, fmt.Errorf("threat: feed line %d: %w", number+1, err)
		}
		kind := DefaultKind
		if len(fields) > 1 {
			kind = strings.ToLower(fields[1])
		}
		entries = append(entries, FeedEntry{Domain: domain, Kind: kind})
	}
	return entries, nil
}

// Index is an immutable lookup from domain to threat kind, published by
// swapping one pointer. A lookup matches the exact name first, then the
// registrable domain, so a feed that classifies example.com also names a
// threat on mail.example.com.
type Index struct {
	exact       map[string]string
	registrable map[string]string
}

// EmptyIndex is the lookup nothing matches, the starting state.
var EmptyIndex = &Index{exact: map[string]string{}, registrable: map[string]string{}}

// BuildIndex folds classified domains into a lookup.
func BuildIndex(domains map[string]string) *Index {
	index := &Index{exact: make(map[string]string, len(domains)), registrable: make(map[string]string)}
	for name, kind := range domains {
		index.exact[name] = kind
		registrable := registrableDomain(name)
		if _, exists := index.registrable[registrable]; !exists {
			index.registrable[registrable] = kind
		}
	}
	return index
}

// ThreatFor names the threat a queried name carries, or "" when no feed
// claims it.
func (ix *Index) ThreatFor(name string) string {
	if kind, exists := ix.exact[name]; exists {
		return kind
	}
	return ix.registrable[registrableDomain(name)]
}
