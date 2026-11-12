// Observer-peer silence watcher.
//
// Existing-path check: internal/alerts/store.go provides Store.AddSystem, the
// single system-alert path used by burn_guard, peer_health and the MCP
// create_alert tool; this file reuses it through the PeerSilenceSink
// interface (no second alert mechanism). internal/server/peer_health.go
// monitors federation peers by HTTP ping and has no observer-peer rule, and
// no alert_rule path covers PeerEntry.LastPushAt (set by
// PeerRegistry.RecordPush), so this watcher is the first consumer of it.
package observer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
)

// PeerSilenceSink is the alert sink; *alerts.Store satisfies it.
type PeerSilenceSink interface {
	AddSystem(level alerts.Level, title, body string) *alerts.Alert
}

// PeerSilenceWatcher raises one alert per outage when a registered observer
// peer stops pushing for longer than the threshold, and one recovery alert
// when it pushes again.
type PeerSilenceWatcher struct {
	reg       *PeerRegistry
	sink      PeerSilenceSink
	threshold func() int // minutes; 0 disables
	now       func() time.Time

	mu       sync.Mutex
	outages  map[string]time.Time // peer name -> LastPushAt (zero if never) at alert time
	selfName string
}

// NewPeerSilenceWatcher builds a watcher. A nil clock defaults to time.Now.
// Peers named "self" or "local" are never watched (see SetSelfName).
func NewPeerSilenceWatcher(reg *PeerRegistry, sink PeerSilenceSink, threshold func() int, clock func() time.Time) *PeerSilenceWatcher {
	if clock == nil {
		clock = time.Now
	}
	return &PeerSilenceWatcher{
		reg: reg, sink: sink, threshold: threshold, now: clock,
		outages: map[string]time.Time{}, selfName: "self",
	}
}

// SetSelfName sets the name of the local (self) peer to ignore.
func (w *PeerSilenceWatcher) SetSelfName(name string) {
	w.mu.Lock()
	w.selfName = name
	w.mu.Unlock()
}

func (w *PeerSilenceWatcher) isSelf(name string) bool {
	return name == w.selfName || name == "self" || name == "local"
}

// Check evaluates every registered peer against now.
func (w *PeerSilenceWatcher) Check(now time.Time) {
	if w.reg == nil || w.sink == nil || w.threshold == nil {
		return
	}
	mins := w.threshold()
	if mins <= 0 {
		return
	}
	limit := time.Duration(mins) * time.Minute
	w.mu.Lock()
	defer w.mu.Unlock()
	seen := map[string]bool{}
	for _, p := range w.reg.List() {
		if w.isSelf(p.Name) {
			continue
		}
		seen[p.Name] = true
		ref := p.LastPushAt
		if ref.IsZero() {
			ref = p.RegisteredAt
		}
		silent := now.Sub(ref)
		prevPush, down := w.outages[p.Name]
		if silent > limit {
			if down && p.LastPushAt.Equal(prevPush) {
				continue
			}
			last := "never"
			if !p.LastPushAt.IsZero() {
				last = p.LastPushAt.UTC().Format(time.RFC3339)
			}
			w.sink.AddSystem(alerts.LevelWarn,
				fmt.Sprintf("Observer peer silent: %s", p.Name),
				fmt.Sprintf("Peer %q has not pushed stats for %s (threshold %s). Last push: %s.",
					p.Name, silent.Round(time.Second), limit, last))
			w.outages[p.Name] = p.LastPushAt
			continue
		}
		if down {
			w.sink.AddSystem(alerts.LevelInfo,
				fmt.Sprintf("Observer peer recovered: %s", p.Name),
				fmt.Sprintf("Peer %q is pushing stats again.", p.Name))
			delete(w.outages, p.Name)
		}
	}
	for name := range w.outages { // deleted peers: drop outage state
		if !seen[name] {
			delete(w.outages, name)
		}
	}
}

// Run checks every interval until ctx is cancelled.
func (w *PeerSilenceWatcher) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Check(w.now())
		}
	}
}
