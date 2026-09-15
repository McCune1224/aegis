// Package blocklist turns the file formats that blocklists ship in into the
// rules the filter engine indexes.
package blocklist

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"aegis/internal/filter"
)

// Format is the file layout of a blocklist.
type Format uint8

const (
	FormatHosts Format = iota
	FormatDomains
	FormatAdBlock

	formatCount
)

var formatNames = [formatCount]string{
	FormatHosts:   "hosts",
	FormatDomains: "domains",
	FormatAdBlock: "adblock",
}

func (f Format) String() string {
	if int(f) >= len(formatNames) {
		return fmt.Sprintf("format(%d)", uint8(f))
	}
	return formatNames[f]
}

// ParseFormat turns a configured name into a Format. The names table is the
// single source, so a new format is one row and both directions keep working.
func ParseFormat(name string) (Format, error) {
	for format, candidate := range formatNames {
		if candidate == name {
			return Format(format), nil
		}
	}
	return 0, fmt.Errorf("blocklist: unknown format %q", name)
}

// ParseResult is what one list file yielded. Skipped counts the lines that hold
// nothing a DNS rule can express, such as an AdBlock cosmetic filter or a
// malformed name, so an operator can see what the import dropped.
type ParseResult struct {
	Rules   []filter.RuleSpec
	Skipped int
}

// ParseList reads one blocklist and returns the rules it holds. It is the
// boundary for list files, so line syntax stops here and RuleSpec values move
// inward.
//
// A line it cannot use is counted in Skipped rather than failing the load. Real
// lists carry comments, cosmetic filters, and the occasional typo, and a single
// bad line should not cost the operator the whole list.
func ParseList(r io.Reader, source filter.Source, format Format) (ParseResult, error) {
	if int(format) >= len(lineParsers) {
		return ParseResult{}, fmt.Errorf("blocklist: unknown format %s", format)
	}
	parseLine := lineParsers[format]

	var result ParseResult
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		entries := parseLine(scanner.Text())
		kept := 0
		for _, entry := range entries {
			domain, err := filter.ParseDomain(entry.name)
			if err != nil {
				continue
			}
			result.Rules = append(result.Rules, filter.RuleSpec{
				ID:     lineRef(source, line, kept),
				Source: source,
				Kind:   entry.kind,
				Domain: domain,
				Action: entry.action,
			})
			kept++
		}
		if kept == 0 {
			result.Skipped++
		}
	}
	if err := scanner.Err(); err != nil {
		return ParseResult{}, fmt.Errorf("blocklist: %s: %w", source.ID, err)
	}
	return result, nil
}

// entry is one rule as a line parser found it, before the name is validated and
// before it carries a rule ID.
type entry struct {
	name   string
	kind   filter.MatchKind
	action filter.Action
}

type lineParser func(text string) []entry

var lineParsers = [formatCount]lineParser{
	FormatHosts:   parseHostsLine,
	FormatDomains: parseDomainsLine,
	FormatAdBlock: parseAdBlockLine,
}

// parseHostsLine reads the hosts layout, where an address precedes one or more
// hostnames. A line with no address is a bare hostname, which some lists use.
func parseHostsLine(text string) []entry {
	fields := strings.Fields(stripComment(text, '#'))
	if len(fields) == 0 {
		return nil
	}
	if _, err := netip.ParseAddr(fields[0]); err == nil {
		fields = fields[1:]
	} else if len(fields) > 1 {
		// A hosts line without an address carries one hostname. Several fields
		// and no address is not a hosts line, so it is skipped rather than
		// turning every word into a rule.
		return nil
	}
	out := make([]entry, 0, len(fields))
	for _, name := range fields {
		out = append(out, entry{name: name, kind: filter.MatchSubdomains, action: filter.ActionBlock})
	}
	return out
}

// parseDomainsLine reads one name per line and ignores anything after it.
func parseDomainsLine(text string) []entry {
	fields := strings.Fields(stripComment(text, '#'))
	if len(fields) == 0 {
		return nil
	}
	return []entry{{name: fields[0], kind: filter.MatchSubdomains, action: filter.ActionBlock}}
}

// parseAdBlockLine reads the network rules an AdBlock list can express in DNS.
// Element hiding, regular expressions, and scriptlet rules have no DNS form, so
// they return nothing and the caller counts them as skipped.
// See https://adguard.com/kb/general/ad-filtering/create-own-filters/.
func parseAdBlockLine(text string) []entry {
	line := strings.TrimSpace(text)
	if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") {
		return nil
	}

	action := filter.ActionBlock
	if rest, ok := strings.CutPrefix(line, "@@"); ok {
		action = filter.ActionAllow
		line = rest
	}

	name, ok := strings.CutPrefix(line, "||")
	if !ok {
		return nil
	}
	// The rule ends at the separator or at the first modifier.
	if cut := strings.IndexAny(name, "^$"); cut >= 0 {
		name = name[:cut]
	}
	if name == "" {
		return nil
	}
	return []entry{{name: name, kind: filter.MatchSubdomains, action: action}}
}

func stripComment(line string, marker byte) string {
	if i := strings.IndexByte(line, marker); i >= 0 {
		return line[:i]
	}
	return line
}

// lineRef names a rule by where it came from, so a verdict can point an
// operator at the line in the list that decided it.
func lineRef(source filter.Source, line, index int) string {
	if index == 0 {
		return fmt.Sprintf("%s:%d", source.ID, line)
	}
	return fmt.Sprintf("%s:%d.%d", source.ID, line, index)
}
