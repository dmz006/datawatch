package mergepipeline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrRecordNotFound means the daemon holds no pipeline record for the project.
var ErrRecordNotFound = errors.New("merge pipeline record not found")

// Record is a pipeline the daemon keeps for one project. It is the fallback
// for a project whose repository has no pipeline file.
type Record struct {
	Project    string     `json:"project"`
	Definition Definition `json:"definition"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// RecordStore keeps one Record per project and persists them as a single JSON
// file, written to a temporary file and renamed so a crash never leaves a
// half-written store. It is safe for concurrent use.
type RecordStore struct {
	mu      sync.RWMutex
	path    string
	records map[string]Record
	now     func() time.Time
}

// NewRecordStore loads (or starts) the store kept at
// <dataDir>/merge-pipeline/records.json.
func NewRecordStore(dataDir string) (*RecordStore, error) {
	s := &RecordStore{
		path:    filepath.Join(dataDir, "merge-pipeline", "records.json"),
		records: map[string]Record{},
		now:     time.Now,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Get returns a copy of the project's record, or ErrRecordNotFound.
func (s *RecordStore) Get(project string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[project]
	if !ok {
		return Record{}, ErrRecordNotFound
	}
	return cloneRecord(r), nil
}

// Set validates def and stores it as the project's record, replacing any
// earlier one. An invalid definition is rejected and nothing is stored.
func (s *RecordStore) Set(project string, def Definition) (Record, error) {
	if strings.TrimSpace(project) == "" {
		return Record{}, errors.New("project name is required")
	}
	if err := def.Validate(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, had := s.records[project]
	rec := cloneRecord(Record{Project: project, Definition: def, UpdatedAt: s.now().UTC()})
	s.records[project] = rec
	if err := s.persist(); err != nil {
		if had {
			s.records[project] = prev
		} else {
			delete(s.records, project)
		}
		return Record{}, err
	}
	return cloneRecord(rec), nil
}

// Delete removes the project's record, or returns ErrRecordNotFound.
func (s *RecordStore) Delete(project string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.records[project]
	if !ok {
		return ErrRecordNotFound
	}
	delete(s.records, project)
	if err := s.persist(); err != nil {
		s.records[project] = prev
		return err
	}
	return nil
}

// List returns every record, sorted by project name.
func (s *RecordStore) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, cloneRecord(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out
}

func cloneRecord(r Record) Record {
	r.Definition.Steps = append([]Step(nil), r.Definition.Steps...)
	return r
}

func (s *RecordStore) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *RecordStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m map[string]Record
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for k, r := range m {
		r.Project = k
		s.records[k] = r
	}
	return nil
}
