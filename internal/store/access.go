package store

import (
	"context"
	"fmt"
	"net/netip"

	"aegis/internal/store/storedb"
)

// SetAccess replaces both stored client sets in one transaction, so a reload
// never reads the new allowed list beside the old disallowed one.
func (s *Store) SetAccess(ctx context.Context, allowed, disallowed []netip.Prefix) error {
	return s.inTx(ctx, func(q *storedb.Queries) error {
		if err := q.DeleteAccess(ctx); err != nil {
			return fmt.Errorf("store: save access: %w", err)
		}
		for _, prefix := range allowed {
			if err := q.InsertAccess(ctx, storedb.InsertAccessParams{Kind: "allow", Cidr: prefix.Masked().String()}); err != nil {
				return fmt.Errorf("store: save access: %w", err)
			}
		}
		for _, prefix := range disallowed {
			if err := q.InsertAccess(ctx, storedb.InsertAccessParams{Kind: "deny", Cidr: prefix.Masked().String()}); err != nil {
				return fmt.Errorf("store: save access: %w", err)
			}
		}
		return nil
	})
}

// Access returns the two stored client sets as the listener gate matches them.
func (s *Store) Access(ctx context.Context) ([]netip.Prefix, []netip.Prefix, error) {
	return loadAccess(ctx, s.queries)
}

func loadAccess(ctx context.Context, q *storedb.Queries) ([]netip.Prefix, []netip.Prefix, error) {
	rows, err := q.ListAccess(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("store: access: %w", err)
	}
	var allowed, disallowed []netip.Prefix
	for _, row := range rows {
		prefix, err := netip.ParsePrefix(row.Cidr)
		if err != nil {
			return nil, nil, fmt.Errorf("store: access row %q %q: %w", row.Kind, row.Cidr, err)
		}
		switch row.Kind {
		case "allow":
			allowed = append(allowed, prefix)
		case "deny":
			disallowed = append(disallowed, prefix)
		default:
			return nil, nil, fmt.Errorf("store: access row %q %q: unknown kind", row.Kind, row.Cidr)
		}
	}
	return allowed, disallowed, nil
}

// validateAccess refuses a prefix that still carries host bits and any prefix
// listed twice: one in each set can never match anything useful, and a
// repeated row is the same kind of operator mistake.
func validateAccess(allowed, disallowed []netip.Prefix) error {
	seen := make(map[netip.Prefix]string, len(allowed)+len(disallowed))
	for _, set := range []struct {
		kind     string
		prefixes []netip.Prefix
	}{{"allowed", allowed}, {"disallowed", disallowed}} {
		for _, prefix := range set.prefixes {
			masked := prefix.Masked()
			if prefix != masked {
				return fmt.Errorf("store: access %s %q has host bits set", set.kind, prefix)
			}
			if where, listed := seen[masked]; listed {
				return fmt.Errorf("store: access prefix %q is listed twice, %s and %s", masked, where, set.kind)
			}
			seen[masked] = set.kind
		}
	}
	return nil
}
