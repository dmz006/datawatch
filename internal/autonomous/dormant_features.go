// Dormant features registry: a read-only list of the shipped Automata
// settings whose current value is off or in shadow mode, with the default,
// a one-sentence purpose and whether the feature has ever been exercised.
//
// The registry never writes or changes a setting and makes no model call.
// Settings that decide which LLM, route, judge, verifier, repository or
// credential is used are out of scope and are deliberately not listed.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// Dormant states.
const (
	DormantStateOff    = "off"
	DormantStateShadow = "shadow"
)

// Registry keys.
const (
	DormantKeyScanFixLoop     = "autonomous.scan.fix_loop_enabled"
	DormantKeyScanRulesGrader = "autonomous.scan.rules_grader_enabled"
	DormantKeyScopeBreadth    = "autonomous.scope_breadth_enabled"
	DormantKeyQualityGates    = "autonomous.default_quality_gates.enabled"
	DormantKeyRemoteReap      = "autonomous.remote_reap_enabled"
	DormantKeyNotifyForward   = "autonomous.notify.forwarding.enabled"
	DormantKeyMergeUntil      = "supervisor_loop.run_merge_until_conflict.enabled"
	DormantKeyMergePending    = "autonomous.automation.merge_pending_mode"
)

// noRecordFound is the Evidence when no record exists for a feature.
const noRecordFound = "no record found"

// DormantFeature is one registry row.
type DormantFeature struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	Default       string `json:"default"`
	Current       string `json:"current"`
	State         string `json:"state"` // off or shadow
	Purpose       string `json:"purpose"`
	EverExercised bool   `json:"ever_exercised"`
	LastExercised string `json:"last_exercised"`
	Evidence      string `json:"evidence"`
}

// DormantRecords is the read-only view of existing records the registry
// consults: decision log entries, audit events, loop job run records, reap
// records and the merge shadow comparison store. It is read once per List
// call. A nil value means no record exists for anything.
type DormantRecords interface {
	// Exercised reports whether a record exists for the feature key, the
	// time of the latest one and a short description of the record.
	Exercised(key string) (found bool, last, evidence string)
	// MergeShadowComparisons is the number of stored merge shadow
	// comparisons, the same data autonomous_merge_shadow_status reads.
	MergeShadowComparisons() int
}

type dormantSpec struct {
	key, name, purpose string
	// value returns the default and current value as words and whether the
	// current value is dormant, plus its state.
	read func(cfg *config.Config) (def, cur, state string, dormant bool)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func boolSpec(key, name, purpose string, def bool, get func(*config.Config) bool) dormantSpec {
	return dormantSpec{key: key, name: name, purpose: purpose,
		read: func(cfg *config.Config) (string, string, string, bool) {
			v := get(cfg)
			return onOff(def), onOff(v), DormantStateOff, !v
		}}
}

func dormantSpecs() []dormantSpec {
	return []dormantSpec{
		boolSpec(DormantKeyScanFixLoop, "Scan fix loop",
			"When on, the scan fix loop opens a fix task for an Automaton whose scan fails and holds it if the fix does not clear the findings.",
			false, func(c *config.Config) bool { return c.Autonomous.Scan.FixLoopEnabled }),
		boolSpec(DormantKeyScanRulesGrader, "Scan rules grader",
			"When on, the scan rules grader reviews a scan against the project rules and adds its findings to the Automaton scan result.",
			false, func(c *config.Config) bool { return c.Autonomous.Scan.RulesGraderEnabled }),
		boolSpec(DormantKeyScopeBreadth, "Scope breadth check",
			"When on, a failed content verification asks whether the task bundles several separable units and marks it too broad for the Automaton.",
			false, func(c *config.Config) bool { return c.Autonomous.ScopeBreadthEnabled }),
		boolSpec(DormantKeyQualityGates, "Default quality gates",
			"When on, every Automaton without its own quality gates runs the test baseline before a task and can block on a regression.",
			false, func(c *config.Config) bool { return c.Autonomous.DefaultQualityGates.Enabled }),
		boolSpec(DormantKeyRemoteReap, "Remote branch reap",
			"When on, the remote branch reap deletes fully merged Automaton work branches from origin after a verified backup; when off it only reports what it would delete.",
			false, func(c *config.Config) bool { return c.Autonomous.RemoteReapEnabled }),
		boolSpec(DormantKeyNotifyForward, "Notification forwarding",
			"When on, Automata lifecycle events are forwarded to and received from registered federation peers.",
			false, func(c *config.Config) bool { return c.Autonomous.Notify.Forwarding.Enabled }),
		{key: DormantKeyMergeUntil, name: "Supervisor loop merge step (run_merge_until_conflict)",
			purpose: "When on, the supervisor loop runs the merge pipeline for finished Automata until one conflicts and keeps the newest conflict for the operator to see.",
			read: func(cfg *config.Config) (string, string, string, bool) {
				def := supervisorloop.DefaultSettings()
				cur, _ := supervisorloop.ParseSettings(cfg.Autonomous.SupervisorLoop)
				d := def.Steps[stepRunMergeUntilConflict]
				v := cur.Steps[stepRunMergeUntilConflict]
				return onOff(d), onOff(v), DormantStateOff, !v
			}},
		{key: DormantKeyMergePending, name: "Merge pending check",
			purpose: "In shadow mode the merge-pending check only records what it would have done for each finished Automaton branch; off skips it and on raises the alert.",
			read: func(cfg *config.Config) (string, string, string, bool) {
				mode := cfg.Autonomous.Automation.EffectiveMergePendingMode()
				switch mode {
				case config.MergePendingModeOff:
					return config.MergePendingModeOff, mode, DormantStateOff, true
				case config.MergePendingModeShadow:
					return config.MergePendingModeOff, mode, DormantStateShadow, true
				}
				return config.MergePendingModeOff, mode, "", false
			}},
	}
}

// ListDormantFeatures returns every registered setting whose current value is
// off or in shadow mode. It reads cfg and rec only and changes neither.
func ListDormantFeatures(cfg *config.Config, rec DormantRecords) []DormantFeature {
	out := []DormantFeature{}
	if cfg == nil {
		return out
	}
	for _, sp := range dormantSpecs() {
		def, cur, state, dormant := sp.read(cfg)
		if !dormant {
			continue
		}
		f := DormantFeature{Key: sp.key, Name: sp.name, Default: def, Current: cur,
			State: state, Purpose: sp.purpose, Evidence: noRecordFound}
		if rec != nil {
			if found, last, ev := rec.Exercised(sp.key); found {
				f.EverExercised, f.LastExercised = true, last
				f.Evidence = ev
			}
		}
		if sp.key == DormantKeyMergeUntil || sp.key == DormantKeyMergePending {
			if rec == nil || rec.MergeShadowComparisons() == 0 {
				f.Evidence += "; no merge shadow comparison exists"
			}
		}
		out = append(out, f)
	}
	return out
}

// DormantFeatureList is the List(cfg) entry point of the registry.
func DormantFeatureList(cfg *config.Config, rec DormantRecords) []DormantFeature {
	return ListDormantFeatures(cfg, rec)
}
