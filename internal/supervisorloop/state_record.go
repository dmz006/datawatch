package supervisorloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Sections of the pinned state record.
const (
	SectionDecision   = "decision"   // standing decision
	SectionWork       = "work"       // open work item (owner and next step)
	SectionChecked    = "checked"    // what was last checked, and when
	SectionCorrection = "correction" // correction the operator gave
)

var stateSections = map[string]bool{
	SectionDecision: true, SectionWork: true, SectionChecked: true, SectionCorrection: true,
}

// Errors returned by the state record.
var (
	ErrEntryNotFound = errors.New("state record entry not found")
	ErrBadEntry      = errors.New("invalid state record entry")
)

// MemoryStore is the memory the pinned record lives in. Pinned entries are
// part of the wake-up bundle.
type MemoryStore interface {
	// LoadPinned returns the pinned entry under key; ok is false when absent.
	LoadPinned(key string) (text string, ok bool, err error)
	// SavePinned writes the pinned entry under key, replacing any previous one.
	SavePinned(key, text string) error
	// Recall returns stored lesson texts matching the query.
	Recall(query string) ([]string, error)
}

// Edit records one change: who, when and why.
type Edit struct {
	Who    string    `json:"who"`
	When   time.Time `json:"when"`
	Why    string    `json:"why"`
	Action string    `json:"action"` // set, change
}

// StateEntry is one line of the record.
type StateEntry struct {
	ID       string `json:"id"`
	Section  string `json:"section"`
	Text     string `json:"text"`
	Owner    string `json:"owner,omitempty"`
	NextStep string `json:"next_step,omitempty"`
	Edits    []Edit `json:"edits"`
}

// StateRecord is the pinned record for one project and role.
type StateRecord struct {
	Project string       `json:"project"`
	Role    string       `json:"role"`
	Entries []StateEntry `json:"entries"`
}

// EntryChange lists the fields ChangeEntry may alter; nil leaves a field.
type EntryChange struct {
	Text     *string
	Owner    *string
	NextStep *string
}

// StateRecords reads and edits pinned state records through a MemoryStore.
type StateRecords struct {
	store MemoryStore
	clock Clock
}

// NewStateRecords builds the accessor.
func NewStateRecords(store MemoryStore, clock Clock) *StateRecords {
	return &StateRecords{store: store, clock: clock}
}

// StateKey is the memory key of the pinned record for a project and role.
func StateKey(project, role string) string {
	return "supervisor-state/" + project + "/" + role
}

// Get returns the record; a missing record comes back empty.
func (s *StateRecords) Get(project, role string) (*StateRecord, error) {
	if project == "" || role == "" {
		return nil, fmt.Errorf("%w: project and role are required", ErrBadEntry)
	}
	text, ok, err := s.store.LoadPinned(StateKey(project, role))
	if err != nil {
		return nil, err
	}
	rec := &StateRecord{Project: project, Role: role}
	if !ok || strings.TrimSpace(text) == "" {
		return rec, nil
	}
	if err := json.Unmarshal([]byte(text), rec); err != nil {
		return nil, fmt.Errorf("decode state record: %w", err)
	}
	return rec, nil
}

func (s *StateRecords) save(rec *StateRecord) error {
	sort.SliceStable(rec.Entries, func(i, j int) bool { return rec.Entries[i].ID < rec.Entries[j].ID })
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.store.SavePinned(StateKey(rec.Project, rec.Role), string(b))
}

func find(rec *StateRecord, id string) int {
	for i := range rec.Entries {
		if rec.Entries[i].ID == id {
			return i
		}
	}
	return -1
}

// SetEntry creates the entry, or replaces it when the id exists. Writing the
// same content again changes nothing and records no edit.
func (s *StateRecords) SetEntry(project, role string, e StateEntry, who, why string) error {
	if e.ID == "" || !stateSections[e.Section] || who == "" || why == "" {
		return fmt.Errorf("%w: id, valid section, who and why are required", ErrBadEntry)
	}
	rec, err := s.Get(project, role)
	if err != nil {
		return err
	}
	i := find(rec, e.ID)
	if i >= 0 {
		old := rec.Entries[i]
		if old.Section == e.Section && old.Text == e.Text && old.Owner == e.Owner && old.NextStep == e.NextStep {
			return nil
		}
		e.Edits = old.Edits
	} else {
		e.Edits = nil
	}
	e.Edits = append(e.Edits, Edit{Who: who, When: s.clock.Now(), Why: why, Action: "set"})
	if i >= 0 {
		rec.Entries[i] = e
	} else {
		rec.Entries = append(rec.Entries, e)
	}
	return s.save(rec)
}

// ChangeEntry alters fields of an existing entry. A change that alters
// nothing records no edit and writes nothing.
func (s *StateRecords) ChangeEntry(project, role, id string, c EntryChange, who, why string) error {
	if who == "" || why == "" {
		return fmt.Errorf("%w: who and why are required", ErrBadEntry)
	}
	rec, err := s.Get(project, role)
	if err != nil {
		return err
	}
	i := find(rec, id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrEntryNotFound, id)
	}
	e := rec.Entries[i]
	changed := false
	apply := func(dst *string, src *string) {
		if src != nil && *src != *dst {
			*dst = *src
			changed = true
		}
	}
	apply(&e.Text, c.Text)
	apply(&e.Owner, c.Owner)
	apply(&e.NextStep, c.NextStep)
	if !changed {
		return nil
	}
	e.Edits = append(append([]Edit(nil), e.Edits...), Edit{Who: who, When: s.clock.Now(), Why: why, Action: "change"})
	rec.Entries[i] = e
	return s.save(rec)
}

// RemoveEntry deletes an entry. Removing an absent entry is a no-op.
func (s *StateRecords) RemoveEntry(project, role, id, who, why string) error {
	if who == "" || why == "" {
		return fmt.Errorf("%w: who and why are required", ErrBadEntry)
	}
	rec, err := s.Get(project, role)
	if err != nil {
		return err
	}
	i := find(rec, id)
	if i < 0 {
		return nil
	}
	rec.Entries = append(rec.Entries[:i], rec.Entries[i+1:]...)
	return s.save(rec)
}

// Lessons recalls lessons from the same store to seed a fresh session.
// Blank and duplicate lessons are dropped; order is kept.
func (s *StateRecords) Lessons(project, role string) ([]string, error) {
	got, err := s.store.Recall("lessons " + project + " " + role)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, l := range got {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out, nil
}
