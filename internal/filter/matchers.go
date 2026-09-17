package filter

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// ParsePattern validates the text a wildcard or regex rule matches with and
// returns its canonical form. A wildcard is lowercased the way query names
// are, and every label must be a DNS label or a lone *. A regex is kept as
// written, because lowercasing could corrupt a character class, and matching
// is case-insensitive instead.
func ParsePattern(kind MatchKind, raw string) (string, error) {
	pattern := strings.TrimSpace(raw)
	switch kind {
	case MatchWildcard:
		return canonicalWildcard(pattern)
	case MatchRegex:
		if pattern == "" {
			return "", errors.New("filter: a regex rule needs a pattern")
		}
		if _, err := compileRegex(pattern); err != nil {
			return "", err
		}
		return pattern, nil
	default:
		return "", fmt.Errorf("filter: kind %s does not take a pattern", kind)
	}
}

// ParseNetwork parses the CIDR a network rule matches with. The prefix is
// masked, so 192.168.1.5/24 and 192.168.1.0/24 are the same rule.
func ParseNetwork(raw string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("filter: %w", err)
	}
	return prefix.Masked(), nil
}

// compilePattern turns a wildcard or regex pattern into the matcher the rule
// set holds. It validates as well as compiles, so a rule a boundary let
// through still fails the load here instead of failing open.
func compilePattern(kind MatchKind, pattern string) (*regexp.Regexp, error) {
	switch kind {
	case MatchWildcard:
		return compileWildcard(pattern)
	case MatchRegex:
		if pattern == "" {
			return nil, errors.New("filter: a regex rule needs a pattern")
		}
		return compileRegex(pattern)
	default:
		return nil, fmt.Errorf("filter: kind %s does not take a pattern", kind)
	}
}

func canonicalWildcard(pattern string) (string, error) {
	if pattern == "" {
		return "", errors.New("filter: a wildcard rule needs a pattern")
	}
	pattern = strings.ToLower(pattern)
	for _, label := range strings.Split(pattern, ".") {
		if label == "*" {
			continue
		}
		if !validName(label) {
			return "", fmt.Errorf("filter: wildcard %q has a label DNS cannot carry", pattern)
		}
	}
	return pattern, nil
}

func compileWildcard(pattern string) (*regexp.Regexp, error) {
	canonical, err := canonicalWildcard(pattern)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(`^`)
	for i, label := range strings.Split(canonical, ".") {
		if i > 0 {
			b.WriteString(`\.`)
		}
		if label == "*" {
			b.WriteString(`[^.]+`)
		} else {
			b.WriteString(regexp.QuoteMeta(label))
		}
	}
	b.WriteString(`$`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("filter: wildcard %q: %w", pattern, err)
	}
	return re, nil
}

func compileRegex(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(`(?i)` + pattern)
	if err != nil {
		return nil, fmt.Errorf("filter: regex %q: %w", pattern, err)
	}
	return re, nil
}
