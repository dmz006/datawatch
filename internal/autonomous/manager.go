// BL24 — central coordinator. Wires the store, decomposer, and the
// background loop. Mirrors nightwire/autonomous/manager.py shape.

package autonomous

import (
	"context"
	"fmt"
	"github.com/dmz006/datawatch/internal/capacity"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/pipeline"
)

// maxDecisionsPerPRD (BL291, v5.5.0) — cap on PRD.Decisions to prevent
// unbounded growth on long-lived PRDs. The autonomous loop appends a
// row on every decompose / approve / run / verify / edit / set_llm /
// set_task_llm action; without a cap a PRD that's been re-decomposed +
// re-run hundreds of times grows to multi-MB JSONL rows that bloat
// every Store.SavePRD write. Trim keeps the most recent N entries.
const maxDecisionsPerPRD = 200

// trimDecisions returns ds capped at maxDecisionsPerPRD entries (most
// recent kept). Caller passes the slice after appending; safe on nil
// or empty.
func trimDecisions(ds []Decision) []Decision {
	if len(ds) <= maxDecisionsPerPRD {
		return ds
	}
	return ds[len(ds)-maxDecisionsPerPRD:]
}

// Config is the operator-tunable knobs for the autonomous system.
// Mirrors session.* config fields per the no-hard-coded-config rule.
// JSON tags use snake_case so REST + YAML payloads can be applied
// directly via SetConfig.
type Config struct {
	Enabled             bool `json:"enabled"`
	PollIntervalSeconds int  `json:"poll_interval_seconds,omitempty"`
	// MaxParallelTasks is deprecated and has no effect: no executor code
	// reads it. Kept for one release so old configs still load; use
	// MaxConcurrentTasks.
	MaxParallelTasks int `json:"max_parallel_tasks,omitempty"`
	// BL304: renamed from DecompositionBackend; JSON key updated to planning_backend.
	// Old key "decomposition_backend" still accepted via SetConfig migration.
	PlanningBackend     string `json:"planning_backend,omitempty"`
	VerificationBackend string `json:"verification_backend,omitempty"`
	// ExecutionBackend is the fallback backend used when a task and its
	// parent PRD both have an empty Backend field. Empty string means
	// fall through to the global session.llm_backend default.
	ExecutionBackend string `json:"execution_backend,omitempty"`
	// BL304: renamed from DecompositionEffort; JSON key updated to planning_effort.
	PlanningEffort     string `json:"planning_effort,omitempty"`
	VerificationEffort string `json:"verification_effort,omitempty"`
	// v5.26.16 — operator-pinned model overrides paired with the
	// backend fields above. Empty = backend default.
	// BL304: renamed from DecompositionModel; JSON key updated to planning_model.
	PlanningModel string `json:"planning_model,omitempty"`
	// PlanningTimeoutSeconds overrides the effort-scaled LLM call timeout
	// used during PRD decomposition (default: 120s quick/low, 300s normal,
	// 900s high/max). Set to 0 to use the effort-scaled defaults.
	// Exposed as autonomous.planning_timeout_seconds in config.
	PlanningTimeoutSeconds int `json:"planning_timeout_seconds,omitempty"`
	// VerificationTimeoutSeconds is the sibling of PlanningTimeoutSeconds.
	// Exposed as autonomous.verification_timeout_seconds in config. The
	// manager itself does not consume it: the daemon's verifier/guardrail
	// wiring in cmd/datawatch/main.go reads it from config.Config. The field
	// exists so the live manager config round-trip carried by
	// handleAutonomousConfig does not drop the key, the way the
	// VerificationBackends omitempty bug described in the config.go comment
	// dropped its value.
	VerificationTimeoutSeconds int `json:"verification_timeout_seconds,omitempty"`
	// SessionIdleTimeoutSeconds mirrors autonomous.session_idle_timeout_seconds
	// in config. The manager does not consume it: the daemon's autonomous
	// worker/verifier wait loops in cmd/datawatch/main.go and
	// cmd/datawatch/session_judgment.go read it from config.Config. The field
	// exists so the handleAutonomousConfig live round-trip
	// (internal/server/autonomous.go) does not drop the key.
	SessionIdleTimeoutSeconds int `json:"session_idle_timeout_seconds,omitempty"`
	// LoopDetectTurns mirrors autonomous.loop_detect_turns in config.
	// *int: nil = unset → default 3 (config.DefaultLoopDetectTurns);
	// 0 = detector OFF; n>0 = window. The manager does not consume it:
	// cmd/datawatch's worker wait loop reads it from config.Config (see
	// LoopDetectTurnsEffective there); the field exists so the
	// handleAutonomousConfig live round-trip
	// (internal/server/autonomous.go) does not drop the key.
	LoopDetectTurns   *int   `json:"loop_detect_turns,omitempty"`
	VerificationModel string `json:"verification_model,omitempty"`
	// VerificationBackends (v8.36.9) — see config.AutonomousConfig's doc
	// comment. Ordered list of LLM registry names to load-balance
	// verifier/guardrail capacity across; empty = single-backend
	// behavior via VerificationBackend/PRD backend/"ollama" fallback.
	//
	// B124 follow-up — no `omitempty`, deliberately: see
	// config.AutonomousConfig.VerificationBackends's doc comment.
	// internal/server/autonomous.go's handleAutonomousConfig marshals
	// THIS struct and unmarshals the result onto config.AutonomousConfig
	// before persisting — omitempty here would elide an explicit clear
	// from that marshal, silently reverting it on the next load.
	VerificationBackends []string `json:"verification_backends"`
	// TaskOverrideStatuses (v9.5.5) — PRD statuses that accept task-level and
	// story-level LLM overrides. Empty = default; see config.AutonomousConfig's doc comment.
	// No omitempty, same round-trip reason as VerificationBackends.
	TaskOverrideStatuses []string `json:"task_override_statuses"`
	StaleTaskSeconds     int      `json:"stale_task_seconds,omitempty"`
	AutoFixRetries       int      `json:"auto_fix_retries,omitempty"`

	// BL410 Phase 1 — per-category retry budgets (daemon-wide default;
	// PRD.RetryPolicy > named RetryProfile > this). Keys: "infra",
	// "content", "scope", "policy" (see FailureReason's doc comment for
	// the taxonomy). "content"/"policy" fall back to AutoFixRetries
	// above when absent from this map — the backward-compat anchor, so
	// a PRD that never touches this feature gets byte-for-byte today's
	// retry count. "infra"'s real default (2) and "scope"'s (0) are
	// seeded here by DefaultConfig, not hardcoded in the resolver.
	RetryPolicy map[string]int `json:"retry_policy,omitempty"`
	// RetryCeilingTotal caps auto-retries summed across every category
	// and unit for one PRD's whole lifecycle (default 5 — per the
	// operator's own framing: a normal release PRD failing more than
	// ~5 times means the spec needs updating or datawatch itself is
	// broken, not a problem one more attempt fixes). PRD.RetryCeilingTotal
	// or a named RetryProfile can raise this for genuinely iterative
	// work (the operator's example: a CVE test-and-fix PRD).
	RetryCeilingTotal int `json:"retry_ceiling_total,omitempty"`
	// InfraRetryEscalateAfter — how many retries keep the EXACT
	// original backend/model config before an infra-category retry
	// falls back to the global default (default 2, per the operator's
	// "retry appropriately, more than 1x before falling back").
	InfraRetryEscalateAfter int `json:"infra_retry_escalate_after,omitempty"`
	// ContentAutoFixEnabled/AutoFixSkipApproval have no omitempty — see
	// AutoApproveChildren's comment below for why a bool here needs
	// the explicit-clear discipline (an operator writing false must
	// not silently no-op on the next config round-trip). Defaults:
	// both true (content auto-fix on, synthesized corrective work
	// skips PRD.PerStoryApproval) — the operator's own "work auto the
	// default" answer.
	ContentAutoFixEnabled bool `json:"content_auto_fix_enabled"`
	AutoFixSkipApproval   bool `json:"auto_fix_skip_approval"`
	// BlockOnFileDrift — strict foreign-file-drift enforcement: when
	// true, a task whose run modified tracked files outside its own
	// scope is blocked instead of merely flagged. No omitempty for the
	// same explicit-clear reason as ContentAutoFixEnabled above.
	// Default false (opt-in); overridable per GuardrailProfile and per
	// PRD (resolveBlockOnFileDrift).
	BlockOnFileDrift bool `json:"block_on_file_drift"`
	// VerifierDiffMaxBytes caps the git diff injected into the verifier
	// prompt for evidence grounding (BL366). 0 = 8192.
	VerifierDiffMaxBytes int  `json:"verifier_diff_max_bytes,omitempty"`
	SecurityScan         bool `json:"security_scan,omitempty"`

	// BL191 Q4 (v5.9.0) — recursive child-PRD knobs. MaxRecursionDepth
	// caps the parent→child PRD chain length (default 5; 0 disables
	// recursion entirely). AutoApproveChildren skips the
	// needs_review→approved gate for spawned children — needed for any
	// useful recursion, since otherwise every level hangs on operator
	// review. Defaults: depth=5, auto_approve=true.
	MaxRecursionDepth int `json:"max_recursion_depth,omitempty"`
	// AutoApproveChildren has no omitempty: SetConfig (internal/autonomous/api.go)
	// merges an incoming patch onto the CURRENT config via json.Unmarshal, which
	// only touches keys actually present in the JSON. omitempty on a bool means
	// an explicit "set this to false" is marshaled as an absent key and silently
	// never overwrites a previously-true value — found live 2026-10-05 via this
	// exact bug on PerStoryApproval below (SEC/A3 E2E investigation).
	AutoApproveChildren bool `json:"auto_approve_children"`

	// BL191 Q6 (v5.10.0) — guardrails at story + task level. Empty list =
	// disabled at that level (PRD-level guardrails are handled by the
	// BL117 orchestrator). When non-empty, the executor invokes each
	// named guardrail after the corresponding unit completes; a `block`
	// verdict halts the parent PRD with status PRDBlocked. The
	// well-known guardrail names match the orchestrator: rules,
	// security, release-readiness, docs-diagrams-architecture.
	PerTaskGuardrails  []string `json:"per_task_guardrails,omitempty"`
	PerStoryGuardrails []string `json:"per_story_guardrails,omitempty"`

	// BL418 — daemon-wide default for the "prd" level (PRD-completion
	// rules review). Same off-by-default, explicit-opt-in shape as the
	// two lists above: empty means runPRDCompletionRulesReview never
	// fires, even when a rules/context file exists. An operator adds
	// "rules-review" here (or to PRD.PerPRDGuardrails) to turn it on.
	// "ci-watch" is the other valid PRD-level name: it waits for the
	// CI runs of this automaton's own pushed commit (see
	// CIWatchTimeoutSeconds) and is equally off unless listed.
	PerPRDGuardrails []string `json:"per_prd_guardrails,omitempty"`

	// Phase 3 (v5.26.61) — per-story approval gate. When true, PRD-
	// level approval transitions every story to "awaiting_approval"
	// and the runner skips those until the operator approves each.
	// Default false preserves the v5.26.x behavior (PRD approval
	// implicitly approves every story).
	//
	// No omitempty — see AutoApproveChildren's comment above. A PUT
	// /api/config '{"autonomous.per_story_approval":false}' restore
	// call was silently dropped by SetConfig's merge-unmarshal while
	// this field had omitempty, leaving every subsequent PRD approval
	// wrongly gating its stories into awaiting_approval forever (no
	// code path ever calls approve_story for them) — found live
	// 2026-10-05 chasing a hung E2E run.
	PerStoryApproval bool `json:"per_story_approval"`

	// BL221 (v6.2.0) Phase 3 — scan framework config.
	Scan scan.Config `json:"scan,omitempty"`

	// BL406 — path to this project's prescriptive rules file (its
	// AGENT.md equivalent). Relative to a PRD's project_dir. Empty =
	// "AGENT.md" applied at read time, never stored as a default.
	RulesFile string `json:"rules_file,omitempty"`
	// ContextFile — path to this project's descriptive "state of the
	// project" doc. Explicitly optional. Empty = "CONTEXT.md" applied
	// at read time only when that file actually exists.
	ContextFile string `json:"context_file,omitempty"`
	// UpstreamRepos — named targets a project-rules file_upstream_issue
	// action can reference.
	UpstreamRepos []scan.UpstreamRepo `json:"upstream_repos,omitempty"`

	// BL407 Phase 1 — WorktreeBaseRepo is the git repo a PRD's
	// isolated worktree is created from (e.g. this checkout, for
	// self-build work). Empty = feature off, fully backward
	// compatible: a PRD with no explicit ProjectDir/ProjectProfile/
	// ClusterProfile runs the same way it always has. WorktreeDir is
	// where worktrees are created (default ~/.datawatch/prd-worktrees
	// when empty). See docs/plans/2026-10-10-prd-git-workflow.md.
	WorktreeBaseRepo string `json:"worktree_base_repo,omitempty"`
	WorktreeDir      string `json:"worktree_dir,omitempty"`
	// BL407 Phase 5 — daemon-wide default for a new PRD's Git.AutoPR.
	// Applied once, at CreatePRD time (not resolved at use-time the
	// way DefaultQualityGates/ContinueOnStoryFailure are) — Git.AutoPR
	// is a plain bool with no pointer/override indirection, so a
	// resolve-at-use-time default couldn't distinguish "operator
	// explicitly turned it back off on this PRD" from "never set"; a
	// one-time default at creation avoids that ambiguity entirely and
	// leaves the field as the PRD's own real, inspectable, freely
	// overridable value from then on (via the new set_git action).
	DefaultAutoPR bool `json:"default_auto_pr,omitempty"`

	// BL303 S2 — default skills automatically assigned to every new Automaton.
	DefaultSkills []string `json:"default_skills,omitempty"`

	// BL367 — default quality gate config for all PRDs. Per-PRD
	// QualityGates field overrides this when set. Disabled by default
	// (Enabled: false) to preserve v8.15.x behaviour.
	DefaultQualityGates pipeline.QualityGateConfig `json:"default_quality_gates,omitempty"`

	// BL369 — prompt injection guard. When true, user-supplied text
	// (PRD spec, task spec edits) is scanned for known injection phrases
	// at the API boundary. Findings are always logged; with BlockOnInjection
	// true the request is rejected with 400.
	// No omitempty on either bool — see PerStoryApproval's comment above.
	InjectionGuard   bool `json:"injection_guard"`
	BlockOnInjection bool `json:"block_on_injection"`

	// BL370 — task concurrency. MaxConcurrentTasks controls how many task
	// sessions the executor may have in-flight simultaneously. Default 1
	// preserves the existing sequential behaviour. Values > 1 fan out
	// independent (no-dependency) tasks up to the specified limit. Per-PRD
	// MaxConcurrentTasks overrides this global default.
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`
	// MaxEnginesPerNode is how many local-model engine sessions one
	// compute node may run at once. 1 = one per node (default). Mirrors
	// config.AutonomousConfig.MaxEnginesPerNode; the runtime copy is what
	// REST and MCP writes change.
	MaxEnginesPerNode int `json:"max_engines_per_node,omitempty"`
	// BL428 §3a ii — refuse to admit a task onto a project tree that
	// has uncommitted changes: the canonical tree's `git status
	// --porcelain` is non-empty. Hard failure at admission (before the
	// tree lock), never a retry. Default false; per-PRD override
	// PRD.RefuseDirtyTreeStart.
	RefuseDirtyTreeStart bool `json:"refuse_dirty_tree_start"`

	// ContinueOnStoryFailure controls whether the executor keeps running
	// later, independent stories after one story fails. Default false
	// (zero value) HALTS the PRD (status -> PRDBlocked) the moment a
	// story rolls up to StoryFailed, so a broken first story can't let
	// the run barrel ahead into stories 2-4 unattended. Set true to
	// restore the old "continue regardless" behavior. Per-PRD
	// PRD.ContinueOnStoryFailure overrides this when non-nil.
	// No omitempty — see PerStoryApproval's comment above.
	ContinueOnStoryFailure bool `json:"continue_on_story_failure"`

	// Capacity admission. nil = enabled (default on).
	CapacityEnabled *bool `json:"capacity_enabled,omitempty"`
	// CapacityWaitTimeoutSeconds bounds a capacity wait (default 14400).
	CapacityWaitTimeoutSeconds int `json:"capacity_wait_timeout_seconds,omitempty"`
	// CIWatchTimeoutSeconds bounds how long the ci-watch completion guardrail waits for this automaton's own pushed commit's CI runs to finish (default 1500).
	// Default rationale: this repo's own tag Release workflow took ~18
	// minutes on the most recent real run (gh run list) and ordinary
	// push workflows 1-11 minutes, so 25 minutes covers the slowest real
	// run with headroom. 0 = use the default (see ciWatchTimeout).
	CIWatchTimeoutSeconds int `json:"ci_watch_timeout_seconds,omitempty"`
}

// DefaultConfig returns sane defaults — autonomous OFF until operator opts in.
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		PollIntervalSeconds: 30,
		MaxConcurrentTasks:  1,
		MaxEnginesPerNode:   1,
		AutoFixRetries:      1,
		SecurityScan:        true,
		MaxRecursionDepth:   5,
		AutoApproveChildren: true,
		Scan:                scan.DefaultConfig(),
		// BL410 Phase 1 — see Config.RetryPolicy's doc comment for why
		// "content"/"policy" are deliberately absent here (they fall
		// back to AutoFixRetries above). infra's budget (4) must stay
		// greater than InfraRetryEscalateAfter (2) below — otherwise
		// the per-category budget always exhausts at attempt 3, the
		// exact attempt escalation first becomes eligible at, and
		// escalation would be structurally unreachable under defaults
		// (caught by TestRunGuardrailWithInfraRetry_IncidentReproduction).
		RetryPolicy:             map[string]int{"infra": 4, "scope": 0},
		RetryCeilingTotal:       5,
		InfraRetryEscalateAfter: 2,
		ContentAutoFixEnabled:   true,
		BlockOnFileDrift:        false, // opt-in strict drift enforcement
		AutoFixSkipApproval:     true,
	}
}

// checkInjectionGuard (BL369) scans user-supplied text for prompt injection
// phrases. Always logs findings; returns an error only when BlockOnInjection
// is enabled — that lets callers reject the request at the API boundary.
func (m *Manager) checkInjectionGuard(label, text string) error {
	m.mu.Lock()
	guard := m.cfg.InjectionGuard
	block := m.cfg.BlockOnInjection
	m.mu.Unlock()
	if !guard {
		return nil
	}
	hits := ScanForInjection(text)
	if len(hits) == 0 {
		return nil
	}
	metrics.InjectionGuardHitsTotal.Inc()
	log.Printf("[BL369] injection-guard hit in %s: %s", label, strings.Join(hits, "; "))
	if block {
		return fmt.Errorf("injection-guard: potentially unsafe content detected in %s (%s)", label, hits[0])
	}
	return nil
}

// resolveQualityGates (BL367) returns the quality gate config to use for
// a PRD: per-PRD override takes precedence over the daemon-wide default.
func (m *Manager) resolveQualityGates(prd *PRD) pipeline.QualityGateConfig {
	if prd.QualityGates != nil {
		return *prd.QualityGates
	}
	m.mu.Lock()
	d := m.cfg.DefaultQualityGates
	m.mu.Unlock()
	return d
}

// resolveContinueOnStoryFailure returns whether the executor should keep
// running independent later stories after a story fails: per-PRD override
// takes precedence over the daemon-wide default (which itself defaults to
// false — halt — unless the operator opts in globally).
func (m *Manager) resolveContinueOnStoryFailure(prd *PRD) bool {
	if prd.ContinueOnStoryFailure != nil {
		return *prd.ContinueOnStoryFailure
	}
	m.mu.Lock()
	d := m.cfg.ContinueOnStoryFailure
	m.mu.Unlock()
	return d
}

// GuardrailFn (BL191 Q6, v5.10.0) is the indirection that runs one
// guardrail attestation at the story or task level. Wired in main.go
// to the same BL103 validator-agent path the BL117 orchestrator uses
// at the PRD level so guardrail behavior is uniform across levels.
// Tests inject a fake.
type GuardrailFn func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error)

// GuardrailInvocation is the input to a per-story / per-task guardrail
// call. Level is "task" or "story"; UnitID is the Task.ID or Story.ID.
type GuardrailInvocation struct {
	PRDID      string
	Level      string // "story" | "task"
	UnitID     string
	UnitTitle  string
	UnitSpec   string
	Guardrail  string // name from Config.PerTaskGuardrails / PerStoryGuardrails
	ProjectDir string
	// BL369 Layer 3 — federation trust boundary. Non-empty when the PRD
	// originated from a remote federation peer (PRD.OwnerPeer). The
	// guardrail prompt includes an untrusted-origin notice when set.
	OwnerPeer string
	// BL416 Phase 3 — tooled deterministic guardrails. ScriptRan is true
	// only when the unit's VerificationScript was actually attempted;
	// ScriptExitCode/ScriptOutput carry its real result (output is
	// combined stdout+stderr, capped by truncateScriptOutput) so the
	// judgment call is grounded in real output, not a guess.
	// GuardrailBackend/GuardrailModel are ALREADY-RESOLVED values from
	// resolveGuardrailBackendModel — callers never pass raw task/PRD
	// override fields here.
	ScriptRan        bool
	ScriptExitCode   int
	ScriptOutput     string
	GuardrailBackend string
	GuardrailModel   string
}

// AutomatonType (BL221 v6.2.0 Phase 4) is a registered automaton type.
// Well-known types are pre-registered; operators add custom types via the REST API.
type AutomatonType struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"` // CSS color for badges
}

// builtinTypes is the factory-default type registry.
var builtinTypes = []AutomatonType{
	{"software", "Software", "Feature development, bug fixes, refactoring", "#6366f1"},
	{"research", "Research", "Investigation, analysis, documentation", "#10b981"},
	{"operational", "Operational", "Deployments, infra changes, monitoring", "#f59e0b"},
	{"personal", "Personal", "Personal productivity and learning tasks", "#8b5cf6"},
}

// Manager is the public façade for the autonomous package.
type Manager struct {
	mu          sync.Mutex
	cfg         Config
	store       *Store
	automations *AutomationStore
	dataDir     string         // 2026-10-07 — root for ScratchDir (PRD scratch artifacts), same root Store itself was opened under
	templates   *TemplateStore // BL221 (v6.2.0) — dedicated template store
	decompose   DecomposeFn
	guardrail   GuardrailFn

	// pairingLogged records each pairing warning already written to the
	// daemon log, so a config set does not repeat a warning it logged before.
	pairingLogged map[string]bool

	// BL451 — per-PRD count of executor-owned saves (savePRDOwn), so the
	// completion rollup can discount its own Rev bumps. Guarded by mu.
	ownSaves map[string]int

	// loopFired (loop_rescue.go) — loop-rescue fires-once dedup: keys are
	// sessionID + \x00 + LoopFinding.Key(); a (session, finding) pair
	// already rescued must not re-fire on later polls (no re-record,
	// re-emit, re-send, or re-stop). Guarded by mu.
	loopFired map[string]bool

	// BL427 — in-process lifecycle event dispatcher (lifecycle_event.go).
	// Has its own lock; never guarded by mu. lifecycleHost is
	// os.Hostname, cached once.
	lifecycle     *lifecycleDispatcher
	lifecycleHost string
	lifecycleOnce sync.Once

	// BL221 (v6.2.0) Phase 3 — scan framework
	graderFn     scan.GraderFn
	ruleEditorFn scan.RuleEditorFn
	scanResults  sync.Map // prdID → *scan.Result

	// BL406 Phase 4 — injectable so tests can fake the GitHub call
	// without shelling out to the real `gh` binary (that layer already
	// has its own direct unit tests in internal/git). nil means "use
	// the real git.Resolve" — see fireUpstreamIssueActions.
	gitProviderFn func(kind string) git.Provider

	// ci-watch completion guardrail seams (see ci_watch.go). Guarded by
	// mu like guardrail. nil/zero means "use the real implementation":
	// ciGitFn → defaultCIGit, ciGitSHAFn → defaultCIGitSHA, ciRunsFn →
	// defaultCIRuns, ciRerunFn →
	// defaultCIRerun, ciPollInterval → 30s, ciFollowUpDelay → the
	// ci-watch timeout (ciWatchTimeout). Injectable so tests never shell
	// out to the real git/gh binaries or wait real minutes.
	ciGitFn         func(ctx context.Context, projectDir string) (headSHA, originURL string, pushed bool, err error)
	ciGitSHAFn      func(ctx context.Context, projectDir, sha string) (originURL string, pushed bool, err error)
	ciRunsFn        func(ctx context.Context, ownerRepo, sha string) ([]CIWatchRun, error)
	ciRerunFn       func(ctx context.Context, ownerRepo string, runID int64) error
	ciPollInterval  time.Duration
	ciFollowUpDelay time.Duration

	// BL406 Phase 5 — nil until BL405 Phase 8 wires real CouncilProfile
	// support; resolveGuidedModeSource falls "council" back to
	// "operator" while this is nil, rather than silently skipping the
	// gate. Signature intentionally loose (consensus decision as a
	// bool + free-form note) since the real CouncilProfile type
	// doesn't exist yet — this is a placeholder seam, not a finished
	// contract.
	councilApprovalFn func(ctx context.Context, prd *PRD, t *Task) (approved bool, note string, err error)

	// BL221 (v6.2.0) Phase 4 — type registry
	typesMu sync.Mutex
	types   []AutomatonType

	// BL303 S2 — guardrail library + skill guardrail loader
	guardrailLibMu sync.RWMutex
	guardrailLib   []GuardrailEntry
	skillsDir      string // path to ~/.datawatch/skills (injected from main.go)

	// v5.26.19 — F10 profile resolver for PRD profile validation.
	// Injected from main.go so internal/autonomous stays free of
	// internal/profile dependencies. Nil = profile attachment skips
	// validation (operator gets an unchecked profile name).
	profileResolver ProfileResolver

	// BL244 — plugin session injection. When set, called with the PRD's
	// Type before each task spawn; non-empty result is forwarded in
	// SpawnRequest.ContextPrepend. Nil = no plugin context injected.
	contextFn func(prdType string) string

	// sessionKillerFn — when set, Cancel calls this for each task
	// session that is still running at the time of cancellation.
	// Injected from main.go to avoid a circular import on session.Manager.
	sessionKillerFn func(sessionID string) error

	// sessionAliveFn — when set, reconcileStuckTasks uses it to skip
	// tasks whose session is still running (B90). Nil = conservative:
	// all stuck tasks are failed regardless of session state.
	sessionAliveFn func(sessionID string) bool
	// decomposeSessionsFn lists live planning sessions linked to a PRD.
	decomposeSessionsFn func(prdID string) []string

	// Capacity admission (see capacity.go).
	capacity     *capacity.Ledger
	capacityKeys CapacityKeysFn

	// sessionDeleteFn — when set, called after a task reaches a terminal
	// state (completed/failed/cancelled) to remove the ephemeral executor
	// session from the session list. Nil = sessions remain as killed entries.
	sessionDeleteFn func(sessionID string)

	// memorySeedFn (BL386 Phase 1) — when set, called after each spawn
	// when the PRD's MemorySeedConfig.Enabled=true. Seeds session-local
	// from applicable scopes. Nil = seeding silently skipped.
	memorySeedFn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error

	// memoryHarvestFn (BL386 Phase 2) — when set, called after each task
	// reaches TaskCompleted when the PRD's MemoryHarvestConfig.Enabled=true.
	// Promotes session-local memories to story-shared or prd-shared.
	// Nil = harvest silently skipped.
	memoryHarvestFn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemoryHarvestConfig) error

	// auditFn — when set, called for audit-worthy autonomous events
	// (today: autonomous_file_drift_detected). Wired from main.go to the
	// operator audit log so the event lands in both JSON-lines and CEF.
	// Guarded by mu. Nil = audit silently skipped.
	auditFn func(action string, details map[string]any)

	// driftGit — git runner for checkForeignFileDrift's committed-file
	// lookup. nil means defaultDriftGitRunner; tests inject a counting
	// fake.
	driftGit driftGitRunner

	// memoryVerifierFn (BL387 Phase 1a) — when set, called once per verification
	// issue after verify() returns !OK. Writes the issue to prd-shared scope so
	// retry tasks can seed from it. Nil = verifier writes silently skipped.
	memoryVerifierFn func(ctx context.Context, prdID, projectDir, content, role string) error

	// memoryScopeSeedFn (BL387 Phase 1b) — when set, called in recurseChildPRD
	// after the child PRD is created and before it is decomposed. Copies the
	// parent's prd-shared memories into the child's prd-shared scope.
	// Nil = child-PRD inheritance silently skipped.
	memoryScopeSeedFn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int) error

	// memoryContextFn (BL387 Phase 2a) — when set, called before building the
	// decomposer prompt. Returns a formatted markdown block of relevant prior
	// context from project-shared memory. Empty return = no injection.
	// Nil = context enrichment silently skipped.
	memoryContextFn func(ctx context.Context, projectDir, query string, topK int) (string, error)

	// memoryCrossSeedFn (BL387 Phase 2b) — when set, called at PRD first-run
	// for each entry in MemorySeed.FromPRDs. Seeds the referenced PRD's
	// prd-shared into this PRD's prd-shared. Nil = cross-PRD seeding skipped.
	memoryCrossSeedFn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int, roleFilter []string) error

	// memoryReportFn (BL387 Phase 3) — when set, called asynchronously when a
	// PRD reaches PRDCompleted. Returns a markdown summary of the prd-shared
	// memory scope; the result is stored on PRD.MemoryReport. Nil = no report.
	memoryReportFn func(ctx context.Context, prdID, projectDir string) (string, error)

	// loop state
	ctx    context.Context
	cancel context.CancelFunc
	status LoopStatus
}

// ProfileResolver lets Manager validate that named F10 project +
// cluster profiles exist before attaching them to a PRD. Implemented
// by main.go which has access to internal/profile's stores.
type ProfileResolver interface {
	HasProjectProfile(name string) bool
	HasClusterProfile(name string) bool
}

// SetProfileResolver wires the profile-existence-check indirection
// (v5.26.19). Nil disables validation — operators can attach
// arbitrary profile names but the executor will fail at run time if
// the profile turns out not to exist.
func (m *Manager) SetProfileResolver(r ProfileResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profileResolver = r
}

// SetContextFn (BL244) wires the plugin session-injection lookup. fn is called
// with the PRD's Type before each task spawn; a non-empty return is forwarded
// to SpawnRequest.ContextPrepend so the worker receives plugin-injected context.
func (m *Manager) SetContextFn(fn func(prdType string) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contextFn = fn
}

// SetSessionKillerFn wires the session-kill callback used by Cancel to
// terminate any task sessions still running when a PRD is cancelled.
// Injected from main.go; nil disables the kill (sessions age out normally).
func (m *Manager) SetSessionKillerFn(fn func(sessionID string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionKillerFn = fn
}

// SetDecomposeSessionsFn wires the lookup of live planning (decompose)
// sessions for a PRD, used by boot-resume to clean up orphans.
func (m *Manager) SetDecomposeSessionsFn(fn func(prdID string) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decomposeSessionsFn = fn
}

// killOrphanDecomposeSessions kills any live planning session for the PRD.
// After a daemon restart the decompose goroutine that would consume its output
// is gone, so the session is an orphan burning compute.
func (m *Manager) killOrphanDecomposeSessions(prdID string) int {
	m.mu.Lock()
	find, kill := m.decomposeSessionsFn, m.sessionKillerFn
	m.mu.Unlock()
	if find == nil || kill == nil {
		return 0
	}
	n := 0
	for _, id := range find(prdID) {
		if err := kill(id); err != nil {
			log.Printf("[autonomous] boot-resume: kill orphan decompose session %s (prd=%s): %v", id, prdID, err)
			continue
		}
		log.Printf("[autonomous] boot-resume: killed orphan decompose session %s (prd=%s)", id, prdID)
		n++
	}
	return n
}

// SetSessionAliveFn wires the liveness check used by reconcileStuckTasks (B90).
// fn returns true when sessionID still refers to a non-terminal session.
// Nil is safe: stuck tasks are conservatively marked failed on daemon start.
func (m *Manager) SetSessionAliveFn(fn func(sessionID string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionAliveFn = fn
}

// SetSessionDeleteFn wires the post-completion session cleanup callback.
// When set, it is called after each task reaches a terminal state so the
// ephemeral executor session is removed from the session list automatically.
// Nil = sessions remain visible as killed entries (pre-v8.33.42 behavior).
func (m *Manager) SetSessionDeleteFn(fn func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionDeleteFn = fn
}

// SetMemorySeedFn (BL386 Phase 1) wires the warm-start seeding callback.
// Called after each task spawn when the PRD's MemorySeedConfig.Enabled=true.
// Nil disables seeding (no-op; no error).
func (m *Manager) SetMemorySeedFn(fn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memorySeedFn = fn
}

// SetMemoryHarvestFn (BL386 Phase 2) wires the harvest-on-completion callback.
// Called after each task completes when the PRD's MemoryHarvestConfig.Enabled=true.
// Nil disables harvest (no-op; no error).
func (m *Manager) SetMemoryHarvestFn(fn func(ctx context.Context, sessionID, prdID, storyID, projectDir string, cfg MemoryHarvestConfig) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryHarvestFn = fn
}

// SetAuditFn wires the audit-event callback used for autonomous events
// that must reach the operator audit log (JSON-lines + CEF mirror),
// e.g. autonomous_file_drift_detected. Nil disables (no-op; no error).
func (m *Manager) SetAuditFn(fn func(action string, details map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditFn = fn
}

// SetMemoryVerifierFn (BL387 Phase 1a) wires the verifier-finding write callback.
// Called once per issue after verify() fails on a task. Writes the issue to
// prd-shared so retry tasks can seed from it. Nil disables writes (no-op; no error).
func (m *Manager) SetMemoryVerifierFn(fn func(ctx context.Context, prdID, projectDir, content, role string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryVerifierFn = fn
}

// SetMemoryScopeSeedFn (BL387 Phase 1b) wires the child-PRD prd-shared inheritance
// callback. Called in recurseChildPRD after the child PRD is created and before
// Decompose. Copies parent's prd-shared into child's prd-shared. Nil = skipped.
func (m *Manager) SetMemoryScopeSeedFn(fn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryScopeSeedFn = fn
}

// SetMemoryContextFn (BL387 Phase 2a) wires the decomposer context-enrichment
// callback. Called before building the decomposer prompt; returns a formatted
// markdown block of relevant prior context from project-shared. Empty string =
// no injection. Nil = enrichment silently skipped.
func (m *Manager) SetMemoryContextFn(fn func(ctx context.Context, projectDir, query string, topK int) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryContextFn = fn
}

// SetMemoryCrossSeedFn (BL387 Phase 2b) wires the cross-PRD seeding callback.
// Called at PRD first-run for each entry in MemorySeed.FromPRDs. Copies the
// referenced PRD's prd-shared into the current PRD's prd-shared. Nil = skipped.
func (m *Manager) SetMemoryCrossSeedFn(fn func(ctx context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int, roleFilter []string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryCrossSeedFn = fn
}

// SetMemoryReportFn (BL387 Phase 3) wires the auto-report callback. Called
// asynchronously when a PRD reaches PRDCompleted; result stored in PRD.MemoryReport.
// Nil = no report generated on completion.
func (m *Manager) SetMemoryReportFn(fn func(ctx context.Context, prdID, projectDir string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryReportFn = fn
}

// decisionKindExists reports whether prd.Decisions already carries a decision
// of the given kind. Used to guard against appending a duplicate boot audit
// entry when the same PRD is saved more than once during the same boot pass
// (e.g. both the reconcile and the resume pass touch it).
func decisionKindExists(prd *PRD, kind string) bool {
	if prd == nil {
		return false
	}
	for i := range prd.Decisions {
		if prd.Decisions[i].Kind == kind {
			return true
		}
	}
	return false
}

// resetInProgressTasksForResume resets tasks left mid-flight by a prior
// daemon instance so the executor re-runs them cleanly instead of spawning
// a duplicate alongside an orphaned session. TaskInProgress tasks are always
// reset (the spawn goroutine never reached TaskVerifying, so any session is
// necessarily orphaned). TaskVerifying/TaskRunningTests tasks with a dead or
// absent session resolve via ResolveVerifyingTask: a recorded ok verification
// verdict is applied (TaskCompleted) instead of blindly re-running the task;
// otherwise the task is reset to TaskPending. A genuinely live session is
// left alone (still TaskVerifying) so executeOne's own skip-spawn check picks
// it up and verifies it directly, matching reconcileStuckTasks' logic
// (B90/B93). This is the
// canonical boot-time task/session reconciliation; executeOne's own
// kill-before-respawn guard (executor.go) is the second line of defense if
// this pass or its liveness check is wrong.
// Called from api.resumeRunningPRDs before re-launching the executor.
func (m *Manager) resetInProgressTasksForResume(prd *PRD) {
	m.mu.Lock()
	killerFn := m.sessionKillerFn
	aliveFn := m.sessionAliveFn
	m.mu.Unlock()
	if prd == nil {
		return
	}
	var reset []pendingTaskEvent
	completedCount, requeuedCount := 0, 0
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			prevStatus := t.Status
			switch t.Status {
			case TaskWaitingCapacity:
				// Waiter died with the prior daemon; it holds no session.
			case TaskInProgress:
				// Spawn goroutine died with the prior daemon before reaching
				// TaskVerifying — any session is necessarily orphaned.
			case TaskVerifying, TaskRunningTests:
				switch ResolveVerifyingTask(aliveFn, t) {
				case TaskVerifying:
					// Session still live; leave it for executeOne to resume. Re-take
					// its capacity lease now (BL462 F1) so admission counts it before
					// any other task on this PRD or another PRD is admitted.
					m.adoptCapacity(prd, t)
					log.Printf("[autonomous] boot-resume: prd=%s task %s kept (was %s) on live session %s, capacity lease re-adopted", prd.ID, t.ID, prevStatus, t.SessionID)
					continue
				case TaskCompleted:
					// A recorded ok verification verdict stands: complete the task
					// instead of resetting it for a blind re-run. Verification is
					// kept untouched.
					if killerFn != nil && t.SessionID != "" {
						if err := killerFn(t.SessionID); err != nil {
							log.Printf("[autonomous] boot-resume: kill orphaned session %s: %v", t.SessionID, err)
						}
					}
					now := time.Now()
					t.Status = TaskCompleted
					t.CompletedAt = &now
					t.Error = ""
					oldSession := t.SessionID
					t.SessionID = ""
					t.UpdatedAt = now
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: oldSession})
					completedCount++
					log.Printf("[autonomous] boot-resume: completed task %s (was %s) for prd=%s from recorded ok verification", t.ID, prevStatus, prd.ID)
					continue
				}
				// TaskPending: fall through to the reset-to-pending path below;
				// it was an orphaned verifying task re-queued for retry.
				requeuedCount++
			default:
				continue
			}
			if killerFn != nil && t.SessionID != "" {
				if err := killerFn(t.SessionID); err != nil {
					log.Printf("[autonomous] boot-resume: kill orphaned session %s: %v", t.SessionID, err)
				}
			}
			now := time.Now()
			oldSession := t.SessionID
			m.releaseCapacity(t.ID)
			t.SessionID = ""
			t.Status = TaskPending
			t.Error = ""
			t.UpdatedAt = now
			reset = append(reset, pendingTaskEvent{t: t, oldStatus: prevStatus, oldSession: oldSession})
			log.Printf("[autonomous] boot-resume: reset task %s (was %s) for prd=%s", t.ID, prevStatus, prd.ID)
		}
	}
	if len(reset) == 0 {
		return
	}
	now := time.Now()
	prd.UpdatedAt = now
	// Auditable verification of the orphaned-verifying resolutions: record how
	// many verifying tasks completed from a recorded ok verdict vs how many
	// were re-queued for retry. Guarded so a PRD saved twice in the same boot
	// pass never carries two verifying_resolution entries.
	if completedCount+requeuedCount > 0 && !decisionKindExists(prd, verifyingResolution) {
		prd.Decisions = append(prd.Decisions, Decision{
			At:    now,
			Kind:  verifyingResolution,
			Actor: "autonomous",
			Note:  fmt.Sprintf("boot-resume resolved %d orphaned verifying task(s) to completed (recorded ok verification) and re-queued %d for retry", completedCount, requeuedCount),
		})
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] boot-resume: save prd=%s: %v", prd.ID, err)
		return
	}
	m.emitPendingTaskEvents(prd, reset, lastDecision(prd, verifyingResolution))
}

// resetKilledTasksForResume checks a PRDFailed PRD: if every failed task has
// either no SessionID or a dead session (no live session), it resets those
// tasks to pending so the executor can retry them after a daemon restart.
// Returns true if any task was reset (meaning the caller should re-run the PRD).
// Stories that are in a failed/blocked state are also reset to pending.
func (m *Manager) resetKilledTasksForResume(prd *PRD) bool {
	m.mu.Lock()
	aliveFn := m.sessionAliveFn
	killerFn := m.sessionKillerFn
	m.mu.Unlock()

	// Check whether any task is in an in-progress state with a live session.
	// A task that is already in `failed` state cannot have an actively running
	// session for THIS task (the session may have been reused for other work),
	// so we only guard on non-terminal statuses.
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if isTaskTerminal(t.Status) {
				continue // terminal tasks can't have live executor sessions
			}
			if aliveFn != nil && t.SessionID != "" && aliveFn(t.SessionID) {
				return false // a task is genuinely still running; leave it
			}
		}
	}

	// Check that at least one failed task exists and no task is completed.
	// A completed task means real work was done before the kill — partial runs
	// should not be auto-recovered; use reset_task per task instead.
	hasResettable := false
	for si := range prd.Story {
		for _, t := range prd.Story[si].Tasks {
			if t.Status == TaskCompleted {
				return false
			}
			if t.Status == TaskFailed {
				hasResettable = true
			}
		}
	}
	if !hasResettable {
		return false
	}

	// Reset all failed tasks to pending.
	now := time.Now()
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.Status != TaskFailed {
				continue
			}
			if killerFn != nil && t.SessionID != "" {
				if err := killerFn(t.SessionID); err != nil {
					log.Printf("[autonomous] boot-resume: kill orphaned session %s: %v", t.SessionID, err)
				}
			}
			oldStatus, oldSession := t.Status, t.SessionID
			t.Status = TaskPending
			t.Error = ""
			t.SessionID = ""
			t.RetryCount = 0
			t.UpdatedAt = now
			if m.store.SaveTask(t) == nil {
				m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				m.emitTaskSession(prd, t, oldSession)
			}
			log.Printf("[autonomous] boot-resume: reset killed task %s to pending for prd=%s", t.ID, prd.ID)
		}
		// Reset stories that are not completed back to pending.
		s := &prd.Story[si]
		if s.Status != StoryCompleted {
			s.Status = StoryPending
		}
	}
	old := prd.Status
	prd.Status = PRDRunning
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At:    now,
		Kind:  "boot_resume_recovery",
		Actor: "autonomous",
		Note:  "PRDFailed auto-recovered: all tasks reset to pending after daemon restart",
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] boot-resume: save prd=%s: %v", prd.ID, err)
		return false
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "boot_resume_recovery"), nil)
	return true
}

// killPRDSessions iterates every task in prd and calls sessionKillerFn for
// each non-empty SessionID. Best-effort — errors are logged but not returned.
func (m *Manager) killPRDSessions(prd *PRD) {
	m.mu.Lock()
	fn := m.sessionKillerFn
	m.mu.Unlock()
	if fn == nil || prd == nil {
		return
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			sid := prd.Story[si].Tasks[ti].SessionID
			if sid != "" {
				if err := fn(sid); err != nil {
					log.Printf("[autonomous] cancel: kill session %s: %v", sid, err)
				}
			}
		}
	}
}

// SetGuardrail wires the per-story / per-task guardrail indirection
// (BL191 Q6, v5.10.0). Nil = guardrails disabled even when
// PerTaskGuardrails / PerStoryGuardrails are configured (the executor
// short-circuits with a clear log line). main.go wires this to the
// BL103 validator path during daemon startup.
func (m *Manager) SetGuardrail(fn GuardrailFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guardrail = fn
}

// EmitPRDUpdate (v5.24.0) is the explicit broadcast trigger for
// callers that wrote through Store.SavePRD directly and want the WS
// broadcast to fire. The Manager's own mutator methods (Approve,
// Reject, Decompose, etc.) call this after a successful save; the
// `Store` layer is unaware of the WS path and stays decoupled.
//
// BL427 — publishes a KindPRDUpdated LifecycleEvent through the
// lifecycle dispatcher (formerly a single-slot callback). A PRD that
// no longer loads is reported with Deleted=true. main.go's WS adapter
// subscribes to KindPRDUpdated and maps the event back to the legacy
// frame with PRDUpdatePayload. Delivery is async per subscriber.
func (m *Manager) EmitPRDUpdate(prdID string) {
	if m == nil {
		return
	}
	ev := LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: prdID}
	if prd, _ := m.store.GetPRD(prdID); prd != nil {
		ev.NewStatus = string(prd.Status)
		ev.ParentPRDID = prd.ParentPRDID
	} else {
		ev.Deleted = true
	}
	m.emit(ev)
}

// NewManager constructs the Manager and opens the store under dataDir.
func NewManager(dataDir string, cfg Config, decompose DecomposeFn) (*Manager, error) {
	st, err := NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	automs, err := NewAutomationStore(st.dir, func(id string) (*PRD, bool) {
		return st.GetPRD(id)
	})
	if err != nil {
		return nil, err
	}
	types := make([]AutomatonType, len(builtinTypes))
	copy(types, builtinTypes)
	m := &Manager{
		cfg:         cfg,
		store:       st,
		automations: automs,
		dataDir:     dataDir,
		templates:   newTemplateStore(dataDir),
		decompose:   decompose,
		types:       types,
	}
	m.initGuardrailLibrary() // BL303 S2
	m.lifecycleBus()         // BL427 — create dispatcher + cache hostname
	return m, nil
}

// Templates exposes the TemplateStore for REST/API wiring.
func (m *Manager) Templates() *TemplateStore { return m.templates }

// SetConfig replaces the runtime config (hot-reload entry).
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// Config returns a copy of the current config.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Store exposes the underlying store (REST handlers and tests use it).
func (m *Manager) Store() *Store { return m.store }

// AutomationStore exposes the AutomationStore (REST handlers and tests).
func (m *Manager) AutomationStore() *AutomationStore { return m.automations }

// CreatePRD records a draft PRD without decomposing — call Decompose
// next or pass to Run() which decomposes lazily.
func (m *Manager) CreatePRD(spec, projectDir, backend, model string, effort Effort) (*PRD, error) {
	if err := m.checkInjectionGuard("prd spec", spec); err != nil {
		return nil, err
	}
	prd, err := m.store.CreatePRD(spec, projectDir, backend, model, effort)
	if err != nil {
		return nil, err
	}
	// BL303 S2 T09 — apply default skills from config.
	m.mu.Lock()
	defaults := append([]string{}, m.cfg.DefaultSkills...)
	m.mu.Unlock()
	if len(defaults) > 0 {
		prd.Skills = defaults
		_ = m.store.SavePRD(prd)
	}
	// BL407 Phase 5 — apply the daemon-wide AutoPR default at creation
	// time, same shape as DefaultSkills above.
	m.mu.Lock()
	defaultAutoPR := m.cfg.DefaultAutoPR
	m.mu.Unlock()
	if defaultAutoPR && !prd.Git.AutoPR {
		prd.Git.AutoPR = true
		_ = m.store.SavePRD(prd)
	}
	return prd, nil
}

// SetSkillsDir wires the path to the skills directory (BL303 S2 T08).
// When set, Manager.loadSkillGuardrails reads skill manifests to register
// skill-contributed guardrails before PRD execution.
func (m *Manager) SetSkillsDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skillsDir = dir
}

// Decompose calls the LLM via the configured DecomposeFn and persists
// the resulting story tree. Returns the updated PRD.
//
// BL191 (v5.2.0) — status transitions: PRDDraft (or PRDRevisionsAsked)
// → PRDPlanning → PRDNeedsReview. The operator must explicitly
// Approve before Run is allowed; see Manager.Approve and Manager.Run.
// Planning records a Decision row on the PRD with the LLM call
// metadata (backend, prompt size, response size).
func (m *Manager) Decompose(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if m.decompose == nil {
		return nil, fmt.Errorf("decompose fn not configured")
	}
	if prd.IsTemplate {
		return nil, fmt.Errorf("prd %q is a template; instantiate it first", prdID)
	}
	// BL407 Phase 1 extension (found live 2026-10-11 creating BL404's
	// own PRD): session-based decompose needs a real project_dir, same
	// as Run() does, so a worktree-mode PRD gets one here too — not
	// just at Run() time, which decompose happens well before. See
	// ensureWorktreeIfConfigured's own doc comment.
	if created, werr := m.ensureWorktreeIfConfigured(prd); werr != nil {
		return nil, fmt.Errorf("prd %q worktree setup: %w", prdID, werr)
	} else if created {
		log.Printf("[autonomous] prd=%s worktree created at %s (branch=%s) for decompose", prdID, prd.ProjectDir, prd.Git.Branch)
	}
	// v8.20.0 — use the per-PRD planning backend (decomposition_profile) when
	// set; otherwise fall back to the global planning_backend config. The
	// execution backend (Backend) is reserved for task sessions only.
	backend := prd.DecompositionProfile
	if backend == "" {
		backend = m.cfg.PlanningBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.PlanningEffort)
	}
	// Mark in-flight so the operator can see it from /api/autonomous/prds/{id}.
	// Also persists the worktree assignment above in the same write.
	planOld := prd.Status
	prd.Status = PRDPlanning
	prd.UpdatedAt = time.Now()
	if m.store.SavePRD(prd) == nil {
		m.emitPRDStatus(prd, planOld, nil, nil)
	}

	// BL387 Phase 2a — enrich decomposer prompt with prior context from project-shared.
	prompt := fmt.Sprintf(PlanningPrompt, prd.Spec)
	m.mu.Lock()
	ctxFn := m.memoryContextFn
	m.mu.Unlock()
	if ctxFn != nil && prd.ProjectDir != "" {
		if priorCtx, cerr := ctxFn(context.Background(), prd.ProjectDir, prd.Spec, 15); cerr == nil && priorCtx != "" {
			prompt = fmt.Sprintf(PlanningPrompt, prd.Spec+"\n\n"+priorCtx)
		}
	}
	// BL406 — see rules_context.go. Found live 2026-10-11: this was
	// promised by the plan's own Context section and never implemented.
	prompt += m.rulesContextSuffix(prd)
	// BL416 Phase 2 — see verification_script_prompt.go. Twin call in decomposeStreamingCore(); keep both in sync.
	prompt += m.verificationScriptSuffix(prd)

	planModel := resolvePlannedModel(backend, prd.DecompositionModel, prd.Backend, prd.Model, m.cfg.PlanningBackend, m.cfg.PlanningModel)
	// BL410 remediation (2026-10-13) — bounded, hint-fed retry for an
	// infra/scope-classified decompose failure, instead of immediately
	// rolling back to PRDDraft on the very first failure. Parse failures
	// are NOT in this class: a parse failure means the session DID write
	// a plan (the session-based decompose fn only returns non-empty
	// content when the output file was written), so it fails immediately
	// below rather than consuming infra retry budget. See
	// retryDecomposeFailure's doc comment for the full rationale; see
	// decomposeStreamingCore's identical loop for the twin copy this
	// drifted-duplicate pair requires.
	var raw, title string
	var stories []Story
	for {
		attemptPrompt := prompt
		if prd.LastDecomposeFailure != nil {
			attemptPrompt += fmt.Sprintf("\n\nYour previous attempt failed: %s\nFix this in your next response.", prd.LastDecomposeFailure.Detail)
		}
		r, derr := m.decomposeAdmitted(prd, backend, planModel, DecomposeRequest{Spec: attemptPrompt, Backend: backend, Effort: effort, Model: planModel, ProjectDir: prd.ProjectDir, ScopeBlock: ScopeBlock(prd), PRDID: prd.ID, TimeoutSeconds: m.cfg.PlanningTimeoutSeconds})
		if derr != nil {
			if isOneShotNoOutput(derr) {
				if m.noteDecomposeSessionFailure(prd, backend, planModel, derr) {
					return nil, derr
				}
				continue
			}
			if m.retryDecomposeFailure(prd, "infra", "decompose-llm", derr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-llm", derr)
			return nil, fmt.Errorf("LLM plan: %w", derr)
		}
		t, s, perr := ParsePlanning(r)
		if perr != nil {
			// Written-verdict class (2026-10-19 incident; same class as
			// BL478's 28KB-discarded-plan bug): a parse failure means the
			// session wrote an output file that failed to parse — a verdict
			// was produced, so this is NOT an infra-category failure and
			// must not consume infra retry budget (retrying risks re-running
			// twice and discarding a good second plan). Fail immediately;
			// failDecomposeAttempt still records the parse reason on
			// LastDecomposeFailure.Detail so an operator sees WHY.
			m.failDecomposeAttempt(prd, "decompose-parse", perr)
			return nil, perr
		}
		// A valid-JSON plan with no usable stories must not be stored as
		// a finished decompose. Route it through the same retry path as
		// a parse failure so the reason lands on LastDecomposeFailure.
		if zerr := validateParsedPlan(s); zerr != nil {
			if m.retryDecomposeFailure(prd, "content", "decompose-zero-stories", zerr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-zero-stories", zerr)
			return nil, zerr
		}
		// B124 — kick back a tooled plan that skipped verification
		// scripts entirely despite planning real file changes (see this
		// function's own doc comment). Decompose() is a lower-level
		// call used directly by tests and any caller that wants a
		// non-streaming result, so this check must live here too, not
		// only in decomposeStreamingCore.
		if verr := validateVerificationScriptCoverage(prd.ProjectDir, s); verr != nil {
			if m.retryDecomposeFailure(prd, "scope", "verification-script-coverage", verr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "verification-script-coverage", verr)
			return nil, verr
		}
		// SetStories is where depends_on gets resolved + validated: it
		// rejects the whole plan (naming the exact bad entry or the
		// cycle, storing nothing) when a reference can't be resolved. That
		// is a content rejection, so it takes the same retry-then-fail
		// path as the checks above. The only other error SetStories can
		// return is "prd not found", which cannot happen here (this PRD
		// was just loaded and is alive in the store for the duration of
		// this call), so every SetStories failure takes this branch.
		if err := m.store.SetStories(prdID, s); err != nil {
			if m.retryDecomposeFailure(prd, "content", "decompose-depends-on", err.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-depends-on", err)
			return nil, err
		}
		raw, title, stories = r, t, s
		prd.LastDecomposeFailure = nil // success — clear the single-use hint
		prd.DecomposeSessionFailures = 0
		break
	}
	if title != "" {
		prd.Title = title
	}
	// BL191 Q1: operator review/approve gate. Status lands in needs_review
	// rather than active/running. Approve / Reject / RequestRevision are
	// the explicit transitions out.
	reviewOld := prd.Status
	prd.Status = PRDNeedsReview
	prd.Decisions = append(prd.Decisions, Decision{
		At:            time.Now(),
		Kind:          "decompose",
		Backend:       backend,
		PromptChars:   len(prompt),
		ResponseChars: len(raw),
		Actor:         "autonomous",
	})
	prd.ScopeWarnings = LintPlanScope(prd, stories)
	// SetStories (with depends_on resolution + validation) already ran
	// inside the retry loop above — the decompose-depends-on branch; the
	// stories are stored and only the PRD record itself needs saving now.
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	// Only the persisted state counts: a decompose whose stories did not
	// land in the store is a failure, and its output artifact is kept.
	if perr := m.checkPersistedDecompose(prdID); perr != nil {
		m.failDecomposeAttempt(prd, "decompose-persist", perr)
		return nil, perr
	}
	RemoveDecomposeOutputArtifact(m.dataDir, prdID)
	m.emitPRDStatus(prd, reviewOld, lastDecision(prd, "decompose"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// StoryCallback is called by decomposeStreamingCore as each story
// becomes available. index is 0-based; total is the total story count.
// The callback must not block.
type StoryCallback func(index int, total int, story Story)

// DecomposeStreaming satisfies the server.AutonomousAPI interface.
// cb receives each story as an any-typed Story value so the server
// package doesn't need to import internal/autonomous.
//
// BL328 — used by the async POST /decompose handler.
func (m *Manager) DecomposeStreaming(prdID string, cb func(index, total int, story any)) (any, error) {
	var wrapped StoryCallback
	if cb != nil {
		wrapped = func(index, total int, s Story) { cb(index, total, s) }
	}
	return m.decomposeStreamingCore(prdID, wrapped)
}

// decomposeStreamingCore is the typed implementation used internally and
// by tests. It runs the LLM planning phase and calls cb for each story.
func (m *Manager) decomposeStreamingCore(prdID string, cb StoryCallback) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if m.decompose == nil {
		return nil, fmt.Errorf("decompose fn not configured")
	}
	if prd.IsTemplate {
		return nil, fmt.Errorf("prd %q is a template; instantiate it first", prdID)
	}
	// BL407 Phase 1 extension — see Decompose()'s identical call for
	// the full explanation. decomposeStreamingCore is a drifted
	// duplicate of Decompose() by design history (see this function's
	// own "operator-reported" comment below); kept in sync here too
	// rather than risk a third hand-copied gate.
	if created, werr := m.ensureWorktreeIfConfigured(prd); werr != nil {
		return nil, fmt.Errorf("prd %q worktree setup: %w", prdID, werr)
	} else if created {
		log.Printf("[autonomous] prd=%s worktree created at %s (branch=%s) for decompose", prdID, prd.ProjectDir, prd.Git.Branch)
	}
	backend := prd.DecompositionProfile
	if backend == "" {
		backend = m.cfg.PlanningBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.PlanningEffort)
	}
	// Mark in-flight. Also persists the worktree assignment above.
	planOld := prd.Status
	prd.Status = PRDPlanning
	prd.UpdatedAt = time.Now()
	if m.store.SavePRD(prd) == nil {
		m.emitPRDStatus(prd, planOld, nil, nil)
	}

	// BL387 Phase 2a — enrich decomposer prompt with prior context from project-shared.
	prompt := fmt.Sprintf(PlanningPrompt, prd.Spec)
	m.mu.Lock()
	ctxFn2 := m.memoryContextFn
	m.mu.Unlock()
	if ctxFn2 != nil && prd.ProjectDir != "" {
		if priorCtx, cerr := ctxFn2(context.Background(), prd.ProjectDir, prd.Spec, 15); cerr == nil && priorCtx != "" {
			prompt = fmt.Sprintf(PlanningPrompt, prd.Spec+"\n\n"+priorCtx)
		}
	}
	// BL406 — see rules_context.go and Decompose()'s identical call.
	prompt += m.rulesContextSuffix(prd)
	// BL416 Phase 2 — see verification_script_prompt.go. Twin call in Decompose(); keep both in sync.
	prompt += m.verificationScriptSuffix(prd)

	// Operator-reported: a PRD configured for claude-code kept running
	// decompose against the daemon's global qwen default instead. Root
	// cause: this streaming path is a drifted duplicate of Decompose()'s
	// setup and was missing the per-PRD DecompositionModel tier entirely,
	// jumping straight to the global default whenever one was configured
	// — backend resolution above was correct (so the claude CLI launched
	// fine), but the --model argument fed into it was always wrong.
	// Restored to the same tiered resolution Decompose() uses.
	planModel2 := resolvePlannedModel(backend, prd.DecompositionModel, prd.Backend, prd.Model, m.cfg.PlanningBackend, m.cfg.PlanningModel)
	// BL410 remediation (2026-10-13) — see Decompose()'s identical loop
	// for the full explanation; retryDecomposeFailure is the shared
	// helper both copies of this drifted-duplicate pair call, so the
	// retry logic itself exists in exactly one place.
	var raw, title string
	var stories []Story
	for {
		attemptPrompt := prompt
		if prd.LastDecomposeFailure != nil {
			attemptPrompt += fmt.Sprintf("\n\nYour previous attempt failed: %s\nFix this in your next response.", prd.LastDecomposeFailure.Detail)
		}
		r, derr := m.decomposeAdmitted(prd, backend, planModel2, DecomposeRequest{Spec: attemptPrompt, Backend: backend, Effort: effort, Model: planModel2, ProjectDir: prd.ProjectDir, ScopeBlock: ScopeBlock(prd), PRDID: prd.ID, TimeoutSeconds: m.cfg.PlanningTimeoutSeconds})
		if derr != nil {
			if isOneShotNoOutput(derr) {
				if m.noteDecomposeSessionFailure(prd, backend, planModel2, derr) {
					return nil, derr
				}
				continue
			}
			if m.retryDecomposeFailure(prd, "infra", "decompose-llm", derr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-llm", derr)
			return nil, fmt.Errorf("LLM plan: %w", derr)
		}
		t, s, perr := ParsePlanning(r)
		if perr != nil {
			// See Decompose()'s identical branch: a parse failure IS a
			// written verdict (non-empty content only returns when the
			// output file was written), so it fails immediately rather than
			// retrying as infra — 2026-10-19 incident class. The parse
			// reason still lands on LastDecomposeFailure.Detail.
			m.failDecomposeAttempt(prd, "decompose-parse", perr)
			return nil, perr
		}
		// Zero-story guard — see Decompose()'s identical call. Kept in
		// sync with the twin loop so both paths reject the same output.
		if zerr := validateParsedPlan(s); zerr != nil {
			if m.retryDecomposeFailure(prd, "content", "decompose-zero-stories", zerr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-zero-stories", zerr)
			return nil, zerr
		}
		// B124 — see Decompose()'s identical call for the full
		// explanation; decomposeStreamingCore is a drifted duplicate of
		// Decompose() by design history, kept in sync here too rather
		// than risk a third hand-copied gate.
		if verr := validateVerificationScriptCoverage(prd.ProjectDir, s); verr != nil {
			if m.retryDecomposeFailure(prd, "scope", "verification-script-coverage", verr.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "verification-script-coverage", verr)
			return nil, verr
		}
		// SetStories + depends_on validation — see Decompose()'s identical
		// block for the full rationale; kept in sync here rather than risk
		// a drifted duplicate.
		if err := m.store.SetStories(prdID, s); err != nil {
			if m.retryDecomposeFailure(prd, "content", "decompose-depends-on", err.Error()) {
				continue
			}
			m.failDecomposeAttempt(prd, "decompose-depends-on", err)
			return nil, err
		}
		raw, title, stories = r, t, s
		prd.LastDecomposeFailure = nil // success — clear the single-use hint
		prd.DecomposeSessionFailures = 0
		break
	}
	if title != "" {
		prd.Title = title
	}
	reviewOld := prd.Status
	prd.Status = PRDNeedsReview
	prd.Decisions = append(prd.Decisions, Decision{
		At:            time.Now(),
		Kind:          "decompose",
		Backend:       backend,
		PromptChars:   len(prompt),
		ResponseChars: len(raw),
		Actor:         "autonomous",
	})
	prd.ScopeWarnings = LintPlanScope(prd, stories)
	// SetStories (with depends_on resolution + validation) already ran
	// inside the retry loop above — the decompose-depends-on branch; the
	// stories are stored and only the PRD record itself needs saving now.
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	// Only the persisted state counts: a decompose whose stories did not
	// land in the store is a failure, and its output artifact is kept.
	if perr := m.checkPersistedDecompose(prdID); perr != nil {
		m.failDecomposeAttempt(prd, "decompose-persist", perr)
		return nil, perr
	}
	RemoveDecomposeOutputArtifact(m.dataDir, prdID)
	m.emitPRDStatus(prd, reviewOld, lastDecision(prd, "decompose"), nil)
	updated, _ := m.store.GetPRD(prdID)
	// Yield stories via callback after persisting.
	if cb != nil {
		for i, s := range stories {
			cb(i, len(stories), s)
		}
	}
	return updated, nil
}

// Approve transitions a PRD from needs_review → approved so the loop /
// Manager.Run is allowed to start workers. BL191 Q1.
func (m *Manager) Approve(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is not approvable", prdID, prd.Status)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDApproved
	// BL427 — fired after the save below.
	emitApproved := func() { m.emitPRDStatus(prd, old, lastDecision(prd, "approve"), nil) }
	prd.ApprovedBy = actor
	prd.ApprovedAt = &now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "approve", Actor: actor, Note: note})
	// Phase 3 (v5.26.61) — when per_story_approval is on, transition
	// every pending/freshly-decomposed story to "awaiting_approval"
	// so the runner skips them until the operator approves each via
	// POST .../approve_story. Stories already in a non-pending state
	// (in_progress / completed / blocked / failed) are left alone.
	if m.cfg.PerStoryApproval {
		for si := range prd.Story {
			if prd.Story[si].Status == "" || prd.Story[si].Status == StoryPending {
				prd.Story[si].Status = StoryAwaitingApproval
				prd.Story[si].UpdatedAt = now
			}
		}
	}
	// BL406 Phase 5 — same shape, at task granularity: when
	// GuidedModeSource resolves to "operator" (the original BL384
	// intent, now actually built — or "council" falling back since
	// BL405 Phase 8 doesn't exist yet), transition every pending task
	// to awaiting_approval so the runner skips them until the operator
	// approves each via ApproveTask. Independent of PerStoryApproval —
	// a PRD can use either, both, or neither gate.
	var gated []pendingTaskEvent
	if m.resolveGuidedModeSource(prd) == GuidedModeOperator {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.Status == "" || t.Status == TaskPending {
					gated = append(gated, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskAwaitingApproval
					t.UpdatedAt = now
				}
			}
		}
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	emitApproved()
	m.emitPendingTaskEvents(prd, gated, nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Reject marks a PRD as terminally rejected. The decomposition stays
// stored for inspection but the loop will never run it.
func (m *Manager) Reject(prdID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q already completed; nothing to reject", prdID)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDRejected
	prd.RejectionReason = reason
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "reject", Actor: actor, Note: reason})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "reject"), nil)
	// A rejected PRD never ran (or, if it was running, the operator
	// has still declared it dead) — clean up any decompose-time
	// worktree now rather than leave it orphaned until someone
	// eventually archives this PRD. Non-fatal and best-effort; see
	// cleanupOrphanedWorktree's own doc comment for why it's safe.
	m.cleanupOrphanedWorktree(prd)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RequestRevision asks the operator (or a follow-up Decompose call)
// for a new decomposition. The PRD lands back in revisions_asked;
// calling Decompose again resets it to needs_review.
func (m *Manager) RequestRevision(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDApproved && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q does not accept revisions", prdID, prd.Status)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDRevisionsAsked
	prd.RevisionsRequested++
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "request_revision", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "request_revision"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ResetToDraft resets a PRD back to draft so the operator can reconfigure
// the backend and re-decompose. Stories and tasks are cleared so the next
// Decompose starts fresh. Blocked only while a run is in flight (running /
// planning) or when the PRD has been archived.
func (m *Manager) ResetToDraft(id, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	switch prd.Status {
	case PRDRunning, PRDActive, PRDPlanning:
		return nil, fmt.Errorf("prd %q is %s; stop it before resetting to draft", id, prd.Status)
	case PRDArchived:
		return nil, fmt.Errorf("prd %q is archived and cannot be reset", id)
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDDraft
	prd.Story = nil
	prd.ApprovedBy = ""
	prd.ApprovedAt = nil
	prd.RejectionReason = ""
	// A reset starts a fresh decompose streak: the no-output counter and
	// its terminal hint must not carry into the next attempt.
	prd.DecomposeSessionFailures = 0
	if prd.LastDecomposeFailure != nil && prd.LastDecomposeFailure.Source == "decompose-session" {
		prd.LastDecomposeFailure = nil
	}
	prd.UpdatedAt = now
	prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
		At: now, Kind: "reset_to_draft", Actor: actor,
		Note: fmt.Sprintf("reset from %s by %s", prd.Status, actor),
	}))
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "reset_to_draft"), nil)
	updated, _ := m.store.GetPRD(id)
	return updated, nil
}

// Archive moves a terminal PRD (completed/rejected/cancelled) to
// PRDArchived status so it disappears from the active list without
// being permanently deleted.
func (m *Manager) Archive(id string) (*PRD, error) {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	terminal := map[PRDStatus]bool{
		PRDCompleted: true, PRDRejected: true, PRDCancelled: true, PRDArchived: true,
	}
	if !terminal[prd.Status] {
		return nil, fmt.Errorf("prd %q status %q is not terminal; cancel or complete it first", id, prd.Status)
	}
	if prd.Status == PRDArchived {
		return prd, nil // idempotent
	}
	now := time.Now()
	old := prd.Status
	prd.Status = PRDArchived
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "archive", Actor: "operator"})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "archive"), nil)
	// Universal orphan-cleanup backstop: by the time a PRD reaches
	// Archive, the operator has said "done with this forever" —
	// whatever Decision 5's "keep for inspection" grace period on a
	// cancelled/failed/blocked PRD was for has ended. A no-op for a
	// completed PRD (Phase 2 already removed its worktree on success)
	// or one that never had a worktree.
	m.cleanupOrphanedWorktree(prd)
	updated, _ := m.store.GetPRD(id)
	return updated, nil
}

// DeletePRD (v5.19.0) hard-removes a PRD from the store, including
// any descendants spawned via SpawnPRD. The operator-facing surface
// for "I'm done with this PRD, remove it from the list". Distinct
// from Cancel which only flips Status to cancelled. Refuses while
// the PRD is running OR any descendant PRD is running — operator
// must Cancel the running ones first (v5.26.8 cascade-aware guard;
// previously only the top-level was checked, leaving spawned children
// dangling mid-execution after a hard-delete).
func (m *Manager) DeletePRD(id string) error {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	if prd.Status == PRDRunning {
		return fmt.Errorf("prd %q is running; cancel before deleting", id)
	}
	// v5.26.8 — walk descendants and refuse if any are running. The
	// store-side cascade is happy to delete running children, but the
	// executor goroutine would keep writing to a now-deleted PRD;
	// catch it at the operator boundary instead.
	all := m.store.ListPRDs()
	for _, running := range descendantsOf(id, all) {
		if running.Status == PRDRunning {
			return fmt.Errorf("descendant prd %q is running; cancel it before deleting parent %q", running.ID, id)
		}
	}
	return m.store.DeletePRD(id)
}

// descendantsOf walks the SpawnPRD tree rooted at parentID and
// returns every descendant in the supplied snapshot. Helper for
// DeletePRD's cascade-aware check; does NOT include the root PRD.
func descendantsOf(parentID string, all []*PRD) []*PRD {
	var out []*PRD
	frontier := map[string]bool{parentID: true}
	for changed := true; changed; {
		changed = false
		for _, p := range all {
			if p.ParentPRDID != "" && frontier[p.ParentPRDID] && !frontier[p.ID] {
				frontier[p.ID] = true
				out = append(out, p)
				changed = true
			}
		}
	}
	return out
}

// SetPRDProfiles (v5.26.19) attaches F10 project + cluster profiles
// to a PRD. Profile names are validated against the wired
// ProfileResolver (when present); empty strings clear the field.
// Refuses while the PRD is running — operator must Cancel first.
// Records a Decision audit row.
func (m *Manager) SetPRDProfiles(id, projectProfile, clusterProfile string) error {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	if prd.Status == PRDRunning {
		return fmt.Errorf("prd %q is running; cancel before attaching profiles", id)
	}
	m.mu.Lock()
	resolver := m.profileResolver
	m.mu.Unlock()
	if resolver != nil {
		if projectProfile != "" && !resolver.HasProjectProfile(projectProfile) {
			return fmt.Errorf("project profile %q not found", projectProfile)
		}
		if clusterProfile != "" && !resolver.HasClusterProfile(clusterProfile) {
			return fmt.Errorf("cluster profile %q not found", clusterProfile)
		}
	}
	prd.ProjectProfile = projectProfile
	prd.ClusterProfile = clusterProfile
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At:    time.Now(),
		Kind:  "set_profiles",
		Actor: "operator",
		Note:  fmt.Sprintf("project=%q cluster=%q", projectProfile, clusterProfile),
	})
	return m.store.SavePRD(prd)
}

// EditPRDFields (v5.19.0) edits PRD-level title + spec on a non-
// running PRD. Records a Decision so the timeline shows the edit.
// Use EditTaskSpec for per-task spec changes.
func (m *Manager) EditPRDFields(id, title, spec, actor string) (*PRD, error) {
	if spec != "" {
		if err := m.checkInjectionGuard("prd spec edit", spec); err != nil {
			return nil, err
		}
	}
	prd, err := m.store.UpdatePRDFields(id, title, spec)
	if err != nil {
		return nil, err
	}
	if actor == "" {
		actor = "operator"
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At:    time.Now(),
		Kind:  "edit",
		Actor: actor,
		Note:  fmt.Sprintf("title=%q spec_chars=%d", title, len(spec)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	return prd, nil
}

// EditTaskSpec lets the operator rewrite an LLM-decomposed task's spec
// before approving the PRD (needs_review / revisions_asked), after
// cancelling it (cancelled), or — BL421 — in place while the PRD is
// blocked, so a stuck task (e.g. one the pre-spawn scope lint rejected)
// can be corrected and then retried via ResetTask without
// cancel -> reset_to_draft -> re-decompose regenerating the whole tree.
// While blocked, only a failed or blocked task may be edited: the point is
// to patch the one stuck task, not to replan its pending siblings. A
// completed task's spec is immutable in every PRD status, since it is the
// record of work already done. Records a Decision so the timeline shows
// the edit.
func (m *Manager) EditTaskSpec(prdID, taskID, newSpec, actor string) (*PRD, error) {
	if err := m.checkInjectionGuard("task spec edit", newSpec); err != nil {
		return nil, err
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled && prd.Status != PRDBlocked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled / blocked accept task edits", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				ts := prd.Story[si].Tasks[ti].Status
				if ts == TaskCompleted {
					return nil, fmt.Errorf("task %q is completed; completed task specs are immutable", taskID)
				}
				if prd.Status == PRDBlocked && ts != TaskFailed && ts != TaskBlocked {
					if ts == "" {
						ts = TaskPending
					}
					return nil, fmt.Errorf("task %q status %q cannot be edited while prd %q is blocked; only failed or blocked tasks can be patched in place", taskID, ts, prdID)
				}
				prd.Story[si].Tasks[ti].Spec = newSpec
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "edit_task", Actor: actor,
		Note: fmt.Sprintf("task=%s spec_chars=%d", taskID, len(newSpec)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RepairDependsOn (v8.36.6) — see Store.RepairDependsOn's doc comment.
func (m *Manager) RepairDependsOn(prdID, actor string) (*PRD, error) {
	prd, err := m.store.RepairDependsOn(prdID)
	if err != nil {
		return nil, err
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "repair_depends_on", Actor: actor,
		Note: "resolved DependsOn titles to real story/task IDs",
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ResetTask (v8.23.0) resets a failed or blocked task back to pending so
// the autonomous loop will retry it. Only allowed while the PRD is running.
// Clears status, error, session_id, verification, and retry_count.
// ResetTask resets a failed/blocked task to pending for retry.
// BL382: force=true also requeues completed or cancelled tasks.
// recoverPendingTask reopens a terminal PRD whose target task is already
// pending (BL451). The task is left untouched; the containing story is
// reopened if terminal and the PRD returns to PRDRunning.
func (m *Manager) recoverPendingTask(prd *PRD, si int, taskID, actor string) (*PRD, error) {
	s := &prd.Story[si]
	if s.Status == StoryCompleted || s.Status == StoryFailed || s.Status == StoryCancelled {
		s.Status = StoryPending
	}
	// Same start-over guarantee as ResetTask: a manual reset of an
	// already-pending task must drop the per-task git anchors too.
	for ti := range s.Tasks {
		if s.Tasks[ti].ID == taskID {
			s.Tasks[ti].BaseSHA = ""
			s.Tasks[ti].AttemptCommitSHAs = nil
			break
		}
	}
	old := prd.Status
	now := time.Now()
	prd.Status = PRDRunning
	prd.UpdatedAt = now
	kind := "recover_pending_task"
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: kind, Actor: actor,
		Note: fmt.Sprintf("task=%s already pending; prd reopened from %s (force=true recovery)", taskID, old)})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, kind), nil)
	updated, _ := m.store.GetPRD(prd.ID)
	return updated, nil
}

func (m *Manager) ResetTask(prdID, taskID, actor string, force bool) (*PRD, error) {
	return m.ResetTaskWithOptions(prdID, taskID, actor, force, false)
}

// ResetTaskWithOptions is ResetTask with the clean-restart option. cleanRestart
// true makes the next attempt skip the restore of preserved attempt work
// (Task.CleanRestart); false keeps the restore on, as ResetTask always did.
func (m *Manager) ResetTaskWithOptions(prdID, taskID, actor string, force, cleanRestart bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	// PRDBlocked included: a guardrail block or (default) story-failure halt
	// both leave the PRD blocked with no other way to retry the offending
	// task — without this the operator has no path back except the
	// destructive reset_to_draft (which wipes all story/task state).
	// PRDCancelled included: operator-requested — cancelling a run to fix a
	// task's spec (e.g. adding a missing reference) shouldn't force a full
	// reset_to_draft + fresh decompose just to retry that one task.
	// PRDCompleted included (v8.36.6): a PRD can reach "completed" with a
	// cancelled story inside it (e.g. one story was cancelled-and-never-
	// restarted while a sibling that depends_on it still ran and completed —
	// see the depends_on title/ID resolution fix). The task-level check below
	// still only allows this via force=true on a completed/cancelled task, so
	// a non-force reset can never reach here (a truly-complete PRD has no
	// failed/blocked tasks to reset in the first place).
	if prd.Status != PRDRunning && prd.Status != PRDFailed && prd.Status != PRDBlocked && prd.Status != PRDCancelled && prd.Status != PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is not recoverable; reset_task requires PRDRunning, PRDFailed, PRDBlocked, PRDCancelled, or PRDCompleted", prdID, prd.Status)
	}
	// BL451 recovery: a terminal PRD holding an already-pending task (a
	// force reset flipped the task, then a stale rollup overwrote the PRD
	// status) has no other way back — approve/run refuse a completed PRD.
	// With force=true, leave the task pending and just reopen the PRD.
	if prd.Status != PRDRunning {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.ID != taskID || (t.Status != "" && t.Status != TaskPending) {
					continue
				}
				if !force {
					return nil, fmt.Errorf("task %q is already pending but prd %q is %q; use force=true to reopen the PRD (recover from a stale rollup)", taskID, prdID, prd.Status)
				}
				return m.recoverPendingTask(prd, si, taskID, actor)
			}
		}
	}
	found := false
	foundSI := -1
	emitReset := func(*Decision) {}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				isRetryable := t.Status == TaskFailed || t.Status == TaskBlocked
				isRequeue := force && (t.Status == TaskCompleted || t.Status == TaskCancelled)
				if !isRetryable && !isRequeue {
					if force {
						return nil, fmt.Errorf("task %q status %q cannot be requeued even with force=true; only completed or cancelled tasks qualify", taskID, t.Status)
					}
					return nil, fmt.Errorf("task %q status %q cannot be reset; only failed or blocked tasks can be retried (use force=true to requeue completed/cancelled tasks)", taskID, t.Status)
				}
				// v8.36.1 — capture why the previous attempt didn't land
				// before clearing it, so the retry's worker gets told what
				// happened instead of the bare original spec (see
				// PriorAttemptHint's doc comment on Task).
				hintParts := make([]string, 0, 2)
				if t.Error != "" {
					hintParts = append(hintParts, "Previous attempt error: "+t.Error)
				}
				if t.Verification != nil && !t.Verification.OK {
					vSummary := fmt.Sprintf("Previous verification failed (%s): %s", t.Verification.Severity, t.Verification.Summary)
					if len(t.Verification.Issues) > 0 {
						vSummary += " Specific issues: " + strings.Join(t.Verification.Issues, "; ")
					}
					hintParts = append(hintParts, vSummary)
				}
				if len(hintParts) > 0 {
					// v8.36.2 — found live retrying this exact hint on PRD
					// a2833a5e: the original wording ("verify/finish it
					// rather than starting over") was ambiguous enough that
					// the worker checked the files looked fine, wrote only a
					// CHECKPOINT.md note, and produced no diff touching the
					// actual required files — which the verifier (correctly
					// scoped to just this run's diff) then rejected as no
					// output at all. Now explicit: touching/recommitting
					// the real deliverables themselves is what's required,
					// not just a checkpoint note about them.
					t.PriorAttemptHint = "This is a retry of a previous attempt on this task. " +
						strings.Join(hintParts, " ") +
						" Check the project directory's current state (git log, existing files) before redoing work — the previous attempt may have already produced correct output that only failed for an unrelated reason (e.g. the verifier itself erroring). If so, fix the specific issues above (or reconfirm correctness) by editing the actual required deliverable files themselves — do not run git add or git commit; your work is committed automatically when you finish. Verification only sees this run's own diff, so a checkpoint note or other side file alone will not satisfy it, even if the underlying content is already right."
				}
				resetEv := pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID}
				t.Status = ""
				t.Error = ""
				t.SessionID = ""
				// A human reset means starting over: drop the per-task git
				// anchors so the fresh attempt re-captures them at spawn.
				// Automatic verifier retries never go through here — BaseSHA
				// is written only when empty in the executor's spawn path,
				// so in-loop retries keep the first attempt's anchor.
				t.BaseSHA = ""
				t.AttemptCommitSHAs = nil
				// BL427 — fired after the SavePRD below succeeds.
				emitReset = func(dec *Decision) { m.emitPendingTaskEvents(prd, []pendingTaskEvent{resetEv}, dec) }
				t.Verification = nil
				t.RetryCount = 0
				// v9.2.1 — a manual (operator) reset is the strongest "this is a
				// retry" signal: to reach this block the task already reached a
				// terminal-ish status (failed/blocked, or completed/cancelled
				// requeued via force), i.e. at least one prior attempt happened.
				// RetryCount is zeroed for the next attempt's auto-retry budget,
				// but PriorAttemptCount is cumulative and is what the verifier
				// consults to know a retry's empty diff is legitimate (the prior
				// attempt may have already landed correct work) rather than a
				// genuine no-output. Auto in-loop retries don't go through
				// ResetTask and leave this at 0 (see Task's field doc).
				t.PriorAttemptCount++
				// Operator's choice for this reset only: a plain reset leaves
				// the flag false, so an unconsumed clean restart never carries
				// over into a later reset.
				t.CleanRestart = cleanRestart
				t.StartedAt = nil
				t.CompletedAt = nil
				t.UpdatedAt = time.Now()
				found = true
				foundSI = si
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	// B99: if the containing story was completed/failed/cancelled, reopen it
	// to pending so the progress bar and status dot reflect that work is
	// still in flight. StoryCancelled added v8.36.6 — same recovery path as
	// PRDCancelled/PRDCompleted above; a cancelled story force-requeued via
	// one of its tasks needs to actually resume, not stay cancelled.
	if foundSI >= 0 {
		s := &prd.Story[foundSI]
		if s.Status == StoryCompleted || s.Status == StoryFailed || s.Status == StoryCancelled {
			s.Status = StoryPending
		}
	}
	now := time.Now()
	prd.UpdatedAt = now
	// B97: when called on a PRDFailed PRD, restore it to PRDRunning so the
	// executor picks it back up without the operator having to reset_to_draft
	// (which would discard all completed task work). Same for PRDBlocked
	// (guardrail block or story-failure halt) and PRDCancelled (operator
	// cancelled to fix a task's spec) — but the executor's Run loop has
	// already exited by the time a PRD reaches any of these, so restoring
	// the status alone isn't enough; the operator (or PWA) must call Run
	// again (boot-resume does this automatically on the next restart, but
	// that's not required for it to take effect).
	resetOld := prd.Status
	if prd.Status == PRDFailed || prd.Status == PRDBlocked || prd.Status == PRDCancelled || prd.Status == PRDCompleted {
		prd.Status = PRDRunning
	}
	kind := "reset_task"
	note := fmt.Sprintf("task=%s reset to pending for retry", taskID)
	if force {
		kind = "requeue_task"
		note = fmt.Sprintf("task=%s requeued (force=true)", taskID)
	}
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: kind, Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	if prd.Status != resetOld {
		m.emitPRDStatus(prd, resetOld, lastDecision(prd, kind), nil)
	}
	emitReset(lastDecision(prd, kind))
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RefreshTaskProgress (progress visibility) records the latest live-progress
// snapshot for a task — read from that task's worker session's own log by the
// executor's periodic session poll — plus the current (possibly empty)
// looping warning. This goes through MutateTask so it is synchronized with
// other task mutations and persisted; the Store also re-checks the
// "in progress only" invariant at persist time.
//
// progress may be nil (log not locatable for this backend): in that case the
// warning is applied but the snapshot is left untouched.
func (m *Manager) RefreshTaskProgress(taskID string, progress *TaskProgress, warning string) bool {
	return m.store.MutateTask(taskID, func(t *Task) {
		if progress != nil {
			t.SetProgress(progress, warning)
		} else {
			t.LoopingWarning = warning
		}
	})
}

// RecordUnmodifiedRetry (v9.2.1) records the audit trail for a retry that
// passed verification via the unmodified-retry escape hatch — the retry
// itself produced no diff, but the task's own VerificationScript re-run
// against the already-committed deliverable passed, so the verifier
// completed the task rather than failing it. Like recordAutoRetryDecision it
// appends a single Decision to the PRD (the autonomous package's own
// already-universally-exposed audit trail, surfaced via REST/MCP/PWA)
// rather than inventing a new mechanism. Called from the cmd/datawatch
// verifier only on the exact rare path it exists for, and only AFTER the
// task has already been marked complete by the caller, so a lookup/save
// failure there is a lost audit note, never a lost outcome — best-effort
// callers log the returned error and move on. A prd that doesn't exist is
// a hard error so the caller doesn't silently claim a record it never made.
func (m *Manager) RecordUnmodifiedRetry(prdID, taskID, note string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	now := time.Now()
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At:    now,
		Kind:  "unmodified_retry_completed",
		Actor: "autonomous",
		Note:  fmt.Sprintf("task=%s unmodified retry completed verification script passed (%s)", taskID, note),
	})
	return m.store.SavePRD(prd)
}

// CancelStory (BL382) — operator cancels a single story without cancelling the whole PRD.
// The story is marked cancelled; any in-progress tasks for that story have their sessions killed.
// Returns 409 if the story is already in a terminal state.
func (m *Manager) CancelStory(prdID, storyID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	terminalStory := map[StoryStatus]bool{
		StoryCompleted: true, StoryFailed: true, StoryCancelled: true,
	}
	now := time.Now()
	found := false
	var cancelled []pendingTaskEvent
	// BL427 — fired after the SavePRD below succeeds.
	emitCancelled := func() { m.emitPendingTaskEvents(prd, cancelled, lastDecision(prd, "cancel_story")) }
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			if terminalStory[prd.Story[si].Status] {
				return nil, fmt.Errorf("409: story %q is already in terminal state %q", storyID, prd.Story[si].Status)
			}
			// Kill any running task sessions for this story.
			m.mu.Lock()
			fn := m.sessionKillerFn
			m.mu.Unlock()
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.SessionID != "" && fn != nil {
					if err := fn(t.SessionID); err != nil {
						log.Printf("[autonomous] cancel_story: kill session %s: %v", t.SessionID, err)
					}
				}
				if t.Status != TaskCompleted && t.Status != TaskCancelled {
					cancelled = append(cancelled, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskCancelled
					t.UpdatedAt = now
				}
			}
			prd.Story[si].Status = StoryCancelled
			prd.Story[si].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	note := fmt.Sprintf("story=%s cancelled", storyID)
	if reason != "" {
		note += ": " + reason
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "cancel_story", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	for _, ev := range cancelled {
		m.releaseCapacity(ev.t.ID)
	}
	emitCancelled()
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// CancelTask (BL382) — operator cancels a single task without cancelling its story or PRD.
// Returns 409 if the task is already in a terminal state.
func (m *Manager) CancelTask(prdID, taskID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	terminalTask := map[TaskStatus]bool{
		TaskCompleted: true, TaskCancelled: true,
	}
	now := time.Now()
	found := false
	emitCancelled := func() {}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				if terminalTask[t.Status] {
					return nil, fmt.Errorf("409: task %q is already in terminal state %q", taskID, t.Status)
				}
				// Kill the session if it's running.
				if t.SessionID != "" {
					m.mu.Lock()
					fn := m.sessionKillerFn
					m.mu.Unlock()
					if fn != nil {
						if err := fn(t.SessionID); err != nil {
							log.Printf("[autonomous] cancel_task: kill session %s: %v", t.SessionID, err)
						}
					}
				}
				oldStatus := t.Status
				t.Status = TaskCancelled
				t.UpdatedAt = now
				// BL427 — fired after the SavePRD below succeeds.
				emitCancelled = func() { m.emitTaskStatus(prd, t, oldStatus, lastDecision(prd, "cancel_task"), nil) }
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	note := fmt.Sprintf("task=%s cancelled", taskID)
	if reason != "" {
		note += ": " + reason
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "cancel_task", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.releaseCapacity(taskID)
	emitCancelled()
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// EditStory (v5.26.32) lets the operator rewrite an LLM-decomposed
// story's title + description before approving the PRD. Mirrors
// EditTaskSpec — only allowed in needs_review or revisions_asked,
// records a Decision so the timeline shows the edit. Operator-asked:
// "i don't see a story review or approval or story edit option."
// AddStory (operator-requested: structural edits without re-decomposing
// and handing the whole structure back to the LLM) appends a new, empty
// story to a PRD. Same review-phase gate as EditStory — the story/task
// structure is only mutable before the PRD is approved and workers start.
func (m *Manager) AddStory(prdID, title, description, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	story, err := m.store.AddStory(prdID, title, description)
	if err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "add_story", Actor: actor,
		Note: fmt.Sprintf("story=%s title=%q", story.ID, title),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RemoveStory deletes a story (and its tasks) from a PRD.
func (m *Manager) RemoveStory(prdID, storyID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if err := m.store.RemoveStory(prdID, storyID); err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "remove_story", Actor: actor,
		Note: fmt.Sprintf("story=%s", storyID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// AddTask appends a new task to an existing story.
func (m *Manager) AddTask(prdID, storyID, title, spec, actor string, dependsOn []string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	task, err := m.store.AddTask(prdID, storyID, title, spec, dependsOn)
	if err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	note := fmt.Sprintf("story=%s task=%s title=%q", storyID, task.ID, title)
	if len(dependsOn) > 0 {
		note += fmt.Sprintf(" depends_on=%v", dependsOn)
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "add_task", Actor: actor,
		Note: note,
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RemoveTask deletes one task from a story (structural edit — distinct
// from CancelTask, which stops a task that's already running/queued
// without removing it from the PRD).
func (m *Manager) RemoveTask(prdID, storyID, taskID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept structural edits", prdID, prd.Status)
	}
	if err := m.store.RemoveTask(prdID, storyID, taskID); err != nil {
		return nil, err
	}
	prd, _ = m.store.GetPRD(prdID)
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "remove_task", Actor: actor,
		Note: fmt.Sprintf("story=%s task=%s", storyID, taskID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

func (m *Manager) EditStory(prdID, storyID, newTitle, newDescription, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept story edits", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			if newTitle != "" {
				prd.Story[si].Title = newTitle
			}
			// Description is optional — empty newDescription clears it
			// only when the operator explicitly passes a sentinel; we
			// treat empty-string as "leave unchanged" so a title-only
			// edit doesn't accidentally drop the description.
			if newDescription != "" {
				prd.Story[si].Description = newDescription
			}
			prd.Story[si].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "edit_story", Actor: actor,
		Note: fmt.Sprintf("story=%s title_chars=%d desc_chars=%d", storyID, len(newTitle), len(newDescription)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetStoryProfile (Phase 3, v5.26.60) — operator overrides a single
// story's execution profile. Empty string clears the override
// (story falls back to PRD.ProjectProfile). Validated against the
// profile resolver if one is wired. Allowed in needs_review /
// revisions_asked only — same lock-after-approve gate as
// EditTaskSpec / EditStory.
func (m *Manager) SetStoryProfile(prdID, storyID, profile, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept story profile changes", prdID, prd.Status)
	}
	if profile != "" && m.profileResolver != nil {
		if !m.profileResolver.HasProjectProfile(profile) {
			return nil, fmt.Errorf("invalid execution profile %q: project profile not found", profile)
		}
	}
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].ExecutionProfile = profile
			prd.Story[si].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_profile", Actor: actor,
		Note: fmt.Sprintf("story=%s profile=%q", storyID, profile),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ApproveStory (Phase 3, v5.26.60) — operator approves an individual
// story for execution. Only effective when the per_story_approval
// config flag is on (otherwise PRD-level approval implicitly approves
// every story). Sets Story.Approved=true, ApprovedBy, ApprovedAt;
// records a kind=approve_story decision. Allowed when the PRD itself
// is approved or running (per-story gate runs *after* PRD-level gate).
func (m *Manager) ApproveStory(prdID, storyID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning {
		return nil, fmt.Errorf("prd %q status %q does not accept per-story approval; PRD must be approved or running first", prdID, prd.Status)
	}
	now := time.Now()
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].Approved = true
			prd.Story[si].ApprovedBy = actor
			prd.Story[si].ApprovedAt = &now
			prd.Story[si].RejectedReason = ""
			// Transition awaiting_approval → pending so the runner
			// picks it up. Other states left alone.
			if prd.Story[si].Status == StoryAwaitingApproval {
				prd.Story[si].Status = StoryPending
			}
			prd.Story[si].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "approve_story", Actor: actor,
		Note: fmt.Sprintf("story=%s", storyID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ApproveTask (BL406 Phase 5 — B111's real fix) — operator approves an
// individual task for execution. Only meaningful when
// GuidedModeSource resolves to "operator"; same shape as ApproveStory.
// Sets Task.Approved=true, ApprovedBy, ApprovedAt; records a
// kind=approve_task decision. Allowed when the PRD itself is approved
// or running (same precondition as ApproveStory — the task-level gate
// runs after the PRD-level gate, never before it).
func (m *Manager) ApproveTask(prdID, taskID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning {
		return nil, fmt.Errorf("prd %q status %q does not accept per-task approval; PRD must be approved or running first", prdID, prd.Status)
	}
	now := time.Now()
	found := false
	emitPending := func() {}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID == taskID {
				t.Approved = true
				t.ApprovedBy = actor
				t.ApprovedAt = &now
				t.RejectedReason = ""
				// Transition awaiting_approval → pending so the runner
				// picks it up. Other states left alone.
				if t.Status == TaskAwaitingApproval {
					t.Status = TaskPending
					// BL427 — fired after the SavePRD below succeeds.
					emitPending = func() { m.emitTaskStatus(prd, t, TaskAwaitingApproval, lastDecision(prd, "approve_task"), nil) }
				}
				t.UpdatedAt = now
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "approve_task", Actor: actor,
		Note: fmt.Sprintf("task=%s", taskID),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	emitPending()
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RejectStory (Phase 3, v5.26.60) — operator blocks a single story.
// Sets Status=blocked + records the reason. The runner skips blocked
// stories; the operator can later approve to resume.
func (m *Manager) RejectStory(prdID, storyID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if reason == "" {
		return nil, fmt.Errorf("reason is required when rejecting a story")
	}
	now := time.Now()
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].Status = StoryBlocked
			prd.Story[si].Approved = false
			prd.Story[si].RejectedReason = reason
			prd.Story[si].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "reject_story", Actor: actor,
		Note: fmt.Sprintf("story=%s reason=%q", storyID, reason),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// FindTaskBySessionID (Phase 4 follow-up, v5.26.67) — given a
// datawatch session id (the SpawnResult.SessionID written into
// Task.SessionID by the executor), returns the (prd_id, task_id)
// pair so the post-session diff callback can route to
// RecordTaskFilesTouched. Returns ("","") when the session isn't
// linked to any autonomous task — the post-session hook treats
// that as a no-op.
func (m *Manager) FindTaskBySessionID(sessionID string) (prdID, taskID string) {
	if sessionID == "" {
		return "", ""
	}
	for _, prd := range m.store.ListPRDs() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if prd.Story[si].Tasks[ti].SessionID == sessionID {
					return prd.ID, prd.Story[si].Tasks[ti].ID
				}
			}
		}
	}
	return "", ""
}

// SetStoryFiles (Phase 4, v5.26.64) — operator overrides a story's
// FilesPlanned list. Allowed in needs_review / revisions_asked only
// (lock-after-approve); appends a `set_story_files` audit decision.
// Empty list clears the field. Capped at 50 paths to keep PRD
// records bounded (per the design doc's 5KB-per-story budget).
func (m *Manager) SetStoryFiles(prdID, storyID string, files []string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept story file edits", prdID, prd.Status)
	}
	if len(files) > 50 {
		files = files[:50]
	}
	found := false
	for si := range prd.Story {
		if prd.Story[si].ID == storyID {
			prd.Story[si].FilesPlanned = files
			prd.Story[si].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_files", Actor: actor,
		Note: fmt.Sprintf("story=%s n=%d", storyID, len(files)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskFiles (Phase 4, v5.26.64) — operator overrides a task's
// FilesPlanned. Same gates as SetStoryFiles.
func (m *Manager) SetTaskFiles(prdID, taskID string, files []string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept task file edits", prdID, prd.Status)
	}
	if len(files) > 50 {
		files = files[:50]
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].FilesPlanned = files
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_files", Actor: actor,
		Note: fmt.Sprintf("task=%s n=%d", taskID, len(files)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RecordTaskFilesTouched (Phase 4, v5.26.64) — daemon-internal hook
// fired by the post-session diff callback to record what the worker
// actually changed. Capped at 50 paths. No lock-after-approve gate
// — this fires after the worker session ends.
func (m *Manager) RecordTaskFilesTouched(prdID, taskID string, files []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if len(files) > 50 {
		files = files[:50]
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].FilesTouched = files
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// RecordTaskAttemptCommit (v5.26.64 sibling of RecordTaskFilesTouched) —
// daemon-internal hook fired by the autonomous verifier once the worker
// session's deterministic commit sha is known. Appends it to the task's
// ordered, de-duplicated AttemptCommitSHAs record (Task.RecordAttemptCommit
// already ignores empty values and duplicates). No lock-after-approve gate
// — this fires after the worker session ends.
func (m *Manager) RecordTaskAttemptCommit(prdID, taskID, sha string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].RecordAttemptCommit(sha)
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetStoryLLM (BL381) lets the operator override a story's worker LLM
// (backend / effort / model). All tasks in this story inherit these
// values unless overridden at the task level. Empty string clears the
// override (falls back to PRD-level then global). Accepted-while is
// governed by autonomous.task_override_statuses (default: needs_review /
// revisions_asked / cancelled / blocked).
func (m *Manager) SetStoryLLM(prdID, storyID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	found := false
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			prd.Story[i].Backend = backend
			prd.Story[i].Effort = Effort(effort)
			prd.Story[i].Model = model
			prd.Story[i].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_llm", Actor: actor,
		Note: fmt.Sprintf("story=%s backend=%s effort=%s model=%s", storyID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskLLM (BL203, v5.4.0) lets the operator override a task's
// worker LLM (backend / effort / model) before approval. Empty string
// clears the override (falls back to story-level then PRD-level then global).
// Accepted-while is governed by autonomous.task_override_statuses (default:
// needs_review / revisions_asked / cancelled / blocked); outside those
// statuses the worker is locked in.
func (m *Manager) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].Backend = backend
				prd.Story[si].Tasks[ti].Effort = Effort(effort)
				prd.Story[si].Tasks[ti].Model = model
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s backend=%s effort=%s model=%s", taskID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskGuardrailLLM (BL416) overrides the backend/model used for a
// task's per-task guardrail judge. Presence-aware: a nil pointer leaves
// that field unchanged; a pointer to "" clears it so resolution falls
// back to the PRD-level then global guardrail_backend/guardrail_model.
// Same accepted-while rule as SetTaskLLM (autonomous.task_override_statuses).
func (m *Manager) SetTaskGuardrailLLM(prdID, taskID string, backend, model *string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var task *Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				task = &prd.Story[si].Tasks[ti]
				break
			}
		}
		if task != nil {
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	if backend != nil {
		task.GuardrailBackend = *backend
	}
	if model != nil {
		task.GuardrailModel = *model
	}
	task.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_guardrail_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s guardrail_backend=%s guardrail_model=%s", taskID, task.GuardrailBackend, task.GuardrailModel),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDLLM (BL203, v5.4.0) overrides the PRD-level worker LLM defaults
// (backend / effort / model / decomposition LLM). Presence-aware: a nil
// pointer leaves that field unchanged; a pointer to "" clears it so
// resolution falls back to the global default. Only allowed pre-Run.
// Tasks without per-task overrides will inherit these.
func (m *Manager) SetMemorySeed(prdID string, cfg MemorySeedConfig, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.MemorySeed = cfg
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_memory_seed", Actor: actor,
		Note: fmt.Sprintf("enabled=%v max_per_scope=%d", cfg.Enabled, cfg.MaxPerScope),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// validPermissionModes mirrors the claude-code permission-mode set the
// start_session MCP tool already validates against (internal/mcp/server.go).
var validPermissionModes = map[string]bool{
	"default": true, "plan": true, "acceptEdits": true,
	"auto": true, "bypassPermissions": true, "dontAsk": true,
}

// SetPermissionMode (GH#172 D75) updates the PRD's permission_mode, which
// tasks spawned under it inherit as their claude-code --permission-mode
// default (see Task.PermissionMode's own doc comment). Empty clears it
// back to the session/config default.
func (m *Manager) SetPermissionMode(prdID, mode, actor string) (*PRD, error) {
	if mode != "" && !validPermissionModes[mode] {
		return nil, fmt.Errorf("invalid permission_mode %q — valid values: default, plan, acceptEdits, auto, bypassPermissions, dontAsk", mode)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.PermissionMode = mode
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_permission_mode", Actor: actor,
		Note: fmt.Sprintf("permission_mode=%s", mode),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Pause (GH#172 D52) marks a running PRD paused. The already-live
// executor goroutine (if any) notices this on its next result-loop
// iteration (see executor.go's Run) and drains in-flight work without
// starting anything new — this method only flips the stored status;
// it does not itself touch any goroutine.
func (m *Manager) Pause(prdID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDRunning {
		return nil, fmt.Errorf("prd %q status %q is not running; only a running PRD can be paused", prdID, prd.Status)
	}
	old := prd.Status
	prd.Status = PRDPaused
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "pause", Actor: actor})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "pause"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Resume (GH#172 D52) flips a paused PRD back to running. It does not
// itself relaunch the executor goroutine — the caller (API.Resume)
// does that the same way API.Run always does, since only the API
// layer holds the spawn/verify callbacks Manager.Run needs.
func (m *Manager) Resume(prdID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDPaused {
		return nil, fmt.Errorf("prd %q status %q is not paused; nothing to resume", prdID, prd.Status)
	}
	old := prd.Status
	prd.Status = PRDRunning
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "resume", Actor: actor})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "resume"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetMemoryHarvest (BL386 Phase 2) updates the PRD's harvest-on-completion config.
func (m *Manager) SetMemoryHarvest(prdID string, cfg MemoryHarvestConfig, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.MemoryHarvest = cfg
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_memory_harvest", Actor: actor,
		Note: fmt.Sprintf("enabled=%v promote_to=%s max=%d", cfg.Enabled, cfg.PromoteTo, cfg.Max),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

func (m *Manager) SetPRDLLM(prdID string, backend, effort, model, decompositionProfile, decompositionModel *string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDRunning || prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is locked; PRD LLM overrides only apply pre-Run", prdID, prd.Status)
	}
	if backend != nil {
		prd.Backend = *backend
	}
	if effort != nil {
		prd.Effort = Effort(*effort)
	}
	if model != nil {
		prd.Model = *model
	}
	if decompositionProfile != nil {
		prd.DecompositionProfile = *decompositionProfile
	}
	if decompositionModel != nil {
		prd.DecompositionModel = *decompositionModel
	}
	prd.UpdatedAt = time.Now()
	// Decision-log note lists ONLY the fields included in this call:
	// a pointer to "" shows the field as cleared (empty value), a nil
	// field is not mentioned at all.
	var parts []string
	for _, p := range []struct {
		name string
		v    *string
	}{
		{"backend", backend},
		{"effort", effort},
		{"model", model},
		{"decomposition_profile", decompositionProfile},
		{"decomposition_model", decompositionModel},
	} {
		if p.v != nil {
			parts = append(parts, p.name+"="+*p.v)
		}
	}
	note := strings.Join(parts, " ")
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_llm", Actor: actor,
		Note: note,
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDGuardrailLLM (BL416) overrides the PRD-level backend/model used
// by the per-task / per-story guardrail judges. Presence-aware: a nil
// pointer leaves that field unchanged; a pointer to "" clears it so
// resolution falls back to the global guardrail_backend/guardrail_model.
// Same lock rule as SetPRDLLM.
func (m *Manager) SetPRDGuardrailLLM(prdID string, backend, model *string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDRunning || prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is locked; PRD LLM overrides only apply pre-Run", prdID, prd.Status)
	}
	if backend != nil {
		prd.GuardrailBackend = *backend
	}
	if model != nil {
		prd.GuardrailModel = *model
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_guardrail_llm", Actor: actor,
		Note: fmt.Sprintf("guardrail_backend=%s guardrail_model=%s", prd.GuardrailBackend, prd.GuardrailModel),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// validVerificationEffort reports whether e is empty (inherit) or one
// of the Effort constants.
func validVerificationEffort(e string) bool {
	switch Effort(e) {
	case "", EffortLow, EffortMedium, EffortHigh, EffortMax, EffortQuick, EffortNormal, EffortThorough:
		return true
	}
	return false
}

// verificationLLMLocked applies SetTaskLLM's lock rule: verifier LLM
// overrides are accepted only in needs_review / revisions_asked /
// cancelled.
func verificationLLMLocked(prd *PRD) error {
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked && prd.Status != PRDCancelled {
		return fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked / cancelled accept LLM overrides", prd.ID, prd.Status)
	}
	return nil
}

// taskOverrideStatusesKnown is the PRD status set a
// task_override_statuses list may name (the models.go PRDStatus constants).
var taskOverrideStatusesKnown = []string{
	string(PRDDraft), string(PRDPlanning), string(PRDNeedsReview), string(PRDApproved),
	string(PRDRevisionsAsked), string(PRDActive), string(PRDRunning), string(PRDCompleted), string(PRDRejected),
	string(PRDCancelled), string(PRDArchived), string(PRDBlocked), string(PRDFailed),
	string(PRDPaused),
}

// TaskOverrideStatusesKnown returns the 14 PRD status names a
// task_override_statuses list may name (the models.go status constants).
func TaskOverrideStatusesKnown() []string {
	return append([]string(nil), taskOverrideStatusesKnown...)
}

// TaskOverrideStatusValid reports whether s is a known PRD status name.
func TaskOverrideStatusValid(s string) bool {
	for _, k := range taskOverrideStatusesKnown {
		if s == k {
			return true
		}
	}
	return false
}

// TaskOverrideStatusesDefault — v9.5.5 default accepted-while list: the
// pre-existing pre-Run trio plus blocked. A blocked PRD is
// operator-held and unblocking it usually starts with re-pointing the
// next retry's LLM, which used to be refused with
// `status "blocked" is locked`.
func TaskOverrideStatusesDefault() []string {
	return []string{string(PRDNeedsReview), string(PRDRevisionsAsked), string(PRDCancelled), string(PRDBlocked)}
}

// EffectiveTaskOverrideStatuses resolves the empty-list-means-default
// convention: non-empty lists are returned verbatim (copy), empty/nil
// lists return the default 4.
func EffectiveTaskOverrideStatuses(list []string) []string {
	if len(list) == 0 {
		return TaskOverrideStatusesDefault()
	}
	return append([]string(nil), list...)
}

// ValidateTaskOverrideStatuses reports the first unknown name and the
// known set. Empty list is VALID (= revert to defaults). Error must be
// exactly the shape (so every surface returns the same message).
func ValidateTaskOverrideStatuses(list []string) error {
	for _, s := range list {
		if !TaskOverrideStatusValid(s) {
			return fmt.Errorf("task_override_statuses: unknown PRD status %q (known: %s)",
				s, strings.Join(taskOverrideStatusesKnown, ", "))
		}
	}
	return nil
}

// taskOverrideLocked enforces the configurable accepted-while rule for
// task-level and story-level LLM override calls (v9.5.5). PRD-level
// overrides keep using verificationLLMLocked / their inline checks.
func (m *Manager) taskOverrideLocked(prd *PRD, prdID string) error {
	eff := EffectiveTaskOverrideStatuses(m.cfg.TaskOverrideStatuses)
	for _, s := range eff {
		if s == string(prd.Status) {
			return nil
		}
	}
	return fmt.Errorf("prd %q status %q is locked; only %s accept LLM overrides (autonomous.task_override_statuses)",
		prdID, prd.Status, strings.Join(eff, " / "))
}

// applyVerificationLLM writes the presence-aware overrides onto the
// three fields: a nil pointer leaves the field unchanged; a pointer to
// "" clears it so resolution falls back to the next level.
func applyVerificationLLM(dstBackend, dstModel *string, dstEffort *Effort, backend, model, effort *string) {
	if backend != nil {
		*dstBackend = *backend
	}
	if model != nil {
		*dstModel = *model
	}
	if effort != nil {
		*dstEffort = Effort(*effort)
	}
}

// SetTaskVerificationLLM overrides the verifier backend/model/effort
// for one task (most specific level; see ResolveVerificationLLM).
// Presence-aware: a nil pointer leaves that field unchanged; a pointer
// to "" clears it so resolution falls back to the story, then the
// Automaton, then the global verification_* config. Same accepted-while
// rule as SetTaskLLM (autonomous.task_override_statuses).
func (m *Manager) SetTaskVerificationLLM(prdID, taskID string, backend, model, effort *string, actor string) (*PRD, error) {
	if effort != nil && !validVerificationEffort(*effort) {
		return nil, fmt.Errorf("invalid verification effort %q", *effort)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var task *Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				task = &prd.Story[si].Tasks[ti]
				break
			}
		}
		if task != nil {
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	applyVerificationLLM(&task.VerificationBackend, &task.VerificationModel, &task.VerificationEffort, backend, model, effort)
	task.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_verification_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s verification_backend=%s verification_model=%s verification_effort=%s",
			taskID, task.VerificationBackend, task.VerificationModel, task.VerificationEffort),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetStoryVerificationLLM overrides the verifier backend/model/effort
// for every task in one story that has no task-level override.
// Presence-aware like SetTaskVerificationLLM; "" falls back to the
// Automaton then the global verification_* config. Same accepted-while
// rule as SetTaskLLM (autonomous.task_override_statuses).
func (m *Manager) SetStoryVerificationLLM(prdID, storyID string, backend, model, effort *string, actor string) (*PRD, error) {
	if effort != nil && !validVerificationEffort(*effort) {
		return nil, fmt.Errorf("invalid verification effort %q", *effort)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var story *Story
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			story = &prd.Story[i]
			break
		}
	}
	if story == nil {
		return nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	applyVerificationLLM(&story.VerificationBackend, &story.VerificationModel, &story.VerificationEffort, backend, model, effort)
	story.UpdatedAt = time.Now()
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_story_verification_llm", Actor: actor,
		Note: fmt.Sprintf("story=%s verification_backend=%s verification_model=%s verification_effort=%s",
			storyID, story.VerificationBackend, story.VerificationModel, story.VerificationEffort),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDVerificationLLM overrides the Automaton-level verifier
// backend/model/effort. Presence-aware like SetTaskVerificationLLM; ""
// falls back to the global verification_* config. Same lock rule as
// SetPRDLLM.
func (m *Manager) SetPRDVerificationLLM(prdID string, backend, model, effort *string, actor string) (*PRD, error) {
	if effort != nil && !validVerificationEffort(*effort) {
		return nil, fmt.Errorf("invalid verification effort %q", *effort)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := verificationLLMLocked(prd); err != nil {
		return nil, err
	}
	applyVerificationLLM(&prd.VerificationBackend, &prd.VerificationModel, &prd.VerificationEffort, backend, model, effort)
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_verification_llm", Actor: actor,
		Note: fmt.Sprintf("verification_backend=%s verification_model=%s verification_effort=%s",
			prd.VerificationBackend, prd.VerificationModel, prd.VerificationEffort),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// InstantiateTemplate (BL191 Q2) takes a template PRD + caller-supplied
// vars and stores a fresh executable PRD with substitutions applied to
// spec, title, and per-task spec strings. Required vars without a value
// are an error; defaults fill missing optionals.
func (m *Manager) InstantiateTemplate(templateID string, vars map[string]string, actor string) (*PRD, error) {
	tmpl, ok := m.store.GetPRD(templateID)
	if !ok {
		return nil, fmt.Errorf("template %q not found", templateID)
	}
	if !tmpl.IsTemplate {
		return nil, fmt.Errorf("prd %q is not a template", templateID)
	}
	resolved := make(map[string]string, len(tmpl.TemplateVars))
	for _, v := range tmpl.TemplateVars {
		if val, ok := vars[v.Name]; ok && val != "" {
			resolved[v.Name] = val
			continue
		}
		if v.Default != "" {
			resolved[v.Name] = v.Default
			continue
		}
		if v.Required {
			return nil, fmt.Errorf("template var %q is required but missing", v.Name)
		}
	}
	subst := func(s string) string {
		out := s
		for name, val := range resolved {
			out = replaceAll(out, "{{"+name+"}}", val)
		}
		return out
	}
	newPRD, err := m.store.CreatePRD(subst(tmpl.Spec), tmpl.ProjectDir, tmpl.Backend, tmpl.Model, tmpl.Effort)
	if err != nil {
		return nil, err
	}
	newPRD.Title = subst(tmpl.Title)
	newPRD.TemplateOf = tmpl.ID
	newPRD.VerificationBackend = tmpl.VerificationBackend
	newPRD.VerificationModel = tmpl.VerificationModel
	newPRD.VerificationEffort = tmpl.VerificationEffort
	// Copy stories with substitutions applied to per-task specs.
	for _, st := range tmpl.Story {
		st2 := st
		tasks2 := make([]Task, 0, len(st.Tasks))
		for _, t := range st.Tasks {
			t2 := t
			t2.Spec = subst(t.Spec)
			t2.Title = subst(t.Title)
			tasks2 = append(tasks2, t2)
		}
		st2.Tasks = tasks2
		newPRD.Story = append(newPRD.Story, st2)
	}
	if len(newPRD.Story) > 0 {
		// Pre-decomposed from template — land in needs_review so operator
		// can confirm before Run.
		newPRD.Status = PRDNeedsReview
	}
	newPRD.Decisions = append(newPRD.Decisions, Decision{
		At: time.Now(), Kind: "template_instantiate", Actor: actor,
		Note: fmt.Sprintf("from=%s vars=%d", tmpl.ID, len(resolved)),
	})
	if err := m.store.SavePRD(newPRD); err != nil {
		return nil, err
	}
	if newPRD.Status == PRDNeedsReview {
		// Freshly created PRD: no prior status, so OldStatus is empty.
		m.emitPRDStatus(newPRD, "", lastDecision(newPRD, "template_instantiate"), nil)
	}
	updated, _ := m.store.GetPRD(newPRD.ID)
	return updated, nil
}

// replaceAll is a small allocation-friendly substring replace; bypasses
// strings.ReplaceAll only because we want zero-package-import in this
// hot path.
func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Status returns a snapshot of the loop state.
//
// v5.22.0 — observability fill-in for BL191 Q4 + Q6. Counts
// ChildPRDs (parent_prd_id != ""), MaxDepthSeen, BlockedPRDs
// (status == PRDBlocked), and VerdictCounts (outcome rollup across
// every Story.Verdicts + Task.Verdicts) so operators querying
// /api/autonomous/status get visible signal for the recursion + per-
// task/story guardrail features.
func (m *Manager) Status() LoopStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	st.Running = m.ctx != nil && m.ctx.Err() == nil
	verdictCounts := map[string]int{}
	for _, p := range m.store.ListPRDs() {
		if p.Status == PRDActive || p.Status == PRDRunning {
			st.ActivePRDs++
		}
		if p.ParentPRDID != "" {
			st.ChildPRDsTotal++
		}
		if p.Depth > st.MaxDepthSeen {
			st.MaxDepthSeen = p.Depth
		}
		if p.Status == PRDBlocked {
			st.BlockedPRDs++
		}
		for _, s := range p.Story {
			for _, v := range s.Verdicts {
				if v.Outcome != "" {
					verdictCounts[v.Outcome]++
				}
			}
			for _, t := range s.Tasks {
				switch t.Status {
				case TaskQueued:
					st.QueuedTasks++
				case TaskInProgress, TaskRunningTests, TaskVerifying:
					st.RunningTasks++
				}
				for _, v := range t.Verdicts {
					if v.Outcome != "" {
						verdictCounts[v.Outcome]++
					}
				}
				// BL367 — quality gate counters.
				if t.QualityGateResult != nil {
					st.QualityGateRuns++
					if t.QualityGateResult.Regression {
						st.QualityGateFail++
						st.QualityGateRegressions++
					} else {
						st.QualityGatePass++
					}
				}
				// BL382 — cancelled task count.
				if t.Status == TaskCancelled {
					st.TasksCancelled++
				}
			}
			// BL382 — cancelled story count.
			if s.Status == StoryCancelled {
				st.StoriesCancelled++
			}
		}
		// BL382 — requeued task count from decision log.
		for _, d := range p.Decisions {
			if d.Kind == "requeue_task" {
				st.TasksRequeued++
			}
		}
	}
	if len(verdictCounts) > 0 {
		st.VerdictCounts = verdictCounts
	}
	st.Warnings = PairingWarnings(m.cfg, m.pairingDecomposeRefs(m.cfg))
	return st
}

// Start kicks the background loop. Idempotent.
func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil && m.ctx.Err() == nil {
		return // already running
	}
	if !m.cfg.Enabled {
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	go m.run()
}

// Stop signals the loop to exit (does not block).
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.ctx = nil
	}
}

// reconcileStuckTasks scans all PRDs on daemon start and resolves any stuck
// TaskInProgress/TaskVerifying/TaskRunningTests whose session is gone (B90).
// TaskVerifying/TaskRunningTests tasks resolve via ResolveVerifyingTask: a
// recorded ok verification verdict is applied (TaskCompleted) instead of a
// blind reset; everything else — TaskInProgress, or a verifying task with a
// failed or missing verdict — resets to TaskPending (not failed) so the
// executor restart picks it up naturally, avoiding the bug where tasks end up
// stuck in failed state because resetInProgressTasksForResume only looks for
// TaskInProgress (which reconcileStuckTasks would have already converted).
func (m *Manager) reconcileStuckTasks() {
	m.mu.Lock()
	aliveFn := m.sessionAliveFn
	killerFn := m.sessionKillerFn
	m.mu.Unlock()

	prds := m.store.ListPRDs()
	for _, prd := range prds {
		if prd.Status != PRDRunning && prd.Status != PRDActive {
			continue
		}
		changed := false
		verdictCompleted, verdictRequeued := 0, 0
		var reset []pendingTaskEvent
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.Status != TaskInProgress && t.Status != TaskVerifying && t.Status != TaskRunningTests {
					continue
				}
				completing := false
				if t.Status == TaskVerifying || t.Status == TaskRunningTests {
					switch ResolveVerifyingTask(aliveFn, t) {
					case TaskVerifying:
						continue // session still live; leave it
					case TaskCompleted:
						// A recorded ok verification verdict stands: complete the
						// task instead of blindly re-running it. Verification is
						// kept untouched.
						completing = true
					}
				} else if aliveFn != nil && t.SessionID != "" && aliveFn(t.SessionID) {
					continue // session still live; leave it (TaskInProgress)
				}
				// Kill any surviving tmux session so the new executor doesn't
				// spawn alongside it.
				if killerFn != nil && t.SessionID != "" {
					if err := killerFn(t.SessionID); err != nil {
						log.Printf("[autonomous] boot-reconcile: kill orphaned session %s: %v", t.SessionID, err)
					}
				}
				// Count the orphaned verifying resolutions for the audit entry.
				if t.Status == TaskVerifying || t.Status == TaskRunningTests {
					if completing {
						verdictCompleted++
					} else {
						verdictRequeued++
					}
				}
				now := time.Now()
				if completing {
					log.Printf("[autonomous] boot-reconcile: prd=%s task=%s status=%s session=%s → TaskCompleted (recorded ok verification, dead session)",
						prd.ID, t.ID, t.Status, t.SessionID)
					// The session is gone, so the task's node lease is freed here
					// rather than left for the periodic reaper.
					m.releaseCapacity(t.ID)
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskCompleted
					t.CompletedAt = &now
					t.Error = ""
					t.SessionID = ""
					t.UpdatedAt = now
				} else {
					log.Printf("[autonomous] boot-reconcile: prd=%s task=%s status=%s session=%s → TaskPending (session gone on daemon restart)",
						prd.ID, t.ID, t.Status, t.SessionID)
					m.releaseCapacity(t.ID)
					reset = append(reset, pendingTaskEvent{t: t, oldStatus: t.Status, oldSession: t.SessionID})
					t.Status = TaskPending
					t.Error = ""
					t.SessionID = ""
					t.UpdatedAt = now
				}
				changed = true
			}
		}
		if !changed {
			continue
		}
		now := time.Now()
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{
			At:    now,
			Kind:  "boot_reconcile",
			Actor: "autonomous",
			Note:  "orphaned task(s) resolved to completed (recorded ok verification) or pending on daemon restart",
		})
		// Auditable verification of the orphaned-verifying resolutions: record
		// how many verifying tasks completed from a recorded ok verdict vs how
		// many were re-queued for retry. Guarded so a PRD saved twice in the
		// same boot pass never carries two verifying_resolution entries.
		if verdictCompleted+verdictRequeued > 0 && !decisionKindExists(prd, verifyingResolution) {
			prd.Decisions = append(prd.Decisions, Decision{
				At:    now,
				Kind:  verifyingResolution,
				Actor: "autonomous",
				Note:  fmt.Sprintf("boot pass resolved %d orphaned verifying task(s) to completed (recorded ok verification) and re-queued %d for retry", verdictCompleted, verdictRequeued),
			})
		}
		if err := m.store.SavePRD(prd); err != nil {
			log.Printf("[autonomous] boot-reconcile: save prd=%s: %v", prd.ID, err)
		} else {
			m.emitPendingTaskEvents(prd, reset, lastDecision(prd, "boot_reconcile"))
		}
	}
}

// run is the loop body. v1 is intentionally minimal: marks newly-
// active PRDs so external schedulers (e.g. main daemon's
// pipeline.Executor or operator-driven `datawatch autonomous run`)
// can pick them up. The actual Task → Session dispatch is done by
// the executor in executor.go (called from REST handler).
func (m *Manager) run() {
	m.reconcileStuckTasks()

	interval := time.Duration(m.cfg.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			m.status.LastTickAt = time.Now()
			m.mu.Unlock()
			// Future: scan for stuck tasks and surface to operator.
		}
	}
}

// CloneToTemplate creates a new Template from a completed (or any terminal)
// PRD's spec + title. Strips execution state; use_count starts at 0.
func (m *Manager) CloneToTemplate(prdID, description, actor string) (*Template, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	return m.templates.Create(prd.Title, description, prd.Spec, "", nil)
}

// InstantiateFromTemplateStore (BL221 v6.2.0) — substitutes vars in a
// TemplateStore entry and creates a new draft PRD with the resulting spec.
func (m *Manager) InstantiateFromTemplateStore(templateID string, vars map[string]string, projectDir, backend string, effort Effort) (*PRD, error) {
	spec, _, err := m.templates.Instantiate(templateID, vars)
	if err != nil {
		return nil, err
	}
	return m.store.CreatePRD(spec, projectDir, backend, "", effort)
}

// ── BL221 (v6.2.0) Phase 3 — scan framework ──────────────────────────────

// SetScanGrader wires the LLM rules-check grader (Option C: inline LLM via
// POST /api/ask loopback). Nil disables LLM grading even when
// cfg.Scan.RulesGraderEnabled is true.
func (m *Manager) SetScanGrader(fn scan.GraderFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.graderFn = fn
}

// SetRuleEditorFn wires the LLM rule-editor (Phase 3b): proposes AGENT.md
// changes for scan violations. Nil disables rule editing.
func (m *Manager) SetRuleEditorFn(fn scan.RuleEditorFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ruleEditorFn = fn
}

// ScanConfig returns a copy of the current scan sub-config.
func (m *Manager) ScanConfig() scan.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Scan
}

// SetScanConfig replaces the scan sub-config at runtime.
func (m *Manager) SetScanConfig(sc scan.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Scan = sc
}

// RunScan executes the configured scanners against the PRD's project_dir.
// Results are cached in memory (last result per PRD); re-run to refresh.
func (m *Manager) RunScan(prdID string) (*scan.Result, error) {
	m.mu.Lock()
	cfg := m.cfg
	grader := m.graderFn
	m.mu.Unlock()

	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.ProjectDir == "" {
		return nil, fmt.Errorf("prd %q has no project_dir set", prdID)
	}

	sc := cfg.Scan
	var scanners []scan.Scanner
	if sc.SASTEnabled {
		scanners = append(scanners, scan.NewSASTScanner())
	}
	if sc.SecretsEnabled {
		scanners = append(scanners, scan.NewSecretsScanner())
	}
	if sc.DepsEnabled {
		scanners = append(scanners, scan.NewDepsScanner())
	}
	// BL406 Phase 1 — the fourth, data-driven scan category. Runs
	// whenever there are rules configured, independent of the three
	// boolean toggles above (project rules aren't security scanners).
	if len(sc.ProjectRules) > 0 {
		scanners = append(scanners, scan.NewProjectRulesScanner(sc.ProjectRules, buildPRDParityContext(prd)))
	}

	gradeFn := grader
	if !sc.RulesGraderEnabled {
		gradeFn = nil
	}

	r := scan.Run(prd.ProjectDir, sc, scanners, gradeFn)
	r.PRDID = prdID
	m.scanResults.Store(prdID, &r)
	m.fireUpstreamIssueActions(sc.ProjectRules, r.Findings)
	return &r, nil
}

// GetScanResult returns the latest cached scan result for a PRD.
func (m *Manager) GetScanResult(prdID string) (*scan.Result, bool) {
	v, ok := m.scanResults.Load(prdID)
	if !ok {
		return nil, false
	}
	r, ok := v.(*scan.Result)
	return r, ok
}

// CreateFixPRD creates a child PRD whose spec targets the violations in the
// last cached scan result for prdID. Returns the new child PRD.
func (m *Manager) CreateFixPRD(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	v, hasScan := m.scanResults.Load(prdID)
	if !hasScan {
		return nil, fmt.Errorf("no scan result for prd %q; run scan first", prdID)
	}
	result, _ := v.(*scan.Result)
	if result == nil || len(result.Findings) == 0 {
		return nil, fmt.Errorf("no findings to fix in prd %q", prdID)
	}
	spec := scan.BuildFixSpec(result.Findings)
	child, err := m.store.CreatePRD(spec, prd.ProjectDir, prd.Backend, prd.Model, prd.Effort)
	if err != nil {
		return nil, err
	}
	child.ParentPRDID = prdID
	if err := m.store.SavePRD(child); err != nil {
		return nil, err
	}
	return child, nil
}

// ProposeRuleEdits calls the rule-editor LLM to suggest AGENT.md changes
// based on the latest scan findings for prdID.
func (m *Manager) ProposeRuleEdits(prdID string) (string, error) {
	m.mu.Lock()
	fn := m.ruleEditorFn
	m.mu.Unlock()
	if fn == nil {
		return "", fmt.Errorf("rule editor not configured (set rules_grader_enabled + ask-compatible backend)")
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return "", fmt.Errorf("prd %q not found", prdID)
	}
	v, hasScan := m.scanResults.Load(prdID)
	if !hasScan {
		return "", fmt.Errorf("no scan result for prd %q; run scan first", prdID)
	}
	result, _ := v.(*scan.Result)
	if result == nil || len(result.Findings) == 0 {
		return "", fmt.Errorf("no findings to propose rules for in prd %q", prdID)
	}
	return fn(result.Findings, prd.ProjectDir)
}

// ── BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills ──────────

// ListTypes returns the current type registry (builtin + operator-registered).
func (m *Manager) ListTypes() []AutomatonType {
	m.typesMu.Lock()
	defer m.typesMu.Unlock()
	out := make([]AutomatonType, len(m.types))
	copy(out, m.types)
	return out
}

// RegisterType adds or replaces a type in the registry. ID must be non-empty.
func (m *Manager) RegisterType(t AutomatonType) error {
	if t.ID == "" {
		return fmt.Errorf("type id required")
	}
	m.typesMu.Lock()
	defer m.typesMu.Unlock()
	for i, existing := range m.types {
		if existing.ID == t.ID {
			m.types[i] = t
			return nil
		}
	}
	m.types = append(m.types, t)
	return nil
}

// SetPRDType updates the Type field on a PRD (hot-path, no executor needed).
func (m *Manager) SetPRDType(prdID, typ string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Type = typ
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDGuidedMode toggles Guided Mode on a PRD.
func (m *Manager) SetPRDGuidedMode(prdID string, guided bool) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.GuidedMode = guided
	// BL406 Phase 5 — B111's real fix. migrateGuidedMode applies this same
	// rule at store-load time; applying it live here too means toggling
	// the checkbox actually gates tasks immediately, not only after the
	// next daemon restart reloads the PRD from disk.
	if guided {
		if prd.GuidedModeSource == "" {
			prd.GuidedModeSource = GuidedModeOperator
		}
	} else {
		prd.GuidedModeSource = ""
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDSkills replaces the skills list on a PRD.
func (m *Manager) SetPRDSkills(prdID string, skills []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Skills = skills
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDConcurrency (BL370) sets the per-PRD max_concurrent_tasks override.
// n <= 0 clears the override (falls back to global config).
func (m *Manager) SetPRDConcurrency(prdID string, n int) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if n < 0 {
		n = 0
	}
	prd.MaxConcurrentTasks = n
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDContinueOnStoryFailure sets the per-PRD override for whether the
// executor continues into later, independent stories after one story
// fails. nil would mean "inherit the daemon default" but a REST/MCP call
// always supplies an explicit true/false, so this always sets an override.
func (m *Manager) SetPRDContinueOnStoryFailure(prdID string, continueOnFailure bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.ContinueOnStoryFailure = &continueOnFailure
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// SetPRDGit (BL407 Phase 5) sets the per-PRD Git.AutoPR/BaseBranch
// overrides. Both fields are set unconditionally on every call (same
// shape as SetPRDGuardrails' multi-field replace) — a caller wanting
// to change only one of the two reads the current PRD first, same as
// every other multi-field setter in this file. Branch/URL/Provider/
// PRURL are read-only here: they're runtime state the executor and
// git-completion hooks own, not operator-settable config.
func (m *Manager) SetPRDGit(prdID string, autoPR bool, baseBranch string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.Git.AutoPR = autoPR
	prd.Git.BaseBranch = baseBranch
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// SetPRDPriority sets the PRD's capacity-queue priority.
func (m *Manager) SetPRDPriority(prdID string, n int) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Priority = n
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDDirs sets the per-PRD read-only and writable directory scope. Nil
// leaves a list unchanged; an empty non-nil slice clears it.
func (m *Manager) SetPRDDirs(prdID string, readDirs, writeDirs []string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if err := ValidateScopeDirs(readDirs); err != nil {
		return err
	}
	if err := ValidateScopeDirs(writeDirs); err != nil {
		return err
	}
	if readDirs != nil {
		prd.ReadDirs = cleanDirs(readDirs)
	}
	if writeDirs != nil {
		prd.WriteDirs = cleanDirs(writeDirs)
	}
	prd.ScopeWarnings = LintPlanScope(prd, prd.Story)
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}

// SetPRDQualityGates (BL367) sets per-PRD quality gate config.
func (m *Manager) SetPRDQualityGates(prdID string, enabled bool, testCommand string, timeout int, blockOnRegression bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.QualityGates = &pipeline.QualityGateConfig{
		Enabled:           enabled,
		TestCommand:       testCommand,
		Timeout:           timeout,
		BlockOnRegression: blockOnRegression,
	}
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// SetPRDGuardrails (BL303 S2 T06; BL418 adds perPRD) sets per-Automaton
// guardrail overrides. blockOnFileDrift is assigned as-is like the
// other overrides here (nil = inherit profile/global).
func (m *Manager) SetPRDGuardrails(prdID, profile string, perTask, perStory, perPRD []string, blockOnFileDrift *bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.GuardrailProfile = profile
	prd.PerTaskGuardrails = perTask
	prd.PerStoryGuardrails = perStory
	prd.PerPRDGuardrails = perPRD
	prd.BlockOnFileDrift = blockOnFileDrift
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// CreateGuardrailProfile (BL303 S2 T05) creates a new guardrail profile.
func (m *Manager) CreateGuardrailProfile(name, description string, guardrails []string, blockOnFileDrift *bool) (*GuardrailProfile, error) {
	if name == "" {
		return nil, fmt.Errorf("profile name is required")
	}
	p := &GuardrailProfile{
		ID:               newProfileID(),
		Name:             name,
		Description:      description,
		Guardrails:       guardrails,
		BlockOnFileDrift: blockOnFileDrift,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	return p, m.store.SaveGuardrailProfile(p)
}

// UpdateGuardrailProfile (BL303 S2 T05) updates an existing profile.
// A nil blockOnFileDrift leaves the profile's current value unchanged.
func (m *Manager) UpdateGuardrailProfile(id, name, description string, guardrails []string, blockOnFileDrift *bool) (*GuardrailProfile, error) {
	p, ok := m.store.GetGuardrailProfile(id)
	if !ok {
		return nil, fmt.Errorf("guardrail profile %q not found", id)
	}
	if name != "" {
		p.Name = name
	}
	p.Description = description
	p.Guardrails = guardrails
	if blockOnFileDrift != nil {
		p.BlockOnFileDrift = blockOnFileDrift
	}
	p.UpdatedAt = time.Now()
	return p, m.store.SaveGuardrailProfile(p)
}

// DeleteGuardrailProfile (BL303 S2 T05) removes a profile.
func (m *Manager) DeleteGuardrailProfile(id string) error {
	return m.store.DeleteGuardrailProfile(id)
}

// ListGuardrailProfiles (BL303 S2 T05) returns all profiles.
func (m *Manager) ListGuardrailProfiles() []*GuardrailProfile {
	return m.store.ListGuardrailProfiles()
}

// CreateRetryProfile (BL410 Phase 1) creates a new retry profile.
func (m *Manager) CreateRetryProfile(name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (*RetryProfile, error) {
	if name == "" {
		return nil, fmt.Errorf("profile name is required")
	}
	p := &RetryProfile{
		ID:                      newProfileID(),
		Name:                    name,
		Description:             description,
		Budgets:                 budgets,
		CeilingTotal:            ceilingTotal,
		InfraRetryEscalateAfter: infraRetryEscalateAfter,
		ContentAutoFixEnabled:   contentAutoFixEnabled,
		AutoFixSkipApproval:     autoFixSkipApproval,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}
	return p, m.store.SaveRetryProfile(p)
}

// UpdateRetryProfile (BL410 Phase 1) updates an existing retry profile.
func (m *Manager) UpdateRetryProfile(id, name, description string, budgets map[string]int, ceilingTotal, infraRetryEscalateAfter int, contentAutoFixEnabled, autoFixSkipApproval *bool) (*RetryProfile, error) {
	p, ok := m.store.GetRetryProfile(id)
	if !ok {
		return nil, fmt.Errorf("retry profile %q not found", id)
	}
	if name != "" {
		p.Name = name
	}
	p.Description = description
	p.Budgets = budgets
	p.CeilingTotal = ceilingTotal
	p.InfraRetryEscalateAfter = infraRetryEscalateAfter
	p.ContentAutoFixEnabled = contentAutoFixEnabled
	p.AutoFixSkipApproval = autoFixSkipApproval
	p.UpdatedAt = time.Now()
	return p, m.store.SaveRetryProfile(p)
}

// DeleteRetryProfile (BL410 Phase 1) removes a retry profile.
func (m *Manager) DeleteRetryProfile(id string) error {
	return m.store.DeleteRetryProfile(id)
}

// ListRetryProfiles (BL410 Phase 1) returns all retry profiles.
func (m *Manager) ListRetryProfiles() []*RetryProfile {
	return m.store.ListRetryProfiles()
}

// GetRetryProfile (BL410 Phase 1) returns one retry profile by id.
func (m *Manager) GetRetryProfile(id string) (*RetryProfile, bool) {
	return m.store.GetRetryProfile(id)
}

// SetPRDRetryPolicy (BL410 Phase 1) sets per-Automaton retry-policy
// overrides, mirroring SetPRDGuardrails' shape exactly.
func (m *Manager) SetPRDRetryPolicy(prdID, profile string, policy map[string]int, ceilingTotal int, contentAutoFixEnabled, autoFixSkipApproval *bool) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	prd.RetryProfile = profile
	prd.RetryPolicy = policy
	prd.RetryCeilingTotal = ceilingTotal
	prd.ContentAutoFixEnabled = contentAutoFixEnabled
	prd.AutoFixSkipApproval = autoFixSkipApproval
	prd.UpdatedAt = time.Now()
	return prd, m.store.SavePRD(prd)
}

// GetGuardrailProfile (BL303 S2 T05) returns one profile by ID.
func (m *Manager) GetGuardrailProfile(id string) (*GuardrailProfile, bool) {
	return m.store.GetGuardrailProfile(id)
}
