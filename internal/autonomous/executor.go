// BL24 — Task executor. Walks a PRD's story/task DAG in dependency
// order, dispatches each task via the injected SpawnFn (which the
// daemon wires to session.Manager.Start under F10), and records the
// returned session ID + final status.
//
// Verification (BL25) runs after the task reports complete via the
// injected VerifyFn. Failed verification triggers up to AutoFixRetries
// re-prompts of the same backend with the verifier's findings prepended.

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/pipeline"
	"github.com/dmz006/datawatch/internal/tooling"
)

// SpawnRequest is what the executor hands to SpawnFn for each task.
//
// BL203 (v5.4.0) — Backend / Effort / Model now reflect the resolved
// per-task override (most-specific wins). The executor honors
// Task.{Backend,Effort,Model}, then PRD.{Backend,Effort,Model}, then
// the SpawnFn's own fallback (typically session.llm_backend global).
type SpawnRequest struct {
	TaskID     string
	StoryID    string
	PRDID      string
	Title      string
	Spec       string
	ProjectDir string
	Backend    string
	Effort     Effort
	Model      string
	// ThinkingLevel — pi --thinking level (off|minimal|low|medium|high),
	// resolved per-task → per-story → per-PRD by the executor. Empty =
	// no override; the session manager then applies its configured
	// pi.thinking_level (lowest precedence in the chain).
	ThinkingLevel string
	// PermissionMode (v5.27.5) — claude-code --permission-mode forwarded
	// to the worker session. Resolved per-task → per-PRD → empty (use
	// global cfg.Session.permission_mode default) by the executor before
	// the SpawnFn fires.
	PermissionMode string
	RetryHint      string // populated on retry: prior verifier's findings
	// v5.26.19 — F10 profile dispatch. When ClusterProfile is set,
	// SpawnFn dispatches the worker to the cluster (POST /api/agents)
	// instead of a local tmux session. ProjectProfile alone causes the
	// worker to clone the profile's git URL into ProjectDir before
	// running the task. Either or both can be empty.
	ProjectProfile string
	ClusterProfile string
	// Branch (BL407 Phase 4) — PRD.Git.Branch, threaded through so a
	// cluster-dispatched SpawnFn can forward it as agents.SpawnRequest.Branch.
	// The worker then creates/checks out this exact branch (worktree
	// mode's own branch, mirrored into cluster mode) instead of
	// silently landing on whatever branch its Project Profile's
	// GitSpec names. Empty for a PRD that never reaches either Phase
	// 1's EnsureWorktree or this phase's cluster-mode counterpart
	// (e.g. AutoPR-less PRDs with an explicit ProjectDir).
	Branch string
	// Git — true when the PRD has git settings (prd.Git populated — a
	// worktree-set PRD has Branch — or a project profile, i.e. it runs
	// against a project repo). The local-session SpawnFn passes this to
	// StartOptions.Git so PRD task sessions get git activity (pre-session
	// snapshot + completion commit), because PRDs are development work.
	Git bool
	// BL244 — plugin session injection. Non-empty when a plugin declares
	// session_injection for the PRD's type; prepended to the task spec
	// by the SpawnFn so plugin context arrives in the worker session.
	ContextPrepend string
	// LSPLanguage (v8.7.1) — language server preset for OpenCode backends.
	// Resolved per-task → per-PRD → empty (no LSP).
	LSPLanguage string
	// MemorySeed (BL386 Phase 1) — warm-start seeding config, copied from
	// the PRD's MemorySeedConfig. Zero value = seeding disabled.
	MemorySeed MemorySeedConfig
	// SkipDirtyCapture — true when ProjectDir is a PRD-owned git
	// worktree (PRD.Git.Branch set). A fresh per-Automaton worktree has
	// no operator edits to protect, so the SpawnFn skips the pre-task
	// dirty-file capture (CapturePreTaskDirtyFiles) for it.
	SkipDirtyCapture bool
	// WriteScope (auto-commit leak fix) — the task's effective write
	// directories (EffectiveTaskWriteDirs: task.ProjectDir override
	// else PRD.WriteDirs). When non-empty, the local-session SpawnFn
	// forwards them to the session manager so automatic commits stage
	// only files under these directories instead of `git add -A` of the
	// whole repo. Nil/empty = stage only files changed since the session
	// (run) started (warns).
	WriteScope []string
	// AttemptRefs — the task's preserved attempt snapshot refs, in order
	// (Task.AttemptRefs). The SpawnFn restores the last one into ProjectDir
	// before the worker starts when preserve_attempt_work is enabled.
	AttemptRefs []string
	// CleanRestart — Task.CleanRestart at spawn time. The SpawnFn skips the
	// attempt-work restore when set; the executor clears the task's flag once
	// this spawn has run.
	CleanRestart bool
	// ComputeNode — the compute node this task's worker lease was granted on
	// (capacity.Lease.Node, read after admit succeeds). The local-session
	// SpawnFn forwards it as compute_node with the backend, so the session
	// runs on the node the ledger placed the task on. Empty when capacity is
	// off or the lease names no node; the session then resolves its node as
	// it always has.
	ComputeNode string
}

// prdHasGit reports whether a PRD has git settings: prd.Git populated
// (provider/url/base_branch/branch/auto_pr or a project repo) or a
// project profile, i.e. the PRD runs against a project repo. PRDs with
// git settings are development work, so their task sessions opt into
// git activity (StartOptions.Git).
func prdHasGit(prd *PRD) bool {
	if prd == nil {
		return false
	}
	return prd.Git != (GitWorkflow{}) || prd.ProjectProfile != ""
}

// prdAutoCommits reports whether the PRD should give its task sessions git
// activity (StartOptions.Git), which auto-commits the run's work. Rule: a PRD
// gets a session auto-commit only when it has git settings AND either runs in
// worktree mode (its commit scope is the worktree itself) or declares the
// write dirs a commit may cover. A direct-mode PRD (no worktree) with no
// declared write dirs must NOT auto-commit: with no declared scope the commit
// would widen to the whole project directory and stage everything changed
// there, committing work the PRD never claimed ownership of.
func prdAutoCommits(prd *PRD) bool {
	if !prdHasGit(prd) {
		return false
	}
	return prd.Git.Branch != "" || len(prd.WriteDirs) > 0
}

// SpawnResult is what SpawnFn returns. SessionID is the datawatch
// session ID; Err is fatal (non-fatal failures should be encoded in
// VerifyResult.OK=false from VerifyFn instead).
type SpawnResult struct {
	SessionID string
	// PreTaskSHA is the git HEAD SHA captured immediately before the worker
	// session started. The verifier uses it to produce git diff grounding
	// evidence (BL366). Empty when ProjectDir is not a git repo or when the
	// task is cluster-dispatched to a remote worker.
	PreTaskSHA string
	// PreTaskUntrackedFiles is the list of untracked files (git ls-files --others)
	// captured alongside PreTaskSHA. Used by the verifier to detect newly created
	// files that are not yet tracked by git (v8.25.12).
	PreTaskUntrackedFiles []string
	// PreTaskDirtyFiles is the list of tracked files with unstaged OR
	// staged-but-uncommitted modifications (git diff --name-only HEAD ∪
	// git diff --name-only --cached HEAD) captured alongside PreTaskSHA.
	// Used by the foreign-file-drift check (DetectForeignFileDrift).
	PreTaskDirtyFiles []string
	// ComputeNode is the compute node the start handler placed the worker on,
	// which differs from SpawnRequest.ComputeNode when the handler re-placed
	// the session. Empty when the spawn path does not report one.
	ComputeNode string
	Err         error
}

// SpawnFn is the indirection used by the executor to start a worker.
type SpawnFn func(ctx context.Context, req SpawnRequest) (SpawnResult, error)

// VerifyFn is the BL25 hook. It receives the just-completed task and
// returns a VerificationResult. Daemon wires this to the BL103
// validator agent (or a fresh session.Manager.Start with a different
// backend); tests inject a fake.
type VerifyFn func(ctx context.Context, prd *PRD, task *Task) (VerificationResult, error)

// Run walks the PRD and runs every task to completion (or failure).
// Honors task DependsOn for ordering. When prd.MaxConcurrentTasks (or
// cfg.MaxConcurrentTasks) is > 1, independent tasks are fanned out with
// a semaphore-bounded goroutine pool.
func (m *Manager) Run(ctx context.Context, prdID string, spawn SpawnFn, verify VerifyFn) error {
	// wake carries operator review decisions to a live executor. It is nil
	// outside one, and a nil channel never delivers a decision.
	wake := reviewWakeFrom(ctx)
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	if prd.IsTemplate {
		return fmt.Errorf("prd %q is a template; instantiate it first", prdID)
	}
	if len(prd.Story) == 0 {
		return fmt.Errorf("prd %q has no stories — call Decompose first", prdID)
	}
	// BL191 Q1 (v5.2.0) — Run requires explicit operator approval. Legacy
	// PRDActive value (pre-v5.2.0 stores) is honored for back-compat so
	// upgraded daemons don't strand in-flight work.
	//
	// The Store's mutex only protects its id->*PRD map; once a caller holds
	// a *PRD, field access on it (e.g. .Status) is unsynchronized against
	// any other goroutine holding the same pointer (GetPRD/ListPRDs never
	// copy). resumeRunningPRDs (api.go) reads prd.Status on every stored
	// PRD from a background goroutine right after SetExecutors — racing
	// with an immediate Run() call on a PRD that's still Approved, both
	// touching the same shared pointer. Scoped fix: serialize the
	// read-check-write of prd.Status here against resumeRunningPRDs'
	// read via the Manager's own mutex (does not address unsynchronized
	// access to other PRD/Task fields elsewhere).
	m.mu.Lock()
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning {
		status := prd.Status
		m.mu.Unlock()
		return fmt.Errorf("prd %q status %q is not runnable; call /approve first", prdID, status)
	}
	// BL387 Phase 2b — cross-PRD seeding at first run.
	// Only fires when PRD is transitioning from approved (not a resume from Running).
	// BL469 — refuse to start while another running PRD holds this tree.
	if err := m.admitTree(prd); err != nil {
		m.mu.Unlock()
		return err
	}
	// Model/backend preflight — refuse to start a PRD whose task would spawn
	// a pair that cannot run (e.g. pi-prompt + bare claude-haiku-5-5). The
	// PRD stays approved and nothing is dispatched.
	if err := m.preflightModelBackend(prdID, prd); err != nil {
		m.mu.Unlock()
		return err
	}
	isFirstRun := prd.Status == PRDApproved || prd.Status == PRDActive
	runOld := prd.Status
	m.store.WithLock(func() { prd.Status = PRDRunning })
	m.mu.Unlock()
	// BL427 — the running event fires only once the "run" Decision
	// below is saved; an early return (worktree setup) emits nothing.
	emitRunning := func() { m.emitPRDStatus(prd, runOld, lastDecision(prd, "run"), nil) }
	// BL407 Phase 1 — a PRD with no explicit ProjectDir/ProjectProfile/
	// ClusterProfile never has to run bare in the daemon's own
	// checkout: when the operator has configured WorktreeBaseRepo,
	// create (or, on a resumed run, reuse — EnsureWorktree is
	// idempotent) an isolated git worktree and point this PRD at it.
	// Self-gating on ProjectDir being empty rather than isFirstRun:
	// once a worktree is created, ProjectDir is persisted non-empty
	// below, so a later resume naturally skips this without a
	// separate "already did this" flag. Must run before
	// EnsureIgnoredPatterns/cross-seed below, both of which read
	// prd.ProjectDir. Shared with Decompose()/DecomposeStreaming()
	// (found live 2026-10-11: decompose needs a real project_dir too,
	// for session-based planning backends — this used to be the only
	// place a worktree got created, so decompose could never succeed
	// for a worktree-mode PRD at all).
	if created, werr := m.ensureWorktreeIfConfigured(prd); werr != nil {
		return fmt.Errorf("prd %q worktree setup: %w", prdID, werr)
	} else if created {
		log.Printf("[autonomous] prd=%s worktree created at %s (branch=%s)", prdID, prd.ProjectDir, prd.Git.Branch)
	}
	// BL407 Phase 4 — cluster-dispatched counterpart to the worktree
	// branch above. No local worktree to create here (the worker's
	// own container does the real clone — Phase 3), but the PRD still
	// needs its own dedicated branch name decided up front, by the
	// parent, before the first dispatch: SpawnFn threads it through
	// as agents.SpawnRequest.Branch, and the worker creates it fresh
	// on first clone (worker_clone.go's CreateBranch path) instead of
	// landing on the Project Profile's shared static default branch —
	// closing the Phase 3 limitation (every cluster task on one PRD
	// landing on the same shared branch). Idempotent the same way the
	// worktree branch is: only fires while Git.Branch is still empty,
	// so a resumed run never regenerates a second name.
	if prd.ClusterProfile != "" && prd.Git.Branch == "" {
		prd.Git.Branch = branchNameFor(prd)
		log.Printf("[autonomous] prd=%s cluster-mode branch assigned: %s", prdID, prd.Git.Branch)
	}
	// 2026-10-07 — gitignore datawatch's own PRD scratch artifacts
	// (.decompose-output-*.json, CHECKPOINT.md) as defense-in-depth for
	// any window before relocateCheckpoint/decomposeFnSession's own
	// cleanup runs (e.g. a daemon crash). Once per PRD, not every resume.
	// Not a real LLM backend, so uses EnsureIgnoredPatterns directly
	// rather than registering a pseudo-entry in BackendArtifacts (which
	// QueryAllStatus assumes is exclusively real backends).
	if isFirstRun && prd.ProjectDir != "" {
		if _, err := tooling.EnsureIgnoredPatterns(prd.ProjectDir, automatonScratchIgnorePatterns); err != nil {
			log.Printf("[autonomous] EnsureIgnoredPatterns for %s: %v", prd.ProjectDir, err)
		}
		// 2026-10-15 — BL449 incident: a task with its own ProjectDir
		// override (BL447; e.g. a matrix cell with its own isolated git
		// repo) never got this .gitignore entry, because it was only
		// ever written into prd.ProjectDir. CHECKPOINT.md then leaked
		// into real worker commits. Same per-task directory, once each.
		seen := map[string]bool{prd.ProjectDir: true}
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				dir := ResolveTaskProjectDir(prd, &prd.Story[si].Tasks[ti])
				if dir == "" || seen[dir] {
					continue
				}
				seen[dir] = true
				if _, err := tooling.EnsureIgnoredPatterns(dir, automatonScratchIgnorePatterns); err != nil {
					log.Printf("[autonomous] EnsureIgnoredPatterns for task dir %s: %v", dir, err)
				}
			}
		}
	}
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "run", Actor: "autonomous"})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	emitRunning()
	if isFirstRun && prd.MemorySeed.Enabled && len(prd.MemorySeed.FromPRDs) > 0 {
		m.mu.Lock()
		crossSeedFn := m.memoryCrossSeedFn
		m.mu.Unlock()
		if crossSeedFn != nil {
			maxEntries := prd.MemorySeed.MaxPerScope
			if maxEntries <= 0 {
				maxEntries = 20
			}
			for _, fromID := range prd.MemorySeed.FromPRDs {
				if ferr := crossSeedFn(ctx, fromID, prd.ID, prd.ProjectDir, maxEntries, prd.MemorySeed.RoleFilter); ferr != nil {
					log.Printf("[autonomous] cross-seed: from=%s to=%s: %v", fromID, prd.ID, ferr)
				}
			}
		}
	}
	m.loadSkillGuardrails(prd) // BL303 S2 T08
	tasks := flattenTasks(prd)
	order, err := topoSort(tasks)
	if err != nil {
		return err
	}
	retries := m.cfg.AutoFixRetries
	if retries < 0 {
		retries = 0
	}
	// BL367 — quality gate: capture baseline before first task runs.
	qgCfg := m.resolveQualityGates(prd)
	var qgBaseline *pipeline.TestResult
	if qgCfg.Enabled && qgCfg.TestCommand != "" && prd.ProjectDir != "" {
		r := runQualityGateTests(qgCfg.TestCommand, prd.ProjectDir, qgCfg.EffectiveTimeout(), m.allowedProjectRoots())
		qgBaseline = &r
		log.Printf("[autonomous] %s quality-gate baseline: %d pass / %d fail", prdID, r.PassCount, r.FailCount)
		// Validate the baseline before trusting it. An invalid one
		// (command/build/environment failure, e.g. a generated file missing
		// from a worktree) is recorded on the PRD with the plain reason and
		// logged as a warning; from here on the gate warns only and never
		// blocks or retries a task on numbers we cannot trust.
		if valid, reason := pipeline.ClassifyBaseline(r); !valid {
			m.markGateInvalid(prdID, prd, reason)
			log.Printf("[autonomous] %s WARNING: quality-gate baseline invalid (%s) — gate warns only and never blocks a task", prdID, reason)
		}
	}
	// BL447: the PRD-level baseline is only comparable for tasks that run
	// in prd.ProjectDir; tasks with another resolved dir get a lazily
	// captured, per-dir baseline cached for the rest of this run.
	qgBaselines := newQualityGateBaselines(qgCfg, prd.ProjectDir, qgBaseline, m.allowedProjectRoots())
	// Lazily captured per-dir baselines get the same validation.
	qgBaselines.onInvalid = func(dir, reason string) {
		m.markGateInvalid(prdID, prd, fmt.Sprintf("task dir %s: %s", dir, reason))
		log.Printf("[autonomous] %s WARNING: quality-gate baseline for dir %s invalid (%s) — gate warns only and never blocks a task", prdID, dir, reason)
	}
	// Gate scope: when the resolved scope is non-full, capture the vet
	// baseline in the PRD's own dir now (other resolved task dirs capture
	// theirs lazily in the same cache), so new in-scope vet failures can
	// be judged regressions against it.
	if qgCfg.Enabled && qgCfg.TestCommand != "" && prd.ProjectDir != "" {
		if scope, _, _ := m.resolveGateScopeSettings(prd); scope != pipeline.GateScopeFull {
			qgBaselines.vetForDir(prd.ProjectDir)
		}
	}

	// BL370 — resolve concurrency limit: per-PRD overrides global config.
	// 0 or 1 = sequential (existing behaviour); > 1 = concurrent pool.
	m.mu.Lock()
	globalConc := m.cfg.MaxConcurrentTasks
	m.mu.Unlock()
	concurrency := effectiveTaskConcurrency(prd.MaxConcurrentTasks, globalConc)
	if concurrency <= 1 {
		// Sequential path (default) — tasks run in dependency order.
		// failedIDs tracks tasks that failed so dependents can be skipped.
		failedIDs := make(map[string]bool)
		// heldIDs holds the tasks held for operator review, and the tasks
		// waiting behind one. Neither runs nor counts as failed.
		heldIDs := make(map[string]bool)
		for i := 0; ; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			// An operator review decision (a retry or a cancel) landed while
			// a task ran. Re-read the Automaton and rebuild the order, then
			// walk it again from the top. Terminal tasks are skipped, held
			// tasks stay pending, and the first task that is neither runs
			// next. The check also runs after the last task, so a decision
			// made during it is not lost before the walk returns.
			nudged := reviewWakeNudged(wake)
			// The walk is done but a task waits on an operator decision. The
			// executor stays alive and walks again when the decision is
			// answered, so the task starts with no second Run.
			if !nudged && i >= len(order) && m.decisionWaitPending(prd) {
				if awaitDecisionAnswer(ctx, wake) {
					if st, ok := m.prdStatusOf(prdID); !ok || st != PRDRunning {
						return nil
					}
					nudged = true
				} else if err := ctx.Err(); err != nil {
					return err
				}
			}
			if nudged {
				if latest, _ := m.store.GetPRD(prdID); latest != nil {
					prd = latest
				}
				var tasks []*Task
				m.store.WithLock(func() { tasks = flattenTasks(prd) })
				if o, err := topoSort(tasks); err != nil {
					log.Printf("[autonomous] %s: rebuild run order after review decision: %v", prdID, err)
				} else {
					order = o
				}
				failedIDs = make(map[string]bool)
				heldIDs = make(map[string]bool)
				i = -1
				continue
			}
			if i >= len(order) {
				break
			}
			tid := order[i]
			t := lookupTask(prd, tid)
			if t == nil {
				continue
			}
			// Skip tasks already in a terminal state (completed/failed/cancelled)
			// so executor restarts don't re-run finished work.
			//
			// v8.36.5 — found live on PRD a2833a5e: only TaskFailed populated
			// failedIDs here, so a task whose dependency was TaskCancelled
			// (e.g. an earlier story the operator cancelled and never
			// restarted) saw an empty depFailed check below and proceeded as
			// if that dependency were satisfied — "Write final
			// recommendations" completed depending on two never-produced
			// files from a cancelled story. A cancelled dependency was never
			// fulfilled either; it must propagate the same as a failed one.
			if m.taskTerminal(t) {
				if t.Status == TaskFailed || t.Status == TaskCancelled {
					failedIDs[tid] = true
				}
				continue
			}
			// A held task, or one waiting behind a held task, stays pending.
			if m.taskOnReview(t) || m.waitsOnReview(prd, t, heldIDs) {
				heldIDs[tid] = true
				continue
			}
			// Skip tasks whose dependencies failed — propagate failure forward.
			var depFailed string
			for _, dep := range t.DependsOn {
				if failedIDs[dep] {
					depFailed = dep
					break
				}
			}
			if depFailed != "" {
				oldStatus := t.Status
				m.store.WithLock(func() {
					t.Status = TaskFailed
				})
				verb := "failed"
				if dt := lookupTask(prd, depFailed); dt != nil && dt.Status == TaskCancelled {
					verb = "was cancelled"
				}
				m.store.WithLock(func() {
					t.Error = "dependency " + depFailed + " " + verb
				})
				if m.store.SaveTask(t) == nil {
					m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				}
				m.releaseCapacity(t.ID)
				failedIDs[tid] = true
			} else if m.decisionGateHold(prd, t) {
				// The spec names a decision that is not answered. The task
				// stays parked and its dependents wait behind it.
				heldIDs[tid] = true
				continue
			} else {
				// Mark the owning story in_progress when its first task begins.
				if s := lookupStoryByTaskID(prd, tid); s != nil && (s.Status == "" || s.Status == StoryPending) {
					m.store.WithLock(func() { s.Status = StoryInProgress })
					_ = m.store.SavePRD(prd)
				}
				if err := m.executeOne(ctx, prd, t, spawn, verify, retries, qgCfg, qgBaselines); err != nil {
					m.releaseCapacity(t.ID)
					oldStatus := t.Status
					m.store.WithLock(func() {
						t.Status = TaskFailed
						t.Error = err.Error()
					})
					if m.store.SaveTask(t) == nil {
						m.emitTaskStatus(prd, t, oldStatus, nil, nil)
					}
				}
			}
			// Re-read authoritative status: executeOne returns nil even after
			// exhausting retries (it saves TaskFailed itself), so we must
			// inspect the stored record rather than relying on the error return.
			latest, _ := m.store.GetPRD(prdID)
			if latest != nil {
				if lt := lookupTask(latest, tid); lt != nil {
					if lt.Status == TaskFailed {
						failedIDs[tid] = true
					}
					if lt.Status == TaskReviewNeeded {
						heldIDs[tid] = true
					}
					// BL191 Q6 (v5.10.0) — when a task lands in TaskBlocked from a
					// per-task guardrail block verdict, halt the walk so the
					// operator can review before more tasks run.
					if lt.Status == TaskBlocked {
						old := latest.Status
						m.store.WithLock(func() {
							latest.Status = judgeHoldOrBlocked(lastBlockVerdict(lt.Verdicts))
						})
						latest.BlockedReason = blockedReasonFromVerdicts(lt.Verdicts)
						if m.store.SavePRD(latest) == nil {
							m.emitPRDStatus(latest, old, nil, lastBlockVerdict(lt.Verdicts))
							m.EmitPRDUpdate(latest.ID)
						}
						return nil
					}
				}
				// Roll up story status when all its tasks reached a terminal state.
				if s := lookupStoryByTaskID(latest, tid); s != nil && m.storyDone(s) {
					m.store.WithLock(func() { s.Status = storyRollupStatus(s) })
					// A story failure halts the PRD by default so the operator
					// can re-edit/rerun the failed story instead of the executor
					// barreling ahead into later, independent stories unattended.
					// Opt out per-PRD or globally via ContinueOnStoryFailure.
					if s.Status == StoryFailed && !m.resolveContinueOnStoryFailure(latest) && m.nonTerminalRemain(latest) {
						old := latest.Status
						m.store.WithLock(func() {
							latest.Status = PRDBlocked
						})
						if m.store.SavePRD(latest) == nil {
							m.emitPRDStatus(latest, old, nil, nil)
							m.EmitPRDUpdate(latest.ID)
						}
						return nil
					}
					_ = m.store.SavePRD(latest)
				}
				prd = latest
			}
		}
	} else {
		// BL370 — concurrent path: goroutine pool bounded by concurrency.
		// Coordinator goroutine is the only writer to completedIDs/failedIDs/inFlight;
		// worker goroutines each own their task pointer (no shared writes).
		type taskResult struct {
			tid string
			err error
		}
		results := make(chan taskResult, len(order))
		completedIDs := make(map[string]bool, len(order))
		failedIDs := make(map[string]bool, len(order))
		inFlight := make(map[string]bool, concurrency)
		// heldIDs holds the tasks held for operator review. They are never
		// completed, so their dependents never become ready.
		heldIDs := make(map[string]bool, len(order))
		remaining := len(order)

		// v8.36.5 — seed tracking for tasks that are already terminal before
		// this Run() call even starts (a prior attempt, or — found live on
		// PRD a2833a5e — an operator-cancelled story that was never
		// restarted). Without this, isReady()'s `!completedIDs[dep]` check
		// never resolves for a dependent of an already-cancelled task: it's
		// not in completedIDs (never ran this pass) and not in failedIDs
		// either, so the dependent never becomes ready — a silent deadlock
		// (the "no ready tasks, nothing in-flight" safety below breaks the
		// loop, but the dependent is left stuck pending, not visibly failed
		// or blocked). Match the sequential path: completed tasks satisfy
		// deps, failed/cancelled ones propagate as unmet.
		for _, tid := range order {
			t := lookupTask(prd, tid)
			if t == nil || !m.taskTerminal(t) {
				continue
			}
			completedIDs[tid] = true
			if t.Status == TaskFailed || t.Status == TaskCancelled {
				failedIDs[tid] = true
			}
			remaining--
		}

		isReady := func(tid string) bool {
			t := lookupTask(prd, tid)
			if t == nil || completedIDs[tid] || inFlight[tid] || heldIDs[tid] {
				return false
			}
			for _, dep := range t.DependsOn {
				if !completedIDs[dep] {
					return false
				}
			}
			return true
		}

		// autoFailDeps marks any pending tasks whose deps are in failedIDs as
		// TaskFailed immediately, so they don't stall the coordinator loop.
		autoFailDeps := func() {
			for _, tid := range order {
				if completedIDs[tid] || inFlight[tid] {
					continue
				}
				t := lookupTask(prd, tid)
				if t == nil {
					continue
				}
				var depFailed string
				for _, dep := range t.DependsOn {
					if failedIDs[dep] {
						depFailed = dep
						break
					}
				}
				if depFailed != "" {
					oldStatus := t.Status
					m.store.WithLock(func() {
						t.Status = TaskFailed
						t.Error = "dependency " + depFailed + " failed"
					})
					if m.store.SaveTask(t) == nil {
						m.emitTaskStatus(prd, t, oldStatus, nil, nil)
					}
					m.releaseCapacity(t.ID)
					failedIDs[tid] = true
					completedIDs[tid] = true
					remaining--
				}
			}
		}

		launch := func() {
			for _, tid := range order {
				if len(inFlight) >= concurrency {
					break
				}
				if completedIDs[tid] || inFlight[tid] {
					continue
				}
				if !isReady(tid) {
					continue
				}
				t := lookupTask(prd, tid)
				if t == nil {
					continue
				}
				// A task whose spec names an unanswered decision stays parked.
				// It never completes, so its dependents never become ready.
				if m.decisionGateHold(prd, t) {
					continue
				}
				if s := lookupStoryByTaskID(prd, tid); s != nil && (s.Status == "" || s.Status == StoryPending) {
					m.store.WithLock(func() { s.Status = StoryInProgress })
					_ = m.store.SavePRD(prd)
				}
				inFlight[tid] = true
				capturedPRD := prd // goroutine captures the prd at launch time
				go func(tid string, t *Task) {
					err := m.executeOne(ctx, capturedPRD, t, spawn, verify, retries, qgCfg, qgBaselines)
					results <- taskResult{tid, err}
				}(tid, t)
			}
		}

		// reviewDecisions brings the coordinator in line with an operator
		// review decision made while tasks were running. The order is fixed
		// when the pass starts, so a task the operator retried, or cancelled,
		// is found here by re-reading the Automaton. remaining is recounted
		// rather than adjusted, because a task held when the pass started was
		// never in the order and never took a slot in remaining. The decision
		// never starts a second executor: launch() stays the only place a task
		// starts.
		reviewDecisions := func() {
			latest, _ := m.store.GetPRD(prdID)
			if latest == nil || latest.Status == PRDPaused {
				// A paused Automaton launches nothing. The result path drains
				// the tasks in flight and returns.
				return
			}
			prd = latest
			// The order leaves out tasks held for review, so a task the
			// operator has since retried is not in it. Rebuild the order from
			// the Automaton as it stands now. A cycle keeps the old order.
			var tasks []*Task
			m.store.WithLock(func() { tasks = flattenTasks(latest) })
			if o, err := topoSort(tasks); err != nil {
				log.Printf("[autonomous] %s: rebuild run order after review decision: %v", prdID, err)
			} else {
				order = o
			}
			for _, tid := range order {
				if inFlight[tid] {
					continue
				}
				lt := lookupTask(latest, tid)
				if lt == nil {
					continue
				}
				switch {
				case lt.Status == TaskReviewNeeded:
					// Still held for review; the operator has not acted yet.
				case m.taskTerminal(lt):
					// Cancelled or otherwise finished by the operator. Mark it
					// finished so its dependents resolve, and fail them when it
					// did not succeed.
					delete(heldIDs, tid)
					completedIDs[tid] = true
					if lt.Status == TaskFailed || lt.Status == TaskCancelled {
						failedIDs[tid] = true
					}
				case normTaskStatus(lt.Status) == TaskPending:
					// Retried by the operator. It runs again, so it is no
					// longer finished or held. An empty status reads as pending.
					delete(heldIDs, tid)
					delete(completedIDs, tid)
					delete(failedIDs, tid)
				}
			}
			// Every task in flight, and every task in the order that is not
			// finished or held, still owes a result. Only results and
			// autoFailDeps move remaining, so it is set from that count.
			remaining = len(inFlight)
			for _, tid := range order {
				if !completedIDs[tid] && !heldIDs[tid] && !inFlight[tid] {
					remaining++
				}
			}
			autoFailDeps()
			launch()
		}

		// Propagate failure from any task whose only dependency is one of
		// the already-terminal failed/cancelled tasks seeded above, before
		// the first launch — otherwise it's merely "not ready" (never
		// marked failed) until some other in-flight completion happens to
		// trigger autoFailDeps again.
		// finalizeDrained persists a task's terminal failure status for a
		// result read off the results channel after the coordinator has
		// already decided to halt (PRDBlocked) — without this, a task that
		// was genuinely still in-flight when a sibling failure (or
		// guardrail block) triggered the halt stays frozen at
		// TaskInProgress forever, even though it actually finished. Found
		// live via TS-695: Story B's scope-guard failure halted the PRD
		// while Story A's task was still running; draining discarded A's
		// result instead of persisting it, orphaning the task indefinitely.
		finalizeDrained := func(dr taskResult) {
			if dr.err == nil {
				return
			}
			if t := lookupTask(prd, dr.tid); t != nil && !m.taskTerminal(t) {
				oldStatus := t.Status
				m.store.WithLock(func() {
					t.Status = TaskFailed
					t.Error = dr.err.Error()
				})
				if m.store.SaveTask(t) == nil {
					m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				}
				// The task's worker may still be running here; free its slot
				// so a task waiting on the same node is not held up.
				m.releaseCapacity(t.ID)
			}
		}

		autoFailDeps()
		launch()

		for remaining > 0 {
			if len(inFlight) == 0 {
				// A decision can land just before the coordinator would exit.
				// Process it, so a task it made ready is not left behind.
				if reviewWakeNudged(wake) {
					reviewDecisions()
					continue
				}
				// A task waits on an operator decision. Wait for the answer
				// instead of leaving; reviewDecisions launches the task once
				// the gate lets it through.
				if m.decisionWaitPending(prd) {
					if awaitDecisionAnswer(ctx, wake) {
						if st, ok := m.prdStatusOf(prdID); !ok || st != PRDRunning {
							return nil
						}
						reviewDecisions()
						continue
					}
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				// No ready tasks and nothing in-flight — dependency deadlock
				// should not happen with a valid topo sort.
				break
			}
			select {
			case <-ctx.Done():
				// Drain in-flight goroutines before returning.
				for len(inFlight) > 0 {
					r := <-results
					delete(inFlight, r.tid)
				}
				return ctx.Err()
			case <-wake:
				// An operator review decision landed while tasks were running.
				// Re-read the Automaton and launch what it made ready. Tasks
				// still in flight keep running and report through results.
				reviewDecisions()
			case r := <-results:
				delete(inFlight, r.tid)
				remaining--
				if r.err != nil {
					if t := lookupTask(prd, r.tid); t != nil {
						m.releaseCapacity(t.ID)
						oldStatus := t.Status
						m.store.WithLock(func() {
							t.Status = TaskFailed
							t.Error = r.err.Error()
						})
						if m.store.SaveTask(t) == nil {
							m.emitTaskStatus(prd, t, oldStatus, nil, nil)
						}
					}
					failedIDs[r.tid] = true
				}
				latest, _ := m.store.GetPRD(prdID)
				// A task held for operator review is neither completed nor
				// failed, so its dependents stay unready behind it.
				if latest != nil && m.taskHeldIn(latest, r.tid) {
					heldIDs[r.tid] = true
				} else {
					completedIDs[r.tid] = true
				}
				if latest != nil {
					// GH#172 D52 — operator paused this PRD while tasks were
					// in flight. Let already-launched goroutines finish
					// naturally (same drain used for PRDBlocked below), but
					// skip autoFailDeps()/launch() so nothing new starts,
					// and don't touch latest.Status — Pause() already set
					// it and this loop must not race back to PRDRunning.
					if latest.Status == PRDPaused {
						for len(inFlight) > 0 {
							dr := <-results
							finalizeDrained(dr)
							delete(inFlight, dr.tid)
						}
						return nil
					}
					if lt := lookupTask(latest, r.tid); lt != nil {
						// executeOne may have stored TaskFailed with nil error return.
						if lt.Status == TaskFailed {
							failedIDs[r.tid] = true
						}
						if lt.Status == TaskBlocked {
							old := latest.Status
							m.store.WithLock(func() {
								latest.Status = PRDBlocked
							})
							latest.BlockedReason = blockedReasonFromVerdicts(lt.Verdicts)
							if m.store.SavePRD(latest) == nil {
								m.emitPRDStatus(latest, old, nil, lastBlockVerdict(lt.Verdicts))
								m.EmitPRDUpdate(latest.ID)
							}
							// Drain remaining goroutines.
							for len(inFlight) > 0 {
								dr := <-results
								finalizeDrained(dr)
								delete(inFlight, dr.tid)
							}
							return nil
						}
					}
					if s := lookupStoryByTaskID(latest, r.tid); s != nil && m.storyDone(s) {
						m.store.WithLock(func() { s.Status = storyRollupStatus(s) })
						if s.Status == StoryFailed && !m.resolveContinueOnStoryFailure(latest) && m.nonTerminalRemain(latest) {
							old := latest.Status
							m.store.WithLock(func() {
								latest.Status = PRDBlocked
							})
							if m.store.SavePRD(latest) == nil {
								m.emitPRDStatus(latest, old, nil, nil)
								m.EmitPRDUpdate(latest.ID)
							}
							// Drain remaining goroutines already in flight — they were
							// launched before this failure was known, so let them
							// finish, but launch() below is skipped so nothing new starts.
							for len(inFlight) > 0 {
								dr := <-results
								finalizeDrained(dr)
								delete(inFlight, dr.tid)
							}
							return nil
						}
						_ = m.store.SavePRD(latest)
					}
					prd = latest
				}
				autoFailDeps()
				launch()
			}
		}
	}
	// BL191 Q6 (v5.10.0) — fire per-story guardrails after each story's
	// tasks complete. Done as a second pass so the per-task guardrails
	// settle first; story-level block also halts the PRD.
	for i := range prd.Story {
		s := &prd.Story[i]
		if !m.storyDone(s) {
			continue
		}
		if blocked, contentFixVerdict, err := m.runPerStoryGuardrails(ctx, prd, s); err != nil {
			return err
		} else if blocked {
			old := prd.Status
			prd.Status = judgeHoldOrBlocked(lastBlockVerdict(s.Verdicts))
			prd.BlockedReason = blockedReasonFromVerdicts(s.Verdicts)
			if m.store.SavePRD(prd) == nil {
				m.emitPRDStatus(prd, old, nil, lastBlockVerdict(s.Verdicts))
				m.EmitPRDUpdate(prd.ID)
			}
			return nil
		} else if contentFixVerdict != nil {
			// BL410 Phase 2 — synthesize corrective work into THIS
			// story, then recurse: Run() re-flattens tasks (now
			// including the new one), executes it, and re-checks this
			// same per-story guardrail once it completes. Fully
			// unattended, bounded by the content budget/ceiling already
			// consumed inside runPerStoryGuardrails.
			if _, _, serr := m.synthesizeCorrectiveWork(prd, *contentFixVerdict, s.ID); serr != nil {
				return serr
			}
			_ = m.store.SavePRD(prd)
			return m.Run(ctx, prd.ID, spawn, verify)
		}
	}
	// A task held for operator review keeps the Automaton open. Every task that
	// can run has run by now, so the Automaton blocks until the operator acts,
	// and the roll-up below neither completes nor fails it.
	if held := m.reviewHeldTaskID(prd); held != "" {
		m.blockForTaskReview(prd, held)
		return nil
	}
	// Roll up PRD status: completed/failed once every task is in a terminal state.
	//
	// v8.36.3 — found live on PRD a2833a5e: isTaskTerminal treats
	// TaskCancelled as terminal (correctly — a cancelled task will never
	// run again), but this loop then also counted it as a *failure*, so a
	// PRD with one deliberately-cancelled story and every other story
	// genuinely completed still rolled up to PRDFailed. Cancellation is not
	// failure — only an actual TaskFailed should mark the PRD failed.
	allDone := true
	anyFailed := false
	m.store.WithLock(func() {
		for _, s := range prd.Story {
			for _, t := range s.Tasks {
				if !isTaskTerminal(t.Status) {
					allDone = false
				} else if t.Status == TaskFailed {
					anyFailed = true
				}
			}
		}
	})
	if !allDone {
		return m.store.SavePRD(prd)
	}
	// BL451 — every terminal assignment below goes through
	// commitRollupStatus, which commits only if nobody else wrote the PRD
	// since this pass observed it (Rev, minus the guardrail helpers' own
	// saves) and every task is still terminal under the store lock. The
	// 2026-10-15 incident: reset_task landed while ci-watch was in flight
	// and this rollup then overwrote it with PRDCompleted.
	observed := prd.Status
	baseRev, _ := m.store.PRDRev(prd.ID)
	baseOwn := m.rollupOwnSaves(prd.ID)
	expectedRev := func() int { return baseRev + m.rollupOwnSaves(prd.ID) - baseOwn }
	stillRunning := func() bool {
		cur, ok := m.store.GetPRD(prd.ID)
		return ok && cur.Status == observed && prdAllTasksTerminal(cur)
	}
	if anyFailed {
		old, ok := m.commitRollupStatus(prd, observed, expectedRev(), PRDFailed, "failed", "")
		if ok {
			m.emitPRDStatus(prd, old, nil, nil)
		}
		return nil
	}
	if blocked, err := m.runPRDCompletionProjectRulesCheck(prd); err != nil {
		return err
	} else if blocked {
		// BL406 Phase 3 — third completion checkpoint. No separate
		// PerPRDGuardrails list (unlike task/story): a prd_complete
		// project rule firing is itself the signal, same PRDBlocked
		// status the task/story guardrail "block" outcome already
		// uses above.
		if old, ok := m.commitRollupStatus(prd, observed, expectedRev(), PRDBlocked, "project_rules_block", ""); ok {
			m.emitPRDStatus(prd, old, lastDecision(prd, "project_rules_block"), nil)
		}
		return nil
	}
	if blocked, contentFixVerdict, err := m.runPRDCompletionRulesReview(ctx, prd); err != nil {
		return err
	} else if blocked {
		// B124 extension — the judgment-based sibling of the
		// deterministic check just above. Same PRDBlocked signal;
		// see rules_review.go's own doc comment for why this is a
		// judgment review against whatever rules file the operator
		// configured, not a second hardcoded checklist.
		// A judge that timed out on every backend holds the PRD in
		// needs_review instead: the operator decides, nothing is
		// blocked or passed silently.
		status, reason := rulesReviewBlockStatus(prd)
		if old, ok := m.commitRollupStatus(prd, observed, expectedRev(), status, reason, blockedReasonFromVerdicts(prd.Verdicts)); ok {
			m.emitPRDStatus(prd, old, nil, lastBlockVerdict(prd.Verdicts))
		}
		return nil
	} else if contentFixVerdict != nil {
		// BL410 Phase 2 — no single task/story owns a PRD-wide
		// rules-review finding: synthesize a new, standalone
		// corrective story+task on this SAME PRD, then recurse.
		// Run() re-flattens tasks, executes the new one, and
		// re-reaches this exact completion check once it's done.
		// BL451 — skip if a concurrent reset/cancel moved the PRD.
		if !stillRunning() {
			m.recordRollupAborted(prd.ID, "corrective_work(rules_review)")
			return nil
		}
		if _, _, serr := m.synthesizeCorrectiveWork(prd, *contentFixVerdict, ""); serr != nil {
			return serr
		}
		_ = m.store.SavePRD(prd)
		return m.Run(ctx, prd.ID, spawn, verify)
	}
	if blocked, contentFixVerdict, err := m.runPRDCompletionCIWatch(ctx, prd); err != nil {
		return err
	} else if blocked {
		// ci-watch — the local checks above passed, but the CI run
		// for the commit this automaton itself pushed did not, and
		// the judge (against the operator's own rules file) or an
		// exhausted infra budget says so. Same PRDBlocked signal.
		// Push-mechanism-agnostic: it only acts when the final HEAD
		// already reached the remote (direct-push release tasks,
		// cluster pushes). Worktree/auto-PR automata push below,
		// inside the PRDCompleted branch (handleWorktreeCompletion),
		// so for them HEAD is usually unpushed here and ci-watch
		// records a skip — see ci_watch.go's header.
		if old, ok := m.commitRollupStatus(prd, observed, expectedRev(), PRDBlocked, "ci_watch_block", blockedReasonFromVerdicts(prd.Verdicts)); ok {
			m.emitPRDStatus(prd, old, nil, lastBlockVerdict(prd.Verdicts))
		}
		return nil
	} else if contentFixVerdict != nil {
		// Same as rules-review: a fixable CI failure gets standalone
		// corrective work on this SAME PRD, then Run() recurses and
		// re-reaches this completion check (which watches the new
		// commit's CI once the fix is pushed).
		if !stillRunning() {
			m.recordRollupAborted(prd.ID, "corrective_work(ci_watch)")
			return nil
		}
		if _, _, serr := m.synthesizeCorrectiveWork(prd, *contentFixVerdict, ""); serr != nil {
			return serr
		}
		_ = m.store.SavePRD(prd)
		return m.Run(ctx, prd.ID, spawn, verify)
	}
	// Last check before the Automaton is reported finished: the whole
	// worktree must be committed, or the operator must decide. A blocked
	// Automaton is never PRDCompleted, so the merge-pending pass never lists
	// it as merge ready.
	if blocked, err := m.checkWorktreeUncommittedAtFinish(ctx, prd); err != nil {
		return err
	} else if blocked {
		if old, ok := m.commitRollupStatus(prd, observed, expectedRev(), PRDBlocked, uncommittedRollupReason, uncommittedRollupBlockedReason(prd)); ok {
			m.emitPRDStatus(prd, old, lastDecision(prd, uncommittedHeldDecisionKind), nil)
		}
		return nil
	}
	// BL451 — commit PRDCompleted first; completion side effects only
	// run once the status actually landed.
	completedOld, ok := m.commitRollupStatus(prd, observed, expectedRev(), PRDCompleted, "completed", "")
	if !ok {
		return nil
	}
	m.recordProgressMilestone(prd.ID)
	// BL387 Phase 3 — fire auto-report asynchronously on completion.
	m.mu.Lock()
	reportFn := m.memoryReportFn
	m.mu.Unlock()
	if reportFn != nil && prd.MemorySeed.Enabled {
		prdID := prd.ID
		projectDir := prd.ProjectDir
		// Track the report goroutine so a caller can deterministically wait for
		// it to finish (reportFn + PRD write) instead of racing it (BL387
		// known flake: the report could still be writing when the test ended).
		m.memoryReportWG.Add(1)
		go func() { // #nosec G118 -- background report after PRD completion; request context already done
			defer m.memoryReportWG.Done()
			report, rerr := reportFn(context.Background(), prdID, projectDir)
			if rerr != nil {
				log.Printf("[autonomous] memory-report: prd=%s: %v", prdID, rerr)
				return
			}
			if report == "" {
				return
			}
			if p, ok := m.store.GetPRD(prdID); ok {
				now := time.Now().UTC()
				m.store.WithLock(func() {
					p.MemoryReport = report
					p.MemoryReportAt = &now
				})
				if serr := m.store.SavePRD(p); serr != nil {
					log.Printf("[autonomous] memory-report save: prd=%s: %v", prdID, serr)
				}
			}
		}()
	}
	// BL407 Phase 2 — push + open a PR for worktree-mode PRDs
	// with AutoPR on. Deliberately synchronous, unlike the
	// report goroutine above: git push + gh pr create are fast,
	// bounded network calls (unlike unbounded LLM report
	// generation), and Run() already blocks synchronously on
	// slower work in this same function (the quality-gate test
	// run above). Running it here, not in a goroutine, also
	// avoids a real data race a background goroutine would
	// introduce: *PRD is an unsynchronized shared pointer once
	// a caller holds it (see the race-conditions note at the
	// top of this function), so mutating it after Run() has
	// already returned to its own caller is unsafe against
	// anything else reading the same PRD concurrently (e.g. a
	// REST GET while a background goroutine is still writing).
	m.handleWorktreeCompletion(prd)
	// BL407 Phase 3 — cluster-dispatched counterpart. Also
	// synchronous and for the same reason: fast, bounded
	// (open-PR only, no local push) network calls, run before
	// anything else can observe *prd concurrently.
	m.handleClusterCompletion(prd)
	// Worktree cleanup — completion end path: release the work
	// branch and its work tree when the commits are already on the
	// base branch. The cleanup's safety criteria still apply (live
	// session or lease, dirty work tree, retention window), and an
	// entry they hold is left alone with the reason recorded; the
	// coordinator's periodic cleanup pass retries later.
	m.ReleaseAutomatonWorktree(prd)
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	// BL427 — fired after the final save so subscribers see the
	// completion side effects (worktree PR, cluster PR) already persisted.
	m.emitPRDStatus(prd, completedOld, nil, nil)
	return nil
}

// relocateCheckpoint reads CHECKPOINT.md from the project_dir if the just-
// finished attempt wrote one, relocates it to the PRD's scratch dir under
// the daemon's own data directory, and returns its content for folding
// into the next attempt's retry hint (empty string if absent or on a
// retrievable error -- best-effort, never fails the caller). See
// scratch.go's package doc for why this can't just live in the data
// directory from the start.
func (m *Manager) relocateCheckpoint(prd *PRD, t *Task) string {
	scratchDir, err := ScratchDir(m.dataDir, prd.ID)
	if err != nil {
		log.Printf("[autonomous] relocateCheckpoint: scratch dir for prd=%s: %v", prd.ID, err)
		scratchDir = "" // still try the read+delete below; just skip archiving
	}
	// BL447: the worker wrote CHECKPOINT.md in its own (resolved) dir.
	content, ok := RelocateProjectFile(m.resolveTaskProjectDir(prd, t), "CHECKPOINT.md", scratchDir, t.ID+".md")
	if !ok {
		return ""
	}
	return content
}

// executeOne handles a single task: spawn → wait → verify → (retry).
//
// BL191 Q4 (v5.9.0) — when t.SpawnPRD is true, the task spec is treated
// as a *child PRD* rather than a worker prompt. The executor delegates
// to recurseChildPRD which walks the same Decompose → (auto-)Approve →
// Run cycle the parent went through. Recursion depth is bounded by
// Config.MaxRecursionDepth so a runaway decomposition can't tank the
// daemon.
func (m *Manager) executeOne(ctx context.Context, prd *PRD, t *Task, spawn SpawnFn, verify VerifyFn, retries int, qgCfg pipeline.QualityGateConfig, qgBaselines *qualityGateBaselines) error {
	if t.SpawnPRD {
		return m.recurseChildPRD(ctx, prd, t, spawn, verify, retries)
	}
	// v8.36.1 — a fresh Run() (operator reset_task, boot-resume) starts
	// executeOne with no in-loop retry history, but the task itself may
	// carry context from a previous, separately-triggered attempt (see
	// PriorAttemptHint's doc comment on Task). Seed the first attempt's
	// hint from it and clear it — single-use, matching the in-loop hint's
	// own lifecycle.
	hint := t.PriorAttemptHint
	if hint != "" {
		t.PriorAttemptHint = ""
		_ = m.store.SaveTask(t)
	}
	// BL410 Phase 3 — tracks the most recent failure's category across
	// every retryable branch in the loop below (stall, quality-gate
	// regression, content-hint retry, a genuine vr.OK==false), so the
	// Learning entry recorded on final exhaustion reflects what
	// actually just happened, not a stale/zero-value vr left over from
	// a branch that never touched it.
	lastFailureCategory := ""
	defer m.releaseCapacity(t.ID)
	// BL428 §3a — per-canonical-tree advisory write lock. Admit order:
	// capacity (m.admit, per attempt) → refuse-dirty-tree gate → tree
	// lock → spawn. The lock is held across spawn, verify and the retry
	// loop; the single deferred release below covers EVERY exit path of
	// this goroutine (spawn error, verify failure, retry abandon,
	// skipSpawn fall-through, quality-gate block, happy path). The
	// closure is local to this executeOne run — concurrent tasks on the
	// same PRD each own and release their own acquisition.
	var treeRelease func()
	defer func() {
		if treeRelease != nil {
			treeRelease()
		}
		m.detachTreeRelease(t.ID)
	}()
	holdTree := func() error {
		if treeRelease != nil {
			return nil // already held for this executeOne (idempotent)
		}
		rel, terr := m.acquireTreeWritersLock(ctx, prd, t)
		if terr != nil {
			return terr
		}
		treeRelease = rel
		// Design §6.ii — register the release closure so an await-external
		// cycle (m.AwaitExternal) can release this lock while the task waits
		// on an external condition when the tree is fully pushed, and point
		// it at the re-claimed lock on re-admission.
		m.attachTreeRelease(t.ID, &treeRelease)
		return nil
	}
	spawn = m.capacityRetrySpawn(t, spawn)
	// Attempt budget. With failure escalation on, the task gets MaxFailures
	// failed attempts counted from the last operator action. With it off, the
	// legacy bound applies: one attempt plus auto_fix_retries.
	esc := m.resolveFailureEscalation(prd, t)
	budget := retries + 1
	if esc.Enabled {
		budget = esc.MaxFailures - m.escalationFailureCount(t)
	}
	var run attemptRun
	for attempt := 0; attempt < budget; attempt++ {
		var err error
		run = attemptRun{RefsBefore: len(t.AttemptRefs)}

		// On daemon restart the task may already be in TaskVerifying with a
		// session that survived the restart. Skip the spawn so we don't create
		// a duplicate session alongside the still-running one. Only applies on
		// the first attempt (attempt > 0 means the previous session already
		// finished and failed verification, so we must spawn a new one).
		skipSpawn := false
		if attempt == 0 && t.SessionID != "" && (t.Status == TaskVerifying || (t.Status == TaskInProgress && t.AdoptedSession)) {
			m.mu.Lock()
			aliveFn, outputFn, waitFn := m.sessionAliveFn, m.sessionOutputFn, m.sessionWaitFn
			m.mu.Unlock()
			alive := aliveFn != nil && aliveFn(t.SessionID)
			switch {
			case alive && t.Status == TaskVerifying:
				log.Printf("[autonomous] executeOne: task %s resuming verify for live session %s (daemon restart)", t.ID, t.SessionID)
				m.adoptCapacity(prd, t)
				skipSpawn = true
			case alive:
				// Adopted at boot-resume: never spawn (and never reach the
				// kill-before-respawn block). Wait for the session's completion
				// marker, then verify it like a freshly finished worker.
				log.Printf("[autonomous] executeOne: task %s waiting on adopted session %s (daemon restart)", t.ID, t.SessionID)
				m.adoptCapacity(prd, t)
				skipSpawn = true
				adopted := AdoptedWait{PRDID: prd.ID, TaskID: t.ID, SessionID: t.SessionID, NudgedAt: t.NudgeTimeForSession(t.SessionID)}
				startConfirm := m.adoptedStartWatchFor(t)
				if werr := waitForAdoptedSession(ctx, waitFn, aliveFn, adopted, startConfirm); werr != nil {
					if ctx.Err() != nil {
						return nil
					}
					m.releaseCapacity(t.ID)
					if errors.Is(werr, ErrSessionNeverStarted) {
						// The adopted session never produced its first assistant turn:
						// a START failure, counted against SessionStartRetryBudget the
						// same way as the worker path below. The normal retry budget is
						// left untouched.
						m.store.WithLock(func() {
							t.AdoptedSession = false
							t.StartFailureTail = startConfirm.failureTail()
						})
						exhausted, serr := m.countSessionStartFailure(ctx, prd, t, run, werr)
						if serr != nil || exhausted {
							return serr
						}
						attempt-- // offset the loop's post-iteration attempt++
						continue
					}
					// A stall or a failed/killed session takes the normal retry path:
					// free the lease; the retry kills the leftover session and
					// re-admits a fresh spawn like any other failed attempt.
					lastFailureCategory = "infra"
					hint = werr.Error()
					m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeForError(werr), Category: lastFailureCategory, Summary: hint})
					m.store.WithLock(func() { t.Error = hint; t.AdoptedSession = false })
					_ = m.store.SaveTask(t)
					continue
				}
				oldStatus := t.Status
				m.store.WithLock(func() {
					t.AdoptedSession = false
					t.Status = TaskVerifying
				})
				if m.store.SaveTask(t) == nil {
					m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				}
			case t.Status == TaskVerifying:
				// Finished while the daemon was down (boot-resume kept it
				// verifying): its recorded output holds the marker, or the task
				// carries a recorded worker result, so verify it instead of
				// killing and respawning.
				finished := t.Verification == nil && taskHasWorkerResult(t)
				if !finished && outputFn != nil {
					if out, ok := outputFn(t.SessionID); ok && outputHasCompletionMarker(out) {
						finished = true
					}
				}
				if finished {
					log.Printf("[autonomous] executeOne: task %s verifying finished session %s from recorded result (daemon restart)", t.ID, t.SessionID)
					skipSpawn = true
				}
			}
		}

		// prevNode is the node the previous attempt's lease named, passed to
		// admission as the tie breaker for this attempt's placement.
		prevNode := ""
		if !skipSpawn {
			// Kill any session left over from a previous attempt (a failed-
			// verification retry within this call, or a session inherited from
			// disk after a daemon restart whose liveness check above came back
			// false) before spawning its replacement. Without this, the old
			// session's tmux pane and backend process (opencode/claude) keep
			// running as an orphan — wasting resources and confusing session
			// tracking — while t.SessionID gets silently overwritten below.
			oldStatus, oldSession := t.Status, t.SessionID
			if t.SessionID != "" {
				m.mu.Lock()
				killerFn := m.sessionKillerFn
				m.mu.Unlock()
				if killerFn != nil {
					why := fmt.Sprintf("task %s relaunch: the session of its previous attempt is replaced (task was %s)", t.ID, oldStatus)
					if err := killerFn(t.SessionID, "autonomous.executor.respawn", why); err != nil {
						log.Printf("[autonomous] executeOne: kill stale session %s for task %s before respawn: %v", t.SessionID, t.ID, err)
					} else {
						log.Printf("[autonomous] executeOne: killed stale session %s for task %s before respawn", t.SessionID, t.ID)
					}
				}
				m.store.WithLock(func() { t.SessionID = "" })
			}
			// End every other live session of this task or its worktree so the
			// replacement is the only one.
			if ended := m.endLiveSessionsOfTask(prd, t, "", "autonomous.executor.respawn", fmt.Sprintf("task %s relaunch: other live sessions of the task or its worktree end so the replacement is the only one", t.ID)); len(ended) > 0 {
				m.store.WithLock(func() { recordEndedSessions(prd, t, ended, "replacement session launching") })
				_ = m.store.SavePRD(prd)
			}
			// The old engine process is gone, so its slot can be freed. The
			// lease is released here, not at the end of the attempt, because
			// Acquire returns at once for a holder that is already admitted and
			// the relaunch would keep the previous node without placement.
			if prevNode = m.releaseForRelaunch(t); prevNode != "" {
				log.Printf("[autonomous] task %s relaunch: previous attempt held node %s; admitting through placement", t.ID, prevNode)
			}
			now := m.attemptNow()
			m.store.WithLock(func() {
				t.StartedAt = &now
				t.Status = TaskInProgress
				t.RetryCount = attempt
				// A genuine in-loop retry (attempt > 0) follows a prior attempt that
				// already ran, so it registers as one for the verifier's already-landed
				// check. attempt 0 is the first run and adds nothing.
				if attempt > 0 {
					t.PriorAttemptCount++
				}
			})
			if m.store.SaveTask(t) == nil {
				m.emitTaskSession(prd, t, oldSession)
				m.emitTaskStatus(prd, t, oldStatus, nil, nil)
			}

			// BL203 (v5.4.0) + BL381 — most-specific LLM override wins.
			// per-task → per-story → per-PRD → Automation → cfg.ExecutionBackend → global session.llm_backend.
			story := findStory(prd, t.StoryID)
			auto := m.AutomationForPRD(prd)
			backend, model, execSource := resolveTaskExecutionLLM(t, story, prd, auto, m.cfg.ExecutionBackend)
			effort := resolveTaskEffort(t, story, prd, auto)
			// Failure escalation: the ladder step that the failed attempts so
			// far select replaces the worker for this attempt only. Backend,
			// model and effort come from the same step (a model never cascades
			// from another level), and t.Backend/t.Model are not written. A
			// worker the operator chose in a review retry is kept instead.
			escalated, stepIdx := false, -1
			if esc.Enabled && !t.OperatorWorkerSet {
				if step, idx, ok := selectEscalationStep(esc, m.escalationFailureCount(t)); ok {
					backend, model, execSource = step.Backend, step.Model, ExecutorSourceEscalation
					if step.Effort != "" {
						effort = step.Effort
					}
					escalated, stepIdx = true, idx
				}
				if note := m.escalationWorkerChangeNote(t, backend, model, stepIdx); note != "" {
					m.recordFailureEscalationDecision(prd.ID, t.ID, note, backend, model)
				}
			}
			// Backstop for the Run preflight: never spawn an invalid pair.
			if merr := validateModelForBackend(backend, model); merr != nil {
				mismatch := fmt.Errorf("task %s not spawned: %w", t.ID, merr)
				log.Printf("[autonomous] prd=%s task=%s model/backend mismatch, not spawning: %v", prd.ID, t.ID, merr)
				oldStatus := t.Status
				m.store.WithLock(func() {
					t.Status = TaskFailed
					t.Error = mismatch.Error()
				})
				if m.store.SaveTask(t) == nil {
					m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				}
				return mismatch
			}
			// One model value from here on. The pairing check above judged the
			// model as written; a pi-family model with no provider now takes the
			// launch's provider, so admission picks the node the session runs on
			// and the spawn, the lease and the session start all agree.
			model = m.qualifyPiFamilyModel(backend, model)
			// Record which level supplied the backend before the session is
			// spawned, so it is visible while the task runs; a later attempt
			// overwrites it.
			m.store.WithLock(func() { t.ExecutorSource = execSource })
			_ = m.store.SaveTask(t)
			run.Backend, run.Model, run.Effort, run.StartedAt = backend, model, effort, now
			run.Escalated = escalated
			// v5.27.5 — most-specific permission-mode wins: per-task →
			// per-PRD → SpawnFn falls through to session default.
			permMode := t.PermissionMode
			if permMode == "" {
				permMode = prd.PermissionMode
			}
			// Most-specific pi --thinking override wins: per-task →
			// per-story → per-PRD → the session manager's configured
			// pi.thinking_level (applied when the resolved value is empty).
			thinkingLevel := resolveThinkingLevel(t, story, prd, "")
			lspLang := t.LSPLanguage
			if lspLang == "" {
				lspLang = prd.LSPLanguage
			}
			var contextPrepend string
			m.mu.Lock()
			if m.contextFn != nil {
				contextPrepend = m.contextFn(prd.Type)
			}
			m.mu.Unlock()
			if aerr := m.admitPreferring(ctx, prd, t, backend, model, capacity.RoleWorker, prevNode); aerr != nil {
				if errors.Is(aerr, errCancelledWhileWaiting) {
					return nil
				}
				return aerr
			}
			// A multi-node LLM entry needs its model resident on the leased node
			// before the spawn; a single-node entry skips this step entirely.
			if lerr := m.loadWorkerNode(ctx, prd, t, backend, model); lerr != nil {
				if errors.Is(lerr, errCancelledWhileWaiting) {
					return nil
				}
				return lerr
			}
			// The node the lease was granted on decides where the worker runs.
			if rerr := m.replaceStaleLease(ctx, prd, t, backend, model); rerr != nil {
				if errors.Is(rerr, errCancelledWhileWaiting) {
					return nil
				}
				return rerr
			}
			heldNode := m.heldNode(t.ID)
			if nerr := m.requireLeasedNode(t, backend, model, heldNode); nerr != nil {
				log.Printf("[autonomous] prd=%s task=%s no node leased, not spawning: %v", prd.ID, t.ID, nerr)
				oldStatus := t.Status
				m.store.WithLock(func() {
					t.Status = TaskFailed
					t.Error = nerr.Error()
				})
				if m.store.SaveTask(t) == nil {
					m.emitTaskStatus(prd, t, oldStatus, nil, nil)
				}
				return nerr
			}
			// BL428 §3a — tree lock after capacity, before spawn: a second
			// writer on the same canonical tree waits here (visible as a
			// waiting task with a "tree lock" reason), and is released by
			// the deferred treeRelease on every exit path below.
			if terr := holdTree(); terr != nil {
				if errors.Is(terr, errCancelledWhileWaiting) {
					return nil
				}
				return terr
			}
			// BL447: lint against the task's own resolved dir/write scope.
			// scope_lint decides what a hit does: enforce blocks the task,
			// warn records it and spawns anyway, off skips the lint.
			if mode := m.scopeLintMode(); mode != scopeLintOff {
				if problems := LintTaskScopeFor(prd, t, "task '"+t.Title+"'", t.Spec, t.FilesPlanned); len(problems) > 0 {
					if mode == scopeLintEnforce {
						return fmt.Errorf("scope: %s (task not spawned)", strings.Join(problems, "; "))
					}
					m.recordScopeLintWarn(prd, t, problems)
				}
			}
			// BL447: cwd, scope block and pre-task capture anchor to the
			// task's resolved dir (override or PRD.ProjectDir).
			taskDir := m.resolveTaskProjectDir(prd, t)
			// Never start a worker in a directory that is not a real worktree.
			if rerr := m.refuseLaunchOutsideWorktree(prd, t, taskDir); rerr != nil {
				return rerr
			}
			var sr SpawnResult
			sr, err = spawn(ctx, SpawnRequest{
				TaskID:           t.ID,
				StoryID:          t.StoryID,
				PRDID:            t.PRDID,
				Title:            t.Title,
				Spec:             TaskScopeBlock(prd, t) + "\n" + t.Spec,
				ProjectDir:       taskDir,
				Backend:          backend,
				Effort:           effort,
				Model:            model,
				PermissionMode:   permMode,
				ThinkingLevel:    thinkingLevel,
				RetryHint:        m.escalationRetryHint(prd, t, hint),
				ProjectProfile:   prd.ProjectProfile,  // v5.26.19
				ClusterProfile:   prd.ClusterProfile,  // v5.26.19
				Branch:           prd.Git.Branch,      // BL407 Phase 4
				Git:              prdAutoCommits(prd), // git activity/auto-commit only in worktree mode or with declared write dirs
				ContextPrepend:   contextPrepend,      // BL244
				LSPLanguage:      lspLang,
				MemorySeed:       prd.MemorySeed,                 // BL386 Phase 1
				SkipDirtyCapture: prd.Git.Branch != "",           // worktree mode: nothing pre-dirty to protect
				WriteScope:       EffectiveTaskWriteDirs(prd, t), // auto-commit staging scope
				AttemptRefs:      t.AttemptRefs,                  // preserved attempt work to restore
				CleanRestart:     t.CleanRestart,                 // operator clean restart: skip the restore once
				ComputeNode:      heldNode,                       // capacity lease's node: where the worker runs
			})
			if err != nil && errors.Is(err, errCancelledWhileWaiting) {
				return nil
			}
			// The spawn ran with the restore skipped (if clean restart was set),
			// so the request is spent. Clear it, success or failure, so later
			// automatic retries restore again.
			m.store.WithLock(func() { t.CleanRestart = false })
			if err != nil {
				return fmt.Errorf("spawn: %w", err)
			}
			if led := m.Capacity(); led != nil {
				led.Bind(t.ID, sr.SessionID)
			}
			placedNode := sr.ComputeNode
			if placedNode == "" {
				placedNode = heldNode
			}
			m.store.WithLock(func() {
				oldSession = t.SessionID
				t.SessionID = sr.SessionID
				t.ComputeNode = placedNode
			})
			// persist SessionID immediately so killPRDSessions can reach it if daemon exits before TaskVerifying is saved
			if m.store.SaveTask(t) == nil {
				m.emitTaskSession(prd, t, oldSession)
			}
			// BL386 Phase 1 — warm-start seed after spawn, best-effort.
			if sr.SessionID != "" && prd.MemorySeed.Enabled {
				m.mu.Lock()
				seedFn := m.memorySeedFn
				m.mu.Unlock()
				if seedFn != nil {
					if serr := seedFn(ctx, sr.SessionID, t.PRDID, t.StoryID, taskDir, prd.MemorySeed); serr != nil { // BL447
						log.Printf("[autonomous] seed: session=%s: %v", sr.SessionID, serr)
					}
				}
			}
			m.store.WithLock(func() { t.PreTaskSHA = sr.PreTaskSHA }) // BL366: persisted for verifier diff grounding
			// BaseSHA anchors the task's FIRST attempt only. Gate on attempt == 0,
			// not on BaseSHA being empty: when attempt 1 captured no HEAD (no
			// commits yet) BaseSHA stays empty, and a retry would otherwise adopt
			// its own pre-task HEAD, which already includes attempt 1's commit.
			// Repro: TestBaseSHA_RetryDoesNotAdoptRetryHeadWhenFirstAttemptHadNoHead
			// (task_base_sha_retry_test.go).
			m.store.WithLock(func() {
				if attempt == 0 && t.BaseSHA == "" && sr.PreTaskSHA != "" {
					t.BaseSHA = sr.PreTaskSHA // per-task anchor: set once on the first attempt, never by retries
				}
				t.PreTaskUntrackedFiles = sr.PreTaskUntrackedFiles // v8.25.12: detect newly created untracked files
				t.PreTaskDirtyFiles = sr.PreTaskDirtyFiles         // foreign-file-drift: tracked files already modified before the worker started
				oldStatus = t.Status
				t.Status = TaskVerifying
			})
			if m.store.SaveTask(t) == nil {
				m.emitTaskStatus(prd, t, oldStatus, nil, nil)
			}
		}

		// BL428 §3a — the skipSpawn fall-through (resumed live session after
		// a daemon restart) verifies against this tree too, so hold the
		// lock across it: the previous executeOne crashed with the lock held,
		// and this task (same holder ID) re-claims its own record via the
		// idempotent-path. No-op on the normal path, which already
		// acquired the lock after m.admit above.
		if terr := holdTree(); terr != nil {
			if errors.Is(terr, errCancelledWhileWaiting) {
				return nil
			}
			return terr
		}

		var vr VerificationResult
		if verify != nil {
			vr, err = verify(ctx, prd, t)
			if err != nil {
				if errors.Is(err, errCancelledWhileWaiting) {
					return nil
				}
				// A spawned session that never produced its first assistant
				// turn (or whose prompt was never submitted) is a START
				// failure, not a verification failure — checked before the stall
				// branch because the daemon's wait loop wraps BOTH sentinels in
				// the same stall error (ErrSessionNeverStarted must win).
				if errors.Is(err, ErrSessionNeverStarted) {
					exhausted, serr := m.countSessionStartFailure(ctx, prd, t, run, err)
					if serr != nil || exhausted {
						return serr
					}
					attempt-- // offset the loop's post-iteration attempt++
					continue
				}
				if errors.Is(err, ErrWorkerStalled) {
					// A stall (SSE, or no log output past stale_task_seconds) is a
					// retryable verification failure, not a fatal error — it consumes
					// the same auto_fix_retries budget as a normal failed verification.
					lastFailureCategory = "infra" // BL410 Phase 3
					// The stalled worker is killed before the retry spawns, so
					// free its capacity lease now; the retry re-admits through
					// the normal queue like any other waiting task.
					m.releaseCapacity(t.ID)
					log.Printf("[autonomous] task %s/%s: stall retry %d of %d after session %s (%s)", prd.ID, t.ID, attempt+1, budget-attempt-1, t.SessionID, StallRetryReason(err))
					hint = err.Error()
					m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeStalled, Category: lastFailureCategory, Summary: hint})
					if cp := m.relocateCheckpoint(prd, t); cp != "" {
						hint += "\n\nCheckpoint from the previous attempt (context only):\n" + cp
					}
					m.store.WithLock(func() {
						t.Error = hint
					})
					_ = m.store.SaveTask(t)
					continue
				}
				return fmt.Errorf("verify: %w", err)
			}
		} else {
			vr = VerificationResult{OK: true, Summary: "no verifier configured", VerifiedAt: time.Now()}
		}
		vrFinal := vr
		m.store.WithLock(func() { t.Verification = &vrFinal })
		// File drift: deterministic, once per worker attempt.
		// Pre-existing findings block under DriftMode "block" (the default)
		// unless the PRD/profile explicitly opts out; in-run findings
		// (the worker's own commits) never block. A block halts the walk
		// the same way a per-task guardrail block does below.
		if m.checkForeignFileDrift(ctx, prd, t) {
			return m.store.SaveTask(t)
		}
		// Undeclared edits: the worker's own edits outside
		// FilesPlanned ∪ FilesGenerated. Runs after the drift step, so a
		// drift block fires first and this is not reached that attempt.
		if m.checkUndeclaredEdits(ctx, prd, t) {
			return m.store.SaveTask(t)
		}
		if vr.OK {
			// BL367 — quality gate: run tests after verifier passes and
			// check for regressions vs. the pre-PRD baseline.
			// BL447: run in the task's resolved dir against a baseline
			// captured in that same dir (qualityGateBaselines.forDir).
			qgDir := m.resolveTaskProjectDir(prd, t)
			if base, ok := qgBaselines.forDir(qgDir); ok {
				// Gate scope: plan (final gate tasks and full scopes run the
				// configured command with the full baseline; scoped plans run
				// the scoped command against the restricted baseline, falling
				// back to the full run with the reason kept on the plan),
				// evaluate, and for non-full scopes also run vet, folding new
				// vet failures in as regressions. The plan is persisted on the
				// result for later tracing (QualityGateResult.ScopePlan).
				cmp := m.runScopedQualityGate(prd, t, qgDir, qgCfg, qgBaselines, base)
				t.QualityGateResult = &cmp
				log.Printf("[autonomous] %s/%s quality-gate: %s", prd.ID, t.ID, cmp.Summary)
				metrics.QualityGateRunsTotal.Inc()
				// The verifier PASSED, so whatever the gate reports
				// (confirmed regression or unusable) is a disagreement the
				// operator's gate_disagreement policy decides.
				policy := m.gateDisagreementPolicy()
				switch {
				case cmp.Invalid:
					// An unusable baseline or run never blocks under ANY
					// policy: it warns only, and counts as neither a pass nor
					// a regression in the metrics.
					cmp.Summary = "gate invalid (warning): " + cmp.InvalidReason
					m.store.WithLock(func() {
						t.QualityGateResult = &cmp
						t.GateDisagreement = &GateDisagreement{
							VerifierOK: true, GateInvalid: true,
							Policy: policy, Summary: cmp.Summary,
						}
					})
					log.Printf("[autonomous] %s/%s WARNING: quality-gate invalid (policy=%s, never blocks): %s", prd.ID, t.ID, policy, cmp.InvalidReason)
					m.recordGateDisagreementDecision(prd.ID, t.ID, policy, "invalid gate (never blocks): "+cmp.InvalidReason)
				case cmp.Regression:
					metrics.QualityGateRegressionTotal.Inc()
					m.store.WithLock(func() {
						t.GateDisagreement = &GateDisagreement{
							VerifierOK: true, GateRegression: true,
							Policy: policy, Summary: cmp.Summary,
						}
					})
					if policy == "block" {
						// Confirmed regression from a valid baseline: fail the
						// task with no auto-fix retry. The operator chose block
						// on this disagreement, so an automated rewrite must
						// not get to re-pass the verifier with the gate still
						// red. Run re-reads the stored task and rolls the
						// Automaton up from the TaskFailed status.
						m.recordGateDisagreementDecision(prd.ID, t.ID, policy, "blocked: verifier ok, gate regression — "+cmp.Summary)
						oldStatus := t.Status
						m.store.WithLock(func() {
							t.Status = TaskFailed
							t.Error = "gate_disagreement: " + cmp.Summary
						})
						m.releaseCapacity(t.ID)
						if m.store.SaveTask(t) == nil {
							m.emitTaskStatus(prd, t, oldStatus, nil, nil)
						}
						return nil
					}
					// Policy "warn" (default): record and let the task
					// complete, unless the per-Automaton knob below says
					// regressions are fatal.
					log.Printf("[autonomous] %s/%s WARNING: verifier passed but quality gate reports a regression (policy=%s): %s", prd.ID, t.ID, policy, cmp.Summary)
					m.recordGateDisagreementDecision(prd.ID, t.ID, policy, "warn: verifier ok, gate regression — "+cmp.Summary)
					if qgCfg.BlockOnRegression {
						lastFailureCategory = "policy" // BL410 Phase 3
						oldStatus := t.Status
						m.store.WithLock(func() {
							t.Status = TaskFailed
							t.Error = "quality_gate_regression: " + cmp.Summary
						})
						if m.store.SaveTask(t) == nil {
							m.emitTaskStatus(prd, t, oldStatus, nil, nil)
						}
						// Fold regression summary into retry hint so the next attempt
						// knows what broke.
						hint = "quality gate regression: " + cmp.Summary
						m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeFailed, Category: lastFailureCategory, Summary: hint, Verification: vr})
						if cp := m.relocateCheckpoint(prd, t); cp != "" {
							hint += "\n\nCheckpoint from the previous attempt (context only):\n" + cp
						}
						_ = m.store.SaveTask(t)
						continue // try auto-fix retry with regression context
					}
				default:
					metrics.QualityGatePassTotal.Inc()
				}
			}
			// A task whose work is not committed is not complete. The check
			// commits the task's own leftover files and holds the task (the
			// way the setup-fault hold above does, consuming none of the
			// auto_fix_retries budget) when files stay uncommitted. It runs
			// before the post-verify SHA is captured so a rescue commit is
			// the commit that gets pinned.
			if held, herr := m.checkUncommittedWork(ctx, prd, t); herr != nil {
				return herr
			} else if held {
				return nil
			}
			done := time.Now()
			m.store.WithLock(func() { t.CompletedAt = &done })
			// Pin this task's own commit (HEAD in its resolved dir — BL465
			// fires the deterministic commit at DATAWATCH_COMPLETE, before
			// verify) so the PRD-completion CI guardrail watches that commit
			// instead of whatever live HEAD is at roll-up time. Best-effort:
			// a capture failure leaves PostVerifySHA empty and the guardrail
			// falls back to live HEAD; it must never fail the task.
			postSHA := CapturePostVerifySHA(ctx, m.resolveTaskProjectDir(prd, t), m.driftGit)
			var preDoneStatus TaskStatus
			m.store.WithLock(func() {
				t.PostVerifySHA = postSHA
				preDoneStatus = t.Status
				t.Status = TaskCompleted
			})
			// Sweep any leftover CHECKPOINT.md out of the shared
			// project_dir now that the task is done — nothing will ever
			// need to resume it again.
			m.relocateCheckpoint(prd, t)
			// BL191 Q6 (v5.10.0) — fire per-task guardrails before
			// declaring the task done. A `block` verdict marks the task
			// blocked + halts the PRD walk via the executor return.
			if blocked, contentHint, err := m.runPerTaskGuardrails(ctx, prd, t); err != nil {
				return err
			} else if blocked {
				m.store.WithLock(func() {
					t.Status = TaskBlocked
				})
				if err := m.store.SaveTask(t); err != nil {
					return err
				}
				m.emitTaskStatus(prd, t, preDoneStatus, nil, lastBlockVerdict(t.Verdicts))
				return nil
			} else if contentHint != "" {
				// BL410 Phase 2 — a content-category guardrail block,
				// auto-fix enabled, budget already consumed inside
				// runPerTaskGuardrails: retry the SAME task via this
				// same loop, exactly like a failed verification or
				// quality-gate regression already does above.
				lastFailureCategory = "" // BL410 Phase 3 — content
				hint = contentHint
				m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeFailed, Category: lastFailureCategory, Summary: hint, Verification: vr})
				m.store.WithLock(func() {
					t.Error = hint
				})
				_ = m.store.SaveTask(t)
				continue
			}
			// BL386 Phase 2 — harvest session-local memories after task completes.
			if t.SessionID != "" && prd.MemoryHarvest.Enabled {
				m.mu.Lock()
				harvestFn := m.memoryHarvestFn
				m.mu.Unlock()
				if harvestFn != nil {
					if herr := harvestFn(ctx, t.SessionID, t.PRDID, t.StoryID, m.resolveTaskProjectDir(prd, t), prd.MemoryHarvest); herr != nil { // BL447
						log.Printf("[autonomous] harvest: session=%s: %v", t.SessionID, herr)
					}
				}
			}
			if err := m.store.SaveTask(t); err != nil {
				return err
			}
			// v9.2.1 — passing-script retry: verification passed via the
			// unmodified-retry escape hatch (the retry itself produced no diff;
			// already-committed work passed the verification script). Append the
			// audit-trail note now that the task is persisted as completed.
			// Best-effort: a failure here is a lost audit note, never a lost
			// outcome — log and move on.
			if vr.UnmodifiedRetry {
				if recErr := m.RecordUnmodifiedRetry(prd.ID, t.ID, "verification script passed on the work an earlier attempt already committed"); recErr != nil {
					log.Printf("[autonomous] %s/%s record unmodified-retry audit note: %v", prd.ID, t.ID, recErr)
				}
			}
			m.emitTaskStatus(prd, t, preDoneStatus, nil, nil)
			m.deleteTaskSession(t.SessionID)
			return nil
		}
		// BL387 Phase 1a — write each verifier issue to prd-shared so subsequent
		// retry tasks can seed from it and avoid repeating the same mistake.
		if prd.MemorySeed.Enabled && len(vr.Issues) > 0 {
			m.mu.Lock()
			verifierFn := m.memoryVerifierFn
			m.mu.Unlock()
			if verifierFn != nil {
				// BL447: findings are filed under the task's resolved dir.
				for _, issue := range vr.Issues {
					if ferr := verifierFn(ctx, t.PRDID, m.resolveTaskProjectDir(prd, t), "[verifier-finding] "+issue, "verifier-finding"); ferr != nil {
						log.Printf("[autonomous] verifier memory: prd=%s: %v", t.PRDID, ferr)
					}
				}
			}
		}
		// setup-fault verdict (three-way retry no-output path): the verdict
		// is an environment fault, not a task fault. Retrying the identical
		// setup against an identical environment reproduces the same error, and
		// TaskFailed would wrongly record a healthy task as failed. Hold the
		// task exactly the way the guardrail 'block' verdict above holds it
		// (TaskBlocked, persisted, status emitted, return), consuming none of
		// the auto_fix_retries budget.
		if vr.Category == "setup" {
			oldStatus := t.Status
			m.store.WithLock(func() {
				t.Status = TaskBlocked
				t.Error = vr.Summary
			})
			t.FailureReason = &FailureReason{
				Category: "setup", Source: "setup-fault", Retryable: false,
				Detail: vr.Summary, OccurredAt: time.Now(),
			}
			if err := m.store.SaveTask(t); err != nil {
				return err
			}
			m.emitTaskStatus(prd, t, oldStatus, nil, nil)
			return nil
		}
		lastFailureCategory = vr.Category // BL410 Phase 3
		m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeFailed, Category: lastFailureCategory, Summary: vr.Summary, Verification: vr})
		hint = vr.Summary
		if len(vr.Issues) > 0 {
			hint += "\nIssues:\n- " + joinLines(vr.Issues)
		}
		if cp := m.relocateCheckpoint(prd, t); cp != "" {
			hint += "\n\nCheckpoint from the previous attempt (context only):\n" + cp
		}
	}
	// Failure escalation: with the policy on, the task is held for operator
	// review rather than marked failed. Disabled, the legacy path below runs.
	if esc.Enabled {
		// No more attempts are coming, so sweep any leftover CHECKPOINT.md,
		// as the legacy exhaustion path does.
		m.relocateCheckpoint(prd, t)
		return m.holdTaskForReview(prd, t, lastFailureCategory, hint)
	}
	exhaustedOld := t.Status
	m.store.WithLock(func() {
		t.Status = TaskFailed
	})
	// A budget-exhausted task must carry the final failing verdict's output
	// (summary + issues — e.g. a failed verification script's output) into
	// Task.Error, not just a bare status: FailureReason.Detail already does,
	// and Task.Error is where operators look first.
	if hint != "" {
		m.store.WithLock(func() {
			t.Error = "verification failed after retries\n" + hint
		})
	} else {
		m.store.WithLock(func() {
			t.Error = "verification failed after retries"
		})
	}
	// BL410 Phase 3 — Learning writer revival: record the final,
	// budget-exhausted failure for future pattern analysis. Fires once,
	// here, not on every retry attempt — a transient failure that
	// self-healed on a later attempt never reaches this line.
	m.recordFailureLearning(prd, t.ID, lastFailureCategory, hint)
	// BL410 remediation (2026-10-13) — Task.FailureReason existed since
	// Phase 1 but was never populated (flagged live: "what about
	// task.failurereason/story.failurereason?"). Same data
	// recordFailureLearning already has, structured instead of a bare
	// string, additive alongside the existing t.Error.
	m.store.WithLock(func() {
		t.FailureReason = &FailureReason{
			Category: orContentCategory(lastFailureCategory), Detail: hint,
			Source: "task-exhausted", Retryable: false, OccurredAt: time.Now(),
		}
	})
	// No more attempts coming — sweep any leftover CHECKPOINT.md too.
	m.relocateCheckpoint(prd, t)
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.emitTaskStatus(prd, t, exhaustedOld, nil, nil)
	m.deleteTaskSession(t.SessionID)
	return nil
}

// deleteTaskSession calls sessionDeleteFn (if wired) to remove an ephemeral
// executor session from the session list after its task reaches terminal state.
// countSessionStartFailure books one session start failure of t against
// SessionStartRetryBudget. Below the budget the task keeps cause as its error
// and the caller re-queues the same attempt. Past the budget the task fails
// with the standard exhaustion shape, keeping its StartFailureTail for
// post-mortem, and its session is deleted; exhausted reports that. The normal
// PRD retry budget is never touched here.
func (m *Manager) countSessionStartFailure(ctx context.Context, prd *PRD, t *Task, run attemptRun, cause error) (exhausted bool, err error) {
	t.StartFailures++
	if t.StartFailures > SessionStartRetryBudget {
		oldStatus := t.Status
		t.Status = TaskFailed
		t.Error = SessionStartFailureReason
		t.FailureReason = &FailureReason{
			Category:   "infra",
			Detail:     fmt.Sprintf("%s (%d consecutive start failures)", SessionStartFailureReason, t.StartFailures),
			Source:     "session-start",
			Retryable:  false,
			OccurredAt: time.Now(),
		}
		log.Printf("[autonomous] task %s failed: %s after %d consecutive start failures",
			t.ID, SessionStartFailureReason, t.StartFailures)
		m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeFailed, Category: "infra", Summary: cause.Error()})
		m.recordFailureLearning(prd, t.ID, "infra", t.FailureReason.Detail)
		m.relocateCheckpoint(prd, t)
		if serr := m.store.SaveTask(t); serr != nil {
			return true, serr
		}
		m.emitTaskStatus(prd, t, oldStatus, nil, nil)
		m.deleteTaskSession(t.SessionID)
		return true, nil
	}
	// Below budget: re-queue the SAME attempt. The normal retry budget is left
	// untouched and no RetryPolicy category is recorded.
	log.Printf("[autonomous] task %s start failure %d/%d — re-queueing without consuming the normal retry budget",
		t.ID, t.StartFailures, SessionStartRetryBudget)
	t.Error = cause.Error()
	m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{Outcome: attemptOutcomeFailed, Category: "infra", Summary: cause.Error(), Requeue: true})
	_ = m.store.SaveTask(t)
	return false, nil
}

func (m *Manager) deleteTaskSession(sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	fn := m.sessionDeleteFn
	m.mu.Unlock()
	if fn != nil {
		fn(sessionID)
	}
}

// flattenTasks extracts every Task from the PRD's stories.
func flattenTasks(prd *PRD) []*Task {
	var out []*Task
	for i := range prd.Story {
		// Phase 3 (v5.26.61) — skip stories that are gated on
		// per-story approval. The runner is re-entered when the
		// operator calls ApproveStory (which transitions awaiting_
		// approval → pending), at which point the story's tasks
		// re-enter this loop. Blocked stories are also skipped —
		// blocked is a terminal state until operator action.
		st := prd.Story[i].Status
		if st == StoryAwaitingApproval || st == StoryBlocked {
			continue
		}
		for j := range prd.Story[i].Tasks {
			// BL406 Phase 5 — same skip, at task granularity: a task
			// gated on GuidedModeSource=="operator" approval is
			// re-entered when ApproveTask transitions it back to
			// pending (see ApproveTask).
			if prd.Story[i].Tasks[j].Status == TaskAwaitingApproval {
				continue
			}
			// A task held for operator review does not run again until the
			// operator resolves it (see ResolveTaskReview).
			if prd.Story[i].Tasks[j].Status == TaskReviewNeeded {
				continue
			}
			out = append(out, &prd.Story[i].Tasks[j])
		}
	}
	return out
}

// lookupTask finds a task by ID in the PRD tree (returns the live
// pointer so SaveTask round-trips correctly).
// effectiveTaskConcurrency resolves how many independent tasks of one
// Automaton may run at once: the Automaton's own override wins when > 0,
// otherwise the daemon-wide default applies. A result <= 1 means the
// sequential path. Config.MaxParallelTasks is deliberately not an input.
func effectiveTaskConcurrency(prdOverride, global int) int {
	if prdOverride > 0 {
		return prdOverride
	}
	return global
}

func lookupTask(prd *PRD, id string) *Task {
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			if prd.Story[i].Tasks[j].ID == id {
				return &prd.Story[i].Tasks[j]
			}
		}
	}
	return nil
}

// lookupStoryByTaskID returns the Story that owns the given task ID.
func lookupStoryByTaskID(prd *PRD, taskID string) *Story {
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			if prd.Story[i].Tasks[j].ID == taskID {
				return &prd.Story[i]
			}
		}
	}
	return nil
}

// topoSort returns task IDs in dependency order (Kahn's algorithm).
// Returns an error if a cycle is detected.
func topoSort(tasks []*Task) ([]string, error) {
	indeg := map[string]int{}
	idx := map[string]*Task{}
	for _, t := range tasks {
		idx[t.ID] = t
		if _, seen := indeg[t.ID]; !seen {
			indeg[t.ID] = 0
		}
	}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := idx[dep]; ok {
				indeg[t.ID]++
			}
		}
	}
	var ready []string
	for id, n := range indeg {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	var out []string
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, id)
		for _, t := range tasks {
			for _, dep := range t.DependsOn {
				if dep == id {
					indeg[t.ID]--
					if indeg[t.ID] == 0 {
						ready = append(ready, t.ID)
					}
				}
			}
		}
		sort.Strings(ready)
	}
	if len(out) != len(tasks) {
		return nil, fmt.Errorf("task dependency cycle detected")
	}
	return out, nil
}

func joinLines(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += "\n- "
		}
		out += x
	}
	return out
}

// recurseChildPRD (BL191 Q4, v5.9.0) is the SpawnPRD shortcut: turn the
// parent task into a child PRD, decompose it, optionally auto-approve,
// run it inline, and roll the child outcome up to the parent task. The
// parent task only completes when the child PRD reaches PRDCompleted;
// any failure inside the child marks the parent task failed with a
// breadcrumb pointing at the child PRD ID.
//
// Depth is capped by Config.MaxRecursionDepth; a parent at depth ==
// MaxRecursionDepth refuses to spawn so a runaway can't tank the daemon.
func (m *Manager) recurseChildPRD(ctx context.Context, parent *PRD, t *Task, spawn SpawnFn, verify VerifyFn, retries int) error {
	// emitSaved persists t and, on success, emits its task-level
	// status change from old.
	emitSaved := func(old TaskStatus) error {
		if err := m.store.SaveTask(t); err != nil {
			return err
		}
		m.emitTaskStatus(parent, t, old, nil, nil)
		return nil
	}
	var old TaskStatus
	maxDepth := m.cfg.MaxRecursionDepth
	if maxDepth <= 0 {
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskFailed
			t.Error = "recursion disabled (autonomous.max_recursion_depth=0); set spawn_prd=false or raise the limit"
		})
		return emitSaved(old)
	}
	if parent.Depth+1 > maxDepth {
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskFailed
			t.Error = fmt.Sprintf("recursion depth %d would exceed autonomous.max_recursion_depth=%d", parent.Depth+1, maxDepth)
		})
		return emitSaved(old)
	}

	now := time.Now()
	t.StartedAt = &now
	old = t.Status
	m.store.WithLock(func() {
		t.Status = TaskInProgress
	})
	_ = emitSaved(old)

	// Inherit LLM defaults from parent task → parent PRD; the child PRD
	// can be re-decomposed with its own backend by the operator.
	// v8.37.2 — model only inherits from parent when backend also does
	// (same reasoning as the sibling cascade above: a model belongs to
	// whichever backend it was set alongside).
	backend, model := t.Backend, t.Model
	if backend == "" {
		backend = parent.Backend
		if model == "" {
			model = parent.Model
		}
	}
	effort := t.Effort
	if effort == "" {
		effort = parent.Effort
	}
	// BL447: the child PRD does this task's work, so it runs in the task's
	// resolved dir. (scopeSeedFn below stays on parent.ProjectDir: it reads
	// the parent's prd-shared memories, which are keyed by that dir.)
	child, err := m.store.CreatePRDWithParent(t.Spec, m.resolveTaskProjectDir(parent, t), backend, model, effort, parent.ID, t.ID, parent.Depth+1)
	if err != nil {
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskFailed
			t.Error = "spawn child PRD: " + err.Error()
		})
		return emitSaved(old)
	}
	t.ChildPRDID = child.ID
	old = t.Status
	m.store.WithLock(func() {
		t.Status = TaskInProgress
	})
	_ = emitSaved(old)

	// BL387 Phase 1b — inherit parent prd-shared memories into child so the
	// child decomposer and tasks start with the parent's accumulated findings.
	if parent.MemorySeed.Enabled {
		m.mu.Lock()
		scopeSeedFn := m.memoryScopeSeedFn
		m.mu.Unlock()
		if scopeSeedFn != nil {
			if serr := scopeSeedFn(ctx, parent.ID, child.ID, parent.ProjectDir, 50); serr != nil {
				log.Printf("[autonomous] child PRD scope-seed: parent=%s child=%s: %v", parent.ID, child.ID, serr)
			}
		}
	}

	// Decompose → (auto-)Approve → Run is the same lifecycle a root PRD
	// goes through. AutoApproveChildren defaults true because every level
	// otherwise hangs on operator review and recursion becomes useless;
	// operators can flip it off for high-stakes pipelines.
	if _, err := m.Decompose(child.ID); err != nil {
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskFailed
			t.Error = fmt.Sprintf("child PRD %s decompose: %v", child.ID, err)
		})
		return emitSaved(old)
	}
	if m.cfg.AutoApproveChildren {
		// approveAuto (not plain Approve): child plans that fail the stored
		// plan-coverage gate in block mode are auto-acknowledged with an
		// auditable record rather than failing the parent task — the
		// operator explicitly enabled this automation.
		if _, err := m.approveAuto(child.ID, "autonomous", fmt.Sprintf("auto-approve from parent task %s/%s", parent.ID, t.ID)); err != nil {
			old = t.Status
			m.store.WithLock(func() {
				t.Status = TaskFailed
				t.Error = fmt.Sprintf("child PRD %s auto-approve: %v", child.ID, err)
			})
			return emitSaved(old)
		}
	} else {
		// Operator-gated child: leave the child in needs_review and mark
		// the parent task blocked. The operator approves the child via
		// the usual /approve endpoint; the parent's loop tick re-checks
		// child status and unblocks when the child reaches PRDCompleted.
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskBlocked
			t.Error = fmt.Sprintf("waiting for operator approval on child PRD %s", child.ID)
		})
		return emitSaved(old)
	}
	if err := m.Run(ctx, child.ID, spawn, verify); err != nil {
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskFailed
			t.Error = fmt.Sprintf("child PRD %s run: %v", child.ID, err)
		})
		return emitSaved(old)
	}

	// Roll up child outcome onto the parent task.
	updated, _ := m.store.GetPRD(child.ID)
	done := time.Now()
	t.CompletedAt = &done
	if updated == nil || updated.Status != PRDCompleted {
		old = t.Status
		m.store.WithLock(func() {
			t.Status = TaskFailed
			t.Error = fmt.Sprintf("child PRD %s ended with status %s", child.ID, statusOrUnknown(updated))
		})
		return emitSaved(old)
	}
	old = t.Status
	m.store.WithLock(func() {
		t.Status = TaskCompleted
	})
	t.Verification = &VerificationResult{
		OK:         true,
		Summary:    fmt.Sprintf("child PRD %s completed (%d stories)", child.ID, len(updated.Story)),
		VerifiedAt: time.Now(),
	}
	return emitSaved(old)
}

func statusOrUnknown(p *PRD) PRDStatus {
	if p == nil {
		return "unknown"
	}
	return p.Status
}

// runPerTaskGuardrails (BL191 Q6, v5.10.0; BL303 S2) invokes each guardrail
// in the resolved task-level list after a task verifies green. Appends
// Verdicts to the task and returns blocked=true on any block outcome.
//
// BL303 S2: guardrail list is resolved via resolveGuardrails (per-PRD
// explicit > named profile > global Config). Scan-type guardrails ("sast-scan",
// "secrets-scan", "deps-scan") are dispatched to invokeScanGuardrail instead
// of GuardrailFn; other names fall through to GuardrailFn. Both paths are
// silent no-ops when neither is available.
//
// BL416 Phase 3: the task's VerificationScript (if any) runs once before
// the loop; a clean exit 0 replaces every non-scan GuardrailFn call with a
// deterministic pass verdict, otherwise its real output rides inv into the
// judgment call. Resolved GuardrailBackend/GuardrailModel always ride inv.
// runPerTaskGuardrails returns (blocked, contentHint, err). contentHint
// is non-empty only when a content-category block occurred, content
// auto-fix is enabled, and budget remains (BL410 Phase 2) — the caller
// (executeOne) should retry the SAME task with contentHint fed in as a
// RetryHint, the exact mechanism a failed verification already uses,
// rather than marking the task blocked. blocked always takes priority
// over a non-empty contentHint (a genuine non-retryable block from one
// guardrail must not be masked by another guardrail's retryable one).
func (m *Manager) runPerTaskGuardrails(ctx context.Context, prd *PRD, t *Task) (blocked bool, contentHint string, err error) {
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	// v9.10.18 — judge resolution: model resolution is paired with the
	// resolved backend and NEVER falls back to the backend's built-in
	// default. An unresolvable route does not run a judge — it records
	// a loud, non-retried infra-category block so the misconfiguration
	// is visible instead of evicting the operator's pinned model.
	gb, gm, pinned, resErr := m.resolveUnitGuardrailModel(t, prd)

	guardrails := m.resolveGuardrails(prd, "task")
	if len(guardrails) == 0 {
		return false, "", nil
	}
	inv := GuardrailInvocation{
		PRDID:      prd.ID,
		Level:      "task",
		UnitID:     t.ID,
		UnitTitle:  t.Title,
		UnitSpec:   t.Spec,
		ProjectDir: m.resolveTaskProjectDir(prd, t), // BL447: judge the task in its own dir
		OwnerPeer:  prd.OwnerPeer,                   // BL369 Layer 3 — federation trust boundary
	}
	if resErr == nil {
		inv.GuardrailBackend, inv.GuardrailModel = gb, gm
		inv.GuardrailPinned = pinned
	}
	scriptPassed := prepareScriptEvidence(&inv, t.VerificationScript, inv.ProjectDir) // BL447
	for _, g := range guardrails {
		inv.Guardrail = g
		var v GuardrailVerdict
		var verr error
		if entry, ok := m.lookupGuardrailEntry(g); ok && entry.Type == "scan" {
			v, verr = m.invokeScanGuardrail(entry, inv)
		} else if scriptPassed {
			v = scriptPassVerdict(t.VerificationScript) // BL416 P3: no LLM call on a clean deterministic pass
		} else if fn != nil {
			if resErr != nil {
				// v9.10.18 — no judge model resolvable at any level: loud,
				// non-retried infra block (constructed directly, never via
				// runGuardrailWithInfraRetry — retrying cannot configure a
				// model that is not configured).
				v = NoGuardrailModelVerdict(g, resErr)
			} else {
				// BL410 Phase 1 — bounded, escalating auto-retry for an
				// infra-category block (the guardrail session itself
				// failed, no real judgment ran). A content/scope/policy
				// block, or infra once the budget is exhausted, falls
				// through unchanged to the TaskBlocked handling below.
				v, verr = m.runGuardrailWithInfraRetry(ctx, prd, inv, fn)
			}
		} else {
			continue // silent no-op: scan not in lib + no GuardrailFn
		}
		if verr != nil {
			return false, "", fmt.Errorf("guardrail %s on task %s: %w", g, t.ID, verr)
		}
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		v.Guardrail = g
		if err := m.recordGuardrailVerdict(prd, t.ID, v); err != nil {
			return false, "", err
		}
		t.Verdicts = append(t.Verdicts, v)
		if v.Outcome == "block" {
			// BL410 Phase 2 — a content-category block (a real judgment,
			// not an exhausted infra retry) gets one auto-fix attempt
			// when enabled and budget remains: retry the SAME task with
			// the guardrail's own findings as a hint, exactly the
			// mechanism a failed verification already uses.
			if v.Category == "" && m.resolveContentAutoFixEnabled(prd) && m.retryBudgetRemaining(prd, "content") {
				_ = m.consumeRetryBudget(prd, "content")
				contentHint = contentFixHint(v)
				m.recordAutoRetryDecision(prd, t.ID, "content", prd.RetryCountsByCategory["content"], false)
				continue
			}
			blocked = true
			m.recordFailureLearning(prd, t.ID, v.Category, contentFixHint(v)) // BL410 Phase 3
			// BL410 remediation (2026-10-13)
			t.FailureReason = &FailureReason{
				Category: orContentCategory(v.Category), Detail: contentFixHint(v),
				Source: "guardrail-task-block", Retryable: false, OccurredAt: time.Now(),
			}
		}
	}
	return blocked, contentHint, nil
}

// contentFixHint (BL410 Phase 2) builds the retry hint text fed into
// SpawnRequest.RetryHint from a content-category guardrail verdict's
// own findings — the same hint shape a failed verification already
// produces, just sourced from a guardrail instead.
func contentFixHint(v GuardrailVerdict) string {
	findings := v.Summary
	if len(v.Issues) > 0 {
		findings = "- " + strings.Join(v.Issues, "\n- ")
	}
	return fmt.Sprintf("The %q guardrail found a real problem with your previous attempt:\n\n%s\n\nFix these issues and re-run the project's own build/lint/test commands yourself. Do not run git add or git commit — your work is committed automatically when you finish; finish with DATAWATCH_COMPLETE: <one-line summary of what you did>.", v.Guardrail, findings)
}

// runPerStoryGuardrails (BL191 Q6, v5.10.0; BL303 S2) — same shape as the
// per-task variant but for a Story. See runPerTaskGuardrails for BL303 notes.
//
// Returns (blocked, contentFixVerdict, err). Unlike the task-level
// variant, a story has no single unit to respawn with a hint — a
// content-category block eligible for auto-fix (BL410 Phase 2) is
// returned via contentFixVerdict instead, for the caller to synthesize
// corrective work from (synthesizeCorrectiveWork) and recurse into
// Run() again, rather than being retried in-loop here.
func (m *Manager) runPerStoryGuardrails(ctx context.Context, prd *PRD, s *Story) (blocked bool, contentFixVerdict *GuardrailVerdict, err error) {
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	// v9.10.18 — see runPerTaskGuardrails: paired model resolution, no
	// backend-default fallback, loud non-retried infra block on failure.
	// No story-level guardrail routing exists, so an empty Task means
	// no task-level tier for this call.
	gb, gm, pinned, resErr := m.resolveUnitGuardrailModel(&Task{}, prd)

	guardrails := m.resolveGuardrails(prd, "story")
	if len(guardrails) == 0 {
		return false, nil, nil
	}
	inv := GuardrailInvocation{
		PRDID:      prd.ID,
		Level:      "story",
		UnitID:     s.ID,
		UnitTitle:  s.Title,
		UnitSpec:   s.Description,
		ProjectDir: storyEvidenceDir(prd, s), // BL447: see storyEvidenceDir
		OwnerPeer:  prd.OwnerPeer,            // BL369 Layer 3 — federation trust boundary
	}
	if resErr == nil {
		inv.GuardrailBackend, inv.GuardrailModel = gb, gm
		inv.GuardrailPinned = pinned
	}
	scriptPassed := prepareScriptEvidence(&inv, s.VerificationScript, inv.ProjectDir) // BL447
	for _, g := range guardrails {
		inv.Guardrail = g
		var v GuardrailVerdict
		var verr error
		if entry, ok := m.lookupGuardrailEntry(g); ok && entry.Type == "scan" {
			v, verr = m.invokeScanGuardrail(entry, inv)
		} else if scriptPassed {
			v = scriptPassVerdict(s.VerificationScript) // BL416 P3: no LLM call on a clean deterministic pass
		} else if fn != nil {
			if resErr != nil {
				// v9.10.18 — see runPerTaskGuardrails for the identical block.
				v = NoGuardrailModelVerdict(g, resErr)
			} else {
				// BL410 Phase 1 — see runPerTaskGuardrails' identical comment.
				v, verr = m.runGuardrailWithInfraRetry(ctx, prd, inv, fn)
			}
		} else {
			continue // silent no-op
		}
		if verr != nil {
			return false, nil, fmt.Errorf("guardrail %s on story %s: %w", g, s.ID, verr)
		}
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		v.Guardrail = g
		if err := m.recordGuardrailVerdict(prd, s.ID, v); err != nil {
			return false, nil, err
		}
		s.Verdicts = append(s.Verdicts, v)
		if v.Outcome == "block" {
			// BL410 Phase 2 — same content-auto-fix eligibility check as
			// runPerTaskGuardrails, but hands the verdict back for the
			// caller to synthesize a new task into this story from,
			// since there's no single existing unit to respawn here.
			if v.Category == "" && m.resolveContentAutoFixEnabled(prd) && m.retryBudgetRemaining(prd, "content") {
				_ = m.consumeRetryBudget(prd, "content")
				vCopy := v
				contentFixVerdict = &vCopy
				m.recordAutoRetryDecision(prd, s.ID, "content", prd.RetryCountsByCategory["content"], false)
				continue
			}
			blocked = true
			m.recordFailureLearning(prd, s.ID, v.Category, contentFixHint(v)) // BL410 Phase 3
			// BL410 remediation (2026-10-13)
			s.FailureReason = &FailureReason{
				Category: orContentCategory(v.Category), Detail: contentFixHint(v),
				Source: "guardrail-story-block", Retryable: false, OccurredAt: time.Now(),
			}
		}
	}
	return blocked, contentFixVerdict, nil
}

// prepareScriptEvidence (BL416 Phase 3) runs the unit's VerificationScript
// ONCE, before any guardrail is invoked, and records its real result on
// inv so every judgment call sees the same evidence. Returns true only
// when the script ran and exited 0. An invocation failure (unsafe path,
// bash missing) is recorded as a failed run — never an auto-pass. An
// empty script leaves inv untouched (pre-BL416 behavior).
func prepareScriptEvidence(inv *GuardrailInvocation, script, projectDir string) (scriptPassed bool) {
	if script == "" {
		return false
	}
	output, exitCode, err := runVerificationScript(script, projectDir)
	inv.ScriptRan = true
	inv.ScriptExitCode = exitCode
	inv.ScriptOutput = output
	if err != nil {
		if inv.ScriptExitCode == 0 {
			inv.ScriptExitCode = -1
		}
		inv.ScriptOutput = "verification script could not be run: " + err.Error()
		if output != "" {
			inv.ScriptOutput += "\n" + output
		}
		return false
	}
	return exitCode == 0
}

// scriptPassVerdict is the deterministic verdict recorded in place of a
// GuardrailFn call when the unit's verification script exited 0.
func scriptPassVerdict(script string) GuardrailVerdict {
	return GuardrailVerdict{
		Outcome:  "pass",
		Severity: "info",
		Summary:  fmt.Sprintf("verification script %s passed (exit 0)", script),
	}
}

// isTaskTerminal returns true for states from which a task will not
// transition further without operator intervention.
func isTaskTerminal(s TaskStatus) bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled
}

// storyDone, nonTerminalRemain and taskTerminal are storyAllTasksDone,
// anyNonTerminalTasksRemain and isTaskTerminal evaluated under the store
// lock, for the Run coordinator reading status fields that concurrent
// executeOne goroutines write.
func (m *Manager) storyDone(s *Story) (done bool) {
	m.store.WithLock(func() { done = storyAllTasksDone(s) })
	return done
}

func (m *Manager) nonTerminalRemain(prd *PRD) (remain bool) {
	m.store.WithLock(func() { remain = anyNonTerminalTasksRemain(prd) })
	return remain
}

func (m *Manager) taskTerminal(t *Task) (term bool) {
	m.store.WithLock(func() { term = isTaskTerminal(t.Status) })
	return term
}

// storyAllTasksDone returns true when every task in the story has reached
// a terminal state (completed, failed, or cancelled).
func storyAllTasksDone(s *Story) bool {
	if len(s.Tasks) == 0 {
		return false
	}
	for _, t := range s.Tasks {
		if !isTaskTerminal(t.Status) {
			return false
		}
	}
	return true
}

// anyNonTerminalTasksRemain reports whether the PRD still has tasks that
// haven't reached a terminal state. Used to decide whether a just-failed
// story's halt actually protects any later, independent work — if this
// was the PRD's only (or last) story, there's nothing left to halt before,
// so the normal end-of-Run rollup should produce PRDFailed rather than an
// operator-facing PRDBlocked with no pending work to review.
func anyNonTerminalTasksRemain(prd *PRD) bool {
	for _, s := range prd.Story {
		for _, t := range s.Tasks {
			if !isTaskTerminal(t.Status) {
				return true
			}
		}
	}
	return false
}

// storyRollupStatus is StoryFailed when any task failed, else StoryCompleted.
func storyRollupStatus(s *Story) StoryStatus {
	for _, t := range s.Tasks {
		if t.Status == TaskFailed {
			return StoryFailed
		}
	}
	return StoryCompleted
}

// findStory returns the Story in prd whose ID matches storyID, or nil.
// BL381 — used by the executor to resolve per-story LLM overrides.
func findStory(prd *PRD, storyID string) *Story {
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			return &prd.Story[i]
		}
	}
	return nil
}

// Execution-source values: the level that supplied a task's execution
// backend (recorded as Task.ExecutorSource). "global" means no level set a
// backend and the fallback backend was used. Mirrors verifier_source.
const (
	ExecutorSourceTask       = "task"
	ExecutorSourceStory      = "story"
	ExecutorSourcePRD        = "prd"
	ExecutorSourceAutomation = "automation"
	ExecutorSourceGlobal     = "global"
	// ExecutorSourceEscalation marks an attempt whose worker was chosen by a
	// failure escalation step rather than by the levels above.
	ExecutorSourceEscalation = "escalation"
)

// resolveTaskBackendModel resolves a task's effective backend and model.
// It is resolveTaskExecutionLLM without the source.
func resolveTaskBackendModel(t *Task, story *Story, prd *PRD, auto *Automation, fallbackBackend string) (backend, model string) {
	backend, model, _ = resolveTaskExecutionLLM(t, story, prd, auto, fallbackBackend)
	return backend, model
}

// resolveTaskExecutionLLM resolves a task's effective backend and model
// (BL203 + BL381, most-specific wins: task → story → PRD → Automation →
// fallback) and reports which level supplied the backend (one of the
// ExecutorSource* values). auto is the Automation the task's PRD belongs
// to, or nil for a standalone PRD.
//
// v8.37.2 — model is resolved from the SAME level backend was found at,
// not independently. A model string belongs to whichever backend it was
// configured alongside; letting it cascade past a level that overrides
// backend pairs a stale model (e.g. an Ollama model name) with a newly
// and incompatible backend (e.g. claude-code), which breaks the spawned
// session outright instead of using that backend's own default. Found
// live: overriding a stalled opencode/qwen3.8:27b task's backend to
// claude-code via set_task_llm (leaving model empty, per its own
// documented "empty = backend default" contract) still passed the PRD's
// stale "ollama/qwen3.8:27b" model string through to the claude-code
// session, which doesn't understand it and got stuck on its own
// interactive "unknown model" prompt instead of completing.
func resolveTaskExecutionLLM(t *Task, story *Story, prd *PRD, auto *Automation, fallbackBackend string) (backend, model, source string) {
	backend, model = t.Backend, t.Model
	switch {
	case backend != "":
		// task-level backend override: model is the task's own
		// (possibly empty = that backend's default).
		source = ExecutorSourceTask
	case story != nil && story.Backend != "":
		backend = story.Backend
		source = ExecutorSourceStory
		if model == "" {
			model = story.Model
		}
	case prd.Backend != "":
		backend = prd.Backend
		source = ExecutorSourcePRD
		if model == "" {
			model = prd.Model
		}
	case auto != nil && auto.Backend != "":
		backend = auto.Backend
		source = ExecutorSourceAutomation
		if model == "" {
			model = auto.Model
		}
	default:
		backend = fallbackBackend
		source = ExecutorSourceGlobal
		if model == "" && story != nil {
			model = story.Model
		}
		if model == "" {
			model = prd.Model
		}
		if model == "" && auto != nil {
			model = auto.Model
		}
	}
	return backend, model, source
}

// resolveTaskEffort resolves a task's execution effort: task → story → PRD →
// Automation. Empty when no level sets one; the global effort fallback is
// applied downstream.
func resolveTaskEffort(t *Task, story *Story, prd *PRD, auto *Automation) Effort {
	effort := t.Effort
	if effort == "" && story != nil {
		effort = story.Effort
	}
	if effort == "" {
		effort = prd.Effort
	}
	if effort == "" && auto != nil {
		effort = auto.Effort
	}
	return effort
}

// resolveGuardrailBackendModel (BL416 Phase 1) was removed in v9.10.18:
// its model half returned an empty model whenever nothing was pinned and
// left the fallback to the execution path, so a pi-family judge launched
// with no override silently ran on the backend's built-in default model.
// Its successor is ResolveGuardrailModel (guardrail_model.go), which
// resolves the model at resolution time — PRD guardrail_model → task
// guardrail_model → autonomous.guardrail_model →
// autonomous.verification_model → autonomous.planning_model — paired
// with the resolved backend, and fails loudly (ErrNoGuardrailModel) when
// no tier resolves, instead of returning a judge with an empty model.
// The backend half (task → PRD → global verification_backend) is preserved
// verbatim inside resolveUnitGuardrailModelLocked (guardrail_model.go).

// prdAllTasksTerminal reports whether every task in every story of p is
// in a terminal state.
func prdAllTasksTerminal(p *PRD) bool {
	for _, s := range p.Story {
		for _, t := range s.Tasks {
			if !isTaskTerminal(t.Status) {
				return false
			}
		}
	}
	return true
}

// commitRollupStatus (BL451) commits the completion-rollup status via
// Store.CommitPRDIfRev: only if the PRD's Rev is still expectedRev, its
// status is still observed and every task is still terminal — all
// re-checked under the store lock. blockedReason is written together with
// status, so a guardrail block carries its reason in the same commit; pass
// "" for every status that is not a guardrail block. On a stale commit
// nothing is set, a rollup_aborted_stale decision is recorded and ok=false
// is returned: the concurrent writer (reset_task, cancel, another Run) is
// authoritative.
func (m *Manager) commitRollupStatus(prd *PRD, observed PRDStatus, expectedRev int, status PRDStatus, reason, blockedReason string) (old PRDStatus, ok bool) {
	err := m.store.CommitPRDIfRev(prd.ID, expectedRev, func(p *PRD) bool {
		if p.Status != observed || !prdAllTasksTerminal(p) {
			return false
		}
		old = p.Status
		p.Status = status
		p.BlockedReason = blockedReason
		return true
	})
	if err == nil {
		return old, true
	}
	if errors.Is(err, ErrPRDStale) {
		m.recordRollupAborted(prd.ID, fmt.Sprintf("%s -> %s", reason, status))
	} else {
		log.Printf("[autonomous] rollup commit: prd=%s: %v", prd.ID, err)
	}
	return "", false
}

// recordGateDisagreementDecision appends a gate_disagreement entry to the
// PRD's decision log (append-only operator timeline) and saves the PRD.
// The PRD is re-fetched from the store: the in-flight prd snapshot passed
// by executeOne may not carry the task's latest saved state, and saving the
// stale snapshot would clobber task rows. Never mutates task state — the
// caller already recorded the outcome on the task itself.
func (m *Manager) recordGateDisagreementDecision(prdID, taskID, policy, note string) {
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		p.Decisions = append(p.Decisions, Decision{
			At:    time.Now(),
			Kind:  "gate_disagreement",
			Actor: "autonomous",
			Note:  fmt.Sprintf("task %s (policy=%s): %s", taskID, policy, note),
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s save gate_disagreement decision: %v", prdID, err)
	}
}

// recordRollupAborted appends a rollup_aborted_stale decision to the
// freshly-read PRD. It never touches Status, so saving it cannot undo
// the concurrent write that made the rollup stale.
func (m *Manager) recordRollupAborted(prdID, note string) {
	log.Printf("[autonomous] prd=%s rollup aborted (stale): %s", prdID, note)
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	p.Decisions = append(p.Decisions, Decision{At: time.Now(), Kind: "rollup_aborted_stale", Actor: "autonomous", Note: note})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] rollup_aborted_stale save: prd=%s: %v", prdID, err)
	}
}

// rollupOwnSaves (BL451) returns how many PRD saves the completion
// guardrail helpers have made for prdID (see noteOwnSave), so the
// rollup can tell its own Rev bumps from a concurrent writer's.
func (m *Manager) rollupOwnSaves(prdID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ownSaves[prdID]
}

// noteOwnSaves counts n saves of the Automaton that a hook made on the
// executor's behalf as its own writes (see savePRDOwn).
func (m *Manager) noteOwnSaves(prdID string, n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ownSaves == nil {
		m.ownSaves = map[string]int{}
	}
	m.ownSaves[prdID] += n
}

// savePRDOwn saves prd and counts it as an executor-owned write.
func (m *Manager) savePRDOwn(prd *PRD) error {
	save := m.store.SavePRD
	if m.savePRDHook != nil {
		save = m.savePRDHook
	}
	err := save(prd)
	if err == nil {
		m.mu.Lock()
		if m.ownSaves == nil {
			m.ownSaves = map[string]int{}
		}
		m.ownSaves[prd.ID]++
		m.mu.Unlock()
	}
	return err
}
