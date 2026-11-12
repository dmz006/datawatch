// usage_lanes.go — the daemon side of the usage reading lane stamp: the adapter that turns
// the capacity ledger and the LLM registry into the usagecalc.LaneCounter, and the lane
// history that the keeper's stamper and the sampler share.
//
// The keeper alone decides the lane count stored on a reading. A lanes value an older
// caller still sends is decoded and dropped (see handleUsageReadingAdd).
package server

import (
	"context"
	"log"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageLaneCounter counts the Claude lanes running now: the distinct lease holders in the
// capacity ledger whose pools include the llm: pool of a Claude Code registry entry. It
// reads the ledger and the registry when asked, so a ledger wired after the series is built
// is still seen.
type usageLaneCounter struct {
	s *Server
}

// Lanes implements usagecalc.LaneCounter. ok is false when the ledger or the registry is
// not wired, so the count is unknown rather than zero.
func (c usageLaneCounter) Lanes() (int, bool) {
	if c.s == nil {
		return 0, false
	}
	ledger, reg := c.s.capacityLedger, c.s.inferenceReg
	if ledger == nil || reg == nil {
		return 0, false
	}
	pools := make(map[string]struct{})
	for _, l := range reg.List() {
		if l != nil && l.Kind == inference.KindClaudeCode {
			pools["llm:"+l.Name] = struct{}{}
		}
	}
	return usagecalc.ClaudeLanes(ledger.Snapshot().Leases, pools), true
}

// usageLaneHistoryLocked returns the lane history the series stamper and the sampler share,
// opening it under the usage store's data directory on first use. The caller holds
// cache.mu.
func (s *Server) usageLaneHistoryLocked(cache *usageSeriesCache) (*usagecalc.LaneHistory, error) {
	if cache.lanes != nil {
		return cache.lanes, nil
	}
	h, err := usagecalc.OpenLaneHistory(s.usageDataDir(), time.Now, usageLaneCounter{s: s})
	if err != nil {
		return nil, err
	}
	cache.lanes = h
	return h, nil
}

// StartUsageLaneHistory starts the lane history sampler once for the daemon. It records the
// Claude lane count whenever it changes and at least every minute, until ctx is cancelled.
// A second call does nothing.
func (s *Server) StartUsageLaneHistory(ctx context.Context) {
	cache := s.usageSeriesCache()
	cache.mu.Lock()
	if cache.sampling {
		cache.mu.Unlock()
		return
	}
	h, err := s.usageLaneHistoryLocked(cache)
	if err != nil {
		cache.mu.Unlock()
		log.Printf("[usage] lane history not started: %v", err)
		return
	}
	cache.sampling = true
	cache.mu.Unlock()
	go h.Run(ctx)
}

// StartUsageLaneHistory starts the lane history sampler on the embedded API server.
func (s *HTTPServer) StartUsageLaneHistory(ctx context.Context) {
	if s.api != nil {
		s.api.StartUsageLaneHistory(ctx)
	}
}
