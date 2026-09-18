// Package rewrite maps configured names to addresses or to other names, and
// answers PTR for the addresses it pins. The handler consults the table
// before the filter, so a rewritten name resolves no matter what the
// blocklists say about it.
package rewrite

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"aegis/internal/filter"
)

// Record is one configured rewrite. Exactly one of Addr and CName holds the
// target: Parse refuses a target that is neither an address nor a name.
type Record struct {
	Pattern  string        // as written, "*.example.com" or "home.local"
	Wildcard bool          // Pattern starts with "*."
	Addr     netip.Addr    // the answer, when the target is an address
	CName    filter.Domain // the target, when the target is a name
}

// Parse reads one rewrite from its stored form. The pattern may carry a
// leading "*.", which matches any subdomain at any depth but not the apex.
func Parse(pattern, target string) (Record, error) {
	raw := strings.TrimSpace(pattern)
	body := strings.TrimPrefix(raw, "*.")
	name, err := filter.ParseDomain(body)
	if err != nil {
		return Record{}, fmt.Errorf("rewrite: pattern %q: %w", pattern, err)
	}
	wildcard := strings.HasPrefix(raw, "*.")
	if strings.TrimSpace(target) == "" {
		return Record{}, fmt.Errorf("rewrite: %q: the target is empty", pattern)
	}
	if address, err := netip.ParseAddr(strings.TrimSpace(target)); err == nil {
		return Record{Pattern: raw, Wildcard: wildcard, Addr: address}, nil
	}
	cname, err := filter.ParseDomain(target)
	if err != nil {
		return Record{}, fmt.Errorf("rewrite: %q: target %q is neither an address nor a name", pattern, target)
	}
	if !wildcard && cname.String() == name.String() {
		return Record{}, fmt.Errorf("rewrite: %q: the target is the name itself", pattern)
	}
	return Record{Pattern: raw, Wildcard: wildcard, CName: cname}, nil
}

// TargetText renders the target the way Parse reads it back, so the store
// round-trips a record without a second shape.
func TargetText(record Record) string {
	if record.Addr.IsValid() {
		return record.Addr.String()
	}
	return record.CName.String()
}

// Table holds the configured rewrites. Exact names live in a map, wildcards
// in a short list scanned longest-suffix-first, so one lookup is a hash probe
// and usually no scan at all.
type Table struct {
	exact     map[string]Record
	wildcards []wildcardEntry
	reverse   map[netip.Addr]filter.Domain
}

type wildcardEntry struct {
	suffix filter.Domain // the pattern without "*."
	record Record
}

// New builds a table from the configured records, dropping none: a caller
// that parsed through Parse cannot hold an unusable record.
func New(records []Record) *Table {
	table := &Table{
		exact:   make(map[string]Record, len(records)),
		reverse: make(map[netip.Addr]filter.Domain),
	}
	for _, record := range records {
		if record.Wildcard {
			suffix, _ := filter.ParseDomain(strings.TrimPrefix(record.Pattern, "*."))
			table.wildcards = append(table.wildcards, wildcardEntry{suffix: suffix, record: record})
			continue
		}
		name, _ := filter.ParseDomain(record.Pattern)
		table.exact[name.String()] = record
		if record.Addr.IsValid() {
			if _, seen := table.reverse[record.Addr]; !seen {
				table.reverse[record.Addr] = name
			}
		}
	}
	// Most specific first, so a scan can stop at the first match.
	sort.SliceStable(table.wildcards, func(i, j int) bool {
		return len(table.wildcards[i].suffix.String()) > len(table.wildcards[j].suffix.String())
	})
	return table
}

// Lookup answers one name. Exact first, then the longest wildcard whose
// suffix matches. The apex of a wildcard is deliberately not matched.
func (t *Table) Lookup(name filter.Domain) (Record, bool) {
	if record, ok := t.exact[name.String()]; ok {
		return record, true
	}
	full := name.String()
	for _, entry := range t.wildcards {
		suffix := entry.suffix.String()
		if strings.HasSuffix(full, "."+suffix) {
			return entry.record, true
		}
	}
	return Record{}, false
}

// Reverse answers a PTR question for the addresses the exact rewrites pin.
func (t *Table) Reverse(address netip.Addr) (filter.Domain, bool) {
	name, ok := t.reverse[address.Unmap()]
	return name, ok
}

// ReverseName renders one address as the name a PTR query arrives with.
func ReverseName(address netip.Addr) string {
	address = address.Unmap()
	if address.Is6() && !address.Is4In6() {
		var builder strings.Builder
		for _, nibble := range nibbles(address.As16()) {
			fmt.Fprintf(&builder, "%x.", nibble)
		}
		builder.WriteString("ip6.arpa")
		return builder.String()
	}
	octets := address.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", octets[3], octets[2], octets[1], octets[0])
}

// ParseReverse reads an arpa name back into the address it asks about. A
// name that is not a reverse name reports false.
func ParseReverse(name filter.Domain) (netip.Addr, bool) {
	labels := strings.Split(name.String(), ".")
	if inAddr := isSuffix(labels, "in-addr.arpa"); inAddr > 0 {
		octets := labels[:inAddr]
		if len(octets) != 4 {
			return netip.Addr{}, false
		}
		var out [4]byte
		for i, label := range octets {
			value, ok := parseByte(label)
			if !ok {
				return netip.Addr{}, false
			}
			out[3-i] = value
		}
		return netip.AddrFrom4(out), true
	}
	if inArpa := isSuffix(labels, "ip6.arpa"); inArpa > 0 {
		nibbleLabels := labels[:inArpa]
		if len(nibbleLabels) != 32 {
			return netip.Addr{}, false
		}
		var out [16]byte
		for i, label := range nibbleLabels {
			value, ok := parseNibble(label)
			if !ok {
				return netip.Addr{}, false
			}
			// The name spells the address from its most significant nibble.
			if i%2 == 0 {
				out[i/2] |= value << 4
			} else {
				out[i/2] |= value
			}
		}
		return netip.AddrFrom16(out), true
	}
	return netip.Addr{}, false
}

func isSuffix(labels []string, suffix string) int {
	parts := strings.Split(suffix, ".")
	if len(labels) < len(parts) {
		return 0
	}
	tail := labels[len(labels)-len(parts):]
	for i, part := range parts {
		if tail[i] != part {
			return 0
		}
	}
	return len(labels) - len(parts)
}

func nibbles(in [16]byte) []byte {
	out := make([]byte, 0, 32)
	for _, b := range in {
		out = append(out, b>>4, b&0x0f)
	}
	return out
}

func parseByte(label string) (byte, bool) {
	if len(label) == 0 || len(label) > 3 {
		return 0, false
	}
	value := 0
	for _, c := range []byte(label) {
		if c < '0' || c > '9' {
			return 0, false
		}
		value = value*10 + int(c-'0')
	}
	if value > 255 {
		return 0, false
	}
	return byte(value), true
}

func parseNibble(label string) (byte, bool) {
	if len(label) != 1 {
		return 0, false
	}
	switch c := label[0]; {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	default:
		return 0, false
	}
}
