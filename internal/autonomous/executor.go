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
type SpawnRequest struct {
	TaskID     string
	StoryID    string
	PRDID      string
	Title      string
	Spec       string
	ProjectDir string
	Backend    string
	Effort     Effort
	RetryHint  string // populated on retry: prior verifier's findings
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
	if len(prd.Story) == 0 {
		return fmt.Errorf("prd %q has no stories — call Decompose first", prdID)
	}
	prd.Status = PRDActive
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
func (m *Manager) executeOne(ctx context.Context, prd *PRD, t *Task, spawn SpawnFn, verify VerifyFn, retries int) error {
	hint := ""
	for attempt := 0; attempt <= retries; attempt++ {
		now := time.Now()
		t.StartedAt = &now
		t.Status = TaskInProgress
		t.RetryCount = attempt
		_ = m.store.SaveTask(t)

		sr, err := spawn(ctx, SpawnRequest{
			TaskID:     t.ID,
			StoryID:    t.StoryID,
			PRDID:      t.PRDID,
			Title:      t.Title,
			Spec:       t.Spec,
			ProjectDir: prd.ProjectDir,
			Backend:    prd.Backend,
			Effort:     prd.Effort,
			RetryHint:  hint,
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
