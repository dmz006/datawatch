package autonomous

import (
	"context"
	"log"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// driftGitRunner runs one git command in dir and returns its stdout.
// Injectable so tests (and callers with their own git plumbing) can
// substitute a fake; nil means defaultDriftGitRunner.
type driftGitRunner func(ctx context.Context, dir string, args ...string) (string, error)

// defaultDriftGitRunner shells out to git with -C dir. Read-only
// subcommands only (diff, log) — this file never mutates the repo.
func defaultDriftGitRunner(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed read-only git subcommands; dir comes from operator-controlled PRD ProjectDir, same trust model as runGitIn (worktree.go)
	out, err := cmd.Output()
	return string(out), err
}

// CapturePreTaskDirtyFiles returns the sorted, de-duplicated set of
// tracked files with unstaged or staged-but-uncommitted modifications
// in dir: git diff --name-only HEAD ∪ git diff --name-only --cached HEAD.
// Best-effort: an empty dir or any git error yields nil, never an error.
func CapturePreTaskDirtyFiles(ctx context.Context, dir string, run driftGitRunner) []string {
	if dir == "" {
		return nil
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	unstaged, err := run(ctx, dir, "diff", "--name-only", "HEAD")
	if err != nil {
		return nil
	}
	staged, err := run(ctx, dir, "diff", "--name-only", "--cached", "HEAD")
	if err != nil {
		return nil
	}
	return sortedUnion(splitLines(unstaged), splitLines(staged))
}

// CommittedFilesSince returns the de-duplicated union of every file
// touched by any commit in preSHA..HEAD in dir (git log --name-only).
// An empty preSHA yields nil, nil.
func CommittedFilesSince(ctx context.Context, dir, preSHA string, run driftGitRunner) ([]string, error) {
	if preSHA == "" {
		return nil, nil
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	out, err := run(ctx, dir, "log", preSHA+"..HEAD", "--name-only", "--format=")
	if err != nil {
		return nil, err
	}
	return sortedUnion(splitLines(out)), nil
}

// CommittedLineStatsSince returns the per-file changed-line count
// (added + removed) between preSHA and HEAD in dir (git diff --numstat).
// Paths are normalized with normalizeDriftPath; renames are reported under
// the new path and binary entries ("-\t-\tpath") count as
// 0, so a binary edit still has an entry. An empty preSHA, or no changed
// files, yields nil, nil.
func CommittedLineStatsSince(ctx context.Context, dir, preSHA string, run driftGitRunner) (map[string]int, error) {
	if preSHA == "" {
		return nil, nil
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	out, err := run(ctx, dir, "diff", "--numstat", preSHA, "HEAD")
	if err != nil {
		return nil, err
	}
	stats := map[string]int{}
	for _, l := range splitLines(out) {
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		n := normalizeDriftPath(numstatNewPath(parts[2]))
		if n == "" {
			continue
		}
		stats[n] += numstatLines(parts[0]) + numstatLines(parts[1])
	}
	if len(stats) == 0 {
		return nil, nil
	}
	return stats, nil
}

// numstatNewPath resolves a numstat path field to the post-change path:
// "a => b" and the brace form "dir/{a => b}/f" yield the new side.
func numstatNewPath(p string) string {
	if !strings.Contains(p, " => ") {
		return p
	}
	if open := strings.Index(p, "{"); open >= 0 {
		if end := strings.Index(p[open:], "}"); end >= 0 {
			inner := p[open+1 : open+end]
			if i := strings.Index(inner, " => "); i >= 0 {
				return strings.ReplaceAll(p[:open]+inner[i+len(" => "):]+p[open+end+1:], "//", "/")
			}
		}
	}
	return p[strings.LastIndex(p, " => ")+len(" => "):]
}

// numstatLines parses one numstat column; "-" (binary) and malformed
// values count as 0.
func numstatLines(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// CapturePostVerifySHA returns dir's current HEAD SHA, or "" when dir is
// empty or not a usable git repo. Best-effort: the caller (the executor's
// task-completion path) uses it to pin the commit the ci-watch
// PRD-completion guardrail should watch; a failure leaves the pin empty,
// never an error.
func CapturePostVerifySHA(ctx context.Context, dir string, run driftGitRunner) string {
	if dir == "" {
		return ""
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	out, err := run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(out)
	if len(sha) < 7 {
		return ""
	}
	return sha
}

// ClassifyFileDrift splits the files a task's own commit(s) touched
// into two disjoint groups, both sorted, de-duplicated and nil when
// empty:
//
//	preExisting — committed files that were ALREADY dirty or untracked
//	before the worker session started and were never planned by the
//	task: (a) in committed AND (b) in preTaskUntracked ∪ preTaskDirty
//	AND (c) NOT in task.FilesPlanned (json "files", the same list
//	LintTaskScope checks). These are files the task commit swept in
//	from outside its own work — exactly what DetectForeignFileDrift
//	returns.
//
//	inRun — committed files that were NOT dirty-or-untracked before
//	the task started: the worker's own edits, planned or not.
//
// A committed file that was pre-dirty/untracked AND in FilesPlanned
// belongs to neither group: it was expected work on a pre-existing
// edit. Paths are compared after normalizeDriftPath on every side.
// Pure and deterministic — no LLM, no git, no guardrail session.
func ClassifyFileDrift(task *Task, preTaskUntracked, preTaskDirty, committed []string) (preExisting, inRun []string) {
	pre := make(map[string]bool, len(preTaskUntracked)+len(preTaskDirty))
	for _, f := range sortedUnion(preTaskUntracked, preTaskDirty) {
		if n := normalizeDriftPath(f); n != "" {
			pre[n] = true
		}
	}
	planned := map[string]bool{}
	if task != nil {
		for _, f := range task.FilesPlanned {
			if n := normalizeDriftPath(f); n != "" {
				planned[n] = true
			}
		}
	}
	var preL, inL []string
	for _, f := range committed {
		n := normalizeDriftPath(f)
		if n == "" {
			continue
		}
		if pre[n] {
			if !planned[n] {
				preL = append(preL, n)
			}
		} else {
			inL = append(inL, n)
		}
	}
	return sortedUnion(preL), sortedUnion(inL)
}

// DetectForeignFileDrift returns the sorted files that a task's own
// commit(s) swept in although they were already dirty or untracked
// before its worker session started and were never planned by the
// task. It is the preExisting half of ClassifyFileDrift and keeps the
// same contract as before: pure and deterministic, nil when nothing
// drifted.
func DetectForeignFileDrift(task *Task, preTaskUntracked, preTaskDirty, committed []string) []string {
	preExisting, _ := ClassifyFileDrift(task, preTaskUntracked, preTaskDirty, committed)
	return preExisting
}

// normalizeDriftPath cleans a repo-relative path so "./x", "x" and
// "a/../x" compare equal. Empty input stays empty.
func normalizeDriftPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = path.Clean(p)
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	if p == "." {
		return ""
	}
	return p
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// sortedUnion returns the sorted, de-duplicated union of lists, or nil
// when it is empty.
func sortedUnion(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, f := range l {
			if f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}

// resolveDriftMode returns the resolved pre-existing-drift
// enforcement mode for prd: "block" or "warn". The mode is sourced
// from the global Config.DriftMode field (PRD/profile do not override
// it — an explicit PRD/profile opt-out is expressed via
// block_on_file_drift=false, see driftBlockExplicitlyOff). An empty or
// unknown value resolves to "block": fail-safe default, since an
// unrecognized mode must never silently relax enforcement.
func (m *Manager) resolveDriftMode(prd *PRD) string {
	_ = prd // reserved for a future per-PRD override; resolution is global today
	m.mu.Lock()
	mode := strings.ToLower(strings.TrimSpace(m.cfg.DriftMode))
	m.mu.Unlock()
	if mode == "warn" {
		return "warn"
	}
	return "block"
}

// driftBlockExplicitlyOff reports whether block_on_file_drift was
// explicitly set to false for prd at the PRD or guardrail-profile
// level — the operator's opt-out from blocking, honored even when
// DriftMode is "block". The global Config.BlockOnFileDrift default
// (false) is NOT an opt-out: it predates DriftMode and keeps its
// legacy opt-in shape for resolveBlockOnFileDrift.
func (m *Manager) driftBlockExplicitlyOff(prd *PRD) bool {
	if prd.BlockOnFileDrift != nil && !*prd.BlockOnFileDrift {
		return true
	}
	if prd.GuardrailProfile != "" {
		if p, ok := m.store.GetGuardrailProfile(prd.GuardrailProfile); ok && p.BlockOnFileDrift != nil && !*p.BlockOnFileDrift {
			return true
		}
	}
	return false
}

// resolveBlockOnFileDrift reports whether detected foreign-file drift
// should block the task (strict enforcement) rather than merely be
// flagged. Priority: explicit PRD override > named GuardrailProfile >
// global Config (default false — opt-in), the same 3-tier shape as
// resolveContentAutoFixEnabled and resolveGuardrails.
func (m *Manager) resolveBlockOnFileDrift(prd *PRD) bool {
	if prd.BlockOnFileDrift != nil {
		return *prd.BlockOnFileDrift
	}
	if prd.GuardrailProfile != "" {
		if p, ok := m.store.GetGuardrailProfile(prd.GuardrailProfile); ok && p.BlockOnFileDrift != nil {
			return *p.BlockOnFileDrift
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.BlockOnFileDrift
}

// checkForeignFileDrift runs the deterministic file-drift check for
// one worker attempt of t, right after its verifier returns. It
// classifies the files committed since t.PreTaskSHA via
// ClassifyFileDrift against the files that were dirty/untracked before
// the worker started:
//
//   - preExisting (committed, pre-dirty/untracked, unplanned) is the
//     operator's own work swept into the task's commit. It is recorded
//     on t.ForeignFileDrift, appended to the PRD as a
//     file_drift_detected Decision, audited and logged as a warning.
//     It BLOCKS the task (TaskBlocked) when the resolved DriftMode is
//     "block" (the default) AND block_on_file_drift was not explicitly
//     set false on the PRD or its guardrail profile (the operator's
//     opt-out); under DriftMode "warn" it is warn-only.
//   - inRun (committed, not pre-dirty/untracked, planned or not) is the
//     worker's own edits. It is recorded as a file_drift_detected
//     Decision noting it is an in-run edit, logged as a warning, and
//     NEVER blocks.
//
// It never reverts, resets, amends, un-stages or otherwise modifies
// any file.
func (m *Manager) checkForeignFileDrift(ctx context.Context, prd *PRD, t *Task) (blocked bool) {
	// Worktree mode runs each task in an isolated tree (see
	// ensureWorktreeIfConfigured) — nothing pre-dirty to protect.
	// BL447: PreTaskSHA was captured in the task's resolved dir, so the
	// committed-files walk must run there too.
	taskDir := m.resolveTaskProjectDir(prd, t)
	if prd.Git.Branch != "" || taskDir == "" || t.PreTaskSHA == "" {
		return false
	}
	committed, err := CommittedFilesSince(ctx, taskDir, t.PreTaskSHA, m.driftGit)
	if err != nil {
		log.Printf("[autonomous] %s/%s file-drift: git log since %s: %v", prd.ID, t.ID, t.PreTaskSHA, err)
		return false
	}
	preExisting, inRun := ClassifyFileDrift(t, t.PreTaskUntrackedFiles, t.PreTaskDirtyFiles, committed)
	if len(preExisting) == 0 && len(inRun) == 0 {
		return false
	}

	// In-run findings: the worker's own edits. Recorded + warned, and
	// structurally incapable of blocking the task.
	if len(inRun) > 0 {
		joined := strings.Join(inRun, ", ")
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: "file_drift_detected", Actor: "autonomous",
			Note: "task " + t.ID + ": in-run edit by the task's own worker (committed file(s) not pre-existing drift): " + joined,
		})
		m.appendDriftAudit(t.ID, prd.ID, "in_run", inRun)
		log.Printf("[autonomous] WARNING %s/%s in-run file drift: task committed its own file(s): %s", prd.ID, t.ID, joined)
	}

	blockPreExisting := false
	var preExistingDec Decision
	if len(preExisting) > 0 {
		joined := strings.Join(preExisting, ", ")
		t.ForeignFileDrift = sortedUnion(t.ForeignFileDrift, preExisting)
		preExistingDec = Decision{
			At: time.Now(), Kind: "file_drift_detected", Actor: "autonomous",
			Note: "task " + t.ID + ": committed pre-existing uncommitted file(s) outside its planned files: " + joined,
		}
		prd.Decisions = append(prd.Decisions, preExistingDec)
		m.appendDriftAudit(t.ID, prd.ID, "pre_existing", preExisting)
		log.Printf("[autonomous] WARNING %s/%s foreign file drift: task committed pre-existing uncommitted file(s) outside its planned files: %s", prd.ID, t.ID, joined)

		// Block only under the resolved mode (default "block") and only
		// when the operator has not explicitly opted out via
		// block_on_file_drift=false on the PRD or its guardrail profile.
		if m.resolveDriftMode(prd) == "block" && !m.driftBlockExplicitlyOff(prd) {
			blockPreExisting = true
		}
	}

	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s file-drift: save PRD: %v", prd.ID, t.ID, err)
	}
	if blockPreExisting {
		t.Status = TaskBlocked
		t.Error = "foreign_file_drift: " + strings.Join(preExisting, ", ")
	}
	_ = m.store.SaveTask(t)
	if len(preExisting) > 0 {
		// Opt-in scope_warning for the pre-existing (foreign) drift the
		// PWA surfaces from t.ForeignFileDrift; emitted after both saves
		// and after the mode handling, so it neither blocks nor alters
		// the task. The in-run group above stays unemitted (it has no
		// PWA warning).
		m.emitScopeWarning(prd, t, preExisting, &preExistingDec)
	}
	return blockPreExisting
}

// appendDriftAudit emits the file-drift audit event for one finding
// group, if an audit sink is wired.
func (m *Manager) appendDriftAudit(taskID, prdID, kind string, files []string) {
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn == nil {
		return
	}
	auditFn("autonomous_file_drift_detected", map[string]any{
		"task_id": taskID,
		"prd_id":  prdID,
		"kind":    kind,
		"files":   append([]string(nil), files...),
	})
}

// checkUndeclaredEdits runs the undeclared-edit check for one worker
// attempt of t, immediately after checkForeignFileDrift. Its input is the
// inRun set from ClassifyFileDrift (the worker's own edits; never
// re-derived here), minus every path in t.FilesPlanned and
// t.FilesGenerated. What remains is undeclared: it is stored on
// t.UndeclaredEdits (line counts summed across retries), appended to the
// PRD as an undeclared_edit_detected Decision, audited
// (autonomous_undeclared_edit_detected), logged and raised as a warn alert
// through the alert sink (nil-safe). Mode (EffectiveUndeclaredEditMode):
// "off" is a full no-op, "warn" never blocks, "block" sets TaskBlocked
// with error "undeclared_edit: <files>" and returns true. Worktree-mode
// PRDs, an empty task dir and an empty PreTaskSHA are skipped, as in
// checkForeignFileDrift. It never reverts, resets or un-stages anything.
func (m *Manager) checkUndeclaredEdits(ctx context.Context, prd *PRD, t *Task) (blocked bool) {
	mode := m.EffectiveUndeclaredEditMode("")
	if mode == "off" {
		return false
	}
	taskDir := m.resolveTaskProjectDir(prd, t)
	if prd.Git.Branch != "" || taskDir == "" || t.PreTaskSHA == "" {
		return false
	}
	committed, err := CommittedFilesSince(ctx, taskDir, t.PreTaskSHA, m.driftGit)
	if err != nil {
		log.Printf("[autonomous] %s/%s undeclared-edit: git log since %s: %v", prd.ID, t.ID, t.PreTaskSHA, err)
		return false
	}
	_, inRun := ClassifyFileDrift(t, t.PreTaskUntrackedFiles, t.PreTaskDirtyFiles, committed)
	declared := map[string]bool{}
	for _, f := range append(append([]string(nil), t.FilesPlanned...), t.FilesGenerated...) {
		if n := normalizeDriftPath(f); n != "" {
			declared[n] = true
		}
	}
	var findings []string
	for _, f := range inRun {
		if !declared[f] {
			findings = append(findings, f)
		}
	}
	if len(findings) == 0 {
		return false
	}
	stats, err := CommittedLineStatsSince(ctx, taskDir, t.PreTaskSHA, m.driftGit)
	if err != nil {
		log.Printf("[autonomous] %s/%s undeclared-edit: git numstat since %s: %v", prd.ID, t.ID, t.PreTaskSHA, err)
		stats = nil
	}
	lineDetail := map[string]int{}
	for _, f := range findings {
		lineDetail[f] = stats[f]
		found := false
		for i := range t.UndeclaredEdits {
			if t.UndeclaredEdits[i].Path == f {
				t.UndeclaredEdits[i].LinesChanged += stats[f]
				found = true
				break
			}
		}
		if !found {
			t.UndeclaredEdits = append(t.UndeclaredEdits, UndeclaredEdit{Path: f, LinesChanged: stats[f]})
		}
	}
	joined := strings.Join(findings, ", ")
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "undeclared_edit_detected", Actor: "autonomous",
		Note: "task " + t.ID + ": edited file(s) not in its planned or generated files: " + joined,
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s undeclared-edit: save PRD: %v", prd.ID, t.ID, err)
	}
	m.mu.Lock()
	auditFn := m.auditFn
	alertFn := m.alertSink
	m.mu.Unlock()
	if auditFn != nil {
		auditFn("autonomous_undeclared_edit_detected", map[string]any{
			"task_id": t.ID,
			"prd_id":  prd.ID,
			"files":   append([]string(nil), findings...),
			"lines":   lineDetail,
			"mode":    mode,
		})
	}
	if alertFn != nil {
		alertFn("warn", "Undeclared edit detected", "prd "+prd.ID+" task "+t.ID+": "+joined)
	}
	log.Printf("[autonomous] WARNING %s/%s undeclared edit: task edited file(s) not in its planned or generated files: %s", prd.ID, t.ID, joined)
	// Opt-in scope_warning at exactly the point the "Undeclared edit
	// detected" warn alert is raised: informational only, raised in both
	// warn and block mode, and the mode handling below stays as is.
	m.emitScopeWarning(prd, t, findings, lastDecision(prd, "undeclared_edit_detected"))
	if mode == "block" {
		t.Status = TaskBlocked
		t.Error = "undeclared_edit: " + joined
		_ = m.store.SaveTask(t)
		return true
	}
	_ = m.store.SaveTask(t)
	return false
}
