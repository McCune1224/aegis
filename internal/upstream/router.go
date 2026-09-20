package upstream

import (
	"cmp"
	"slices"
	"strings"
)

// RouteSpec is one stored route: send one client's queries, or one domain's,
// or the overlap of both, to the named upstream.
type RouteSpec struct {
	ID       int64
	Client   string
	Domain   string
	Upstream string
}

// Router is the immutable route table one pool generation reads. Lookup walks
// the entries in match order and returns the first routed upstream, or the
// empty string when no route claims the query and the pool may choose.
type Router struct {
	entries []RouteSpec
}

// NewRouter bakes the match order in: a client-scoped entry outranks a
// domain-scoped one, a deeper domain outranks a shallower one, and the row id
// breaks the remaining ties. Domains are lowercased at build, so matching is
// ASCII case-insensitive.
func NewRouter(specs []RouteSpec) *Router {
	entries := slices.Clone(specs)
	for i := range entries {
		entries[i].Domain = strings.ToLower(entries[i].Domain)
	}
	slices.SortStableFunc(entries, func(a, b RouteSpec) int {
		if (a.Client != "") != (b.Client != "") {
			if a.Client != "" {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(routeLabels(b.Domain), routeLabels(a.Domain)),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return &Router{entries: entries}
}

// Lookup names the upstream one query must use, or "" when it may use the
// pool. An entry with no client matches every client, and an entry with no
// domain matches every name; a domain entry covers its own name and every
// name under it.
func (r *Router) Lookup(client, domain string) string {
	for _, entry := range r.entries {
		if entry.Client != "" && entry.Client != client {
			continue
		}
		if entry.Domain != "" && domain != entry.Domain && !strings.HasSuffix(domain, "."+entry.Domain) {
			continue
		}
		return entry.Upstream
	}
	return ""
}

// routeLabels counts the labels a domain entry carries, so the deeper entry
// sorts first. A domain-less entry has none and sorts last among its peers.
func routeLabels(domain string) int {
	if domain == "" {
		return 0
	}
	return strings.Count(domain, ".") + 1
}
