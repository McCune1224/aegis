package store

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// QueryEntry is one resolved query as the log records it. The rule is empty
// when no rule matched, which means the default policy allowed the query, and
// the threat is empty when no feed claims the name.
type QueryEntry struct {
	Time    time.Time
	Client  netip.Addr
	Name    filter.Domain
	Type    string
	Verdict filter.Action
	Rule    string
	Threat  string
}

// QueryFilter narrows a log read. A nil Verdict, an empty Client or Name, and
// a Limit of zero mean no constraint.
type QueryFilter struct {
	Client  string
	Name    string
	Verdict *filter.Action
	Limit   int
}

// RecordQueries writes one batch of entries in a single transaction, so a
// flush costs one commit however many queries it holds.
func (s *Store) RecordQueries(ctx context.Context, entries []QueryEntry) error {
	err := s.inLogTx(ctx, func(q *storedb.Queries) error {
		for _, entry := range entries {
			params := storedb.InsertQueriesParams{
				Time:    entry.Time.UnixMilli(),
				Client:  entry.Client.Unmap().String(),
				Name:    entry.Name.String(),
				Type:    entry.Type,
				Verdict: entry.Verdict.String(),
				Rule:    entry.Rule,
				Threat:  entry.Threat,
			}
			if _, err := q.InsertQueries(ctx, params); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: record queries: %w", err)
	}
	return nil
}

// Queries reads the log newest first, narrowed by the filter.
func (s *Store) Queries(ctx context.Context, read QueryFilter) ([]QueryEntry, error) {
	limit := read.Limit
	if limit <= 0 {
		limit = 500
	}
	exactName, globName := "", ""
	if read.Name != "" {
		domain, err := filter.ParseDomain(read.Name)
		if err != nil {
			return nil, fmt.Errorf("store: query log name %q: %w", read.Name, err)
		}
		exactName = domain.String()
		// GLOB's metacharacters (*?[]) are names no parsed domain can carry,
		// so the pattern needs no escaping: the dot anchors the label.
		globName = "*." + domain.String()
	}
	params := storedb.ListQueriesParams{
		Client:    read.Client,
		Verdict:   "",
		ExactName: exactName,
		GlobName:  globName,
		Limit:     int64(limit),
	}
	if read.Verdict != nil {
		params.Verdict = read.Verdict.String()
	}

	rows, err := s.logQuer.ListQueries(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("store: queries: %w", err)
	}

	entries := make([]QueryEntry, 0, len(rows))
	for _, row := range rows {
		entry, err := parseQueryRow(row.Time, row.Client, row.Name, row.Type, row.Verdict, row.Rule)
		if err != nil {
			return nil, err
		}
		entry.Threat = row.Threat
		entries = append(entries, entry)
	}
	return entries, nil
}

// TrimQueries keeps only the newest keep rows. It runs after a flush, so the
// retention bound is enforced by the same writer that fills the table.
func (s *Store) TrimQueries(ctx context.Context, keep int) error {
	if err := s.logQuer.TrimQueries(ctx, int64(keep)); err != nil {
		return fmt.Errorf("store: trim queries: %w", err)
	}
	return nil
}

func parseQueryRow(millis int64, client, name, typ, verdict, rule string) (QueryEntry, error) {
	address, err := netip.ParseAddr(client)
	if err != nil {
		return QueryEntry{}, fmt.Errorf("store: query log client %q: %w", client, err)
	}
	domain, err := filter.ParseDomain(name)
	if err != nil {
		return QueryEntry{}, fmt.Errorf("store: query log name %q: %w", name, err)
	}
	action, err := filter.ParseAction(verdict)
	if err != nil {
		return QueryEntry{}, fmt.Errorf("store: query log verdict %q: %w", verdict, err)
	}
	return QueryEntry{
		Time:    time.UnixMilli(millis),
		Client:  address,
		Name:    domain,
		Type:    typ,
		Verdict: action,
		Rule:    rule,
	}, nil
}
