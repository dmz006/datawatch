// BL274 Sprint 1, v6.16.0 — Trust system.
//
// Operator-decided model (Q6d + Q7c):
//   - Core docs are always trusted (built into the binary).
//   - Skills and plugins both require explicit per-source trust before
//     their docs land in the index.
//   - Trust list lives in two layers: config seed + runtime overrides
//     at ~/.datawatch/docs-trust.json. Runtime is canonical at runtime;
//     no auto-writeback to config (operator runs `docs trust export`
//     to dump runtime → YAML for committing).
//   - New untrusted source arriving emits an alert + adds to the
//     pending-trust queue at ~/.datawatch/docs-trust-pending.json.
//     PWA shows a breadcrumb badge; bulk accept/dismiss available.
//   - Trusted source: chunks ride the index. Dismissed: queue drops
//     them but they're not indexed. Untrusted: index unaffected; the
//     source's docs are simply absent from search.

package docsindex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// TrustState is the operator-managed allow-list of doc sources that
// may contribute chunks to the search index. Persisted to disk; safe
// for concurrent access via the embedded mutex.
type TrustState struct {
	mu      sync.RWMutex
	path    string
	trusted map[string]TrustEntry // source-id → entry
}

// TrustEntry is one source's trust record.
type TrustEntry struct {
	Source     string    `json:"source"` // "skill:<name>" | "plugin:<name>"
	GrantedAt  time.Time `json:"granted_at"`
	GrantedBy  string    `json:"granted_by,omitempty"` // "operator" | "config" | "comm:<channel>"
	Note       string    `json:"note,omitempty"`
}

// NewTrustState loads from disk; if the file doesn't exist, returns an
// empty state (the next Save() will create it). Optional configSeed is
// a list of sources to add as initial trusted entries IF and only if
// the runtime file is empty (first-run bootstrap).
func NewTrustState(path string, configSeed []string) (*TrustState, error) {
	ts := &TrustState{
		path:    path,
		trusted: map[string]TrustEntry{},
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("trust state read %s: %w", path, err)
	}
	if len(data) > 0 {
		var entries map[string]TrustEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("trust state parse %s: %w", path, err)
		}
		ts.trusted = entries
	} else if len(configSeed) > 0 {
		// First-run bootstrap from config-declared trust list.
		for _, src := range configSeed {
			ts.trusted[src] = TrustEntry{
				Source:    src,
				GrantedAt: time.Now().UTC(),
				GrantedBy: "config",
			}
		}
		_ = ts.save()
	}
	return ts, nil
}

// IsTrusted returns true when the source is in the allow-list.
// "core" is always trusted (built-in corpus, can't be revoked).
func (ts *TrustState) IsTrusted(source string) bool {
	if source == "core" {
		return true
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	_, ok := ts.trusted[source]
	return ok
}

// Trust adds a source to the allow-list. Returns true if newly added,
// false if it was already trusted.
func (ts *TrustState) Trust(source, grantedBy, note string) (bool, error) {
	if source == "core" {
		return false, nil // already trusted by definition
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, exists := ts.trusted[source]; exists {
		return false, nil
	}
	ts.trusted[source] = TrustEntry{
		Source:    source,
		GrantedAt: time.Now().UTC(),
		GrantedBy: grantedBy,
		Note:      note,
	}
	return true, ts.save()
}

// Untrust removes a source from the allow-list. Returns true if removed.
func (ts *TrustState) Untrust(source string) (bool, error) {
	if source == "core" {
		return false, fmt.Errorf("core source cannot be untrusted")
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, ok := ts.trusted[source]; !ok {
		return false, nil
	}
	delete(ts.trusted, source)
	return true, ts.save()
}

// List returns all trusted entries sorted by source name. Always
// includes a synthetic "core" entry first.
func (ts *TrustState) List() []TrustEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	out := make([]TrustEntry, 0, len(ts.trusted)+1)
	out = append(out, TrustEntry{Source: "core", GrantedAt: time.Time{}, GrantedBy: "built-in"})
	for _, e := range ts.trusted {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// Export returns the trusted set as a YAML-safe slice for
// `docs trust export` — the operator pastes this into config.yaml's
// docs_search.trust block to make runtime trust survive a wipe.
func (ts *TrustState) Export() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	out := make([]string, 0, len(ts.trusted))
	for src := range ts.trusted {
		out = append(out, src)
	}
	sort.Strings(out)
	return out
}

func (ts *TrustState) save() error {
	if err := os.MkdirAll(filepath.Dir(ts.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ts.trusted, "", "  ")
	if err != nil {
		return err
	}
	tmp := ts.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, ts.path)
}

// ── Pending-trust queue ──────────────────────────────────────────────────

// PendingQueue is the inbox of newly-arrived sources awaiting an
// operator trust decision. Serialized to ~/.datawatch/docs-trust-pending.json
// so it survives daemon restarts.
type PendingQueue struct {
	mu      sync.RWMutex
	path    string
	entries map[string]PendingEntry
}

// PendingEntry describes one source awaiting an operator trust decision.
type PendingEntry struct {
	Source     string    `json:"source"`
	NoticedAt  time.Time `json:"noticed_at"`
	Detail     string    `json:"detail,omitempty"` // e.g. "plugin docs.index=true; 3 files"
}

// NewPendingQueue loads from disk; empty when file is absent.
func NewPendingQueue(path string) (*PendingQueue, error) {
	pq := &PendingQueue{path: path, entries: map[string]PendingEntry{}}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &pq.entries)
	}
	return pq, nil
}

// Add records an untrusted source for later operator review. Idempotent.
func (pq *PendingQueue) Add(source, detail string) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if _, ok := pq.entries[source]; ok {
		return nil
	}
	pq.entries[source] = PendingEntry{
		Source:    source,
		NoticedAt: time.Now().UTC(),
		Detail:    detail,
	}
	return pq.save()
}

// Remove drops a source from the queue (called when operator accepts
// OR dismisses, OR when the source itself is uninstalled).
func (pq *PendingQueue) Remove(source string) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if _, ok := pq.entries[source]; !ok {
		return nil
	}
	delete(pq.entries, source)
	return pq.save()
}

// List returns all pending entries sorted by NoticedAt.
func (pq *PendingQueue) List() []PendingEntry {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	out := make([]PendingEntry, 0, len(pq.entries))
	for _, e := range pq.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NoticedAt.Before(out[j].NoticedAt) })
	return out
}

// Count returns the number of pending entries (for the PWA badge).
func (pq *PendingQueue) Count() int {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return len(pq.entries)
}

func (pq *PendingQueue) save() error {
	if err := os.MkdirAll(filepath.Dir(pq.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pq.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := pq.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, pq.path)
}
