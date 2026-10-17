// Automation persistence — a SEPARATE store from Store so Automations'
// reads/writes don't serialize behind the single mutex guarding PRDs,
// stories, tasks and profiles. Same JSONL convention: one file, full
// rewrite on persist.

package autonomous

import (
	"encoding/json"
	"path/filepath"
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
}

// NewAutomationStore opens (or creates) automations.jsonl under dir —
// the already-created <data_dir>/autonomous/ directory. A missing file
// means an empty store.
func NewAutomationStore(dir string, lookupPRD func(string) (*PRD, bool)) (*AutomationStore, error) {
	s := &AutomationStore{
		dir:         dir,
		automations: map[string]*Automation{},
		lookupPRD:   lookupPRD,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AutomationStore) load() error {
	return loadJSONL(filepath.Join(s.dir, "automations.jsonl"), func(line []byte) error {
		var a Automation
		if err := json.Unmarshal(line, &a); err != nil {
			return err
		}
		s.automations[a.ID] = &a
		return nil
	})
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
