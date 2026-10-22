// Automation persistence — a SEPARATE store from Store so Automations'
// reads/writes don't serialize behind the single mutex guarding PRDs,
// stories, tasks and profiles. Same JSONL convention: one file, full
// rewrite on persist.

package autonomous

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"
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
		// Legacy records carry only the single cursor; seed the cursor set
		// from it so a linear Automation loads unchanged.
		if len(a.ActiveCursors) == 0 && a.CurrentMemberID != "" {
			a.ActiveCursors = []string{a.CurrentMemberID}
		}
		a.ActiveCursors = sortedUniqueStrings(a.ActiveCursors)
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

// nestingOwnerOf returns the ID of a non-terminal Automation that has a
// kind=automation member naming childID, or "" if none does — the store-side
// version of the nesting rule in validateAutomationNesting. It takes s.mu, so
// callers must not hold it.
func (s *AutomationStore) nestingOwnerOf(childID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.automations {
		if a.Status == AutomationCompleted || a.Status == AutomationCancelled {
			continue
		}
		for _, m := range a.Members {
			if m.kind() == MemberKindAutomation && m.AutomationID == childID {
				return a.ID
			}
		}
	}
	return ""
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

// notOwned returns a copy of s.owner for rollback on a failed persist; the
// caller must hold s.mu. A fresh copy is required because the map itself is
// mutated in place, so a bare alias would roll back to itself.
func (s *AutomationStore) ownerSnapshot() map[string]string {
	out := make(map[string]string, len(s.owner))
	for k, v := range s.owner {
		out[k] = v
	}
	return out
}

// Create inserts a new Automation in the draft state. Empty fields are
// defaulted (ID, member IDs, AddedAt/AddedBy, entry member) before
// validateAutomation and validateAutomationOwnership run; on success a deep
// clone of the stored Automation is returned. A persist failure rolls the
// in-memory change back and returns the persist error; validation errors
// are returned as-is.
func (s *AutomationStore) Create(a *Automation) (*Automation, error) {
	if a == nil {
		return nil, fmt.Errorf("automation invalid: nil Automation")
	}
	now := time.Now()
	if a.ID == "" {
		for {
			id := newAutomationID()
			s.mu.Lock()
			used := s.automations[id] != nil
			s.mu.Unlock()
			if !used {
				a.ID = id
				break
			}
		}
	}
	for i := range a.Members {
		m := &a.Members[i]
		if m.ID == "" {
			m.ID = newAutomationID()
		}
		if m.AddedAt.IsZero() {
			m.AddedAt = now
		}
		if m.AddedBy == "" {
			m.AddedBy = "operator"
		}
	}
	a.Status = AutomationDraft
	a.Version = 1
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.EntryMemberID == "" && len(a.Members) > 0 {
		a.EntryMemberID = a.Members[0].ID
	}
	normalizeActiveCursors(a)
	if err := validateAutomation(a); err != nil {
		return nil, err
	}
	if err := validateAutomationOwnership(a, s.lookupPRD, s.OwnerOf); err != nil {
		return nil, err
	}
	if err := validateAutomationNesting(a, s.Get, s.nestingOwnerOf); err != nil {
		return nil, err
	}
	err := s.WithLock(a.ID, func() error {
		clone := cloneAutomation(a)
		s.mu.Lock()
		snapshot := s.ownerSnapshot()
		s.automations[a.ID] = clone
		if a.Status != AutomationCompleted && a.Status != AutomationCancelled {
			for _, m := range a.Members {
				if m.PRDID != "" {
					s.owner[m.PRDID] = a.ID
				}
			}
		}
		s.mu.Unlock()
		if err := s.persist(); err != nil {
			s.mu.Lock()
			delete(s.automations, a.ID)
			s.owner = snapshot
			s.mu.Unlock()
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, _ := s.Get(a.ID)
	return out, nil
}

// Update applies mutate to a copy of the Automation with id and stores the
// result, bumping Version by one and refreshing UpdatedAt. When
// expectedVersion is positive and differs from the current Version the call
// fails with `409: version mismatch` and the store is left unchanged;
// validateAutomation and validateAutomationOwnership re-run after the
// mutation. A missing Automation is a 404.
func (s *AutomationStore) Update(id string, expectedVersion int64, mutate func(*Automation) error) (*Automation, error) {
	var out *Automation
	err := s.WithLock(id, func() error {
		s.mu.Lock()
		existing, ok := s.automations[id]
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("404: automation %s not found", id)
		}
		clone := cloneAutomation(existing)
		s.mu.Unlock()

		if expectedVersion > 0 && clone.Version != expectedVersion {
			return fmt.Errorf("409: version mismatch: current version is %d", clone.Version)
		}
		if mutate != nil {
			if err := mutate(clone); err != nil {
				return err
			}
		}
		clone.ID = id // the store's identity cannot be renamed through mutate
		normalizeActiveCursors(clone)
		if err := validateAutomation(clone); err != nil {
			return err
		}
		if err := validateAutomationOwnership(clone, s.lookupPRD, s.OwnerOf); err != nil {
			return err
		}
		if err := validateAutomationNesting(clone, s.Get, s.nestingOwnerOf); err != nil {
			return err
		}
		clone.Version = existing.Version + 1
		clone.UpdatedAt = time.Now()

		s.mu.Lock()
		snapshot := s.ownerSnapshot()
		s.automations[id] = clone
		s.rebuildIndexLocked()
		s.mu.Unlock()
		if err := s.persist(); err != nil {
			s.mu.Lock()
			s.automations[id] = existing
			s.owner = snapshot
			s.mu.Unlock()
			return err
		}
		out = cloneAutomation(clone)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes the Automation with id. Only draft, completed and cancelled
// Automations may be deleted — a non-deletable status is a 409. The owner
// index drops the Automation's claims on persist success; a persist failure
// rolls the removal back.
func (s *AutomationStore) Delete(id string) error {
	return s.WithLock(id, func() error {
		s.mu.Lock()
		a, ok := s.automations[id]
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("404: automation %s not found", id)
		}
		switch a.Status {
		case AutomationDraft, AutomationCompleted, AutomationCancelled:
		default:
			s.mu.Unlock()
			return fmt.Errorf("409: automation is %s; only draft, completed or cancelled automations can be deleted", a.Status)
		}
		snapshot := s.ownerSnapshot()
		delete(s.automations, id)
		s.rebuildIndexLocked()
		s.mu.Unlock()

		if err := s.persist(); err != nil {
			s.mu.Lock()
			s.automations[id] = a
			s.owner = snapshot
			s.mu.Unlock()
			return err
		}
		return nil
	})
}
