package blocklist

import (
	"fmt"
	"io"
	"sort"

	"aegis/internal/filter"
)

// Diff is the change one refresh would apply: domains the new body adds and
// domains it drops, both sorted.
type Diff struct {
	Added   []string
	Removed []string
}

// Changed reports whether the two bodies produce different rule sets.
func (d Diff) Changed() bool {
	return len(d.Added) > 0 || len(d.Removed) > 0
}

// DiffLists parses both bodies in one format and reports the domains the new
// body would add and remove relative to the old one.
func DiffLists(oldBody, newBody io.Reader, source filter.Source, format Format) (Diff, error) {
	oldRules, err := ParseList(oldBody, source, format)
	if err != nil {
		return Diff{}, fmt.Errorf("blocklist: diff old body: %w", err)
	}
	newRules, err := ParseList(newBody, source, format)
	if err != nil {
		return Diff{}, fmt.Errorf("blocklist: diff new body: %w", err)
	}

	oldDomains := make(map[string]bool, len(oldRules.Rules))
	for _, rule := range oldRules.Rules {
		oldDomains[rule.Domain.String()] = true
	}
	newDomains := make(map[string]bool, len(newRules.Rules))
	for _, rule := range newRules.Rules {
		newDomains[rule.Domain.String()] = true
	}

	diff := Diff{}
	for domain := range newDomains {
		if !oldDomains[domain] {
			diff.Added = append(diff.Added, domain)
		}
	}
	for domain := range oldDomains {
		if !newDomains[domain] {
			diff.Removed = append(diff.Removed, domain)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	return diff, nil
}
