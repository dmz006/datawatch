package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// resolveVerifierBackendModel decides which backend+model the verifier's
// /api/ask call should use. v8.36.0 — previously this always fell back to a
// hardcoded "ollama" (and whatever model that resolved to daemon-wide) when
// verification_backend/model were unconfigured, ignoring the PRD's own
// backend entirely — so a Claude-backed PRD's work still got judged by
// whatever the global Ollama default happened to be (often a small model
// picked for other purposes). Default to the PRD's own backend/model
// instead when unconfigured — but only take effect when that backend
// resolves to an ask-compatible kind (ollama/openwebui, or a
// directly-configured Claude API entry): session-only kinds (claude-code,
// opencode, ...) have no single-shot ask adapter and can never serve this
// call regardless of whose backend it is (see inference.IsSessionBackendKind),
// so those fall through to the "ollama" default exactly as before this change.
//
// resolveAskBackend and askCompatible are passed in rather than closed over
// so this decision logic is a pure, independently testable function.
func resolveVerifierBackendModel(
	cfgBackend, cfgModel, prdBackend, prdModel string,
	resolveAskBackend func(raw string) (kind, model string, err error),
	askCompatible func(kind string) bool,
) (vbackend, vkind, verifyModel string) {
	usingPRDBackend := false
	vbackend = cfgBackend
	if vbackend == "" {
		vbackend = prdBackend
		usingPRDBackend = vbackend != ""
	}
	if vbackend == "" {
		vbackend = "ollama"
	}
	var vmodel string
	vkind, vmodel, _ = resolveAskBackend(vbackend)
	if !askCompatible(vkind) {
		vbackend = "ollama"
		usingPRDBackend = false
		vkind, vmodel, _ = resolveAskBackend(vbackend)
	}
	verifyModel = cfgModel
	if verifyModel == "" {
		if usingPRDBackend && prdModel != "" {
			verifyModel = prdModel
		} else {
			verifyModel = vmodel
		}
	}
	return vbackend, vkind, verifyModel
}

// gitWorkingTreeDiffSince returns the diff of UNCOMMITTED changes to tracked
// files that were modified at or after `since` — evidence the worker
// specifically produced during this task's own run, not a leftover from an
// earlier task, and not a concurrent unrelated edit to the same project_dir
// made before this task started.
//
// This is the signal a commit-range diff (`git diff preTaskSHA..HEAD`) cannot
// see: a worker that edits an already-tracked file without committing —
// the normal case when session.auto_git_commit is off, the operator default —
// leaves HEAD unmoved, so the commit-range diff is always empty even after
// real work. An unscoped `git diff HEAD` fixes that but overcorrects: it
// shows EVERY uncommitted change in the whole repo regardless of when it
// happened, so once any earlier task (or an unrelated concurrent edit to the
// same project_dir) leaves one uncommitted tracked-file change, every
// following task's verification sees that same stale diff as "its" evidence
// and can pass having done nothing — observed live on PRD 0fb4e302
// (2026-09-27): with the unscoped version, task 4 ("eval DAG node") and task
// 5 ("red-team pipeline") both passed verification within seconds of
// spawning, against a diff that was actually this file's own uncommitted
// source edits, not their target markdown files (which were untouched).
// Scoping by mtime-since-task-start closes that: a change made before the
// task's own StartedAt cannot be evidence of this task's work.
func gitWorkingTreeDiffSince(ctx context.Context, projectDir string, since time.Time) ([]byte, error) {
	nameOut, err := exec.CommandContext(ctx, "git", "-C", projectDir, "diff", "--name-only", "HEAD").Output()
	if err != nil {
		return nil, err
	}
	var recent []string
	for _, f := range strings.Split(strings.TrimSpace(string(nameOut)), "\n") {
		if f == "" {
			continue
		}
		fi, statErr := os.Stat(filepath.Join(projectDir, f))
		if statErr != nil {
			continue // deleted since; nothing to show for it
		}
		if !fi.ModTime().Before(since) {
			recent = append(recent, f)
		}
	}
	if len(recent) == 0 {
		return nil, nil
	}
	args := append([]string{"-C", projectDir, "diff", "HEAD", "--"}, recent...)
	return exec.CommandContext(ctx, "git", args...).Output()
}

// taskProducedNoOutput reports whether none of the verifier's evidence
// signals found anything: no committed diff, no uncommitted diff against a
// tracked file made during this task's run, no new untracked file, and no
// pre-existing untracked file the task's planned outputs say it touched.
func taskProducedNoOutput(committedDiff, workingDiff []byte, newUntrackedFiles, overwrittenFilesSections []string) bool {
	return len(committedDiff) == 0 && len(workingDiff) == 0 &&
		len(newUntrackedFiles) == 0 && len(overwrittenFilesSections) == 0
}
