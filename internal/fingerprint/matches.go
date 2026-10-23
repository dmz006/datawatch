// Match records surfaced during fingerprint
// reconciliation (docs/plans/2026-10-14-concurrency-and-budget-design.md
// §3c): a peer reported the same keyed hash as one of this daemon's
// resources. A match is a *suggestion*, never a fact — the design
// recommends explicit operator confirmation before the ledger treats two
// resources as one pool, and nothing here ever acts on a match on its
// own. The full hash is never recorded; only its first 8 hex characters
// are kept for operator display, so the persisted file cannot be used to
// reconstruct (or re-link) the fingerprint.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Match states. A record is born pending; only an operator moves it,
// either way, and can always move it back.
const (
	StatePending   = "pending"
	StateConfirmed = "confirmed"
	StateRejected  = "rejected"
)

// Decision actions reported to OnDecision.
const (
	ActionConfirm = "confirm"
	ActionReject  = "reject"
)

// ErrUnknownMatch is returned by Confirm, Reject and Get for an id the
// store has never seen.
var ErrUnknownMatch = errors.New("fingerprint: unknown match id")

// hashPrefixLen is how much of a fingerprint hash a record (and any
// operator-facing view of it) may retain: first 8 hex characters, only
// for display.
const hashPrefixLen = 8

// hashPrefix returns the first hashPrefixLen characters of hex of hash,
// or hash as-is when it is shorter.
func hashPrefix(hex string) string {
	trimmed := strings.TrimSpace(hex)
	if len(trimmed) <= hashPrefixLen {
		return trimmed
	}
	return trimmed[:hashPrefixLen]
}

// MatchID returns the deterministic short id of a match observation:
// the first 16 hex characters of the SHA-256 of peer + kind + local
// name + peer name, NUL-joined so no field can bleed into another.
// Re-observing the same match, however many times under however many
// hashes, always lands on the same id — which is what keeps Observe
// from ever creating a duplicate record.
func MatchID(peer, kind, localName, peerName string) string {
	sum := sha256.Sum256([]byte(peer + "\x00" + kind + "\x00" + localName + "\x00" + peerName))
	return hex.EncodeToString(sum[:])[:16]
}

// Match is one persisted record of a detected fingerprint match between
// a local resource and a peer's resource of the same kind.
type Match struct {
	ID         string    `json:"id"`
	Peer       string    `json:"peer"`
	Kind       string    `json:"kind"` // KindNode or KindBilling
	LocalName  string    `json:"local_name"`
	PeerName   string    `json:"peer_name"`
	HashPrefix string    `json:"hash_prefix"` // first 8 hex chars, display only
	State      string    `json:"state"`       // StatePending, StateConfirmed, StateRejected
	FirstSeen  time.Time `json:"first_seen"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MatchStore records and persists detected matches. One store per daemon
// data directory (the same DATAWATCH_DATA_DIR root the daemon's other
// ledgers live under, e.g. <dataDir>/autonomous/budget-ledger.jsonl for
// the budget ledger: the daemon wires this store under <dataDir> as
// well). The file is a single JSON envelope, written atomically: a temp
// file in the same directory is filled and renamed over the target, so a
// crash never leaves half a file behind. Safe for concurrent use.
type MatchStore struct {
	// OnDecision, when set, is called after each successful Confirm or
	// Reject with the updated record and the action taken. The daemon
	// wiring uses this to emit audit events; it must not block on it.
	OnDecision func(m Match, action string)

	mu   sync.Mutex
	path string
	byID map[string]Match
}

// NewMatchStore returns a store persisting to path, loading any existing
// file first. A missing file is fine (first run); a present but
// unparseable one is an error, so a corrupt record can never be
// silently forgotten.
func NewMatchStore(path string) (*MatchStore, error) {
	s := &MatchStore{path: path, byID: make(map[string]Match)}
	if path == "" || !fileExists(path) {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope persistedStore
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	for _, m := range envelope.Matches {
		if m.ID == "" {
			continue
		}
		s.byID[m.ID] = m
	}
	return s, nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

type persistedStore struct {
	Matches []Match `json:"matches"`
}

// save writes the whole envelope atomically. Caller holds s.mu.
func (s *MatchStore) save() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(persistedStore{
		Matches: s.snapshotMatches(),
	}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Chmod(0o600)
	if ferr := tmp.Close(); werr == nil && cerr == nil {
		werr = ferr
	}
	if werr != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
	}
}

// snapshotMatches returns the records in stable (sorted-by-id) order.
// Caller holds s.mu.
func (s *MatchStore) snapshotMatches() []Match {
	out := make([]Match, 0, len(s.byID))
	for id := range s.byID {
		out = append(out, s.byID[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Observe records one detected match. It creates a PENDING record when
// the (peer, kind, local name, peer name) quadruple is new and returns
// created=true; when the record already exists — in any state — it is
// left exactly in that state (a rejected match stays rejected even if
// the peer keeps showing the collision) and only UpdatedAt is
// refreshed, returning created=false.
func (s *MatchStore) Observe(peer, kind, localName, peerName, hash string) (Match, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := MatchID(peer, kind, localName, peerName)
	now := time.Now().UTC()
	m, exists := s.byID[id]
	if exists {
		m.UpdatedAt = now
		s.byID[id] = m
		s.save()
		return m, false
	}
	m = Match{
		ID:         id,
		Peer:       peer,
		Kind:       kind,
		LocalName:  localName,
		PeerName:   peerName,
		HashPrefix: hashPrefix(hash),
		State:      StatePending,
		FirstSeen:  now,
		UpdatedAt:  now,
	}
	s.byID[id] = m
	s.save()
	return m, true
}

// List returns records in a stable order (sorted by id), optionally
// restricted to one state; an empty state means all of them.
func (s *MatchStore) List(state string) []Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Match, 0)
	for _, m := range s.snapshotMatches() {
		if state == "" || m.State == state {
			out = append(out, m)
		}
	}
	return out
}

// Get returns the record with the given id.
func (s *MatchStore) Get(id string) (Match, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	return m, ok
}

// Confirm marks a pending or rejected match confirmed. Re-confirming an
// already-confirmed match is a no-op success; confirming an unknown id
// returns ErrUnknownMatch.
func (s *MatchStore) Confirm(id string) (Match, error) {
	return s.decide(id, StateConfirmed, ActionConfirm)
}

// Reject marks a pending or confirmed match rejected — this is how an
// operator un-merges a match they confirmed earlier and the ledger was
// still treating the resources as one pool. Re-rejecting an
// already-rejected match is a no-op success; rejecting an unknown id
// returns ErrUnknownMatch.
func (s *MatchStore) Reject(id string) (Match, error) {
	return s.decide(id, StateRejected, ActionReject)
}

// decide sets the record to want and fires OnDecision. Only an unknown
// id can fail; every state either moves to want or already is it.
func (s *MatchStore) decide(id, want, action string) (Match, error) {
	var m Match
	s.mu.Lock()
	cur, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		return m, ErrUnknownMatch
	}
	if cur.State != want {
		cur.State = want
		cur.UpdatedAt = time.Now().UTC()
		s.byID[id] = cur
		s.save()
	}
	s.mu.Unlock()
	if s.OnDecision != nil {
		s.OnDecision(cur, action)
	}
	return cur, nil
}

// SharedWith returns only the CONFIRMED matches for the named local
// resource of the given kind (KindNode or KindBilling), in stable
// order. This is the only path the ledger uses to learn that a local
// resource is pooled with a peer's; pending and rejected suggestions
// never appear here, so a match is never acted on without an operator
// decision.
func (s *MatchStore) SharedWith(kind, localName string) []Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Match, 0)
	for _, m := range s.snapshotMatches() {
		if m.State == StateConfirmed && m.Kind == kind && m.LocalName == localName {
			out = append(out, m)
		}
	}
	return out
}
