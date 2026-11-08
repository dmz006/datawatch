package autonomous

import (
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/placementtags"
)

// ObjectTagFacts is the tag-derivation fact an Automata object (Automaton,
// task or Automation) carries: the project it belongs to, the Automation
// (arc) it lives in, the Automaton itself, the stage its member slot
// carries and the kind of work it is. These are the same five axes the
// operator enables and templates under tags.placement; a fact left empty
// simply takes no tag on its axis.
type ObjectTagFacts struct {
	Project    string
	Automation string
	Automaton  string
	Stage      string
	Kind       string
}

// DeriveObjectTags returns, sorted and de-duplicated, the system tags the
// object facts f take under the tags.placement config block pc, reusing the
// same placementtags derivation the session placement tags use — enabled
// axes, operator templates and custom axes all apply. Only the object axes
// can appear (project, automation, automaton, stage and kind): compute,
// peer, backend, lane and role are facts of a session run an object does
// not have, so they are disabled for the derivation and can never yield a
// tag on an object, however the block is configured. EffectivePlacement is
// applied, so an unset axis still gets its default template. With the
// master switch off, the result is empty.
func DeriveObjectTags(f ObjectTagFacts, pc config.PlacementConfig) []string {
	if !pc.Enabled {
		return nil
	}
	s := placementTagsSettings(pc.EffectivePlacement())
	// An object never runs: strip the run axes so neither their enabled
	// settings nor any custom axis template that happens to match an object
	// fact can leak a run-axis tag onto it.
	s.Compute = placementtags.AxisSetting{}
	s.Peer = placementtags.AxisSetting{}
	s.Backend = placementtags.AxisSetting{}
	s.Lane = placementtags.AxisSetting{}
	s.Role = placementtags.AxisSetting{}
	return placementtags.Derive(placementtags.PlacementFacts{
		Project:    f.Project,
		Automation: f.Automation,
		Automaton:  f.Automaton,
		Stage:      f.Stage,
		Kind:       f.Kind,
		Fields: map[string]string{
			"Project":    f.Project,
			"Automation": f.Automation,
			"Automaton":  f.Automaton,
			"PRDID":      f.Automaton,
			"Stage":      f.Stage,
			"Kind":       f.Kind,
		},
	}, s)
}

// objectProjectName is the display name of a project a record carries: the
// base name of its project directory, or "" when it carries no directory.
// An Automaton's project/example dir is the project "example"; an
// object with no project directory names no project and takes no project
// tag (shown as "(none)" in the counts, per the tags-and-counts decision).
func objectProjectName(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// objectTagsConfig returns the tags.placement config block the object
// system tags are derived under: the same live source the session
// placement reads (so an operator edit takes effect on the next derivation
// without a restart), or the daemon defaults when the source is not wired
// (tests, daemon unset).
func (m *Manager) objectTagsConfig() config.PlacementConfig {
	m.mu.Lock()
	fn := m.placementTagsSettings
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return config.DefaultPlacementConfig()
}

// ObjectTagFactsForPRD is the object fact an Automaton (stored as a PRD)
// carries for tag derivation: the project of its project directory, the
// Automation it is a member of (empty = standalone, no automation tag)
// and itself. It carries no stage and no kind: the stage the operator
// gave its member slot is derived by the task reader, and the kind is the
// work a session run does, which an Automaton itself does not carry.
func (m *Manager) ObjectTagFactsForPRD(prd *PRD) ObjectTagFacts {
	if prd == nil {
		return ObjectTagFacts{}
	}
	return ObjectTagFacts{
		Project:    objectProjectName(prd.ProjectDir),
		Automation: prd.AutomationID,
		Automaton:  prd.ID,
	}
}

// ObjectTagsForPRD derives the system tags the Automaton itself takes:
// its project, its Automation and itself, under the daemon's current
// tags.placement block. It derives only; it does not write the field.
func (m *Manager) ObjectTagsForPRD(prd *PRD) []string {
	return DeriveObjectTags(m.ObjectTagFactsForPRD(prd), m.objectTagsConfig())
}

// getPRD is a fact read against the Manager's PRD store that tolerates a
// Manager that carries no store (partially-constructed managers in tests
// and the zero value): it names the Automaton as missing instead of
// panicking, which is the same answer as an Automaton that is gone.
func (m *Manager) getPRD(id string) (*PRD, bool) {
	if m == nil || m.store == nil {
		return nil, false
	}
	return m.store.GetPRD(id)
}

// ObjectTagFactsForTask is the object fact a task carries: the same fact
// its Automaton carries (project, Automation, Automaton) plus the stage
// its member slot carries, when the Automaton is stage-typed. A task
// outside an Automation carries no stage, and an Automaton that carries
// no project directory names no project on either.
func (m *Manager) ObjectTagFactsForTask(task *Task) ObjectTagFacts {
	if task == nil {
		return ObjectTagFacts{}
	}
	if m == nil || m.store == nil {
		return ObjectTagFacts{}
	}
	prd, _ := m.store.GetPRD(task.PRDID)
	facts := m.ObjectTagFactsForPRD(prd)
	facts.Stage = m.StageOfPRD(prd)
	return facts
}

// ObjectTagsForTask derives the system tags a task record takes: its
// Automaton's fact (project, Automation, Automaton) plus the stage the
// Automaton's member slot carries, under the daemon's current
// tags.placement block. It derives only; it does not write the field.
func (m *Manager) ObjectTagsForTask(task *Task) []string {
	return DeriveObjectTags(m.ObjectTagFactsForTask(task), m.objectTagsConfig())
}

// ObjectTagFactsForAutomation is the object fact an Automation carries:
// itself, as the automation axis, and the project of its first member
// (the first member whose Automaton it can resolve names the group's
// project). An Automation that cannot name a project takes no project
// tag; it has no Automaton or stage axis of its own.
func (m *Manager) ObjectTagFactsForAutomation(auto *Automation) ObjectTagFacts {
	if auto == nil {
		return ObjectTagFacts{}
	}
	facts := ObjectTagFacts{Automation: auto.ID}
	for i := range auto.Members {
		member := auto.Members[i]
		if member.PRDID == "" {
			continue
		}
		if prd, ok := m.getPRD(member.PRDID); ok {
			facts.Project = objectProjectName(prd.ProjectDir)
			break
		}
	}
	return facts
}

// ObjectTagsForAutomation derives the system tags an Automations record
// takes: itself on the automation axis and the project of its first
// member, under the daemon's current tags.placement block. It derives
// only; it does not write the field.
func (m *Manager) ObjectTagsForAutomation(auto *Automation) []string {
	return DeriveObjectTags(m.ObjectTagFactsForAutomation(auto), m.objectTagsConfig())
}

// ObjectTagFactsForDecisionRecord is the object fact a decision record
// carries: the fact of the Automaton its PRDID names, taken from that
// Automaton (and its Automation and project) — the record itself carries
// stage and kind of its own. A record that names no Automaton names no
// axes, and one whose Automaton is gone names no axes on the axes the
// record can reach; naming a missing Automaton is not an error.
func (m *Manager) ObjectTagFactsForDecisionRecord(rec *DecisionRecord) ObjectTagFacts {
	if rec == nil || strings.TrimSpace(rec.PRDID) == "" {
		return ObjectTagFacts{}
	}
	prd, _ := m.getPRD(rec.PRDID)
	if m == nil || prd == nil {
		return ObjectTagFacts{}
	}
	return m.ObjectTagFactsForPRD(prd)
}

// ObjectTagsForDecisionRecord derives the system tags a decision record
// takes: the fact of the Automaton it names, under the daemon's current
// tags.placement block. It derives only; it does not write the field.
func (m *Manager) ObjectTagsForDecisionRecord(rec *DecisionRecord) []string {
	return DeriveObjectTags(m.ObjectTagFactsForDecisionRecord(rec), m.objectTagsConfig())
}

// ObjectTagFactsForQuestion is the object fact a held question carries:
// the fact of the Automaton its PRDID names, taken from that Automaton
// (and its Automation and project). A question that names no Automaton
// (or whose Automaton is gone) names no axes, which is not an error.
func (m *Manager) ObjectTagFactsForQuestion(q *Question) ObjectTagFacts {
	if q == nil || strings.TrimSpace(q.PRDID) == "" {
		return ObjectTagFacts{}
	}
	prd, _ := m.getPRD(q.PRDID)
	if m == nil || prd == nil {
		return ObjectTagFacts{}
	}
	return m.ObjectTagFactsForPRD(prd)
}

// ObjectTagsForQuestion derives the system tags a held question takes:
// the fact of the Automaton it names, under the daemon's current
// tags.placement block. It derives only; it does not write the field.
func (m *Manager) ObjectTagsForQuestion(q *Question) []string {
	return DeriveObjectTags(m.ObjectTagFactsForQuestion(q), m.objectTagsConfig())
}

// ObjectTagFactsForTemplate is the object fact a template carries: the
// fact of the Automaton it was cloned from, when it records one (its
// SourcePRDID), taken from that Automaton (and its Automation and
// project). A template with no recorded source — a hand-written or
// built-in one — names no axes, and one whose source Automaton is gone
// names none on the axes it can reach; that is not an error.
func (m *Manager) ObjectTagFactsForTemplate(t *Template) ObjectTagFacts {
	if t == nil || strings.TrimSpace(t.SourcePRDID) == "" {
		return ObjectTagFacts{}
	}
	prd, _ := m.getPRD(t.SourcePRDID)
	if m == nil || prd == nil {
		return ObjectTagFacts{}
	}
	return m.ObjectTagFactsForPRD(prd)
}

// ObjectTagsForTemplate derives the system tags a template takes: the
// fact of the Automaton it was cloned from, when it records one, under
// the daemon's current tags.placement block. It derives only; it does
// not write the field.
func (m *Manager) ObjectTagsForTemplate(t *Template) []string {
	return DeriveObjectTags(m.ObjectTagFactsForTemplate(t), m.objectTagsConfig())
}

// ObjectTagDeriver derives the full set of object-derived SystemTags an
// object takes: one method per persisted object type. A Manager always
// satisfies it (each method reads the live tags.placement source), and a
// store that has one wired replaces the record's SystemTags with the
// derived list before it writes the record — on every save of the object,
// so a config edit or a fact change (an Automation moving, an axis
// switched off) is picked up on the next save with no backfill pass. A
// record whose store carries no deriver (standalone stores, older callers
// and tests that want raw writes) keeps whatever SystemTags it holds.
type ObjectTagDeriver interface {
	ObjectTagsForPRD(prd *PRD) []string
	ObjectTagsForTask(task *Task) []string
	ObjectTagsForAutomation(a *Automation) []string
	ObjectTagsForTemplate(t *Template) []string
	ObjectTagsForDecisionRecord(rec *DecisionRecord) []string
	ObjectTagsForQuestion(q *Question) []string
}

var _ ObjectTagDeriver = (*Manager)(nil)

// replaceSystemTags picks the list a save stores as the object's
// SystemTags: the derived list, unless it equals the list already held —
// then the held one, so an unchanged re-save writes the same JSON and
// does not churn the field. An empty derivation clears the list (nil,
// matching the field's omitempty) once the axes no longer apply.
func replaceSystemTags(current, derived []string) []string {
	if tagsEqual(current, derived) {
		return current
	}
	if len(derived) == 0 {
		return nil
	}
	return derived
}
