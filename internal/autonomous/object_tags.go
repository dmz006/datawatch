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
// An Automaton's /home/x/datawatch dir is the project "datawatch"; an
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

// ObjectTagFactsForTask is the object fact a task carries: the same fact
// its Automaton carries (project, Automation, Automaton) plus the stage
// its member slot carries, when the Automaton is stage-typed. A task
// outside an Automation carries no stage, and an Automaton that carries
// no project directory names no project on either.
func (m *Manager) ObjectTagFactsForTask(task *Task) ObjectTagFacts {
	if task == nil {
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
		if prd, ok := m.store.GetPRD(member.PRDID); ok {
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
