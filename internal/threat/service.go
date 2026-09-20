package threat

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"aegis/internal/dns"
	"aegis/internal/store"
)

const (
	// Buffer is how many decisions the analyser may hold before Observe
	// starts dropping. Detection lags before it ever delays a query.
	Buffer = 1024

	// FindingBuffer is how many findings may wait for the writer.
	FindingBuffer = 64

	// FlushPeriod is how long the writer waits between batches at low
	// traffic.
	FlushPeriod = time.Second
)

// Service observes decisions, reduces them to findings, and records them. One
// goroutine owns every window, so the resolver worker running Observe never
// touches shared state and never waits.
type Service struct {
	detector *Detector
	store    *store.Store
	buffer   chan dns.Decision
	findings chan Finding
	dropped  atomic.Uint64
	logger   *slog.Logger
	done     chan struct{}
	flushed  chan struct{}
}

// NewService returns a Service and starts its analyser goroutine.
func NewService(database *store.Store, logger *slog.Logger, options ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{
		detector: NewDetector(options...),
		store:    database,
		buffer:   make(chan dns.Decision, Buffer),
		findings: make(chan Finding, FindingBuffer),
		logger:   logger,
		done:     make(chan struct{}),
		flushed:  make(chan struct{}),
	}
	go service.analyse()
	return service
}

// Observe is the dns.Observer seam. It never waits: a full buffer drops the
// decision and counts it, the way the query log behaves.
func (s *Service) Observe(decision dns.Decision) {
	select {
	case s.buffer <- decision:
	default:
		if s.dropped.Add(1) == 1 {
			s.logger.Warn("threat: the analyser is behind, dropping decisions")
		}
	}
}

// Dropped reports how many decisions never reached the analyser.
func (s *Service) Dropped() uint64 { return s.dropped.Load() }

// Close drains the buffer, records what the analyser found, and stops.
func (s *Service) Close() error {
	close(s.done)
	<-s.flushed
	return nil
}

func (s *Service) analyse() {
	defer close(s.flushed)
	ticker := time.NewTicker(FlushPeriod)
	defer ticker.Stop()

	ctx := context.Background()
	var batch []store.ThreatFinding
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.store.RecordThreatFindings(ctx, batch); err != nil {
			s.logger.Warn("threat: a finding batch was lost", "error", err, "size", len(batch))
		}
		if err := s.store.TrimThreatFindings(ctx); err != nil {
			s.logger.Warn("threat: trim failed", "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-s.done:
			for {
				select {
				case decision := <-s.buffer:
					batch = s.fold(decision, batch)
				default:
					flush()
					return
				}
			}
		case <-ticker.C:
			flush()
		case decision := <-s.buffer:
			batch = s.fold(decision, batch)
		}
	}
}

// fold runs the detector on one decision and queues what it found. A finding
// the queue cannot hold is dropped with a log line rather than blocking the
// analyser: the finding is already in the detector's summary and a repeated
// pattern will flag again.
func (s *Service) fold(decision dns.Decision, batch []store.ThreatFinding) []store.ThreatFinding {
	for _, finding := range s.detector.Observe(decision) {
		select {
		case s.findings <- finding:
		default:
			s.logger.Warn("threat: the finding queue is full, dropping a finding", "kind", string(finding.Kind), "client", finding.Client)
		}
	}
	for {
		select {
		case finding := <-s.findings:
			batch = append(batch, storeFinding(finding))
		default:
			return batch
		}
	}
}

func storeFinding(finding Finding) store.ThreatFinding {
	return store.ThreatFinding{
		Time:     finding.Time,
		Client:   finding.Client,
		Kind:     string(finding.Kind),
		Summary:  finding.Summary,
		Evidence: finding.Evidence,
	}
}
