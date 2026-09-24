package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"time"

	"aegis/internal/blocklist"
	"aegis/internal/client"
	"aegis/internal/filter"
	"aegis/internal/rewrite"
	"aegis/internal/safesearch"
	"aegis/internal/services"
)

// docVersion is the export format this build writes and the only version
// ParseDocument accepts.
const docVersion = 1

// Document is the operator-authored configuration as one JSON file. It carries
// what the operator chose and nothing the server measures: no source body,
// fetch record, health counters, query log, or seeded marker.
type Document struct {
	Version        int           `json:"version"`
	DefaultProfile string        `json:"default_profile"`
	Profiles       []profileDoc  `json:"profiles"`
	Clients        []clientDoc   `json:"clients"`
	Rules          []ruleDoc     `json:"rules"`
	Schedules      []scheduleDoc `json:"schedules"`
	Rewrites       []rewriteDoc  `json:"rewrites"`
	Upstreams      []upstreamDoc `json:"upstreams"`
	Routes         []routeDoc    `json:"routes"`
	Access         accessDoc     `json:"access"`
	Sources        []sourceDoc   `json:"sources"`

	// The four sections below were added after version 1 and are optional:
	// an older file leaves them nil, which means the target keeps what it
	// holds, the same rule every unnamed row follows on import.
	ProfileServices []profileServicesDoc   `json:"profile_services"`
	ClientServices  []clientServicesDoc    `json:"client_services"`
	Safesearch      []profileSafesearchDoc `json:"safesearch"`
	Windows         []windowDoc            `json:"windows"`
	// Services carries only the catalog rows the sections above reference, so
	// an import into an offline store can satisfy its foreign keys and compile
	// before any fetch. The fetch record stays out; a later refresh replaces
	// the rules with the current ones.
	Services []serviceDoc `json:"services"`
}

type profileDoc struct {
	Name    string  `json:"name"`
	Extends string  `json:"extends,omitempty"`
	Mode    *string `json:"mode,omitempty"`
	Custom  *string `json:"custom,omitempty"`
}

type clientDoc struct {
	Key       string   `json:"key"`
	Profile   string   `json:"profile"`
	Notes     string   `json:"notes"`
	Addresses []string `json:"addresses"`
	MACs      []string `json:"macs"`
	Prefixes  []string `json:"prefixes"`
}

type ruleDoc struct {
	ID       int64  `json:"id"`
	Domain   string `json:"domain"`
	Kind     string `json:"kind"`
	Action   string `json:"action"`
	Schedule string `json:"schedule,omitempty"`
	Client   string `json:"client,omitempty"`
	Notes    string `json:"notes,omitempty"`
	Created  string `json:"created,omitempty"`
}

type scheduleWindowDoc struct {
	Days  []time.Weekday `json:"days"`
	Start string         `json:"start"`
	End   string         `json:"end"`
}

type scheduleDoc struct {
	Name     string              `json:"name"`
	Priority int                 `json:"priority"`
	Windows  []scheduleWindowDoc `json:"windows"`
}

type rewriteDoc struct {
	Pattern string `json:"pattern"`
	Target  string `json:"target"`
}

type upstreamDoc struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
	Backup  bool   `json:"backup"`
}

type routeDoc struct {
	Domain   string `json:"domain"`
	Client   string `json:"client"`
	Upstream string `json:"upstream"`
}

type accessDoc struct {
	Allowed    []string `json:"allowed"`
	Disallowed []string `json:"disallowed"`
}

type sourceDoc struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Format  string `json:"format"`
	Enabled bool   `json:"enabled"`
}

type profileServicesDoc struct {
	Profile  string   `json:"profile"`
	Services []string `json:"services"`
}

type clientServicesDoc struct {
	Client   string   `json:"client"`
	Services []string `json:"services"`
}

type profileSafesearchDoc struct {
	Profile string   `json:"profile"`
	Engines []string `json:"engines"`
}

type windowDoc struct {
	Name     string   `json:"name"`
	Action   string   `json:"action"`
	Schedule string   `json:"schedule"`
	Clients  []string `json:"clients"`
	Services []string `json:"services"`
}

type serviceDoc struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Group   string   `json:"group"`
	Rules   []string `json:"rules"`
	IconSVG string   `json:"icon_svg"`
}

// ParseDocument reads one exported configuration. A file without the version
// marker or from another version is refused with the problem named, so an
// import never guesses at what a file means.
func ParseDocument(data []byte) (Document, error) {
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, fmt.Errorf("store: invalid document: %w", err)
	}
	if document.Version == 0 {
		return Document{}, errors.New("store: the document carries no version marker")
	}
	if document.Version != docVersion {
		return Document{}, fmt.Errorf("store: unsupported document version %d", document.Version)
	}
	return document, nil
}

// ReadDocument reads the stored configuration as one export document.
func (s *Store) ReadDocument(ctx context.Context) (Document, error) {
	cfg, err := s.Load(ctx)
	if err != nil {
		return Document{}, err
	}
	rules, err := s.Rules(ctx)
	if err != nil {
		return Document{}, err
	}
	sources, err := s.Sources(ctx)
	if err != nil {
		return Document{}, err
	}

	document := Document{
		Version:        docVersion,
		DefaultProfile: string(cfg.Default),
		Profiles:       make([]profileDoc, 0, len(cfg.Profiles)),
		Clients:        make([]clientDoc, 0, len(cfg.Clients)),
		Rules:          make([]ruleDoc, 0, len(rules)),
		Schedules:      make([]scheduleDoc, 0, len(cfg.Schedules)),
		Rewrites:       make([]rewriteDoc, 0, len(cfg.Rewrites)),
		Upstreams:      make([]upstreamDoc, 0, len(cfg.Upstreams)),
		Routes:         make([]routeDoc, 0, len(cfg.Routes)),
		Access:         accessDoc{Allowed: []string{}, Disallowed: []string{}},
		Sources:        make([]sourceDoc, 0, len(sources)),
	}

	for _, spec := range cfg.Profiles {
		profile := profileDoc{Name: string(spec.ID), Extends: string(spec.Extends)}
		if spec.Mode != nil {
			mode := spec.Mode.String()
			profile.Mode = &mode
		}
		if spec.Custom != nil {
			custom := spec.Custom.String()
			profile.Custom = &custom
		}
		document.Profiles = append(document.Profiles, profile)
	}

	for _, record := range cfg.Clients {
		document.Clients = append(document.Clients, clientDoc{
			Key:       string(record.Key),
			Profile:   string(record.Profile),
			Notes:     record.Notes,
			Addresses: addressesText(record.Addresses),
			MACs:      macsText(record.MACs),
			Prefixes:  prefixesText(record.Prefixes),
		})
	}

	for _, rule := range rules {
		stored := ruleDoc{
			ID:       rule.ID,
			Domain:   rule.Value(),
			Kind:     rule.Kind.String(),
			Action:   rule.Action.String(),
			Schedule: rule.Schedule,
			Client:   string(rule.Client),
			Notes:    rule.Notes,
		}
		if !rule.Created.IsZero() {
			stored.Created = rule.Created.Format(time.RFC3339)
		}
		document.Rules = append(document.Rules, stored)
	}

	for _, schedule := range cfg.Schedules {
		stored := scheduleDoc{Name: schedule.Name, Priority: schedule.Priority, Windows: make([]scheduleWindowDoc, 0, len(schedule.Windows))}
		for _, window := range schedule.Windows {
			stored.Windows = append(stored.Windows, scheduleWindowDoc{
				Days:  window.Days,
				Start: filter.MinuteOfDay(window.Start),
				End:   filter.MinuteOfDay(window.End),
			})
		}
		document.Schedules = append(document.Schedules, stored)
	}

	for _, record := range cfg.Rewrites {
		document.Rewrites = append(document.Rewrites, rewriteDoc{
			Pattern: record.Pattern,
			Target:  rewrite.TargetText(record),
		})
	}

	for _, row := range cfg.Upstreams {
		document.Upstreams = append(document.Upstreams, upstreamDoc(row))
	}

	for _, route := range cfg.Routes {
		document.Routes = append(document.Routes, routeDoc{
			Domain:   route.Domain,
			Client:   route.Client,
			Upstream: route.Upstream,
		})
	}

	for _, prefix := range cfg.Allowed {
		document.Access.Allowed = append(document.Access.Allowed, prefix.String())
	}
	for _, prefix := range cfg.Disallowed {
		document.Access.Disallowed = append(document.Access.Disallowed, prefix.String())
	}

	for _, source := range sources {
		document.Sources = append(document.Sources, sourceDoc{
			Name:    source.Name,
			URL:     source.URL,
			Format:  source.Format.String(),
			Enabled: source.Enabled,
		})
	}

	referenced := make(map[string]bool)
	for _, group := range groupStrings(profileServiceRows(cfg.ProfileServices)) {
		document.ProfileServices = append(document.ProfileServices, profileServicesDoc{Profile: group.Key, Services: group.Values})
		for _, id := range group.Values {
			referenced[id] = true
		}
	}
	for _, group := range groupStrings(clientServiceRows(cfg.ClientServices)) {
		document.ClientServices = append(document.ClientServices, clientServicesDoc{Client: group.Key, Services: group.Values})
		for _, id := range group.Values {
			referenced[id] = true
		}
	}
	for _, group := range groupStrings(safesearchRows(cfg.Safesearch)) {
		document.Safesearch = append(document.Safesearch, profileSafesearchDoc{Profile: group.Key, Engines: group.Values})
	}
	for _, window := range cfg.ServiceWindows {
		clients := make([]string, 0, len(window.Clients))
		for _, client := range window.Clients {
			clients = append(clients, string(client))
		}
		document.Windows = append(document.Windows, windowDoc{
			Name:     window.Name,
			Action:   window.Action.String(),
			Schedule: window.Schedule,
			Clients:  clients,
			Services: window.Services,
		})
		for _, id := range window.Services {
			referenced[id] = true
		}
	}
	for _, row := range cfg.Services {
		if referenced[row.ID] {
			document.Services = append(document.Services, serviceDoc{
				ID:      row.ID,
				Name:    row.Name,
				Group:   row.Group,
				Rules:   row.Rules,
				IconSVG: row.IconSVG,
			})
		}
	}

	return document, nil
}

// ApplyDocument upserts every section of the document into the store. Rows the
// document does not name are left as they are, so an import moves a
// configuration without deleting anything the target already had. The result
// is compiled the way a reload would, so an import that could not serve fails
// here with the reason named.
func (s *Store) ApplyDocument(ctx context.Context, document Document) error {
	for _, profile := range document.Profiles {
		spec, err := profile.spec()
		if err != nil {
			return err
		}
		spec.Extends = ""
		if err := s.SaveProfile(ctx, spec); err != nil {
			return err
		}
	}
	// A profile may extend one the document lists after it, so the references
	// go back once every profile row exists.
	for _, profile := range document.Profiles {
		spec, err := profile.spec()
		if err != nil {
			return err
		}
		if err := s.SaveProfile(ctx, spec); err != nil {
			return err
		}
	}
	if document.DefaultProfile != "" {
		if err := s.SetDefaultProfile(ctx, filter.ProfileID(document.DefaultProfile)); err != nil {
			return err
		}
	}

	for _, client := range document.Clients {
		record, err := client.record()
		if err != nil {
			return err
		}
		if err := s.SaveClient(ctx, record); err != nil {
			return err
		}
	}

	for _, schedule := range document.Schedules {
		spec, err := schedule.spec()
		if err != nil {
			return err
		}
		if err := s.SaveSchedule(ctx, spec); err != nil {
			return err
		}
	}

	knownRules, err := s.ruleIDs(ctx)
	if err != nil {
		return err
	}
	for _, rule := range document.Rules {
		parsed, err := rule.rule()
		if err != nil {
			return err
		}
		if !knownRules[parsed.ID] {
			parsed.ID = 0
		}
		if _, err := s.SaveRule(ctx, parsed); err != nil {
			return err
		}
	}

	for _, record := range document.Rewrites {
		parsed, err := rewrite.Parse(record.Pattern, record.Target)
		if err != nil {
			return fmt.Errorf("store: %w", err)
		}
		if err := s.SaveRewrite(ctx, parsed); err != nil {
			return err
		}
	}

	for _, row := range document.Upstreams {
		stored := Upstream(row)
		if err := s.SaveUpstream(ctx, stored); err != nil {
			return err
		}
	}

	knownRoutes, err := s.Routes(ctx)
	if err != nil {
		return err
	}
	known := make(map[Route]bool, len(knownRoutes))
	for _, route := range knownRoutes {
		known[Route{Domain: route.Domain, Client: route.Client, Upstream: route.Upstream}] = true
	}
	for _, route := range document.Routes {
		stored := Route{Domain: route.Domain, Client: route.Client, Upstream: route.Upstream}
		if known[stored] {
			continue
		}
		if _, err := s.SaveRoute(ctx, stored); err != nil {
			return err
		}
	}

	if document.Access.Allowed != nil || document.Access.Disallowed != nil {
		allowed, err := parsePrefixSet("allowed", document.Access.Allowed)
		if err != nil {
			return err
		}
		disallowed, err := parsePrefixSet("disallowed", document.Access.Disallowed)
		if err != nil {
			return err
		}
		if err := s.SetAccess(ctx, allowed, disallowed); err != nil {
			return err
		}
	}

	for _, source := range document.Sources {
		format, err := blocklist.ParseFormat(source.Format)
		if err != nil {
			return fmt.Errorf("store: source %q: %w", source.Name, err)
		}
		stored := Source{Name: source.Name, URL: source.URL, Format: format, Enabled: source.Enabled}
		if err := s.SaveSource(ctx, stored); err != nil {
			return err
		}
	}

	// Materialize the catalog rows the document references, but only the ones
	// this store does not hold: a repeat import never regresses a fresher copy,
	// and an offline import can still satisfy the enablements that follow.
	if len(document.Services) > 0 {
		existing, err := s.Services(ctx)
		if err != nil {
			return err
		}
		known := make(map[string]bool, len(existing))
		for _, row := range existing {
			known[row.ID] = true
		}
		missing := make([]services.Service, 0, len(document.Services))
		for _, row := range document.Services {
			if !known[row.ID] {
				missing = append(missing, services.Service{
					ID:      row.ID,
					Name:    row.Name,
					Group:   row.Group,
					Rules:   row.Rules,
					IconSVG: row.IconSVG,
				})
			}
		}
		if len(missing) > 0 {
			if err := s.SaveCatalog(ctx, time.Now().UTC(), missing); err != nil {
				return err
			}
		}
	}

	if document.ProfileServices != nil {
		for _, group := range document.ProfileServices {
			if err := s.SetProfileServices(ctx, filter.ProfileID(group.Profile), group.Services); err != nil {
				return err
			}
		}
	}
	if document.ClientServices != nil {
		for _, group := range document.ClientServices {
			if err := s.SetClientServices(ctx, filter.ClientKey(group.Client), group.Services); err != nil {
				return err
			}
		}
	}
	if document.Safesearch != nil {
		for _, group := range document.Safesearch {
			engines := make([]safesearch.EngineID, 0, len(group.Engines))
			for _, id := range group.Engines {
				engines = append(engines, safesearch.EngineID(id))
			}
			if err := s.SetProfileSafesearch(ctx, filter.ProfileID(group.Profile), engines); err != nil {
				return err
			}
		}
	}
	for _, window := range document.Windows {
		action, err := filter.ParseAction(window.Action)
		if err != nil {
			return fmt.Errorf("store: window %q: %w", window.Name, err)
		}
		clients := make([]filter.ClientKey, 0, len(window.Clients))
		for _, client := range window.Clients {
			clients = append(clients, filter.ClientKey(client))
		}
		if err := s.SaveServiceWindow(ctx, ServiceWindow{
			Name:     window.Name,
			Schedule: window.Schedule,
			Action:   action,
			Clients:  clients,
			Services: window.Services,
		}); err != nil {
			return err
		}
	}

	cfg, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("store: imported configuration: %w", err)
	}
	return nil
}

func (p profileDoc) spec() (filter.ProfileSpec, error) {
	spec := filter.ProfileSpec{ID: filter.ProfileID(p.Name), Extends: filter.ProfileID(p.Extends)}
	if p.Mode != nil {
		mode, err := filter.ParseBlockingMode(*p.Mode)
		if err != nil {
			return filter.ProfileSpec{}, fmt.Errorf("store: profile %q: %w", p.Name, err)
		}
		spec.Mode = &mode
	}
	if p.Custom != nil {
		address, err := netip.ParseAddr(*p.Custom)
		if err != nil {
			return filter.ProfileSpec{}, fmt.Errorf("store: profile %q: custom: %w", p.Name, err)
		}
		spec.Custom = &address
	}
	return spec, nil
}

func (c clientDoc) record() (Client, error) {
	record := Client{Key: filter.ClientKey(c.Key), Profile: filter.ProfileID(c.Profile), Notes: c.Notes}
	for i, raw := range c.Addresses {
		address, err := netip.ParseAddr(raw)
		if err != nil {
			return Client{}, fmt.Errorf("store: client %q: addresses[%d]: %w", c.Key, i, err)
		}
		record.Addresses = append(record.Addresses, address)
	}
	for i, raw := range c.MACs {
		hardware, err := net.ParseMAC(raw)
		if err != nil {
			return Client{}, fmt.Errorf("store: client %q: macs[%d]: %w", c.Key, i, err)
		}
		record.MACs = append(record.MACs, hardware)
	}
	for i, raw := range c.Prefixes {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return Client{}, fmt.Errorf("store: client %q: prefixes[%d]: %w", c.Key, i, err)
		}
		record.Prefixes = append(record.Prefixes, prefix)
	}
	return record, nil
}

func (s scheduleDoc) spec() (filter.ScheduleSpec, error) {
	spec := filter.ScheduleSpec{Name: s.Name, Priority: s.Priority}
	for _, window := range s.Windows {
		start, err := filter.ParseMinuteOfDay(window.Start)
		if err != nil {
			return filter.ScheduleSpec{}, fmt.Errorf("store: schedule %q: %w", s.Name, err)
		}
		end, err := filter.ParseMinuteOfDay(window.End)
		if err != nil {
			return filter.ScheduleSpec{}, fmt.Errorf("store: schedule %q: %w", s.Name, err)
		}
		spec.Windows = append(spec.Windows, filter.Window{Days: window.Days, Start: start, End: end})
	}
	return spec, nil
}

func (r ruleDoc) rule() (Rule, error) {
	kind, err := filter.ParseMatchKind(r.Kind)
	if err != nil {
		return Rule{}, fmt.Errorf("store: rule %d: %w", r.ID, err)
	}
	rule := Rule{
		ID:       r.ID,
		Kind:     kind,
		Schedule: r.Schedule,
		Client:   filter.ClientKey(r.Client),
		Notes:    r.Notes,
	}
	if err := rule.SetValue(kind, r.Domain); err != nil {
		return Rule{}, fmt.Errorf("store: rule %d: %w", r.ID, err)
	}
	if rule.Action, err = filter.ParseAction(r.Action); err != nil {
		return Rule{}, fmt.Errorf("store: rule %d: %w", r.ID, err)
	}
	if r.Created != "" {
		rule.Created, err = time.Parse(time.RFC3339, r.Created)
		if err != nil {
			return Rule{}, fmt.Errorf("store: rule %d: %w", r.ID, err)
		}
	}
	return rule, nil
}

func (s *Store) ruleIDs(ctx context.Context) (map[int64]bool, error) {
	rules, err := s.Rules(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[int64]bool, len(rules))
	for _, rule := range rules {
		known[rule.ID] = true
	}
	return known, nil
}

// groupEntry is one scope and the values it holds.
type groupEntry struct {
	Key    string
	Values []string
}

// groupStrings folds (scope, values) rows into one entry per scope, ordered by
// scope, so two exports of the same configuration are byte-identical.
func groupStrings(rows [][2]string) []groupEntry {
	var order []string
	grouped := make(map[string][]string)
	for _, row := range rows {
		if _, seen := grouped[row[0]]; !seen {
			order = append(order, row[0])
		}
		grouped[row[0]] = append(grouped[row[0]], row[1])
	}
	sort.Strings(order)
	entries := make([]groupEntry, 0, len(order))
	for _, key := range order {
		entries = append(entries, groupEntry{Key: key, Values: grouped[key]})
	}
	return entries
}

func profileServiceRows(enables []ProfileService) [][2]string {
	rows := make([][2]string, 0, len(enables))
	for _, enable := range enables {
		rows = append(rows, [2]string{string(enable.Profile), enable.Service})
	}
	return rows
}

func clientServiceRows(enables []ClientService) [][2]string {
	rows := make([][2]string, 0, len(enables))
	for _, enable := range enables {
		rows = append(rows, [2]string{string(enable.Client), enable.Service})
	}
	return rows
}

func safesearchRows(enables []ProfileSafesearch) [][2]string {
	rows := make([][2]string, 0, len(enables))
	for _, enable := range enables {
		rows = append(rows, [2]string{string(enable.Profile), string(enable.Engine)})
	}
	return rows
}

func parsePrefixSet(kind string, list []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(list))
	for i, raw := range list {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("store: access %s[%d]: %w", kind, i, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func addressesText(addresses []netip.Addr) []string {
	text := make([]string, 0, len(addresses))
	for _, address := range addresses {
		text = append(text, address.String())
	}
	return text
}

func macsText(macs []net.HardwareAddr) []string {
	text := make([]string, 0, len(macs))
	for _, hardware := range macs {
		text = append(text, client.NormalizeMAC(hardware))
	}
	return text
}

func prefixesText(prefixes []netip.Prefix) []string {
	text := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		text = append(text, prefix.String())
	}
	return text
}
