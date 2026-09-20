// Package safesearch maps a search engine's hosts to its own safe-mode hosts.
// The mapping is the two lists AdGuard Home builds its SafeSearch from, copied
// into the binary at release time, and a profile turns engines on; the answer
// rides the rewrite path the DNS handler already owns.
package safesearch

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"io"
	"strings"
	"sync"

	"aegis/internal/filter"
	"aegis/internal/rewrite"
)

//go:embed engines_safe_search.txt youtube_safe_search.txt
var embedded embed.FS

// EngineID names a search engine, such as google or youtube.
type EngineID string

// Rule rewrites one search host to the engine's safe host.
type Rule struct {
	Host   filter.Domain
	Target filter.Domain
}

// Engine is one search engine and the hosts that carry it.
type Engine struct {
	ID    EngineID
	Name  string
	Rules []Rule
}

// Enable is one profile turning one engine's safe search on.
type Enable struct {
	Profile filter.ProfileID
	Engine  EngineID
}

// Parse reads one upstream safe-search list. A comment holding a single token
// starts an engine and every later rule belongs to it until the next one. A
// list with no such header, as the YouTube file is, puts its rules on
// fallback. A line the dialect cannot express, or one before any engine, is
// dropped rather than failing the list.
func Parse(r io.Reader, fallback EngineID) ([]Engine, error) {
	var engines []Engine
	position := make(map[EngineID]int)
	current := -1

	add := func(id EngineID, name string) {
		if index, exists := position[id]; exists {
			current = index
			return
		}
		position[id] = len(engines)
		engines = append(engines, Engine{ID: id, Name: name})
		current = len(engines) - 1
	}
	if fallback != "" {
		add(fallback, string(fallback))
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		switch {
		case text == "":
			continue
		case strings.HasPrefix(text, "#"):
			if name, ok := engineHeader(text); ok {
				add(EngineID(strings.ToLower(name)), name)
			}
			continue
		}
		rule, ok := parseRule(text)
		if !ok || current < 0 {
			continue
		}
		engines[current].Rules = append(engines[current].Rules, rule)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("safesearch: %w", err)
	}
	return engines, nil
}

// engineHeader reads an engine name off a comment. Prose comments, which the
// upstream files use for a title, are not engine names.
func engineHeader(text string) (string, bool) {
	name := strings.TrimSpace(strings.TrimPrefix(text, "#"))
	if name == "" {
		return "", false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return "", false
		}
	}
	return name, true
}

// parseRule reads one rule of the upstream dialect, which is an anchored host
// whose dnsrewrite modifier answers with a CNAME.
func parseRule(text string) (Rule, bool) {
	rest, ok := strings.CutPrefix(text, "|")
	if !ok {
		return Rule{}, false
	}
	host, modifiers, ok := strings.Cut(rest, "$")
	if !ok {
		return Rule{}, false
	}
	parts := strings.Split(modifiers, ";")
	if len(parts) != 3 || parts[0] != "dnsrewrite=NOERROR" || parts[1] != "CNAME" {
		return Rule{}, false
	}

	domain, err := filter.ParseDomain(strings.TrimSuffix(host, "^"))
	if err != nil {
		return Rule{}, false
	}
	target, err := filter.ParseDomain(parts[2])
	if err != nil {
		return Rule{}, false
	}
	return Rule{Host: domain, Target: target}, true
}

var (
	catalogOnce sync.Once
	catalog     []Engine
)

// Catalog is the embedded engine catalog, parsed once. The two files are the
// upstream lists AdGuard Home builds from, so a changed safe host is a release
// and not a config change; the list a binary holds is as new as the release.
func Catalog() []Engine {
	catalogOnce.Do(func() {
		parsed, err := parseEmbedded()
		if err != nil {
			panic("safesearch: " + err.Error())
		}
		catalog = parsed
	})
	return catalog
}

func parseEmbedded() ([]Engine, error) {
	enginesBody, err := embedded.ReadFile("engines_safe_search.txt")
	if err != nil {
		return nil, err
	}
	youtubeBody, err := embedded.ReadFile("youtube_safe_search.txt")
	if err != nil {
		return nil, err
	}

	engines, err := Parse(bytes.NewReader(enginesBody), "")
	if err != nil {
		return nil, err
	}
	youtube, err := Parse(bytes.NewReader(youtubeBody), "youtube")
	if err != nil {
		return nil, err
	}
	return append(engines, youtube...), nil
}

// Table answers one name for one profile. Each profile gets its own rewrite
// index built from the engines it enabled, so a query pays one hash probe and
// the enable list is never re-checked on the hot path.
type Table struct {
	byProfile map[filter.ProfileID]*rewrite.Table
}

// New builds one table per profile that enabled at least one engine. An enable
// that names an engine the catalog does not hold is an error, because it would
// otherwise silently never match.
func New(enables []Enable, catalog []Engine) (*Table, error) {
	byID := make(map[EngineID]Engine, len(catalog))
	for _, engine := range catalog {
		byID[engine.ID] = engine
	}

	records := make(map[filter.ProfileID][]rewrite.Record)
	for _, enable := range enables {
		engine, exists := byID[enable.Engine]
		if !exists {
			return nil, fmt.Errorf("safesearch: engine %q is not in the catalog", enable.Engine)
		}
		for _, rule := range engine.Rules {
			records[enable.Profile] = append(records[enable.Profile], rewrite.Record{
				Pattern: rule.Host.String(),
				CName:   rule.Target,
			})
		}
	}

	table := &Table{byProfile: make(map[filter.ProfileID]*rewrite.Table, len(records))}
	for profile, list := range records {
		table.byProfile[profile] = rewrite.New(list)
	}
	return table, nil
}

// Lookup answers one name for one profile, reporting false when that profile
// has no engine that owns the name.
func (t *Table) Lookup(name filter.Domain, profile filter.ProfileID) (rewrite.Record, bool) {
	if t == nil {
		return rewrite.Record{}, false
	}
	profileTable, exists := t.byProfile[profile]
	if !exists {
		return rewrite.Record{}, false
	}
	return profileTable.Lookup(name)
}
