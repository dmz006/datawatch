// Production wiring for the task merge: the model that resolves a
// non-additive conflict goes through the planning model the Manager already
// has (its DecomposeFn), and the verification that gates the answer is the
// task's own verification script run in the merged tree.
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// mergeVerifyTimeout bounds the verification script run on a merged tree.
const mergeVerifyTimeout = 10 * time.Minute

// installDefaultMergeResolver wires the planning-model resolver and the
// script verification when the Manager has a planning model. An explicit
// SetMergeResolver afterwards replaces them.
func (m *Manager) installDefaultMergeResolver() {
	if m.decompose == nil {
		return
	}
	m.SetMergeResolver(m.planningModelMergeResolve, scriptMergeVerify)
}

// planningModelMergeResolve asks the planning model for the resolved content
// of every conflicted file. The answer is a JSON object path -> content.
func (m *Manager) planningModelMergeResolve(_ context.Context, req MergeResolveRequest) (map[string]string, error) {
	var b strings.Builder
	b.WriteString("Resolve the git merge conflicts below. Merge the intent of BOTH sides; drop nothing.\n")
	b.WriteString("Answer with ONLY a raw JSON object mapping each file path to its full resolved content, with no conflict markers, no markdown fences and no prose.\n\n")
	for _, f := range req.Files {
		fmt.Fprintf(&b, "=== FILE %s ===\n--- BASE ---\n%s\n--- PRD BRANCH ---\n%s\n--- TASK BRANCH ---\n%s\n\n", f.Path, f.Base, f.Ours, f.Theirs)
	}
	resp, err := m.decompose(DecomposeRequest{Spec: b.String(), Model: req.Model, ProjectDir: req.Dir, PRDID: req.PRD.ID})
	if err != nil {
		return nil, err
	}
	return parseMergeAnswer(resp)
}

// parseMergeAnswer extracts the path -> content object from a model reply.
func parseMergeAnswer(resp string) (map[string]string, error) {
	s := strings.TrimSpace(resp)
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("model answer has no JSON object")
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(s[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("model answer is not a path->content object: %w", err)
	}
	return out, nil
}

// scriptMergeVerify runs the task's verification script in the merged tree.
// With no script there is nothing to prove the model's resolution, so it is
// refused and the task is held for the operator.
func scriptMergeVerify(ctx context.Context, prd *PRD, t *Task, dir string) error {
	script := strings.TrimSpace(t.VerificationScript)
	if script == "" {
		return fmt.Errorf("task has no verification script to check the resolution")
	}
	if filepath.IsAbs(script) || strings.Contains(script, "..") {
		return fmt.Errorf("verification script path %q is outside the worktree", script)
	}
	ctx, cancel := context.WithTimeout(ctx, mergeVerifyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", filepath.Join(dir, script)) // #nosec G204 -- operator-defined script inside the PRD worktree
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ProposeMergeConflict sends a conflict prompt to the planning model named by
// model (an LLM registry name) and returns the raw reply. It is the same
// invocation planningModelMergeResolve uses, exported so the merge pipeline's
// conflict model can reuse it without a client of its own.
func (m *Manager) ProposeMergeConflict(prompt, model string) (string, error) {
	if m.decompose == nil {
		return "", fmt.Errorf("no planning model is configured")
	}
	return m.decompose(DecomposeRequest{Spec: prompt, Model: model})
}
