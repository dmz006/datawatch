// Release record — one persisted entry per release push, carrying what
// was pushed (project, tag, commit, release path), when, whether the push
// reached the remote, and the CI outcome observed for that release.
//
// Like the repo CI watch record, a release record is a SEPARATE store
// entry rather than a field on a PRD: a release tag has no owning
// Automaton to hang it on. The store uses the same JSONL helpers
// (loadJSONL / writeJSONL) and full-rewrite persistence as the other
// autonomous record stores.
//
// This file only records push and CI outcomes. It never starts a CI
// watch; setReleaseCIOutcome takes the outcome and the time it was
// observed as inputs.
package autonomous

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ReleaseRecordFileName is the JSONL file the release record store
// persists under the <data_dir>/autonomous/ directory.
const ReleaseRecordFileName = "release_records.jsonl"

// Release CI outcomes. A release starts "pending" while its CI run is
// in flight, and moves to "green" or "red" when the run finishes.
// "unknown" is for a run whose result could not be determined.
const (
	ReleaseCIPending = "pending"
	ReleaseCIGreen   = "green"
	ReleaseCIRed     = "red"
	ReleaseCIUnknown = "unknown"
)

// ReleaseCIOutcomes is the CI outcome vocabulary.
var ReleaseCIOutcomes = []string{ReleaseCIPending, ReleaseCIGreen, ReleaseCIRed, ReleaseCIUnknown}

// ReleaseRecord is the persisted record of one release push.
type ReleaseRecord struct {
	// Project is the project the release belongs to.
	Project string `json:"project"`
	// Tag is the release tag that was pushed.
	Tag string `json:"tag"`
	// PushedCommit is the commit the tag points at on the remote.
	PushedCommit string `json:"pushed_commit"`
	// ReleasePath is the path of the release artefact or working tree
	// the push was made from.
	ReleasePath string `json:"release_path"`
	// PushedAt is when the push was attempted.
	PushedAt time.Time `json:"pushed_at"`
	// PushOutcome is the result of the push: "pushed" or a failure
	// reason, as the caller reports it.
	PushOutcome string `json:"push_outcome"`
	// CIOutcome is one of ReleaseCIOutcomes.
	CIOutcome string `json:"ci_outcome"`
	// CIWatchedAt is when the CI outcome was last observed. Nil until
	// an outcome has been recorded.
	CIWatchedAt *time.Time `json:"ci_watched_at,omitempty"`
	// ReleaseSize is the size of this release: patch, minor or major.
	// The release CI fall-back needs it to name the approval mode to
	// change; an empty size makes the fall-back a no-op.
	ReleaseSize string `json:"release_size,omitempty"`
}

// Key identifies a release record: one record per project and tag.
func (rec *ReleaseRecord) Key() string {
	return rec.Project + "\x00" + rec.Tag
}

// ValidReleaseCIOutcome reports whether outcome is in the CI outcome
// vocabulary.
func ValidReleaseCIOutcome(outcome string) bool {
	for _, o := range ReleaseCIOutcomes {
		if o == outcome {
			return true
		}
	}
	return false
}

// ReleaseRecordStore is the in-memory + JSONL-backed store for release
// records, keyed by ReleaseRecord.Key.
type ReleaseRecordStore struct {
	// mu guards records.
	mu      sync.Mutex
	dir     string
	records map[string]*ReleaseRecord
	// persistMu serializes file writes so concurrent persists do not
	// interleave on the same file.
	persistMu sync.Mutex
}

// NewReleaseRecordStore opens (or creates) release_records.jsonl under
// dir. A missing file reads as an empty store.
func NewReleaseRecordStore(dir string) (*ReleaseRecordStore, error) {
	s := &ReleaseRecordStore{
		dir:     dir,
		records: map[string]*ReleaseRecord{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := loadJSONL(filepath.Join(dir, ReleaseRecordFileName), func(line []byte) error {
		var rec ReleaseRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		s.records[rec.Key()] = &rec
		return nil
	}); err != nil {
		return nil, err
	}
	return s, nil
}

// cloneReleaseRecord returns a deep copy of rec via a JSON round trip,
// so callers cannot mutate store state through the copy.
func cloneReleaseRecord(rec *ReleaseRecord) *ReleaseRecord {
	if rec == nil {
		return nil
	}
	b, err := json.Marshal(rec)
	if err != nil {
		panic(err) // plain data; marshaling cannot fail
	}
	var out ReleaseRecord
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return &out
}

// Get returns a deep copy of the record under key; the second result
// reports whether it exists.
func (s *ReleaseRecordStore) Get(key string) (*ReleaseRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, false
	}
	return cloneReleaseRecord(rec), true
}

// GetRelease returns a deep copy of the record for project and tag.
func (s *ReleaseRecordStore) GetRelease(project, tag string) (*ReleaseRecord, bool) {
	return s.Get((&ReleaseRecord{Project: project, Tag: tag}).Key())
}

// List returns deep copies of all records, newest PushedAt first.
func (s *ReleaseRecordStore) List() []*ReleaseRecord {
	s.mu.Lock()
	out := make([]*ReleaseRecord, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, cloneReleaseRecord(rec))
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].PushedAt.Equal(out[j].PushedAt) {
			return out[i].PushedAt.After(out[j].PushedAt)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// persist writes the in-memory state back as JSON lines (full rewrite).
func (s *ReleaseRecordStore) persist() error {
	s.mu.Lock()
	snapshot := make([]*ReleaseRecord, 0, len(s.records))
	for _, rec := range s.records {
		snapshot = append(snapshot, cloneReleaseRecord(rec))
	}
	s.mu.Unlock()

	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return writeJSONL(filepath.Join(s.dir, ReleaseRecordFileName), len(snapshot), func(emit func(any) error) error {
		for _, rec := range snapshot {
			if err := emit(rec); err != nil {
				return err
			}
		}
		return nil
	})
}

// save stores rec under its key, replacing any existing record, and
// persists the change. A persist failure rolls the in-memory change
// back and returns the persist error.
func (s *ReleaseRecordStore) save(rec *ReleaseRecord) error {
	if rec == nil || rec.Project == "" || rec.Tag == "" {
		return fmt.Errorf("release record: project and tag are required")
	}
	key := rec.Key()
	stored := cloneReleaseRecord(rec)
	s.mu.Lock()
	snap, existed := s.records[key]
	if existed {
		snap = cloneReleaseRecord(snap)
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
		return err
	}
	return nil
}

// recordReleasePush records one release push. The record starts with
// CI outcome "pending" and no CI watch time. A push for a project and
// tag that already has a record replaces it, so a re-push refreshes the
// record rather than duplicating it.
func (s *ReleaseRecordStore) recordReleasePush(project, tag, pushedCommit, releasePath string, pushedAt time.Time, pushOutcome string) (*ReleaseRecord, error) {
	rec := &ReleaseRecord{
		Project:      project,
		Tag:          tag,
		PushedCommit: pushedCommit,
		ReleasePath:  releasePath,
		PushedAt:     pushedAt,
		PushOutcome:  pushOutcome,
		CIOutcome:    ReleaseCIPending,
	}
	if err := s.save(rec); err != nil {
		return nil, err
	}
	return cloneReleaseRecord(rec), nil
}

// setReleaseCIOutcome updates the CI outcome of an existing release
// record and stamps the time the outcome was observed. It rejects an
// outcome outside ReleaseCIOutcomes and a missing record.
func (s *ReleaseRecordStore) setReleaseCIOutcome(project, tag, outcome string, watchedAt time.Time) (*ReleaseRecord, error) {
	if !ValidReleaseCIOutcome(outcome) {
		return nil, fmt.Errorf("release record: invalid CI outcome %q", outcome)
	}
	rec, ok := s.GetRelease(project, tag)
	if !ok {
		return nil, fmt.Errorf("release record: no record for project %q tag %q", project, tag)
	}
	rec.CIOutcome = outcome
	rec.CIWatchedAt = &watchedAt
	if err := s.save(rec); err != nil {
		return nil, err
	}
	return cloneReleaseRecord(rec), nil
}
