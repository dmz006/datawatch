// Observer pull protocol (pull mode) — the server-side observer.PullFetcher
// backed by the federation peer store, plus the wiring that keeps an
// observer.PullCollector's target set in sync with that store.
//
// Each pull is a GET {peer URL}/api/observer/stats?v=2 with the peer's
// bearer token, gated before any network call on the entry's opt-in and
// its resolved capability set (observers:read). A gate failure returns an
// error the collector maps to the stable not_permitted stale reason; a
// gate failure never issues a request.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/observer"
)

// observerPull is one server's pull-protocol wiring: a PullFetcher bound to
// the federation peer store plus the PullCollector driven by it.
type observerPull struct {
	s   *Server
	col *observer.PullCollector
}

// NewObserverPullCollector builds the observer.PullCollector for s and
// seeds it with the targets currently present in the peer store. It does
// not start a daemon: the collector's per-target workers are the only
// goroutines it owns, and they stop with the collector's Stop.
func NewObserverPullCollector(s *Server) *observer.PullCollector {
	op := &observerPull{s: s}
	op.col = observer.NewPullCollector(observer.PullCollectorOptions{Fetcher: op})
	s.observerPull = op
	op.col.SetTargets(op.targets())
	return op.col
}

// syncObserverPullTargets re-derives the collector's target set from the
// current peer store. Called from the federation peer add/update/delete
// handlers so a store change takes effect on the next reconciliation.
// No-op until NewObserverPullCollector has wired a collector.
func (s *Server) syncObserverPullTargets() {
	if s == nil || s.observerPull == nil || s.observerPull.col == nil {
		return
	}
	s.observerPull.col.SetTargets(s.observerPull.targets())
}

// targets derives the collector's target list from the peer store. An entry
// is a target only when it is enabled, federated, opted in via
// observer_pull and has a URL; the capability gate stays in Fetch so a peer
// that later loses observers:read surfaces as stale/not_permitted rather
// than silently disappearing from the target set.
func (p *observerPull) targets() []observer.PullTarget {
	s := p.s
	if s == nil || s.serverStore == nil {
		return nil
	}
	def := observerPullDefaults(s)
	var out []observer.PullTarget
	for _, e := range s.serverStore.ListFederated() {
		if e == nil || e.Name == "" || e.URL == "" {
			continue
		}
		if !e.Enabled || !e.Federated || !e.ObserverPull {
			continue
		}
		interval := e.ObserverPullIntervalS
		if interval <= 0 {
			interval = def.DefaultIntervalS
		}
		timeout := e.ObserverPullTimeoutS
		if timeout <= 0 {
			timeout = def.DefaultTimeoutS
		}
		out = append(out, observer.PullTarget{
			Name:     e.Name,
			Interval: time.Duration(interval) * time.Second,
			Timeout:  time.Duration(timeout) * time.Second,
			Enabled:  true,
		})
	}
	return out
}

// observerPullDefaults resolves the observer.pull globals, falling back to
// the shipped defaults for zero-valued fields (an absent observer block).
func observerPullDefaults(s *Server) config.ObserverPullConfig {
	def := config.DefaultObserverPullConfig()
	if s.cfg != nil {
		pull := s.cfg.Observer.Pull
		if pull.DefaultIntervalS > 0 {
			def.DefaultIntervalS = pull.DefaultIntervalS
		}
		if pull.DefaultTimeoutS > 0 {
			def.DefaultTimeoutS = pull.DefaultTimeoutS
		}
	}
	return def
}

// Fetch pulls one peer's v2 stats once; it implements observer.PullFetcher.
//
// Before any network I/O the named entry is gated: it must exist in the
// peer store, be enabled and federated, be opted in via observer_pull, and
// (after group expansion) be granted observers:read. A gate failure returns
// an error the collector maps to not_permitted, and no request is made.
// The bearer token is only ever placed in the Authorization header; it
// never appears in a log line or an error string.
func (p *observerPull) Fetch(ctx context.Context, name string) (*observer.StatsResponse, error) {
	s := p.s
	if s == nil || s.serverStore == nil {
		return nil, fmt.Errorf("%w: the federation peer registry is not configured", observer.ErrNotPermitted)
	}
	entry, ok := s.serverStore.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: peer %q is not in the registry", observer.ErrNotPermitted, name)
	}
	if !entry.Enabled || !entry.Federated || !entry.ObserverPull {
		return nil, fmt.Errorf("%w: peer %q is not opted in to observer pull", observer.ErrNotPermitted, name)
	}
	var custom map[string]*federation.CapabilityGroup
	if s.fedGroupStore != nil {
		custom = s.fedGroupStore.AsMap()
	}
	if !federation.Check(federation.Resolve(entry.Capabilities, custom), federation.CapObserversRead) {
		return nil, fmt.Errorf("%w: peer %q lacks the observers:read capability", observer.ErrNotPermitted, name)
	}
	if entry.URL == "" {
		return nil, fmt.Errorf("%w: peer %q has no URL", observer.ErrNotPermitted, name)
	}

	// Per-peer timeout on the request context (entry value when non-zero,
	// otherwise the observer.pull global).
	def := observerPullDefaults(s)
	timeout := entry.ObserverPullTimeoutS
	if timeout <= 0 {
		timeout = def.DefaultTimeoutS
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// peerClient honours the entry's TLSSkipVerify (and the pinned
	// SHA-256 when one is configured) and caches per peer.
	target := federationTarget{name: entry.Name, url: entry.URL, token: entry.Token, skipVerify: entry.TLSSkipVerify}
	client := s.peerClient(target)

	endpoint := strings.TrimRight(entry.URL, "/") + "/api/observer/stats?v=2"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("observer pull from %q: %v", name, err)
	}
	if entry.Token != "" {
		req.Header.Set("Authorization", "Bearer "+entry.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		// context.DeadlineExceeded (per-peer timeout or caller cancel)
		// is preserved through the wrap; the collector maps it to timeout.
		return nil, fmt.Errorf("observer pull from %q: %w", name, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	switch resp.StatusCode {
	case http.StatusOK:
	default:
		return nil, observer.NewPullHTTPError(resp.StatusCode, fmt.Sprintf("observer stats pull from %q: HTTP %d", name, resp.StatusCode))
	}
	var out observer.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %v", observer.ErrBadPayload, err)
	}
	return &out, nil
}
