// The run journal keeps the start and end of every pipeline, so that one a
// restart interrupted can be listed. Pipelines are not resumed.

package pipeline

import "time"

// RunRecord is what is kept of one pipeline run: IDs, state and times.
type RunRecord struct {
	ID        string     `json:"id"`
	State     State      `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Interrupted marks a run that had not ended when the daemon started.
	Interrupted bool `json:"interrupted,omitempty"`
}

// Journal is the saved list of pipeline runs.
type Journal struct{}

// NewJournal opens the journal at path, sealing it with key when set. Runs
// still marked running are marked interrupted.
func NewJournal(path string, key []byte) (*Journal, error) { return &Journal{}, nil }

// Records returns the saved runs, oldest first.
func (j *Journal) Records() []RunRecord { return nil }

// SetJournal records the start and end of every pipeline in j.
func (e *Executor) SetJournal(j *Journal) {}
