// Package config — project record types.
//
// project_record.go holds the optional, additive fields a registered project
// (ProjectConfigEntry) can carry beyond its directory alias: ownership, the
// repository it tracks, its arcs (ordered Automaton slots), a target, lane
// defaults, a weekly share, branch retention and a major-release trigger.
// Every field is optional; an entry that sets none of them is unchanged on
// load and save.
package config

import (
	"fmt"
	"sort"
)

// Allowed ProjectArc.State values.
const (
	ArcStatePlanned = "planned"
	ArcStateActive  = "active"
	ArcStateDone    = "done"
)

// ProjectArc is one planned slot of a project: an Automaton it runs, its
// position in the project's ordering, and its lifecycle state.
type ProjectArc struct {
	AutomationID string `yaml:"automation_id,omitempty" json:"automation_id,omitempty"`
	Priority     int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	// State is one of planned, active or done. Empty means planned.
	State string `yaml:"state,omitempty" json:"state,omitempty"`
}

// ProjectTarget is the outcome a project is working toward.
type ProjectTarget struct {
	Name             string `yaml:"name,omitempty" json:"name,omitempty"`
	DefinitionOfDone string `yaml:"definition_of_done,omitempty" json:"definition_of_done,omitempty"`
}

// ProjectLaneDefaults are the lane settings new work in a project starts with.
type ProjectLaneDefaults struct {
	Lane    string `yaml:"lane,omitempty" json:"lane,omitempty"`
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
}

// validArcState reports whether s is an allowed arc state. The empty string
// is allowed and means planned.
func validArcState(s string) bool {
	switch s {
	case "", ArcStatePlanned, ArcStateActive, ArcStateDone:
		return true
	}
	return false
}

// projectMajorTriggerValid reports whether s is an allowed major trigger
// (the MajorTrigger* constants in merge_pipeline.go). The empty string is
// allowed and means the daemon default.
func projectMajorTriggerValid(s string) bool {
	switch s {
	case "", MajorTriggerManual, MajorTriggerMilestone, MajorTriggerNever:
		return true
	}
	return false
}

// Validate checks the project record fields of the entry. Entries with none
// of the record fields set always validate.
//
// It rejects: an arc with an empty or duplicate automation id, an arc state
// that is not planned|active|done, a weekly share outside 0-100, a negative
// branch retention, and a major trigger that is not manual|milestone|never.
func (e ProjectConfigEntry) Validate() error {
	seen := make(map[string]struct{}, len(e.Arcs))
	for i, a := range e.Arcs {
		if a.AutomationID == "" {
			return fmt.Errorf("arcs[%d]: automation_id is required", i)
		}
		if _, dup := seen[a.AutomationID]; dup {
			return fmt.Errorf("arcs[%d]: duplicate automation_id %q", i, a.AutomationID)
		}
		seen[a.AutomationID] = struct{}{}
		if !validArcState(a.State) {
			return fmt.Errorf("arcs[%d]: unknown state %q (want planned, active or done)", i, a.State)
		}
	}
	if e.WeeklyShare != nil && (*e.WeeklyShare < 0 || *e.WeeklyShare > 100) {
		return fmt.Errorf("weekly_share %d is outside 0-100", *e.WeeklyShare)
	}
	if e.BranchRetentionDays != nil && *e.BranchRetentionDays < 0 {
		return fmt.Errorf("branch_retention_days %d is negative", *e.BranchRetentionDays)
	}
	if !projectMajorTriggerValid(e.MajorTrigger) {
		return fmt.Errorf("unknown major_trigger %q (want manual, milestone or never)", e.MajorTrigger)
	}
	return nil
}

// OrderedArcs returns a copy of the entry's arcs sorted by Priority, ascending.
// Arcs with equal priority keep their declared order.
func (e ProjectConfigEntry) OrderedArcs() []ProjectArc {
	out := make([]ProjectArc, len(e.Arcs))
	copy(out, e.Arcs)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}
