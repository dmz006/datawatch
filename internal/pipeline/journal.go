// The run journal keeps the start and end of every pipeline, so that one a
// restart interrupted can be listed. Pipelines are not resumed.

package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// RunRecord is what is kept of one pipeline run: IDs, state and times.
type RunRecord struct {
	ID        string     `json:"id"`
	State     State      `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Interrupted marks a run that had not ended when the daemon started.
	Interrupted bool `json:"interrupted,omitempty"`
}

// maxRuns bounds the journal; completed runs go first.
const maxRuns = 1000

type journalFile struct {
	Version int         `json:"version"`
	Runs    []RunRecord `json:"runs"`
}

// Journal is the saved list of pipeline runs.
type Journal struct {
	mu   sync.Mutex
	path string
	key  []byte
	runs []RunRecord
}

// NewJournal opens the journal at path, sealing it with key when set. Runs
// still marked running are marked interrupted. A journal that cannot be read
// is an error and is left as it is.
func NewJournal(path string, key []byte) (*Journal, error) {
	j := &Journal{path: path, key: key}
	data, err := secfile.ReadFile(path, key)
	if errors.Is(err, fs.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pipeline journal: %w", err)
	}
	var f journalFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("pipeline journal %s: %w", path, err)
	}
	j.runs = f.Runs
	changed := false
	for i := range j.runs {
		if j.runs[i].EndedAt == nil && !j.runs[i].Interrupted {
			j.runs[i].Interrupted, changed = true, true
		}
	}
	if changed {
		if err := j.save(j.runs); err != nil {
			return nil, fmt.Errorf("pipeline journal: %w", err)
		}
	}
	return j, nil
}

// Records returns the saved runs, oldest first.
func (j *Journal) Records() []RunRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]RunRecord(nil), j.runs...)
}

func (j *Journal) save(runs []RunRecord) error {
	data, err := json.Marshal(journalFile{Version: 1, Runs: runs})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	return secfile.WriteFileDurable(j.path, data, 0o600, j.key)
}

// begin saves the start of a run.
func (j *Journal) begin(id string, at time.Time) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	next := append(append([]RunRecord(nil), j.runs...), RunRecord{ID: id, State: StateRunning, StartedAt: at.UTC()})
	for i := 0; len(next) > maxRuns && i < len(next); {
		if next[i].State == StateCompleted {
			next = append(next[:i], next[i+1:]...)
			continue
		}
		i++
	}
	if len(next) > maxRuns {
		next = next[len(next)-maxRuns:]
	}
	if err := j.save(next); err != nil {
		return err
	}
	j.runs = next
	return nil
}

// end saves how the newest open run with id ended.
func (j *Journal) end(id string, state State, at time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.runs) - 1; i >= 0; i-- {
		r := &j.runs[i]
		if r.ID != id || r.EndedAt != nil || r.Interrupted {
			continue
		}
		t := at.UTC()
		r.State, r.EndedAt = state, &t
		if err := j.save(j.runs); err != nil {
			// It stays open on disk, so a restart lists it as interrupted.
			log.Printf("[pipeline] %s: the end could not be saved: %v", id, err)
		}
		return
	}
}

// SetJournal records the start and end of every pipeline in j.
func (e *Executor) SetJournal(j *Journal) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.journal = j
}

func (e *Executor) runJournal() *Journal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.journal
}
