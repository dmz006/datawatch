// BL24 — central coordinator. Wires the store, decomposer, and the
// background loop. Mirrors nightwire/autonomous/manager.py shape.

package autonomous

import (
	"context"
	"fmt"
	"sync"
	"time"
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
	Enabled              bool   `json:"enabled"`
	PollIntervalSeconds  int    `json:"poll_interval_seconds,omitempty"`
	MaxParallelTasks     int    `json:"max_parallel_tasks,omitempty"`
	DecompositionBackend string `json:"decomposition_backend,omitempty"`
	VerificationBackend  string `json:"verification_backend,omitempty"`
	DecompositionEffort  string `json:"decomposition_effort,omitempty"`
	VerificationEffort   string `json:"verification_effort,omitempty"`
	// v5.26.16 — operator-pinned model overrides paired with the
	// backend fields above. Empty = backend default.
	DecompositionModel   string `json:"decomposition_model,omitempty"`
	VerificationModel    string `json:"verification_model,omitempty"`
	StaleTaskSeconds     int    `json:"stale_task_seconds,omitempty"`
	AutoFixRetries       int    `json:"auto_fix_retries,omitempty"`
	SecurityScan         bool   `json:"security_scan,omitempty"`

	// BL191 Q4 (v5.9.0) — recursive child-PRD knobs. MaxRecursionDepth
	// caps the parent→child PRD chain length (default 5; 0 disables
	// recursion entirely). AutoApproveChildren skips the
	// needs_review→approved gate for spawned children — needed for any
	// useful recursion, since otherwise every level hangs on operator
	// review. Defaults: depth=5, auto_approve=true.
	MaxRecursionDepth     int  `json:"max_recursion_depth,omitempty"`
	AutoApproveChildren   bool `json:"auto_approve_children,omitempty"`

	// BL191 Q6 (v5.10.0) — guardrails at story + task level. Empty list =
	// disabled at that level (PRD-level guardrails are handled by the
	// BL117 orchestrator). When non-empty, the executor invokes each
	// named guardrail after the corresponding unit completes; a `block`
	// verdict halts the parent PRD with status PRDBlocked. The
	// well-known guardrail names match the orchestrator: rules,
	// security, release-readiness, docs-diagrams-architecture.
	PerTaskGuardrails  []string `json:"per_task_guardrails,omitempty"`
	PerStoryGuardrails []string `json:"per_story_guardrails,omitempty"`

	// Phase 3 (v5.26.61) — per-story approval gate. When true, PRD-
	// level approval transitions every story to "awaiting_approval"
	// and the runner skips those until the operator approves each.
	// Default false preserves the v5.26.x behavior (PRD approval
	// implicitly approves every story).
	PerStoryApproval bool `json:"per_story_approval,omitempty"`
}

// DefaultConfig returns sane defaults — autonomous OFF until operator opts in.
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		PollIntervalSeconds: 30,
		MaxParallelTasks:    3,
		AutoFixRetries:      1,
		SecurityScan:        true,
		MaxRecursionDepth:   5,
		AutoApproveChildren: true,
	}
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
}

// Manager is the public façade for the autonomous package.
type Manager struct {
	mu        sync.Mutex
	cfg       Config
	store       *Store
	decompose   DecomposeFn
	guardrail   GuardrailFn
	onPRDUpdate PRDUpdateFn

	// v5.26.19 — F10 profile resolver for PRD profile validation.
	// Injected from main.go so internal/autonomous stays free of
	// internal/profile dependencies. Nil = profile attachment skips
	// validation (operator gets an unchecked profile name).
	profileResolver ProfileResolver

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

// PRDUpdateFn (v5.24.0) is fired on every successful PRD persist.
// main.go wires this to a WS broadcast so the PWA Autonomous tab
// auto-refreshes on PRD changes (operator-reported regression of the
// pre-v5.24.0 manual-Refresh-button workflow). Empty PRD pointer
// signals a deletion (id supplied separately for the broadcast).
type PRDUpdateFn func(prdID string, prd *PRD)

// SetOnPRDUpdate (v5.24.0) wires the WS broadcast indirection. Nil =
// no broadcast (silent fallback to per-tab manual Refresh).
func (m *Manager) SetOnPRDUpdate(fn PRDUpdateFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPRDUpdate = fn
}

// notifyPRDUpdate is the internal trampoline. Safe under the
// manager's mu so callers don't need to release first.
func (m *Manager) notifyPRDUpdate(prdID string, prd *PRD) {
	m.mu.Lock()
	fn := m.onPRDUpdate
	m.mu.Unlock()
	if fn != nil {
		fn(prdID, prd)
	}
}

// EmitPRDUpdate (v5.24.0) is the explicit broadcast trigger for
// callers that wrote through Store.SavePRD directly and want the WS
// broadcast to fire. The Manager's own mutator methods (Approve,
// Reject, Decompose, etc.) call this after a successful save; the
// `Store` layer is unaware of the WS path and stays decoupled.
func (m *Manager) EmitPRDUpdate(prdID string) {
	prd, _ := m.store.GetPRD(prdID)
	m.notifyPRDUpdate(prdID, prd)
}

// NewManager constructs the Manager and opens the store under dataDir.
func NewManager(dataDir string, cfg Config, decompose DecomposeFn) (*Manager, error) {
	st, err := NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfg:       cfg,
		store:     st,
		decompose: decompose,
	}, nil
}

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

// CreatePRD records a draft PRD without decomposing — call Decompose
// next or pass to Run() which decomposes lazily.
func (m *Manager) CreatePRD(spec, projectDir, backend string, effort Effort) (*PRD, error) {
	return m.store.CreatePRD(spec, projectDir, backend, effort)
}

// Decompose calls the LLM via the configured DecomposeFn and persists
// the resulting story tree. Returns the updated PRD.
//
// BL191 (v5.2.0) — status transitions: PRDDraft (or PRDRevisionsAsked)
// → PRDDecomposing → PRDNeedsReview. The operator must explicitly
// Approve before Run is allowed; see Manager.Approve and Manager.Run.
// Decomposition records a Decision row on the PRD with the LLM call
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
	backend := prd.Backend
	if backend == "" {
		backend = m.cfg.DecompositionBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.DecompositionEffort)
	}
	// Mark in-flight so the operator can see it from /api/autonomous/prds/{id}.
	prd.Status = PRDDecomposing
	prd.UpdatedAt = time.Now()
	_ = m.store.SavePRD(prd)

	prompt := fmt.Sprintf(DecompositionPrompt, prd.Spec)
	raw, err := m.decompose(DecomposeRequest{Spec: prompt, Backend: backend, Effort: effort})
	if err != nil {
		// Roll back to draft so the operator can re-trigger.
		prd.Status = PRDDraft
		prd.UpdatedAt = time.Now()
		_ = m.store.SavePRD(prd)
		return nil, fmt.Errorf("LLM decompose: %w", err)
	}
	title, stories, err := ParseDecomposition(raw)
	if err != nil {
		prd.Status = PRDDraft
		prd.UpdatedAt = time.Now()
		_ = m.store.SavePRD(prd)
		return nil, err
	}
	if title != "" {
		prd.Title = title
	}
	// BL191 Q1: operator review/approve gate. Status lands in needs_review
	// rather than active/running. Approve / Reject / RequestRevision are
	// the explicit transitions out.
	prd.Status = PRDNeedsReview
	prd.Decisions = append(prd.Decisions, Decision{
		At:            time.Now(),
		Kind:          "decompose",
		Backend:       backend,
		PromptChars:   len(prompt),
		ResponseChars: len(raw),
		Actor:         "autonomous",
	})
	if err := m.store.SetStories(prdID, stories); err != nil {
		return nil, err
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
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
	prd.Status = PRDApproved
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
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
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
	prd.Status = PRDRejected
	prd.RejectionReason = reason
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "reject", Actor: actor, Note: reason})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
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
	prd.Status = PRDRevisionsAsked
	prd.RevisionsRequested++
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "request_revision", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
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
// before approving the PRD. Only allowed in needs_review or
// revisions_asked. Records a Decision so the timeline shows the edit.
func (m *Manager) EditTaskSpec(prdID, taskID, newSpec, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept task edits", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
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

// EditStory (v5.26.32) lets the operator rewrite an LLM-decomposed
// story's title + description before approving the PRD. Mirrors
// EditTaskSpec — only allowed in needs_review or revisions_asked,
// records a Decision so the timeline shows the edit. Operator-asked:
// "i don't see a story review or approval or story edit option."
func (m *Manager) EditStory(prdID, storyID, newTitle, newDescription, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept story edits", prdID, prd.Status)
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
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept story profile changes", prdID, prd.Status)
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
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept story file edits", prdID, prd.Status)
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
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept task file edits", prdID, prd.Status)
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

// SetTaskLLM (BL203, v5.4.0) lets the operator override a task's
// worker LLM (backend / effort / model) before approval. Empty string
// clears the override (falls back to PRD-level then global). Allowed
// in needs_review / revisions_asked only — once approved or running
// the worker is locked in.
func (m *Manager) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept LLM overrides", prdID, prd.Status)
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

// SetPRDLLM (BL203, v5.4.0) overrides the PRD-level worker LLM defaults
// (backend / effort / model). Only allowed pre-Run. Tasks without
// per-task overrides will inherit these.
func (m *Manager) SetPRDLLM(prdID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDRunning || prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is locked; PRD LLM overrides only apply pre-Run", prdID, prd.Status)
	}
	prd.Backend = backend
	prd.Effort = Effort(effort)
	prd.Model = model
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_llm", Actor: actor,
		Note: fmt.Sprintf("backend=%s effort=%s model=%s", backend, effort, model),
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
	newPRD, err := m.store.CreatePRD(subst(tmpl.Spec), tmpl.ProjectDir, tmpl.Backend, tmpl.Effort)
	if err != nil {
		return nil, err
	}
	newPRD.Title = subst(tmpl.Title)
	newPRD.TemplateOf = tmpl.ID
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
			}
		}
	}
	if len(verdictCounts) > 0 {
		st.VerdictCounts = verdictCounts
	}
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

// run is the loop body. v1 is intentionally minimal: marks newly-
// active PRDs so external schedulers (e.g. main daemon's
// pipeline.Executor or operator-driven `datawatch autonomous run`)
// can pick them up. The actual Task → Session dispatch is done by
// the executor in executor.go (called from REST handler).
func (m *Manager) run() {
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
