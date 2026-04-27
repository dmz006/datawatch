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
	"fmt"
	"sort"
	"time"
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
	RetryHint  string // populated on retry: prior verifier's findings
	// v5.26.19 — F10 profile dispatch. When ClusterProfile is set,
	// SpawnFn dispatches the worker to the cluster (POST /api/agents)
	// instead of a local tmux session. ProjectProfile alone causes the
	// worker to clone the profile's git URL into ProjectDir before
	// running the task. Either or both can be empty.
	ProjectProfile string
	ClusterProfile string
}

// SpawnResult is what SpawnFn returns. SessionID is the datawatch
// session ID; Err is fatal (non-fatal failures should be encoded in
// VerifyResult.OK=false from VerifyFn instead).
type SpawnResult struct {
	SessionID string
	Err       error
}

// SpawnFn is the indirection used by the executor to start a worker.
type SpawnFn func(ctx context.Context, req SpawnRequest) (SpawnResult, error)

// VerifyFn is the BL25 hook. It receives the just-completed task and
// returns a VerificationResult. Daemon wires this to the BL103
// validator agent (or a fresh session.Manager.Start with a different
// backend); tests inject a fake.
type VerifyFn func(ctx context.Context, prd *PRD, task *Task) (VerificationResult, error)

// Run walks the PRD and runs every task to completion (or failure).
// Honors task DependsOn for ordering; runs unblocked tasks
// sequentially in v1 (max-parallel comes from the loop, not from here).
func (m *Manager) Run(ctx context.Context, prdID string, spawn SpawnFn, verify VerifyFn) error {
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
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning {
		return fmt.Errorf("prd %q status %q is not runnable; call /approve first", prdID, prd.Status)
	}
	prd.Status = PRDRunning
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "run", Actor: "autonomous"})
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	tasks := flattenTasks(prd)
	order, err := topoSort(tasks)
	if err != nil {
		return err
	}
	retries := m.cfg.AutoFixRetries
	if retries < 0 {
		retries = 0
	}
	for _, tid := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := lookupTask(prd, tid)
		if t == nil {
			continue
		}
		if err := m.executeOne(ctx, prd, t, spawn, verify, retries); err != nil {
			t.Status = TaskFailed
			t.Error = err.Error()
			_ = m.store.SaveTask(t)
			// continue to next task — caller decides whether to abort the PRD
		}
		// BL191 Q6 (v5.10.0) — when a task lands in TaskBlocked from a
		// per-task guardrail block verdict, halt the walk so the
		// operator can review before more tasks run.
		latest, _ := m.store.GetPRD(prdID)
		if latest != nil {
			if lt := lookupTask(latest, tid); lt != nil && lt.Status == TaskBlocked {
				latest.Status = PRDBlocked
				_ = m.store.SavePRD(latest)
				return nil
			}
			prd = latest
		}
	}
	// BL191 Q6 (v5.10.0) — fire per-story guardrails after each story's
	// tasks complete. Done as a second pass so the per-task guardrails
	// settle first; story-level block also halts the PRD.
	for i := range prd.Story {
		s := &prd.Story[i]
		if !storyAllTasksDone(s) {
			continue
		}
		if blocked, err := m.runPerStoryGuardrails(ctx, prd, s); err != nil {
			return err
		} else if blocked {
			prd.Status = PRDBlocked
			_ = m.store.SavePRD(prd)
			return nil
		}
	}
	// Roll up PRD status: completed if every task is completed.
	allDone := true
	for _, s := range prd.Story {
		for _, t := range s.Tasks {
			if t.Status != TaskCompleted {
				allDone = false
				break
			}
		}
	}
	if allDone {
		prd.Status = PRDCompleted
	}
	return m.store.SavePRD(prd)
}

// executeOne handles a single task: spawn → wait → verify → (retry).
//
// BL191 Q4 (v5.9.0) — when t.SpawnPRD is true, the task spec is treated
// as a *child PRD* rather than a worker prompt. The executor delegates
// to recurseChildPRD which walks the same Decompose → (auto-)Approve →
// Run cycle the parent went through. Recursion depth is bounded by
// Config.MaxRecursionDepth so a runaway decomposition can't tank the
// daemon.
func (m *Manager) executeOne(ctx context.Context, prd *PRD, t *Task, spawn SpawnFn, verify VerifyFn, retries int) error {
	if t.SpawnPRD {
		return m.recurseChildPRD(ctx, prd, t, spawn, verify, retries)
	}
	hint := ""
	for attempt := 0; attempt <= retries; attempt++ {
		now := time.Now()
		t.StartedAt = &now
		t.Status = TaskInProgress
		t.RetryCount = attempt
		_ = m.store.SaveTask(t)

		// BL203 (v5.4.0) — most-specific LLM override wins. Per-task fields
		// take precedence over PRD-level fields; SpawnFn applies the
		// session.llm_backend global default when both are empty.
		backend := t.Backend
		if backend == "" {
			backend = prd.Backend
		}
		effort := t.Effort
		if effort == "" {
			effort = prd.Effort
		}
		model := t.Model
		if model == "" {
			model = prd.Model
		}
		sr, err := spawn(ctx, SpawnRequest{
			TaskID:         t.ID,
			StoryID:        t.StoryID,
			PRDID:          t.PRDID,
			Title:          t.Title,
			Spec:           t.Spec,
			ProjectDir:     prd.ProjectDir,
			Backend:        backend,
			Effort:         effort,
			Model:          model,
			RetryHint:      hint,
			ProjectProfile: prd.ProjectProfile, // v5.26.19
			ClusterProfile: prd.ClusterProfile, // v5.26.19
		})
		if err != nil {
			return fmt.Errorf("spawn: %w", err)
		}
		t.SessionID = sr.SessionID
		t.Status = TaskVerifying
		_ = m.store.SaveTask(t)

		var vr VerificationResult
		if verify != nil {
			vr, err = verify(ctx, prd, t)
			if err != nil {
				return fmt.Errorf("verify: %w", err)
			}
		} else {
			vr = VerificationResult{OK: true, Summary: "no verifier configured", VerifiedAt: time.Now()}
		}
		t.Verification = &vr
		if vr.OK {
			done := time.Now()
			t.CompletedAt = &done
			t.Status = TaskCompleted
			// BL191 Q6 (v5.10.0) — fire per-task guardrails before
			// declaring the task done. A `block` verdict marks the task
			// blocked + halts the PRD walk via the executor return.
			if blocked, err := m.runPerTaskGuardrails(ctx, prd, t); err != nil {
				return err
			} else if blocked {
				t.Status = TaskBlocked
				return m.store.SaveTask(t)
			}
			return m.store.SaveTask(t)
		}
		hint = vr.Summary
		if len(vr.Issues) > 0 {
			hint += "\nIssues:\n- " + joinLines(vr.Issues)
		}
	}
	t.Status = TaskFailed
	t.Error = "verification failed after retries"
	return m.store.SaveTask(t)
}

// flattenTasks extracts every Task from the PRD's stories.
func flattenTasks(prd *PRD) []*Task {
	var out []*Task
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			out = append(out, &prd.Story[i].Tasks[j])
		}
	}
	return out
}

// lookupTask finds a task by ID in the PRD tree (returns the live
// pointer so SaveTask round-trips correctly).
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
	maxDepth := m.cfg.MaxRecursionDepth
	if maxDepth <= 0 {
		t.Status = TaskFailed
		t.Error = "recursion disabled (autonomous.max_recursion_depth=0); set spawn_prd=false or raise the limit"
		return m.store.SaveTask(t)
	}
	if parent.Depth+1 > maxDepth {
		t.Status = TaskFailed
		t.Error = fmt.Sprintf("recursion depth %d would exceed autonomous.max_recursion_depth=%d", parent.Depth+1, maxDepth)
		return m.store.SaveTask(t)
	}

	now := time.Now()
	t.StartedAt = &now
	t.Status = TaskInProgress
	_ = m.store.SaveTask(t)

	// Inherit LLM defaults from parent task → parent PRD; the child PRD
	// can be re-decomposed with its own backend by the operator.
	backend := t.Backend
	if backend == "" {
		backend = parent.Backend
	}
	effort := t.Effort
	if effort == "" {
		effort = parent.Effort
	}

	child, err := m.store.CreatePRDWithParent(t.Spec, parent.ProjectDir, backend, effort, parent.ID, t.ID, parent.Depth+1)
	if err != nil {
		t.Status = TaskFailed
		t.Error = "spawn child PRD: " + err.Error()
		return m.store.SaveTask(t)
	}
	t.ChildPRDID = child.ID
	t.Status = TaskInProgress
	_ = m.store.SaveTask(t)

	// Decompose → (auto-)Approve → Run is the same lifecycle a root PRD
	// goes through. AutoApproveChildren defaults true because every level
	// otherwise hangs on operator review and recursion becomes useless;
	// operators can flip it off for high-stakes pipelines.
	if _, err := m.Decompose(child.ID); err != nil {
		t.Status = TaskFailed
		t.Error = fmt.Sprintf("child PRD %s decompose: %v", child.ID, err)
		return m.store.SaveTask(t)
	}
	if m.cfg.AutoApproveChildren {
		if _, err := m.Approve(child.ID, "autonomous", fmt.Sprintf("auto-approve from parent task %s/%s", parent.ID, t.ID)); err != nil {
			t.Status = TaskFailed
			t.Error = fmt.Sprintf("child PRD %s auto-approve: %v", child.ID, err)
			return m.store.SaveTask(t)
		}
	} else {
		// Operator-gated child: leave the child in needs_review and mark
		// the parent task blocked. The operator approves the child via
		// the usual /approve endpoint; the parent's loop tick re-checks
		// child status and unblocks when the child reaches PRDCompleted.
		t.Status = TaskBlocked
		t.Error = fmt.Sprintf("waiting for operator approval on child PRD %s", child.ID)
		return m.store.SaveTask(t)
	}
	if err := m.Run(ctx, child.ID, spawn, verify); err != nil {
		t.Status = TaskFailed
		t.Error = fmt.Sprintf("child PRD %s run: %v", child.ID, err)
		return m.store.SaveTask(t)
	}

	// Roll up child outcome onto the parent task.
	updated, _ := m.store.GetPRD(child.ID)
	done := time.Now()
	t.CompletedAt = &done
	if updated == nil || updated.Status != PRDCompleted {
		t.Status = TaskFailed
		t.Error = fmt.Sprintf("child PRD %s ended with status %s", child.ID, statusOrUnknown(updated))
		return m.store.SaveTask(t)
	}
	t.Status = TaskCompleted
	t.Verification = &VerificationResult{
		OK:         true,
		Summary:    fmt.Sprintf("child PRD %s completed (%d stories)", child.ID, len(updated.Story)),
		VerifiedAt: time.Now(),
	}
	return m.store.SaveTask(t)
}

func statusOrUnknown(p *PRD) PRDStatus {
	if p == nil {
		return "unknown"
	}
	return p.Status
}

// runPerTaskGuardrails (BL191 Q6, v5.10.0) invokes each guardrail in
// Config.PerTaskGuardrails after a task verifies green. Appends
// Verdicts to the task and returns blocked=true on any block outcome.
// Returns (false, nil) when no guardrails are configured or no
// GuardrailFn is wired (silent no-op so tests + bare-daemon mode pass
// without forcing the operator to mock the validator chain).
func (m *Manager) runPerTaskGuardrails(ctx context.Context, prd *PRD, t *Task) (bool, error) {
	m.mu.Lock()
	guardrails := append([]string{}, m.cfg.PerTaskGuardrails...)
	fn := m.guardrail
	m.mu.Unlock()
	if len(guardrails) == 0 || fn == nil {
		return false, nil
	}
	blocked := false
	for _, g := range guardrails {
		v, err := fn(ctx, GuardrailInvocation{
			PRDID:      prd.ID,
			Level:      "task",
			UnitID:     t.ID,
			UnitTitle:  t.Title,
			UnitSpec:   t.Spec,
			Guardrail:  g,
			ProjectDir: prd.ProjectDir,
		})
		if err != nil {
			return false, fmt.Errorf("guardrail %s on task %s: %w", g, t.ID, err)
		}
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		v.Guardrail = g
		t.Verdicts = append(t.Verdicts, v)
		if v.Outcome == "block" {
			blocked = true
		}
	}
	return blocked, nil
}

// runPerStoryGuardrails (BL191 Q6, v5.10.0) — same shape as the per-
// task variant but for a Story.
func (m *Manager) runPerStoryGuardrails(ctx context.Context, prd *PRD, s *Story) (bool, error) {
	m.mu.Lock()
	guardrails := append([]string{}, m.cfg.PerStoryGuardrails...)
	fn := m.guardrail
	m.mu.Unlock()
	if len(guardrails) == 0 || fn == nil {
		return false, nil
	}
	blocked := false
	for _, g := range guardrails {
		v, err := fn(ctx, GuardrailInvocation{
			PRDID:      prd.ID,
			Level:      "story",
			UnitID:     s.ID,
			UnitTitle:  s.Title,
			UnitSpec:   s.Description,
			Guardrail:  g,
			ProjectDir: prd.ProjectDir,
		})
		if err != nil {
			return false, fmt.Errorf("guardrail %s on story %s: %w", g, s.ID, err)
		}
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		v.Guardrail = g
		s.Verdicts = append(s.Verdicts, v)
		if v.Outcome == "block" {
			blocked = true
		}
	}
	return blocked, nil
}

func storyAllTasksDone(s *Story) bool {
	if len(s.Tasks) == 0 {
		return false
	}
	for _, t := range s.Tasks {
		if t.Status != TaskCompleted {
			return false
		}
	}
	return true
}
