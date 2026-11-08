// Merge pipeline run records. The merge pipeline runner (package
// mergepipeline, which imports this package, so the runner cannot live here)
// reports each step it runs - step, start, end, result and the source of the
// pipeline in effect - to a MergeRunRecords store. The store is bounded,
// persisted beside the other autonomous stores and shown in the merge status
// (GET /api/autonomous/merge, "pipeline_runs"), so the PWA and CLI can list
// what a merge did.
package autonomous

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// mergeRunRecordLimit bounds the run history kept in memory and on disk.
const mergeRunRecordLimit = 200

// mergeRunRecordsFile sits beside the other autonomous stores.
const mergeRunRecordsFile = "merge_pipeline_runs.json"

// Results of one pipeline step.
const (
	MergeStepOK      = "ok"
	MergeStepFailed  = "failed"
	MergeStepSkipped = "skipped"
	// MergeStepRunning marks the step in progress.
	MergeStepRunning = "running"
)

// Results of a whole run.
const (
	MergeRunOK = "ok"
	// MergeRunFailed means a failing step stopped the run.
	MergeRunFailed = "failed"
	// MergeRunCompletedWithFailures means every step ran but at least one
	// failed and was set to continue.
	MergeRunCompletedWithFailures = "completed_with_failures"
	MergeRunRunning               = "running"
)

// MergeStepRun is one step of one run.
type MergeStepRun struct {
	Step  string     `json:"step"`
	Kind  string     `json:"kind"`
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end"`
	// Result is ok, failed, skipped or running.
	Result string `json:"result"`
	// Source is the pipeline source in effect: file, record or default.
	Source string `json:"source"`
	// OnFailure is the step's failure action (stop or continue).
	OnFailure string `json:"on_failure,omitempty"`
	// Detail is a short plain-language account; secrets are removed.
	Detail string `json:"detail,omitempty"`
}

// MergePipelineRun is one run of a merge pipeline.
type MergePipelineRun struct {
	ID       string   `json:"id"`
	Project  string   `json:"project"`
	Branches []string `json:"branches"`
	Source   string   `json:"source"`
	Reason   string   `json:"reason,omitempty"`
	// ReleaseVersion is the version the run releases, computed once by the
	// set-release-version step and used by the later steps of the run.
	ReleaseVersion string `json:"release_version,omitempty"`
	// Validator is the release-bump validator's result when the run reached
	// its validate-release-bump step; nil otherwise.
	Validator *MergeValidatorResult `json:"validator,omitempty"`
	Start     time.Time             `json:"start"`
	End       *time.Time            `json:"end"`
	Result    string                `json:"result"`
	Steps     []MergeStepRun        `json:"steps"`
}

// MergeRunRecorder receives a run each time it changes. *MergeRunRecords is
// the production one.
type MergeRunRecorder interface {
	RecordMergeRun(MergePipelineRun)
}

// MergeRunRecords is the bounded, persisted run history, newest first. A run
// recorded again under the same ID replaces the earlier copy.
type MergeRunRecords struct {
	mu      sync.Mutex
	path    string
	entries []MergePipelineRun
}

// NewMergeRunRecords opens the history under dir. A missing file is an empty
// history; a corrupt one is logged and replaced, never fatal.
func NewMergeRunRecords(dir string) *MergeRunRecords {
	r := &MergeRunRecords{path: filepath.Join(dir, mergeRunRecordsFile)}
	data, err := os.ReadFile(r.path) // #nosec G304 -- fixed name under the store dir
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] Automaton merge pipeline runs: read %s: %v", r.path, err)
		}
		return r
	}
	if err := json.Unmarshal(data, &r.entries); err != nil {
		log.Printf("[autonomous] Automaton merge pipeline runs: %s is corrupt, starting empty: %v", r.path, err)
		r.entries = nil
		r.save()
	}
	if len(r.entries) > mergeRunRecordLimit {
		r.entries = r.entries[:mergeRunRecordLimit]
	}
	return r
}

// RecordMergeRun stores run, replacing the run with the same ID or adding it
// as the newest.
func (r *MergeRunRecords) RecordMergeRun(run MergePipelineRun) {
	if r == nil {
		return
	}
	run = cloneMergeRun(run)
	r.mu.Lock()
	defer r.mu.Unlock()
	replaced := false
	for i := range r.entries {
		if r.entries[i].ID == run.ID {
			r.entries[i] = run
			replaced = true
			break
		}
	}
	if !replaced {
		r.entries = append([]MergePipelineRun{run}, r.entries...)
		if len(r.entries) > mergeRunRecordLimit {
			r.entries = r.entries[:mergeRunRecordLimit]
		}
	}
	r.save()
}

// List returns up to limit runs, newest first (all when limit is not
// positive).
func (r *MergeRunRecords) List(limit int) []MergePipelineRun {
	out := []MergePipelineRun{}
	if r == nil {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.entries)
	if limit > 0 && limit < n {
		n = limit
	}
	for _, e := range r.entries[:n] {
		out = append(out, cloneMergeRun(e))
	}
	return out
}

func cloneMergeRun(run MergePipelineRun) MergePipelineRun {
	run.Branches = append([]string{}, run.Branches...)
	run.Steps = append([]MergeStepRun{}, run.Steps...)
	return run
}

// save writes the history. The caller holds r.mu.
func (r *MergeRunRecords) save() {
	data, err := json.Marshal(r.entries)
	if err == nil {
		err = atomicWriteFile(r.path, data)
	}
	if err != nil {
		log.Printf("[autonomous] Automaton merge pipeline runs: write %s: %v", r.path, err)
	}
}

// mergeRuns opens the run history on first use.
func (m *Manager) mergeRuns() *MergeRunRecords {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mergeRunStore == nil {
		m.mergeRunStore = NewMergeRunRecords(m.store.dir)
	}
	return m.mergeRunStore
}

// MergeLocks returns the merge lock store, opened under the daemon store
// directory on first use. A store that cannot be opened (for instance a
// corrupt lock file) is returned as an error and is not cached, so the next
// call tries again; a caller must treat that as a refusal, never as free.
func (m *Manager) MergeLocks() (*MergeLocks, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mergeLockStore == nil {
		locks, err := NewMergeLocks(m.store.dir)
		if err != nil {
			return nil, err
		}
		m.mergeLockStore = locks
	}
	return m.mergeLockStore, nil
}

// RecordMergeRun makes the Manager a MergeRunRecorder, so a pipeline run's
// steps land in the merge status the operator already sees.
func (m *Manager) RecordMergeRun(run MergePipelineRun) { m.mergeRuns().RecordMergeRun(run) }

// MergeRuns returns up to limit recorded pipeline runs, newest first.
func (m *Manager) MergeRuns(limit int) []MergePipelineRun { return m.mergeRuns().List(limit) }

// MergeQuestionStore returns the operator question store when the operator
// question channel is on, so a merge conflict nobody could settle reaches the
// operator. It returns nil when the channel is off or not wired.
func (m *Manager) MergeQuestionStore() *QuestionStore {
	store, on := m.operatorQuestionChannel()
	if !on {
		return nil
	}
	return store
}

// MergeBaseRepo is the project directory a merge runs in: the configured
// worktree base repo, empty when none is set.
func (m *Manager) MergeBaseRepo() string { return m.Config().WorktreeBaseRepo }
