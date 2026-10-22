// The exchange half of fingerprint reconciliation
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c): the
// Syncer periodically pulls each opted-in peer's fingerprint list over
// the authenticated federation path (the same call shape as the peer
// spend fetch in internal/autonomous) and records a PENDING match for
// every hash that appears on both sides. A match is a *suggestion* —
// this file has no code path that confirms one, and Observe leaves a
// rejected record rejected, so a re-sync can never re-open an operator
// decision. A peer failure (timeout, not opted in on the peer side,
// bad body) is logged once per failure episode — it never stops the
// other peers or the loop.
package fingerprint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// DefaultSyncInterval is the exchange cadence when Syncer.Interval is
// zero. The exchange answers "is this shared", on a slow cadence — it
// is deliberately not the channel for live load, and the design asks
// for one shared tick, not a new polling loop (open question: no new
// config key; the constant is the knob).
const DefaultSyncInterval = 15 * time.Minute

// syncTimeout bounds one peer pull when no Client is injected, so a
// slow peer is counted as unreachable instead of holding up the pass.
const syncTimeout = 5 * time.Second

// syncBodyLimit caps how much of a peer's reply is read, so a hostile
// or broken peer cannot stream unbounded memory.
const syncBodyLimit = 64 * 1024

// Syncer is the slow periodic fingerprint exchange with opted-in
// federation peers. Peers lists the federated peers that have
// FingerprintSync on — but the flag is RE-CHECKED on every entry
// received (the list source may be stale), and any entry that is off
// gets no HTTP call at all: default off means silence, not a 403.
// Configure the fields before first use; they are read on every pass.
type Syncer struct {
	Peers func() []multiserver.Entry
	// Local exports THIS daemon's fingerprints for a peer-pair key.
	Local *Exporter
	// Matches records the detected matches.
	Matches *MatchStore
	// Client performs the pulls; nil means a client with syncTimeout.
	Client *http.Client
	// Interval between passes; zero means DefaultSyncInterval.
	Interval time.Duration

	mu      sync.Mutex
	failing map[string]bool // peers in a failure episode, so each is logged once
}

func (s *Syncer) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: syncTimeout}
}

// interval returns the effective exchange cadence.
func (s *Syncer) interval() time.Duration {
	if s.Interval <= 0 {
		return DefaultSyncInterval
	}
	return s.Interval
}

type syncJob struct {
	entry multiserver.Entry
	key   []byte // pair key, precomputed
	fail  string // pre-fetch failure reason; empty means the fetch may run
}

// SyncOnce runs one full exchange: every opted-in peer is pulled
// concurrently, each reply is compared against the local export, and
// every hash both sides carry is recorded as a pending match. One
// peer pulling down never affects the others or this call's result;
// failures are logged once per episode in noteFailures.
func (s *Syncer) SyncOnce(ctx context.Context) {
	if s == nil || s.Peers == nil || s.Local == nil || s.Matches == nil {
		return
	}
	var jobs []syncJob
	for _, e := range s.Peers() {
		// Opt-in is re-checked here, not trusted from the lister:
		// off (or unresolvable) means no request goes out at all.
		if !e.FingerprintSync || e.URL == "" {
			continue
		}
		key, err := PairKey(e.Token)
		if err != nil {
			jobs = append(jobs, syncJob{entry: e, fail: "no pair key: " + err.Error()})
			continue
		}
		jobs = append(jobs, syncJob{entry: e, key: key})
	}

	results := make([]string, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j syncJob) {
			defer wg.Done()
			if j.fail != "" {
				results[i] = j.fail
				return
			}
			items, reason := s.fetchPeer(ctx, j.entry, j.key)
			if reason != "" {
				results[i] = reason
				return
			}
			s.reconcile(j, items)
		}(i, j)
	}
	wg.Wait()

	s.noteFailures(jobs, results)
}

// Run is the single tick for the exchange: one immediate pass, then
// one pass per Interval, until ctx is done.
func (s *Syncer) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.SyncOnce(ctx)
	t := time.NewTicker(s.interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SyncOnce(ctx)
		}
	}
}

// fetchPeer asks one peer for its exported fingerprint list. Every
// failure returns a non-empty reason; every success returns the peer's
// items (possibly an empty list — nothing to reconcile).
func (s *Syncer) fetchPeer(ctx context.Context, e multiserver.Entry, key []byte) ([]Item, string) {
	endpoint := strings.TrimRight(e.URL, "/") + "/api/federation/fingerprints"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "unreachable: " + err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, "unreachable: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Sprintf("unreachable: HTTP %d", resp.StatusCode)
	}
	var items []Item
	if err := json.NewDecoder(io.LimitReader(resp.Body, syncBodyLimit)).Decode(&items); err != nil {
		return nil, "bad response: " + err.Error()
	}
	return items, ""
}

// reconcile records one pending match for every (kind, hash) pair the
// local export and the peer's reply both carry. Names are operator
// labels on each side — never compared, never the match criterion; the
// keyed hash is. Observe is the only write: it creates pending records
// and refreshes existing ones in whatever state the operator left
// them, so a rejected match stays rejected across re-syncs.
func (s *Syncer) reconcile(j syncJob, peerItems []Item) {
	local := s.Local.Export(j.key)
	for _, li := range local {
		for _, pi := range peerItems {
			if li.Kind == pi.Kind && li.Hash != "" && li.Hash == pi.Hash {
				s.Matches.Observe(j.entry.Name, li.Kind, li.Name, pi.Name, li.Hash)
			}
		}
	}
}

// noteFailures logs each failing peer once per failure episode and
// forgets an episode when the peer recovers, so a chronically broken
// peer never floods the log while a fresh failure is always visible.
func (s *Syncer) noteFailures(jobs []syncJob, results []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failing == nil {
		s.failing = map[string]bool{}
	}
	live := map[string]bool{}
	for i, j := range jobs {
		live[j.entry.Name] = true
		switch {
		case results[i] != "" && !s.failing[j.entry.Name]:
			s.failing[j.entry.Name] = true
			log.Printf("[fingerprint] peer %s: %s", j.entry.Name, results[i])
		case results[i] == "":
			delete(s.failing, j.entry.Name)
		}
	}
	for name := range s.failing {
		if !live[name] {
			delete(s.failing, name)
		}
	}
}
