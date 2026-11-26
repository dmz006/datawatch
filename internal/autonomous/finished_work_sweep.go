package autonomous

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// finishedWorkLimit bounds the findings kept in memory and on disk.
const finishedWorkLimit = 1000

// finishedWorkSweepFile sits beside the other autonomous stores.
const finishedWorkSweepFile = "finished_work_findings.json"

// FinishedWorkFinding is one finding of the finished-work sweep: an
// Automaton that finished, what was found about its work, and what to do
// about it.
type FinishedWorkFinding struct {
	Automaton string `json:"automaton"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Cause     string `json:"cause"`
	Action    string `json:"action"`
	At        string `json:"at"`
	// Owner is who owns getting the work landed: the operator who last acted
	// on the record, or "operator" when nobody did.
	Owner string `json:"owner,omitempty"`
	// Branch and Ahead name the work: the branch and how many of its commits
	// are not on the default branch.
	Branch string `json:"branch,omitempty"`
	Ahead  int    `json:"ahead,omitempty"`
	// Since is when the daemon first saw the finished work waiting, so its
	// age survives restarts and the daemon's own decisions on the record.
	Since string `json:"since,omitempty"`
	// QuestionID is the open question asking the owner what to do.
	QuestionID string `json:"question_id,omitempty"`
	// ReaskAt is the date a left-alone branch is asked about again.
	ReaskAt string `json:"reask_at,omitempty"`
	// MergeSelection is the merge selection rule's answer: whether datawatch
	// may merge this work, and why not when it may not.
	MergeSelection string `json:"merge_selection,omitempty"`
}

// FinishedWorkSweep is the bounded, persisted findings of the finished-work
// sweep, newest first. Listing the findings never changes them.
type FinishedWorkSweep struct {
	mu       sync.Mutex
	path     string
	findings []FinishedWorkFinding
	dirty    bool
	// cursor is the Automaton id the last bounded pass ended on; the next
	// pass starts after it, so every Automaton is reached across passes.
	cursor string
	state  FinishedWorkState
}

// NewFinishedWorkSweep opens the sweep under dir. A missing file is an empty
// sweep; a corrupt one is logged and replaced with an empty sweep, never
// fatal.
func NewFinishedWorkSweep(dir string) *FinishedWorkSweep {
	s := &FinishedWorkSweep{path: filepath.Join(dir, finishedWorkSweepFile)}
	s.load()
	s.loadState()
	return s
}

func (s *FinishedWorkSweep) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] finished work sweep: read %s: %v", s.path, err)
		}
		return
	}
	if err := json.Unmarshal(data, &s.findings); err != nil {
		log.Printf("[autonomous] finished work sweep: %s is corrupt, starting empty: %v", s.path, err)
		s.dirty = true
		s.save()
		return
	}
	if len(s.findings) > finishedWorkLimit {
		s.findings = s.findings[:finishedWorkLimit]
	}
}

// Record stores one finding, newest first. An empty At reads as now.
func (s *FinishedWorkSweep) Record(f FinishedWorkFinding) {
	if f.At == "" {
		f.At = time.Now().UTC().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findings = append([]FinishedWorkFinding{f}, s.findings...)
	if len(s.findings) > finishedWorkLimit {
		s.findings = s.findings[:finishedWorkLimit]
	}
	s.dirty = true
	s.save()
}

// SweepFindings returns the stored findings, newest first (all of them).
func (s *FinishedWorkSweep) SweepFindings() []FinishedWorkFinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FinishedWorkFinding, len(s.findings))
	copy(out, s.findings)
	return out
}

// save writes the findings when they changed. The caller holds s.mu.
func (s *FinishedWorkSweep) save() {
	if !s.dirty {
		return
	}
	data, err := json.Marshal(s.findings)
	if err == nil {
		err = atomicWriteFile(s.path, data)
	}
	if err != nil {
		log.Printf("[autonomous] finished work sweep: write %s: %v", s.path, err)
		return
	}
	s.dirty = false
}

// FinishedWorkSweep returns the sweep the daemon records finished-work
// findings in (nil when not wired).
func (m *Manager) FinishedWorkSweep() *FinishedWorkSweep {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finishedWork
}

// RecordFinishedWorkFinding stores one finished-work finding (a no-op when
// no sweep is wired).
func (m *Manager) RecordFinishedWorkFinding(f FinishedWorkFinding) {
	m.mu.Lock()
	s := m.finishedWork
	m.mu.Unlock()
	if s != nil {
		s.Record(f)
	}
}

// SweepFindings lists the finished-work findings (none when no sweep is
// wired).
func (m *Manager) SweepFindings() []FinishedWorkFinding {
	m.mu.Lock()
	s := m.finishedWork
	m.mu.Unlock()
	if s == nil {
		return []FinishedWorkFinding{}
	}
	return s.SweepFindings()
}

// FinishedWorkSweep serves the finished_work rows of
// GET /api/autonomous/housekeeping-repairs.
func (a *API) FinishedWorkSweep() *FinishedWorkSweep { return a.M.FinishedWorkSweep() }

// SweepFindings lists the finished-work findings (none when no sweep is
// wired).
func (a *API) SweepFindings() []FinishedWorkFinding { return a.M.SweepFindings() }

// RecordFinishedWorkFinding stores one finished-work finding (a no-op when
// no sweep is wired).
func (a *API) RecordFinishedWorkFinding(f FinishedWorkFinding) { a.M.RecordFinishedWorkFinding(f) }

// Upsert stores one finding for its Automaton: an existing finding of the
// same Automaton is replaced in place (keeping its first-seen time and open
// question when the new one has none), a new one goes first. A repeated pass
// therefore updates a finding and never repeats it.
func (s *FinishedWorkSweep) Upsert(f FinishedWorkFinding) {
	if f.At == "" {
		f.At = time.Now().UTC().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertLocked(f)
	s.dirty = true
	s.save()
}

func (s *FinishedWorkSweep) upsertLocked(f FinishedWorkFinding) {
	for i := range s.findings {
		if s.findings[i].Automaton != f.Automaton {
			continue
		}
		old := s.findings[i]
		if f.Since == "" {
			f.Since = old.Since
		}
		if f.QuestionID == "" && old.Cause == f.Cause {
			f.QuestionID = old.QuestionID
		}
		s.findings[i] = f
		return
	}
	s.findings = append([]FinishedWorkFinding{f}, s.findings...)
	if len(s.findings) > finishedWorkLimit {
		s.findings = s.findings[:finishedWorkLimit]
	}
}

// Remove drops the finding of an Automaton, reporting the one it dropped.
func (s *FinishedWorkSweep) Remove(automaton string) (FinishedWorkFinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.findings {
		if s.findings[i].Automaton == automaton {
			f := s.findings[i]
			s.findings = append(s.findings[:i:i], s.findings[i+1:]...)
			s.dirty = true
			s.save()
			return f, true
		}
	}
	return FinishedWorkFinding{}, false
}

// Get returns the finding of an Automaton.
func (s *FinishedWorkSweep) Get(automaton string) (FinishedWorkFinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.findings {
		if f.Automaton == automaton {
			return f, true
		}
	}
	return FinishedWorkFinding{}, false
}

// Replace swaps the whole set for the findings of a full audit, keeping the
// first-seen time and open question of an Automaton that is still found.
func (s *FinishedWorkSweep) Replace(found []FinishedWorkFinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.findings
	s.findings = nil
	for i := len(found) - 1; i >= 0; i-- {
		f := found[i]
		for _, o := range old {
			if o.Automaton == f.Automaton {
				if f.Since == "" {
					f.Since = o.Since
				}
				if f.QuestionID == "" && o.Cause == f.Cause {
					f.QuestionID = o.QuestionID
				}
			}
		}
		s.upsertLocked(f)
	}
	s.dirty = true
	s.save()
}

// FinishedWorkState is what the sweep remembers between passes and restarts:
// when it last ran, last sent the daily digest and last ran the weekly audit.
type FinishedWorkState struct {
	LastSweep        string `json:"last_sweep,omitempty"`
	LastDigest       string `json:"last_digest,omitempty"`
	LastAudit        string `json:"last_audit,omitempty"`
	AuditWeek        string `json:"audit_week,omitempty"`
	AuditRecordID    string `json:"audit_record_id,omitempty"`
	LastAuditSummary string `json:"last_audit_summary,omitempty"`
}

func (s *FinishedWorkSweep) statePath() string {
	return strings.TrimSuffix(s.path, ".json") + "_state.json"
}

func (s *FinishedWorkSweep) loadState() {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		log.Printf("[autonomous] finished work sweep: %s is corrupt, starting empty: %v", s.statePath(), err)
		s.state = FinishedWorkState{}
	}
}

// State returns the remembered state.
func (s *FinishedWorkSweep) State() FinishedWorkState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// UpdateState changes the remembered state and writes it.
func (s *FinishedWorkSweep) UpdateState(f func(*FinishedWorkState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.state)
	data, err := json.Marshal(s.state)
	if err == nil {
		err = atomicWriteFile(s.statePath(), data)
	}
	if err != nil {
		log.Printf("[autonomous] finished work sweep: write %s: %v", s.statePath(), err)
	}
}
