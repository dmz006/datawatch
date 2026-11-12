package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
)

// TagSweepSession is one Automata-owned session (live or archived) as the
// tag sweep reads it: its id, the system tags it currently carries (the
// derived list, diffed and re-derived by the sweep) and the stored
// placement facts the sweep rebuilds its tags from. The operator's own tags
// (the session's Tags field) are a separate list and are never part of this
// struct, so the sweep can touch the system tags without ever seeing the
// operator's.
type TagSweepSession struct {
	ID         string
	SystemTags []string
	Placement  Placement
}

// TagSweepSessionStore backs the tag sweep's session pass: it lists every
// Automata-owned session (live and archived) with its current system tags
// and the stored placement facts, and writes each one's re-derived system
// tags. The operator tags and the inherited tags are a separate concern and
// are never written here. Wired by the daemon (see SetTagSweepSessions);
// nil means the sweep examines no sessions but the object passes still run.
type TagSweepSessionStore interface {
	// ListTagSweepSessions returns every Automata-owned session the sweep
	// should re-derive tags for.
	ListTagSweepSessions() []TagSweepSession
	// SetSystemTags replaces one session's derived system tags with the
	// given set.
	SetSystemTags(sessionID string, tags []string) error
}

// SweepCount is the per-kind tally one pass of the tag sweep produces.
type SweepCount struct {
	// Examined is the number of objects of this kind the sweep read.
	Examined int `json:"examined"`
	// Changed is the number whose system tags differ from what they held and
	// were re-derived (written when not a dry run).
	Changed int `json:"changed"`
	// Unchanged is the number already carrying the derived set.
	Unchanged int `json:"unchanged"`
	// Failed is the number whose re-derivation write was attempted and
	// returned an error.
	Failed int `json:"failed"`
}

// SweepResult is the tally SweepTags returns: one count per kind of object
// it re-derives the system tags of. A dry run produces the same counts but
// writes nothing.
type SweepResult struct {
	Sessions        SweepCount `json:"sessions"`
	Automata        SweepCount `json:"automata"`
	Automations     SweepCount `json:"automations"`
	Tasks           SweepCount `json:"tasks"`
	Templates       SweepCount `json:"templates"`
	DecisionRecords SweepCount `json:"decision_records"`
	Questions       SweepCount `json:"questions"`
}

// SetTagSweepSessions wires the session store the tag sweep's session pass
// lists and writes through. nil clears the session pass (the object passes
// still run). Guarded by mu.
func (m *Manager) SetTagSweepSessions(s TagSweepSessionStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagSweepSessions = s
}

// SweepTags re-derives the system tags of every existing Automata object and
// session under the tags.placement settings in effect now, replacing each
// object's derived tag list with the newly derived one, and reports a
// per-kind tally of what it examined, changed, left unchanged and failed on.
//
// It is the "apply the current settings to what already exists without a
// fresh edit" path a settings edit and a fact change (an Automation moving,
// an axis switched off) need: turning an axis off removes the axis's tag
// from everything the sweep re-derives, turning it on adds the tag where the
// fact exists. A session gets the full placement derivation (its run axes
// included); an object (Automaton, Automation, task, template, decision
// record, held question) gets the object-only derivation, so neither leaks a
// run-axis tag onto the other.
//
// The sweep only ever writes the derived list (SystemTags). The operator's
// own tags (a session's Tags, or any object's operator-tag field) live in a
// separate field the sweep never reads or writes, so they always survive a
// run. Each object's re-derivation compares the derived set against what it
// already holds (tagsEqual) and only writes when they differ, so a second
// run is a no-op: it changes zero objects and writes nothing.
//
// With dryRun true the sweep performs the full re-derivation and reports the
// would-be counts but writes nothing at all. When a store is not wired (or a
// Manager is partially constructed) that kind's pass simply examines nothing
// and contributes zero, never an error.
func (m *Manager) SweepTags(dryRun bool) SweepResult {
	res := SweepResult{}

	// The one placement block every derivation in this sweep reads, resolved
	// once so a settings edit mid-sweep cannot split the run across two
	// configs.
	pc := m.objectTagsConfig()

	// Session pass (nil store = no sessions examined; object passes still run).
	var sessions TagSweepSessionStore
	m.mu.Lock()
	sessions = m.tagSweepSessions
	m.mu.Unlock()
	if sessions != nil {
		res.Sessions = m.sweepSessions(sessions, pc, dryRun)
	}

	res.Automata = m.sweepAutomata(dryRun)
	res.Tasks = m.sweepTasks(dryRun)
	res.Automations = m.sweepAutomations(dryRun)
	res.Templates = m.sweepTemplates(dryRun)

	// Decision records and held questions live behind the DecisionRecords
	// service; a Manager that never wired it runs no decision-record pass.
	if dr := m.DecisionRecords(); dr != nil {
		res.DecisionRecords = m.sweepDecisionRecords(dr, dryRun)
		res.Questions = m.sweepHeldQuestions(dr, dryRun)
	}

	return res
}

// sweepSessions re-derives the placement tags of every Automata-owned
// session the store lists and re-writes the ones that changed. Session
// placement tags include the run axes an object does not, so they derive
// under the full block.
func (m *Manager) sweepSessions(st TagSweepSessionStore, pc config.PlacementConfig, dryRun bool) SweepCount {
	var c SweepCount
	for _, s := range st.ListTagSweepSessions() {
		c.Examined++
		derived := DerivePlacementTags(s.Placement, pc)
		if tagsEqual(s.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		if err := st.SetSystemTags(s.ID, derived); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}

// sweepAutomata re-derives the object tags of every stored Automaton and
// re-saves the ones whose derived set differs from what they hold.
func (m *Manager) sweepAutomata(dryRun bool) SweepCount {
	var c SweepCount
	if m.store == nil {
		return c
	}
	for _, prd := range m.store.ListPRDs() {
		c.Examined++
		derived := m.ObjectTagsForPRD(prd)
		if tagsEqual(prd.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		next := *prd
		next.SystemTags = derived
		if err := m.store.SavePRD(&next); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}

func (m *Manager) sweepTasks(dryRun bool) SweepCount {
	var c SweepCount
	if m.store == nil {
		return c
	}
	for _, t := range m.store.ListTasks() {
		c.Examined++
		derived := m.ObjectTagsForTask(t)
		if tagsEqual(t.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		next := *t
		next.SystemTags = derived
		if err := m.store.SaveTask(&next); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}

func (m *Manager) sweepAutomations(dryRun bool) SweepCount {
	var c SweepCount
	if m.automations == nil {
		return c
	}
	for _, a := range m.automations.List() {
		c.Examined++
		derived := m.ObjectTagsForAutomation(a)
		if tagsEqual(a.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		// An empty mutate re-derives (and persists) the Automation's tags
		// under the wired deriver — the same refresh the operator-edit path
		// already takes on every automation save.
		if _, err := m.automations.Update(a.ID, 0, nil); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}

func (m *Manager) sweepTemplates(dryRun bool) SweepCount {
	var c SweepCount
	if m.templates == nil {
		return c
	}
	for _, t := range m.templates.List() {
		c.Examined++
		// Built-in templates have no source Automaton to derive from and
		// cannot be edited, so they carry no object tags: a built-in already
		// at an empty set is unchanged; leave it either way.
		if t.IsBuiltin {
			c.Unchanged++
			continue
		}
		derived := m.ObjectTagsForTemplate(t)
		if tagsEqual(t.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		// Empty optional fields and nil tags/vars mean "unchanged": Update
		// leaves Title/Spec/Type/Tags alone and only re-derives SystemTags.
		if _, err := m.templates.Update(t.ID, "", "", "", "", nil, nil); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}

func (m *Manager) sweepDecisionRecords(dr *DecisionRecords, dryRun bool) SweepCount {
	var c SweepCount
	if dr == nil {
		return c
	}
	rs := dr.RecordStore()
	if rs == nil {
		return c
	}
	for _, rec := range rs.List(DecisionRecordFilter{Limit: maxDecisionListLimit}) {
		c.Examined++
		derived := m.ObjectTagsForDecisionRecord(&rec)
		if tagsEqual(rec.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		// Re-saving the record it already holds re-derives its tags under
		// the wired deriver and persists; content is otherwise unchanged.
		if _, err := rs.Update(rec.ID, rec); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}

// sweepHeldQuestions re-derives the tags of the questions still held (open):
// those are the ones a settings edit needs to pick up now.
func (m *Manager) sweepHeldQuestions(dr *DecisionRecords, dryRun bool) SweepCount {
	var c SweepCount
	if dr == nil {
		return c
	}
	qs := dr.QuestionStore()
	if qs == nil {
		return c
	}
	for _, q := range qs.List(QuestionFilter{State: QuestionOpen, Limit: maxQuestionListLimit}) {
		c.Examined++
		derived := m.ObjectTagsForQuestion(&q)
		if tagsEqual(q.SystemTags, derived) {
			c.Unchanged++
			continue
		}
		if dryRun {
			c.Changed++
			continue
		}
		// An empty patch changes nothing but the re-derived system tags; the
		// store writes the refreshed list and nothing else.
		if _, err := qs.Update(q.ID, QuestionPatch{}, "tag-sweep"); err != nil {
			c.Failed++
			continue
		}
		c.Changed++
	}
	return c
}
