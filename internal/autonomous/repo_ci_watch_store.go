// Repo CI watch persistence — a SEPARATE store from Store so repo CI
// watch reads/writes don't serialize behind the single mutex guarding
// PRDs, stories, tasks and profiles. Same JSONL convention as the
// Automation store: one file, full rewrite on persist, loaded at
// startup and saved on every change, a missing file reads as empty.

package autonomous

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RepoCIWatchFileName is the JSONL file the store persists under the
// already-created <data_dir>/autonomous/ directory, next to
// automations.jsonl.
const RepoCIWatchFileName = "repo_ci_watch.jsonl"

// RepoCIWatchStore is the in-memory + JSONL-backed store for repo CI
// watch records, keyed by RepoCIWatchKey. A missing file means an
// empty store; every change re-persists the whole list (same
// full-rewrite approach as AutomationStore).
type RepoCIWatchStore struct {
	// mu guards records.
	mu      sync.Mutex
	dir     string
	records map[string]*RepoCIWatchRecord // keyed by RepoCIWatchKey
	// persistMu serializes file writes so concurrent persist calls don't
	// interleave on the same file (same full-rewrite approach as Store).
	persistMu sync.Mutex
}

// NewRepoCIWatchStore opens (or creates) repo_ci_watch.jsonl under dir
// — the already-created <data_dir>/autonomous/ directory. A missing
// file reads as an empty store.
func NewRepoCIWatchStore(dir string) (*RepoCIWatchStore, error) {
	s := &RepoCIWatchStore{
		dir:     dir,
		records: map[string]*RepoCIWatchRecord{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *RepoCIWatchStore) load() error {
	err := loadJSONL(filepath.Join(s.dir, RepoCIWatchFileName), func(line []byte) error {
		var rec RepoCIWatchRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		s.records[rec.Key()] = &rec
		return nil
	})
	return err
}

// cloneRepoCIWatchRecord returns a deep copy of rec via a JSON round
// trip so callers cannot mutate store state through the copy; a nil
// record stays nil.
func cloneRepoCIWatchRecord(rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	if rec == nil {
		return nil
	}
	b, err := json.Marshal(rec)
	if err != nil {
		panic(err) // Repo CI watch records are plain data; marshaling cannot fail
	}
	var out RepoCIWatchRecord
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return &out
}

// Get returns a deep copy of the record under key; the second result
// reports whether it exists.
func (s *RepoCIWatchStore) Get(key string) (*RepoCIWatchRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, false
	}
	return cloneRepoCIWatchRecord(rec), true
}

// List returns deep copies of all records, newest FirstSeenAt first.
func (s *RepoCIWatchStore) List() []*RepoCIWatchRecord {
	s.mu.Lock()
	n := len(s.records)
	out := make([]*RepoCIWatchRecord, 0, n)
	for _, rec := range s.records {
		out = append(out, cloneRepoCIWatchRecord(rec))
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if !out[i].FirstSeenAt.Equal(out[j].FirstSeenAt) {
			return out[i].FirstSeenAt.After(out[j].FirstSeenAt)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// persist writes the in-memory state back as JSON-lines (full rewrite),
// the same approach as AutomationStore.persist.
func (s *RepoCIWatchStore) persist() error {
	s.mu.Lock()
	n := len(s.records)
	snapshot := make([]*RepoCIWatchRecord, 0, n)
	for _, rec := range s.records {
		snapshot = append(snapshot, rec)
	}
	s.mu.Unlock()

	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return writeJSONL(filepath.Join(s.dir, RepoCIWatchFileName), n, func(emit func(any) error) error {
		for _, rec := range snapshot {
			if err := emit(rec); err != nil {
				return err
			}
		}
		return nil
	})
}

// repoCIWatchValidateKey checks the six identity fields a record needs
// before it can be keyed.
func repoCIWatchValidateKey(rec *RepoCIWatchRecord) error {
	if rec == nil {
		return fmt.Errorf("repo ci watch: nil record")
	}
	required := []struct {
		name  string
		value string
	}{
		{"repo", rec.Repo},
		{"ref_kind", rec.RefKind},
		{"ref_name", rec.RefName},
		{"sha", rec.SHA},
		{"workflow", rec.Workflow},
		{"run_id", rec.RunID},
	}
	for _, f := range required {
		if f.value == "" {
			return fmt.Errorf("repo ci watch: %s is required", f.name)
		}
	}
	return nil
}

// Save stores rec under its composite key (inserting a new record or
// refreshing the existing one) and returns a deep copy of the stored
// record. A persist failure rolls the in-memory change back and
// returns the persist error.
func (s *RepoCIWatchStore) Save(rec *RepoCIWatchRecord) (*RepoCIWatchRecord, error) {
	if err := repoCIWatchValidateKey(rec); err != nil {
		return nil, err
	}
	key := rec.Key()
	s.mu.Lock()
	_, existed := s.records[key]
	snap := cloneRepoCIWatchRecord(s.records[key])
	stored := cloneRepoCIWatchRecord(rec)
	if !existed && stored.FirstSeenAt.IsZero() {
		stored.FirstSeenAt = time.Now()
	}
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = time.Now()
	}
	s.records[key] = stored
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		s.mu.Lock()
		if existed {
			s.records[key] = snap
		} else {
			delete(s.records, key)
		}
		s.mu.Unlock()
		return nil, err
	}
	out, ok := s.Get(key)
	if !ok {
		return nil, fmt.Errorf("repo ci watch: record %q disappeared after save", key)
	}
	return out, nil
}

// AppendNote appends one TriageNote-shaped annotation to the existing
// record under key, refreshing its UpdatedAt. It never creates a
// record; a missing record is an error.
func (s *RepoCIWatchStore) AppendNote(key, source, text string) (*RepoCIWatchRecord, error) {
	s.mu.Lock()
	stored, ok := s.records[key]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("repo ci watch: no record for key %q", key)
	}
	snapshot := cloneRepoCIWatchRecord(stored)
	now := time.Now()
	stored.Notes = append(stored.Notes, TriageNote{At: now, Source: source, Text: text})
	stored.UpdatedAt = now
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		s.mu.Lock()
		s.records[key] = snapshot
		s.mu.Unlock()
		return nil, err
	}
	out, ok := s.Get(key)
	if !ok {
		return nil, fmt.Errorf("repo ci watch: record %q disappeared after append", key)
	}
	return out, nil
}

// Delete removes the record under key and persists the change; a
// missing key is an error.
func (s *RepoCIWatchStore) Delete(key string) error {
	s.mu.Lock()
	_, ok := s.records[key]
	snap := cloneRepoCIWatchRecord(s.records[key])
	delete(s.records, key)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("repo ci watch: no record for key %q", key)
	}

	if err := s.persist(); err != nil {
		s.mu.Lock()
		s.records[key] = snap
		s.mu.Unlock()
		return err
	}
	return nil
}
