package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/dns"
	"aegis/internal/filter"
)

// subscriberBuffer is how many decisions one stream may fall behind before the
// hub starts dropping for it. A dashboard that cannot keep up loses events
// rather than holding a resolver worker open.
const subscriberBuffer = 64

// Hub fans decisions out to every open query stream. Observe never blocks. A
// subscriber whose buffer is full has the decision dropped and counted, which is
// what keeps a stalled browser tab from backing up into the DNS path.
type Hub struct {
	mu          sync.Mutex
	subscribers map[chan dns.Decision]struct{}
	dropped     atomic.Uint64
	logger      *slog.Logger
}

// NewHub returns an empty Hub. A nil logger uses the default.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		subscribers: make(map[chan dns.Decision]struct{}),
		logger:      logger,
	}
}

// Observe sends one decision to every subscriber. It is the dns.Observer seam,
// so it runs on a resolver worker and must return immediately.
func (h *Hub) Observe(decision dns.Decision) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for subscriber := range h.subscribers {
		select {
		case subscriber <- decision:
		default:
			if h.dropped.Add(1) == 1 {
				h.logger.Warn("api: a query stream is behind, dropping decisions")
			}
		}
	}
}

// Subscribe returns one stream's channel and the function that ends it. The
// channel is never closed, because closing it while Observe sends is a data
// race; ending the stream only removes it from the map.
func (h *Hub) Subscribe() (<-chan dns.Decision, func()) {
	channel := make(chan dns.Decision, subscriberBuffer)
	h.mu.Lock()
	h.subscribers[channel] = struct{}{}
	h.mu.Unlock()

	return channel, func() {
		h.mu.Lock()
		delete(h.subscribers, channel)
		h.mu.Unlock()
	}
}

// Dropped is how many decisions no stream received because it was behind.
func (h *Hub) Dropped() uint64 { return h.dropped.Load() }

// decisionEvent is a decision on the wire. It is the stream's boundary type, so
// the filter package's shapes do not leak into the JSON the dashboard parses.
type decisionEvent struct {
	Time    time.Time  `json:"time"`
	Address string     `json:"address"`
	Name    string     `json:"name"`
	Action  string     `json:"action"`
	Rule    *ruleEvent `json:"rule,omitempty"`
}

type ruleEvent struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Pattern string `json:"pattern"`
}

func decisionEventFrom(decision dns.Decision) decisionEvent {
	event := decisionEvent{
		Time:    decision.Time,
		Address: decision.Address.String(),
		Name:    decision.Name.String(),
		Action:  "allow",
	}
	if decision.Action == filter.ActionBlock {
		event.Action = "block"
	}
	if decision.Match != nil {
		event.Rule = &ruleEvent{
			ID:      decision.Match.RuleID,
			Source:  decision.Match.Source.Name,
			Pattern: decision.Match.Pattern,
		}
	}
	return event
}

func (s *Server) streamQueries(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeError(w, errors.New("api: the query stream is not configured"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, errors.New("api: this server cannot stream"))
		return
	}

	decisions, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case decision := <-decisions:
			payload, err := json.Marshal(decisionEventFrom(decision))
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
