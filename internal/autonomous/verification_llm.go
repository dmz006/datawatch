package autonomous

// VerificationLLM is the resolved verifier LLM for one task: which
// backend/model/effort the verifier should run with and which level the
// backend came from.
type VerificationLLM struct {
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	// Source is the level the BACKEND was resolved from: one of the
	// VerifierSource* constants. "global" when no level sets a backend.
	Source string `json:"source"`
}

// VerifierSource values, recorded on VerificationLLM.Source and
// VerificationResult.VerifierSource.
const (
	VerifierSourceTask  = "task"
	VerifierSourceStory = "story"
	VerifierSourcePRD   = "prd"
	// VerifierSourceAutomation marks a backend taken from the Automation
	// (the chain object) the Automaton is a member of.
	VerifierSourceAutomation = "automation"
	VerifierSourceGlobal     = "global"
	// VerifierSourcePool marks a backend picked from the global
	// verification_backends capacity pool rather than by the
	// task → story → Automaton → global chain.
	VerifierSourcePool = "pool"
)

// ResolveVerificationLLM resolves the verifier LLM for task within prd.
// Precedence, most specific wins: task → story (the story containing
// the task) → Automaton → Automation (auto, the chain the Automaton is
// a member of; nil for a standalone Automaton) → global
// cfg.VerificationBackend / VerificationModel / VerificationEffort.
//
// Backend and model follow resolveTaskBackendModel's v8.37.2 rule
// exactly (executor.go): the model is taken from the SAME level the
// backend was found at, and a less specific level's model never
// cascades past a more specific level that overrides the backend.
// Matching resolveTaskBackendModel, a more specific level that sets
// only a model (no backend) still applies that model to the backend
// inherited from a less specific level — e.g. task model "sonnet" with
// story backend "claude-code" resolves to claude-code/sonnet, while the
// Automaton's model is ignored once the story supplies the backend.
// An Automation that sets only a model (no backend) therefore never
// pairs with a more specific level's backend. When no level sets a
// backend, the global backend is used with the first model set at
// task/story/Automaton/Automation level, else the global
// VerificationModel.
//
// Effort resolves independently: first non-empty of task → story →
// Automaton → Automation → global.
//
// This is a separate chain from guardrail resolution
// (resolveGuardrailBackendModel); it never reads the Guardrail* fields,
// nor the Automation's PlanningBackend / PlanningModel.
// A nil task resolves as if no task-level override were set; a task not
// found in any story has no story level.
func ResolveVerificationLLM(prd *PRD, task *Task, auto *Automation, cfg Config) VerificationLLM {
	return resolveVerificationLLM(prd, task, auto, cfg)
}

func resolveVerificationLLM(prd *PRD, task *Task, auto *Automation, cfg Config) VerificationLLM {
	if auto == nil {
		auto = &Automation{}
	}
	if prd == nil {
		prd = &PRD{}
	}
	t := task
	if t == nil {
		t = &Task{}
	}
	var story *Story
	if task != nil && task.ID != "" {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if prd.Story[si].Tasks[ti].ID == task.ID {
					story = &prd.Story[si]
					break
				}
			}
			if story != nil {
				break
			}
		}
	}

	out := VerificationLLM{Backend: t.VerificationBackend, Model: t.VerificationModel}
	switch {
	case out.Backend != "":
		// task-level backend: model is the task's own (possibly
		// empty = that backend's default).
		out.Source = VerifierSourceTask
	case story != nil && story.VerificationBackend != "":
		out.Backend = story.VerificationBackend
		out.Source = VerifierSourceStory
		if out.Model == "" {
			out.Model = story.VerificationModel
		}
	case prd.VerificationBackend != "":
		out.Backend = prd.VerificationBackend
		out.Source = VerifierSourcePRD
		if out.Model == "" {
			out.Model = prd.VerificationModel
		}
	case auto.VerificationBackend != "":
		out.Backend = auto.VerificationBackend
		out.Source = VerifierSourceAutomation
		if out.Model == "" {
			out.Model = auto.VerificationModel
		}
	default:
		out.Backend = cfg.VerificationBackend
		out.Source = VerifierSourceGlobal
		if out.Model == "" && story != nil {
			out.Model = story.VerificationModel
		}
		if out.Model == "" {
			out.Model = prd.VerificationModel
		}
		if out.Model == "" {
			out.Model = auto.VerificationModel
		}
		if out.Model == "" {
			out.Model = cfg.VerificationModel
		}
	}

	switch {
	case t.VerificationEffort != "":
		out.Effort = string(t.VerificationEffort)
	case story != nil && story.VerificationEffort != "":
		out.Effort = string(story.VerificationEffort)
	case prd.VerificationEffort != "":
		out.Effort = string(prd.VerificationEffort)
	case auto.VerificationEffort != "":
		out.Effort = string(auto.VerificationEffort)
	default:
		out.Effort = cfg.VerificationEffort
	}
	return out
}
