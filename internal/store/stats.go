package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"aegis/internal/filter"
	"aegis/internal/store/storedb"
)

// StatsRead scopes one summary. Since is the window's oldest inclusive
// millisecond and Limit caps each top list.
type StatsRead struct {
	Since int64
	Limit int
}

// NameCount is one row of a top list.
type NameCount struct {
	Name  string
	Count int64
}

// ClientCount is a client row of the top list.
type ClientCount struct {
	Client string
	Count  int64
}

// HourBucket is one wall-clock hour of the series.
type HourBucket struct {
	Start     time.Time
	Total     int64
	Blocked   int64
	Rewritten int64
}

// StatsSummary is the aggregate the stats endpoint serves.
type StatsSummary struct {
	Total      int64
	Allowed    int64
	Blocked    int64
	Rewritten  int64
	Series     []HourBucket
	TopQueried []NameCount
	TopBlocked []NameCount
	TopClients []ClientCount
}

const hourMillis = int64(time.Hour / time.Millisecond)

// Stats aggregates the window in SQL. The buckets come back zero-filled and
// oldest first, so the caller renders the series without padding.
func (s *Store) Stats(ctx context.Context, read StatsRead) (StatsSummary, error) {
	if read.Limit <= 0 {
		read.Limit = 10
	}

	summary := StatsSummary{
		Series:     []HourBucket{},
		TopQueried: []NameCount{},
		TopBlocked: []NameCount{},
		TopClients: []ClientCount{},
	}

	totals, err := s.logQuer.StatsVerdictTotals(ctx, read.Since)
	if err != nil {
		return summary, fmt.Errorf("store: stats totals: %w", err)
	}
	for _, row := range totals {
		summary.Total += row.Count
		switch action, parse := filter.ParseAction(row.Verdict); {
		case parse != nil:
			return summary, fmt.Errorf("store: stats verdict %q: %w", row.Verdict, parse)
		case action == filter.ActionBlock:
			summary.Blocked = row.Count
		case action == filter.ActionRewrite:
			summary.Rewritten = row.Count
		default:
			summary.Allowed = row.Count
		}
	}

	buckets, err := s.logQuer.StatsHourBuckets(ctx, read.Since)
	if err != nil {
		return summary, fmt.Errorf("store: stats buckets: %w", err)
	}
	if len(buckets) > 0 {
		oldest, newest := buckets[0].Bucket, buckets[0].Bucket
		for _, row := range buckets {
			oldest = min(oldest, row.Bucket)
			newest = max(newest, row.Bucket)
		}
		byStart := make(map[int64]HourBucket, len(buckets))
		for _, row := range buckets {
			bucket := byStart[row.Bucket]
			bucket.Start = time.UnixMilli(row.Bucket)
			bucket.Total += row.Count
			switch action, parse := filter.ParseAction(row.Verdict); {
			case parse != nil:
				return summary, fmt.Errorf("store: stats verdict %q: %w", row.Verdict, parse)
			case action == filter.ActionBlock:
				bucket.Blocked = row.Count
			case action == filter.ActionRewrite:
				bucket.Rewritten = row.Count
			}
			byStart[row.Bucket] = bucket
		}
		for start := oldest; start <= newest; start += hourMillis {
			summary.Series = append(summary.Series, byStart[start])
		}
	}

	names, err := s.logQuer.StatsTopNames(ctx, storedb.StatsTopNamesParams{Time: read.Since, Limit: int64(read.Limit)})
	if err != nil {
		return summary, fmt.Errorf("store: stats top names: %w", err)
	}
	for _, row := range names {
		summary.TopQueried = append(summary.TopQueried, NameCount{Name: row.Name, Count: row.Count})
	}

	blockedNames, err := s.logQuer.StatsTopBlockedNames(ctx, storedb.StatsTopBlockedNamesParams{Time: read.Since, Limit: int64(read.Limit)})
	if err != nil {
		return summary, fmt.Errorf("store: stats top blocked: %w", err)
	}
	for _, row := range blockedNames {
		summary.TopBlocked = append(summary.TopBlocked, NameCount{Name: row.Name, Count: row.Count})
	}

	clients, err := s.logQuer.StatsTopClients(ctx, storedb.StatsTopClientsParams{Time: read.Since, Limit: int64(read.Limit)})
	if err != nil {
		return summary, fmt.Errorf("store: stats top clients: %w", err)
	}
	for _, row := range clients {
		addr, err := netip.ParseAddr(row.Client)
		if err != nil {
			return summary, fmt.Errorf("store: stats client %q: %w", row.Client, err)
		}
		summary.TopClients = append(summary.TopClients, ClientCount{Client: addr.Unmap().String(), Count: row.Count})
	}
	return summary, nil
}

// ResetStats deletes every logged row inside the window. The log page and the
// summary share one table, so the same span leaves the log.
func (s *Store) ResetStats(ctx context.Context, since int64) error {
	if err := s.logQuer.DeleteStatsWindow(ctx, since); err != nil {
		return fmt.Errorf("store: stats reset: %w", err)
	}
	return nil
}

// TrimQueriesBefore drops rows older than the given millisecond. The log
// writer runs it beside the row bound, so the retention interval holds even
// when traffic never reaches the row cap.
func (s *Store) TrimQueriesBefore(ctx context.Context, before int64) error {
	if err := s.logQuer.TrimQueriesBefore(ctx, before); err != nil {
		return fmt.Errorf("store: trim before %d: %w", before, err)
	}
	return nil
}

// StatsIgnored lists the hosts the summary excludes, exact name or subdomain.
func (s *Store) StatsIgnored(ctx context.Context) ([]string, error) {
	rows, err := s.queries.StatsIgnoredNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: stats ignored: %w", err)
	}
	return rows, nil
}

// SetStatsIgnored replaces the ignored hosts. Every name is parsed at the
// boundary, so the table holds the lowercase form and the SQL needs no
// case folding.
func (s *Store) SetStatsIgnored(ctx context.Context, names []string) error {
	parsed := make([]string, 0, len(names))
	for _, name := range names {
		domain, err := filter.ParseDomain(name)
		if err != nil {
			return fmt.Errorf("store: stats ignored %q: %w", name, err)
		}
		parsed = append(parsed, domain.String())
	}
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		if err := q.ClearStatsIgnored(ctx); err != nil {
			return err
		}
		for _, name := range parsed {
			if err := q.InsertStatsIgnoredName(ctx, name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: stats ignored: %w", err)
	}
	return nil
}

// DefaultStatsInterval is what a fresh store keeps.
const DefaultStatsInterval = 24 * time.Hour

// StatsConfig is the retention knob. Zero disables the summary and the
// time-based pruning; the log keeps its row bound regardless.
type StatsConfig struct {
	Interval time.Duration
}

type statsConfigDoc struct {
	IntervalHours int `json:"interval_hours"`
}

// StatsConfig reads the stored retention, answering the default when no
// operator saved one.
func (s *Store) StatsConfig(ctx context.Context) (StatsConfig, error) {
	value, err := s.queries.GetSetting(ctx, "stats_config")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StatsConfig{Interval: DefaultStatsInterval}, nil
		}
		return StatsConfig{}, fmt.Errorf("store: stats config: %w", err)
	}
	var doc statsConfigDoc
	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		return StatsConfig{}, fmt.Errorf("store: stats config: %w", err)
	}
	return StatsConfig{Interval: time.Duration(doc.IntervalHours) * time.Hour}, nil
}

// SaveStatsConfig stores the retention.
func (s *Store) SaveStatsConfig(ctx context.Context, config StatsConfig) error {
	encoded, err := json.Marshal(statsConfigDoc{IntervalHours: int(config.Interval / time.Hour)})
	if err != nil {
		return fmt.Errorf("store: stats config: %w", err)
	}
	if err := s.queries.SetSetting(ctx, storedb.SetSettingParams{Key: "stats_config", Value: string(encoded)}); err != nil {
		return fmt.Errorf("store: stats config: %w", err)
	}
	return nil
}
