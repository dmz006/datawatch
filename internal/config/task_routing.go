// Task routing config: the autonomous.task_routing block.
//
// The block maps a task type to the worker and verification LLM that a
// newly created task starts with. Rows live in named profiles; one
// profile is active. A per-task, per-story or per-Automaton LLM choice
// still wins over a row (task over story over Automaton over the
// routing table over global). The block is OFF by default.
package config

import (
	"fmt"
	"sort"
	"strings"
)

// Task types: stable snake_case ids used as row keys.
const (
	TaskTypeDocsSet             = "docs_set"
	TaskTypeChangelogEntry      = "changelog_entry"
	TaskTypeParityIssueDraft    = "parity_issue_draft"
	TaskTypeBoundedLoadStep     = "bounded_load_step"
	TaskTypeVersionNote         = "version_note"
	TaskTypeLocaleStrings       = "locale_strings"
	TaskTypeTests               = "tests"
	TaskTypeFormattingCleanup   = "formatting_cleanup"
	TaskTypeUICode              = "ui_code"
	TaskTypeDaemonBackendCode   = "daemon_backend_code"
	TaskTypeMCPAPISurface       = "mcp_api_surface"
	TaskTypeMergeConflictRepair = "merge_conflict_repair"
	TaskTypeFinalGate           = "final_gate"
	TaskTypeReviewRepair        = "review_repair"
)

// TaskRoutingDefaultProfile is the name of the shipped profile.
const TaskRoutingDefaultProfile = "default"

// Backends and models the shipped table uses.
const (
	taskRoutingMechanicalBackend = "pi-prompt"
	taskRoutingMechanicalModel   = "ollama/qwen3.8:27b"
	taskRoutingCodeBackend       = "claude-code"
	taskRoutingCodeModel         = "sonnet"
)

// TaskRoutingTaskTypes lists every known task type in a fixed order.
func TaskRoutingTaskTypes() []string {
	return []string{
		TaskTypeDocsSet, TaskTypeChangelogEntry, TaskTypeParityIssueDraft,
		TaskTypeBoundedLoadStep, TaskTypeVersionNote, TaskTypeLocaleStrings,
		TaskTypeTests, TaskTypeFormattingCleanup, TaskTypeUICode,
		TaskTypeDaemonBackendCode, TaskTypeMCPAPISurface,
		TaskTypeMergeConflictRepair, TaskTypeFinalGate, TaskTypeReviewRepair,
	}
}

// IsTaskRoutingTaskType reports whether t is a known task type.
func IsTaskRoutingTaskType(t string) bool {
	for _, k := range TaskRoutingTaskTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// TaskRoutingChoice is a backend, model and effort.
type TaskRoutingChoice struct {
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
	Effort  string `yaml:"effort,omitempty" json:"effort,omitempty"`
}

// TaskRoutingRow is the routing for one task type.
type TaskRoutingRow struct {
	WorkerBackend string `yaml:"worker_backend,omitempty" json:"worker_backend,omitempty"`
	WorkerModel   string `yaml:"worker_model,omitempty" json:"worker_model,omitempty"`
	// VerificationBackend and VerificationModel empty means follow the
	// global verifier policy.
	VerificationBackend string `yaml:"verification_backend,omitempty" json:"verification_backend,omitempty"`
	VerificationModel   string `yaml:"verification_model,omitempty" json:"verification_model,omitempty"`
	Effort              string `yaml:"effort,omitempty" json:"effort,omitempty"`
	// SecondChoice is the alternative used when the first choice is
	// unavailable.
	SecondChoice *TaskRoutingChoice `yaml:"second_choice,omitempty" json:"second_choice,omitempty"`
	// AllowPremiumModel must be true for a row to name an Opus or Fable
	// model. Without it such a name is refused.
	AllowPremiumModel bool `yaml:"allow_premium_model,omitempty" json:"allow_premium_model,omitempty"`
}

// TaskRoutingProfile maps a task type to its row.
type TaskRoutingProfile map[string]TaskRoutingRow

// TaskRoutingConfig is the autonomous.task_routing block.
type TaskRoutingConfig struct {
	Enabled       bool                          `yaml:"enabled" json:"enabled"`
	ActiveProfile string                        `yaml:"active_profile,omitempty" json:"active_profile,omitempty"`
	Profiles      map[string]TaskRoutingProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// DefaultTaskRouting is the block's factory: one profile named
// "default", routing off. No shipped row names an Opus or Fable model
// and every verification field is empty.
func DefaultTaskRouting() TaskRoutingConfig {
	mech := TaskRoutingRow{WorkerBackend: taskRoutingMechanicalBackend, WorkerModel: taskRoutingMechanicalModel}
	code := TaskRoutingRow{WorkerBackend: taskRoutingCodeBackend, WorkerModel: taskRoutingCodeModel}
	p := TaskRoutingProfile{
		TaskTypeDocsSet:             mech,
		TaskTypeChangelogEntry:      mech,
		TaskTypeParityIssueDraft:    mech,
		TaskTypeLocaleStrings:       mech,
		TaskTypeFormattingCleanup:   mech,
		TaskTypeTests:               mech,
		TaskTypeVersionNote:         mech,
		TaskTypeBoundedLoadStep:     mech,
		TaskTypeUICode:              code,
		TaskTypeDaemonBackendCode:   code,
		TaskTypeMCPAPISurface:       code,
		TaskTypeMergeConflictRepair: code,
		TaskTypeReviewRepair:        code,
		TaskTypeFinalGate:           TaskRoutingRow{},
	}
	return TaskRoutingConfig{
		ActiveProfile: TaskRoutingDefaultProfile,
		Profiles:      map[string]TaskRoutingProfile{TaskRoutingDefaultProfile: p},
	}
}

// Clone returns a deep copy.
func (c TaskRoutingConfig) Clone() TaskRoutingConfig {
	out := c
	if c.Profiles != nil {
		out.Profiles = make(map[string]TaskRoutingProfile, len(c.Profiles))
		for n, p := range c.Profiles {
			out.Profiles[n] = p.Clone()
		}
	}
	return out
}

// Clone returns a deep copy of the profile.
func (p TaskRoutingProfile) Clone() TaskRoutingProfile {
	if p == nil {
		return nil
	}
	out := make(TaskRoutingProfile, len(p))
	for k, r := range p {
		if r.SecondChoice != nil {
			sc := *r.SecondChoice
			r.SecondChoice = &sc
		}
		out[k] = r
	}
	return out
}

// TaskRoutingWithDefaults returns DefaultTaskRouting when the block has
// no profiles (an existing config without the block); otherwise it is
// returned unchanged.
func TaskRoutingWithDefaults(c TaskRoutingConfig) TaskRoutingConfig {
	if len(c.Profiles) > 0 {
		return c
	}
	d := DefaultTaskRouting()
	d.Enabled = c.Enabled
	return d
}

// TaskRoutingChecks supplies the lookups validation needs, so config
// does not import the LLM registry or the autonomous package. A nil
// function skips that check.
type TaskRoutingChecks struct {
	// BackendKnown reports whether the LLM registry knows the backend.
	BackendKnown func(backend string) bool
	// ModelPairs returns an error when the model does not pair with the
	// backend.
	ModelPairs func(backend, model string) error
}

func isPremiumModelName(m string) bool {
	l := strings.ToLower(m)
	return strings.Contains(l, "opus") || strings.Contains(l, "fable")
}

// ValidateTaskRouting returns every problem found in the block.
func ValidateTaskRouting(cfg TaskRoutingConfig, chk TaskRoutingChecks) []error {
	var errs []error
	add := func(f string, a ...any) { errs = append(errs, fmt.Errorf("task_routing: "+f, a...)) }
	if cfg.ActiveProfile != "" {
		if _, ok := cfg.Profiles[cfg.ActiveProfile]; !ok {
			add("active profile %q does not exist", cfg.ActiveProfile)
		}
	} else if cfg.Enabled {
		add("routing is enabled but no active profile is set")
	}
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, pn := range names {
		if strings.TrimSpace(pn) == "" {
			add("a profile has an empty name")
		}
		prof := cfg.Profiles[pn]
		types := make([]string, 0, len(prof))
		for t := range prof {
			types = append(types, t)
		}
		sort.Strings(types)
		for _, t := range types {
			row := prof[t]
			where := fmt.Sprintf("profile %q row %q", pn, t)
			if !IsTaskRoutingTaskType(t) {
				add("%s: unknown task type", where)
			}
			check := func(field, backend, model string) {
				if backend != "" && chk.BackendKnown != nil && !chk.BackendKnown(backend) {
					add("%s: %s names LLM backend %q, which is not known", where, field, backend)
				}
				if model != "" && backend == "" {
					// A model with no backend cannot be paired.
					if field == "worker" || field == "second choice" {
						add("%s: %s names model %q with no backend", where, field, model)
					}
				}
				if backend != "" && model != "" && chk.ModelPairs != nil {
					if err := chk.ModelPairs(backend, model); err != nil {
						add("%s: %s: %v", where, field, err)
					}
				}
				if model != "" && isPremiumModelName(model) && !row.AllowPremiumModel {
					add("%s: %s names model %q, which needs allow_premium_model set on this row", where, field, model)
				}
			}
			check("worker", row.WorkerBackend, row.WorkerModel)
			check("verification", row.VerificationBackend, row.VerificationModel)
			if sc := row.SecondChoice; sc != nil {
				check("second choice", sc.Backend, sc.Model)
			}
		}
	}
	return errs
}
