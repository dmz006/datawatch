// Automation persistence — a SEPARATE store from Store so Automations'
// reads/writes don't serialize behind the single mutex guarding PRDs,
// stories, tasks and profiles. Same JSONL convention: one file, full
// rewrite on persist.

package autonomous

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"sync"
)

// AutomationStore is the in-memory + JSONL-backed Automation store,
// persisted next to prds.jsonl as `automations.jsonl`.
type AutomationStore struct {
	mu          sync.Mutex // guards automations
	dir         string
	automations map[string]*Automation // keyed by Automation.ID
	// persistMu serializes file writes so concurrent persist calls don't
	// interleave on the same file (same full-rewrite approach as Store).
	persistMu sync.Mutex
	// lookupPRD resolves a member's PRD; kept here so validation that
	// needs a PRD can run without holding this store's mutex across the
	// main Store's mutex.
	lookupPRD func(id string) (*PRD, bool)
	// owner maps a member PRDID to the non-terminal Automation that owns
	// it (same rules as validateAutomationOwnership's ownerOf); guarded
	// by s.mu and kept in sync via rebuildIndexLocked.
	owner map[string]string
	// locks is the per-Automation serialization for the coordinator's
	// advance against concurrent edits (design doc "Edits racing an
	// advance"): each Automation gets its own mutex, keyed by ID, so two
	// different Automations never block each other. Guarded by s.mu.
	locks map[string]*sync.Mutex
}

// NewAutomationStore opens (or creates) automations.jsonl under dir —
// the already-created <data_dir>/autonomous/ directory. A missing file
// means an empty store.
func NewAutomationStore(dir string, lookupPRD func(string) (*PRD, bool)) (*AutomationStore, error) {
	s := &AutomationStore{
		dir:         dir,
		automations: map[string]*Automation{},
		lookupPRD:   lookupPRD,
		owner:       map[string]string{},
		locks:       map[string]*sync.Mutex{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AutomationStore) load() error {
	err := loadJSONL(filepath.Join(s.dir, "automations.jsonl"), func(line []byte) error {
		var a Automation
		if err := json.Unmarshal(line, &a); err != nil {
			return err
		}
		s.automations[a.ID] = &a
		return nil
	})
	if err != nil {
		return err
	}
	s.rebuildIndexLocked()
	return nil
}

// rebuildIndexLocked rebuilds s.owner from s.automations: every loaded
// Automation that is not completed or cancelled owns each of its members'
// PRDs. The caller must hold s.mu.
func (s *AutomationStore) rebuildIndexLocked() {
	s.owner = map[string]string{}
	for _, a := range s.automations {
		if a.Status == AutomationCompleted || a.Status == AutomationCancelled {
			continue
		}
		for _, m := range a.Members {
			if m.PRDID == "" {
				continue
			}
			s.owner[m.PRDID] = a.ID
		}
	}
}

// lockFor returns the per-Automation mutex for id, creating it on first
// use under s.mu. Callers use it via WithLock; the s.mu hold is released
// before the caller acts, so fn can freely call Get/List/OwnerOf.
func (s *AutomationStore) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

// WithLock serializes fn under the per-Automation mutex for id, the shared
// lock the future coordinator's advance and the edit operations (insert,
// remove, reorder) take so a mid-flight edit and an advance cannot
// interleave on the same cursor. s.mu is never held while fn runs, so
// fn may call Get, List and OwnerOf. fn's error is returned unchanged.
func (s *AutomationStore) WithLock(id string, fn func() error) error {
	l := s.lockFor(id)
	l.Lock()
	defer l.Unlock()
	return fn()
}

// OwnerOf returns the ID of the non-terminal Automation that currently
// owns the PRD with prdID, or "" if none does — the store-side version of
// the ownerOf rule in validateAutomationOwnership.
func (s *AutomationStore) OwnerOf(prdID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owner[prdID]
}

// cloneAutomation returns a deep copy of a via a JSON round trip so
// callers cannot mutate store state through the copy; a nil Automation
// stays nil.
func cloneAutomation(a *Automation) *Automation {
	if a == nil {
		return nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		panic(err) // Automations are plain data; marshaling cannot fail
	}
	var out Automation
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return &out
}

// Get returns a deep copy of the Automation with id; the second result
// reports whether it exists.
func (s *AutomationStore) Get(id string) (*Automation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.automations[id]
	if !ok {
		return nil, false
	}
	return cloneAutomation(a), true
}

// List returns deep copies of all Automations, newest CreatedAt first.
func (s *AutomationStore) List() []*Automation {
	s.mu.Lock()
	n := len(s.automations)
	out := make([]*Automation, 0, n)
	for _, a := range s.automations {
		out = append(out, cloneAutomation(a))
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// persist writes the in-memory state back as JSON-lines (full rewrite),
// the same approach as Store.persist.
func (s *AutomationStore) persist() error {
	s.mu.Lock()
	n := len(s.automations)
	snapshot := make([]*Automation, 0, n)
	for _, a := range s.automations {
		snapshot = append(snapshot, a)
	}
	s.mu.Unlock()

	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return writeJSONL(filepath.Join(s.dir, "automations.jsonl"), n, func(emit func(any) error) error {
		for _, a := range snapshot {
			if err := emit(a); err != nil {
				return err
			}
		}
		return nil
	})
}

// newAutomationID generates a short hex ID for an Automation (newProfileID
// already produces exactly 8 hex chars).
func newAutomationID() string {
	return newProfileID()
}
