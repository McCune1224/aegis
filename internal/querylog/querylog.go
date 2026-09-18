// Package querylog persists every decision the DNS handler makes. Observe
// appends to a channel and never blocks, so a slow or absent database cannot
// stall a query; the writer drains in batches and trims the table to its
// retention bound.
package querylog

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"aegis/internal/dns"
	"aegis/internal/store"
)

const (
	// Buffer is how many decisions the log may hold before Observe starts
	// dropping. A database stall loses log rows, never query latency.
	Buffer = 1024

	// FlushSize is how many buffered decisions trigger a single-transaction
	// write before the flush period ends.
	FlushSize = 256

	// FlushPeriod is how long the writer waits between flushes at low
	// traffic, so a quiet network still sees its queries land.
	FlushPeriod = time.Second

	// MaxRows is the retention bound. The writer keeps the newest rows and
	// deletes the rest after every flush.
	MaxRows = 100000
)

// QueryLog records decisions into the store. New starts its writer; Close
// flushes what remains and stops it. A QueryLog is single use.
type QueryLog struct {
	store   *store.Store
	buffer  chan dns.Decision
	dropped atomic.Uint64
	logger  *slog.Logger
	done    chan struct{}
	flushed chan struct{}
}

// New returns a QueryLog writing into the store and starts its writer.
func New(database *store.Store, logger *slog.Logger) *QueryLog {
	if logger == nil {
		logger = slog.Default()
	}
	log := &QueryLog{
		store:   database,
		buffer:  make(chan dns.Decision, Buffer),
		logger:  logger,
		done:    make(chan struct{}),
		flushed: make(chan struct{}),
	}
	go log.write()
	return log
}

// Observe is the dns.Observer seam. It runs on a resolver worker and never
// waits: a full buffer drops the decision and counts it.
func (l *QueryLog) Observe(decision dns.Decision) {
	select {
	case l.buffer <- decision:
	default:
		if l.dropped.Add(1) == 1 {
			l.logger.Warn("querylog: the log is behind, dropping decisions")
		}
	}
}

// Dropped reports how many decisions never reached the log because the
// buffer was full.
func (l *QueryLog) Dropped() uint64 { return l.dropped.Load() }

// Close flushes every buffered decision and stops the writer. It blocks until
// the final batch is committed.
func (l *QueryLog) Close() error {
	close(l.done)
	<-l.flushed
	return nil
}

func (l *QueryLog) write() {
	defer close(l.flushed)
	ticker := time.NewTicker(FlushPeriod)
	defer ticker.Stop()

	ctx := context.Background()
	var batch []dns.Decision
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := l.store.RecordQueries(ctx, entriesFrom(batch)); err != nil {
			l.logger.Warn("querylog: a batch was lost", "error", err, "size", len(batch))
		}
		if err := l.store.TrimQueries(ctx, MaxRows); err != nil {
			l.logger.Warn("querylog: trim failed", "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-l.done:
			for {
				select {
				case decision := <-l.buffer:
					batch = append(batch, decision)
				default:
					flush()
					return
				}
			}
		case <-ticker.C:
			flush()
		case decision := <-l.buffer:
			batch = append(batch, decision)
			if len(batch) >= FlushSize {
				flush()
			}
		}
	}
}

func entriesFrom(decisions []dns.Decision) []store.QueryEntry {
	entries := make([]store.QueryEntry, 0, len(decisions))
	for _, decision := range decisions {
		entry := store.QueryEntry{
			Time:    decision.Time,
			Client:  decision.Address,
			Name:    decision.Name,
			Type:    decision.Type,
			Verdict: decision.Action,
		}
		switch {
		case decision.Rewritten != "":
			entry.Rule = decision.Rewritten
		case decision.Match != nil:
			entry.Rule = decision.Match.RuleID
		}
		entries = append(entries, entry)
	}
	return entries
}
