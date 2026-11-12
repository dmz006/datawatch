// Defect-queue intake: route one incoming defect to the tiny-fix lane, to an open
// draft batch of its area, or to a new batch, apply that decision through
// injected interfaces, and append it to a decision log.
//
// Routing order:
//  1. tiny_fix  - ClassifyTinyFix passes and the operator-visible re-check
//     does not refuse.
//  2. open_batch - a draft batch of the same project and area exists with
//     room under size_cap (any draft batch with room when allow_unrelated).
//  3. new_batch - otherwise.
//
// When the tiny-fix lane refuses a change that looked eligible (the re-check
// finds operator-visible or out-of-lane paths), Intake falls back to batch
// routing and records both the refusal and the final route.
package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Intake routes.
const (
	IntakeRouteOpenBatch = "open_batch"
	IntakeRouteNewBatch  = "new_batch"
	IntakeRouteTinyFix   = "tiny_fix"
)

// BatchStatusDraft is the status of a batch that still accepts defects.
const BatchStatusDraft = "draft"

// IntakeDefect is one incoming defect description.
type IntakeDefect struct {
	ID          string
	Title       string
	Description string
	Files       []string
	Kind        string
	Area        string
	Priority    int
	Project     string
}

func (d IntakeDefect) asDefect() QueueDefect {
	return QueueDefect{ID: d.ID, Title: d.Title, Description: d.Description, Priority: d.Priority,
		Project: d.Project, Area: d.Area, Files: append([]string(nil), d.Files...)}
}

// OpenBatch is a batch Automaton the store reports as not yet finished.
type OpenBatch struct {
	ID      string
	Project string
	Area    string
	Status  string
	Size    int
}

// DefectIntakeStore lists open batches and creates or extends Automatons.
type DefectIntakeStore interface {
	ListOpenBatches(project string) ([]OpenBatch, error)
	CreateBatch(d QueueDefect) (batchID string, err error)
	AddToBatch(batchID string, d QueueDefect) error
	CreateTinyFix(d QueueDefect, change TinyFixChange) (automatonID string, err error)
}

// IntakeRecord is one line of the intake decision log.
type IntakeRecord struct {
	At             time.Time `json:"at"`
	DefectID       string    `json:"defect_id"`
	Route          string    `json:"route"`
	Reason         string    `json:"reason"`
	BatchID        string    `json:"batch_id,omitempty"`
	TinyFixRefusal string    `json:"tiny_fix_refusal,omitempty"`
	ApplyError     string    `json:"apply_error,omitempty"`
}

// IntakeLog receives every decision.
type IntakeLog interface {
	Append(IntakeRecord) error
}

// FileIntakeLog is an append-only JSONL decision log at a configurable path.
type FileIntakeLog struct {
	Path string
	mu   sync.Mutex
}

// NewFileIntakeLog returns a log writing to path.
func NewFileIntakeLog(path string) *FileIntakeLog { return &FileIntakeLog{Path: path} }

// DefaultIntakeLogPath is the log location under a data dir.
func DefaultIntakeLogPath(dataDir string) string {
	return filepath.Join(dataDir, "defect_intake_decisions.jsonl")
}

// Append writes one record as a JSON line.
func (l *FileIntakeLog) Append(r IntakeRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// IntakeState is the injected world Intake works against.
type IntakeState struct {
	Store DefectIntakeStore
	Log   IntakeLog
	// Now defaults to time.Now.
	Now func() time.Time
}

// IntakeConfig holds the settings Intake reads.
type IntakeConfig struct {
	Queue config.DefectQueueConfig
	// Recheck is the operator-visible re-check on the tiny-fix lane;
	// defaults to CheckOperatorVisible.
	Recheck func(files []string, cfg config.DefectQueueConfig, project string) TinyFixRefusal
}

// IntakeDecision is the routing outcome.
type IntakeDecision struct {
	Route   string `json:"route"`
	BatchID string `json:"batch_id,omitempty"`
	Reason  string `json:"reason"`
	// TinyFixRefusal is set when the lane refused and Intake fell back.
	TinyFixRefusal string `json:"tiny_fix_refusal,omitempty"`
}

// Intake decides the route for one defect, applies it through the store and
// appends the decision to the log. A store failure is returned and recorded
// in the log entry; the decision is still returned.
func Intake(defect IntakeDefect, state IntakeState, cfg IntakeConfig) (IntakeDecision, error) {
	dec, err := decideIntake(defect, state, cfg)
	if err != nil {
		return dec, err
	}
	var applyErr error
	d := defect.asDefect()
	switch dec.Route {
	case IntakeRouteTinyFix:
		dec.BatchID, applyErr = state.Store.CreateTinyFix(d, TinyFixChange{Files: defect.Files, Kind: defect.Kind})
	case IntakeRouteOpenBatch:
		applyErr = state.Store.AddToBatch(dec.BatchID, d)
	case IntakeRouteNewBatch:
		dec.BatchID, applyErr = state.Store.CreateBatch(d)
	}
	rec := IntakeRecord{DefectID: defect.ID, Route: dec.Route, Reason: dec.Reason,
		BatchID: dec.BatchID, TinyFixRefusal: dec.TinyFixRefusal}
	if applyErr != nil {
		rec.ApplyError = applyErr.Error()
	}
	now := time.Now
	if state.Now != nil {
		now = state.Now
	}
	rec.At = now().UTC()
	if state.Log != nil {
		if lerr := state.Log.Append(rec); lerr != nil && applyErr == nil {
			applyErr = fmt.Errorf("intake decision log: %w", lerr)
		}
	}
	return dec, applyErr
}

func decideIntake(defect IntakeDefect, state IntakeState, cfg IntakeConfig) (IntakeDecision, error) {
	if state.Store == nil {
		return IntakeDecision{}, fmt.Errorf("defect intake needs a store")
	}
	var refusal string
	change := TinyFixChange{Files: defect.Files, Kind: defect.Kind}
	if c := ClassifyTinyFix(change, cfg.Queue, defect.Project); c.Eligible {
		recheck := cfg.Recheck
		if recheck == nil {
			recheck = CheckOperatorVisible
		}
		if r := recheck(defect.Files, cfg.Queue, defect.Project); r.Refused {
			refusal = r.Reason
		} else {
			return IntakeDecision{Route: IntakeRouteTinyFix, Reason: "tiny fix: " + c.Reason}, nil
		}
	}
	dec, err := routeToBatch(defect, state.Store, cfg.Queue)
	if err != nil {
		return dec, err
	}
	if refusal != "" {
		dec.TinyFixRefusal = refusal
		dec.Reason = "tiny-fix refused, sent to the normal path (" + refusal + "); " + dec.Reason
	}
	return dec, nil
}

func routeToBatch(defect IntakeDefect, store DefectIntakeStore, q config.DefectQueueConfig) (IntakeDecision, error) {
	s := q.EffectiveForProject(defect.Project).Batch
	capN := s.SizeCap
	if capN < 1 {
		capN = 1
	}
	open, err := store.ListOpenBatches(defect.Project)
	if err != nil {
		return IntakeDecision{}, fmt.Errorf("list open batches: %w", err)
	}
	area := strings.TrimSpace(defect.Area)
	var sameArea, full, notDraft int
	for _, b := range open {
		if b.Project != defect.Project {
			continue
		}
		match := s.AllowUnrelated || (area != "" && b.Area == area)
		if !match {
			continue
		}
		sameArea++
		if b.Status != BatchStatusDraft {
			notDraft++
			continue
		}
		if b.Size >= capN {
			full++
			continue
		}
		return IntakeDecision{
			Route:   IntakeRouteOpenBatch,
			BatchID: b.ID,
			Reason:  fmt.Sprintf("area %q has open draft batch %s with %d/%d stories", b.Area, b.ID, b.Size, capN),
		}, nil
	}
	var why string
	switch {
	case area == "":
		why = "defect has no area, so no open batch matches"
	case sameArea == 0:
		why = fmt.Sprintf("no open batch for area %q", area)
	default:
		why = fmt.Sprintf("area %q has %d open batch(es) but %d full (size_cap %d) and %d not draft", area, sameArea, full, capN, notDraft)
	}
	return IntakeDecision{Route: IntakeRouteNewBatch, Reason: "starting a new batch: " + why}, nil
}
