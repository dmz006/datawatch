// Task chain repairs for the housekeeping framework (group task_chain).
//
// Three repairs are registered on the framework:
//
//   - dependent reset: a task that failed only because a task it depends on
//     failed is reset (a plain reset, never a force-cancel) once every
//     dependency is fixed, and not before;
//   - stale docs mirror: a final gate that failed because the gitignored
//     embedded docs mirror is out of date runs the documented docs sync
//     command, then the gate is retried once;
//   - leaked file: a leaked marker or stray file that blocks a finish is
//     identified; only when it is untracked and inside the Automaton worktree
//     is the documented cleanup run, then the task is retried once.
//
// Each repair refuses, as an unsafe finding (recorded as skipped with one
// alert), when the failure reason is ambiguous, when a second retry would be
// needed, or when the file is tracked. Everything a repair can do goes
// through TaskChainOps: reset a task, run the documented sync, run the
// documented cleanup, read a path's state. There is no way to merge, ship,
// end a session, change a model or backend, or touch a security workflow.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// docsMirrorDir is the gitignored copy of docs/ that the embedded docs viewer
// reads; the documented sync command refreshes it from docs/.
const docsMirrorDir = "internal/server/web/docs"

// DocsSyncCommand and the cleanup command are the documented commands the
// repairs run. They are fixed here, never read from a failure message.
var (
	docsSyncCommand = []string{"make", "sync-docs"}
	// cleanupCommand removes one untracked path; git refuses to touch a
	// tracked file with -f clean.
	cleanupCommandPrefix = []string{"git", "clean", "-f", "--"}
)

// ChainTask is the view of one task the task chain repairs need.
type ChainTask struct {
	AutomatonID string
	TaskID      string
	Title       string
	Status      TaskStatus
	Kind        string
	DependsOn   []string
	// FailureSource, FailureCategory, FailureDetail and FailureEvidence come
	// from the task's recorded failure reason.
	FailureSource   string
	FailureCategory string
	FailureDetail   string
	FailureEvidence []string
	// PriorAttempts counts earlier resets of this task.
	PriorAttempts int
	// Worktree is the Automaton worktree directory the task runs in.
	Worktree string
}

// ChainPathState describes one path inside a worktree.
type ChainPathState struct {
	Exists  bool
	Tracked bool
}

// TaskChainOps is everything the task chain repairs may do or read.
type TaskChainOps interface {
	// ChainTasks lists the tasks of every Automaton that is not finished.
	ChainTasks(ctx context.Context) []ChainTask
	// ResetTask resets one task for another attempt. It is a plain reset: a
	// task that is not failed or blocked is refused.
	ResetTask(ctx context.Context, automatonID, taskID string) error
	// PathState reads whether rel exists under worktree and whether git
	// tracks it. An error means the state is unknown.
	PathState(ctx context.Context, worktree, rel string) (ChainPathState, error)
	// SyncDocs runs the documented docs sync command in worktree.
	SyncDocs(ctx context.Context, worktree string) (string, error)
	// Cleanup runs the documented cleanup of the one untracked path rel in
	// worktree.
	Cleanup(ctx context.Context, worktree, rel string) (string, error)
}

// NewTaskChainRepairs returns the three task chain repairs over ops. Nil ops
// gives none.
func NewTaskChainRepairs(ops TaskChainOps) []Repair {
	if ops == nil {
		return nil
	}
	return []Repair{
		&dependentResetRepair{ops: ops},
		&staleDocsGateRepair{chainRepairBase{ops: ops, tried: map[string]bool{}}},
		&leakedFileRepair{chainRepairBase{ops: ops, tried: map[string]bool{}}},
	}
}

func chainSubject(t ChainTask) string { return "task " + t.AutomatonID + "/" + t.TaskID }

// failureText is the failure reason's detail and evidence as one string.
func (t ChainTask) failureText() string {
	return strings.TrimSpace(t.FailureDetail + "\n" + strings.Join(t.FailureEvidence, "\n"))
}

// ---- (a) dependent reset ----

type dependentResetRepair struct {
	ops  TaskChainOps
	mu   sync.Mutex
	done map[string]bool
}

func (*dependentResetRepair) Group() string { return RepairGroupTaskChain }

func (r *dependentResetRepair) Detect(ctx context.Context) []RepairFinding {
	tasks := r.ops.ChainTasks(ctx)
	byKey := map[string]ChainTask{}
	for _, t := range tasks {
		byKey[t.AutomatonID+"/"+t.TaskID] = t
	}
	var out []RepairFinding
	for _, t := range tasks {
		if t.Status != TaskFailed || t.FailureSource != dependencyInfraSource {
			continue
		}
		if len(t.DependsOn) == 0 {
			continue
		}
		fixed := true
		for _, d := range t.DependsOn {
			dep, ok := byKey[t.AutomatonID+"/"+d]
			if !ok || dep.Status != TaskCompleted {
				fixed = false // the dependency is not fixed yet: not before
				break
			}
		}
		if !fixed {
			continue
		}
		f := RepairFinding{Subject: chainSubject(t),
			Summary: fmt.Sprintf("task %q failed only because a task it depends on failed, and that task is now done", t.Title)}
		if t.PriorAttempts >= 1 || r.alreadyReset(f.Subject) {
			f.Unsafe = true
			f.UnsafeReason = "the task was already reset once and failed again, so a second reset would be needed"
		}
		out = append(out, f)
	}
	return out
}

func (r *dependentResetRepair) alreadyReset(subject string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done[subject]
}

func (r *dependentResetRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	aid, tid, ok := splitChainSubject(f.Subject)
	if !ok {
		return "", errors.New("unrecognised subject " + f.Subject)
	}
	found := false
	for _, t := range r.ops.ChainTasks(ctx) {
		if t.AutomatonID == aid && t.TaskID == tid && t.Status == TaskFailed {
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("the task is gone or no longer failed; nothing was reset")
	}
	r.mu.Lock()
	if r.done == nil {
		r.done = map[string]bool{}
	}
	r.done[f.Subject] = true
	r.mu.Unlock()
	if err := r.ops.ResetTask(ctx, aid, tid); err != nil {
		return "", err
	}
	return "reset the task so it runs again now its dependency is done", nil
}

func (*dependentResetRepair) UndoHint(RepairFinding) string {
	return "cancel the task again from the Automaton's task list"
}

func splitChainSubject(s string) (aid, tid string, ok bool) {
	rest, found := strings.CutPrefix(s, "task ")
	if !found {
		return "", "", false
	}
	aid, tid, ok = strings.Cut(rest, "/")
	return aid, tid, ok && aid != "" && tid != ""
}

// ---- shared base for the retry-once repairs ----

type chainRepairBase struct {
	ops   TaskChainOps
	mu    sync.Mutex
	tried map[string]bool
}

func (b *chainRepairBase) markTried(subject string) {
	b.mu.Lock()
	b.tried[subject] = true
	b.mu.Unlock()
}

func (b *chainRepairBase) wasTried(subject string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tried[subject]
}

// (b) and (c) use these signals to read a failure. A failure that matches
// both, or names a signal without what is needed to act, is ambiguous.
var (
	staleWords    = regexp.MustCompile(`(?i)\b(stale|out of date|out-of-date|not in sync|drift|differs|mismatch)\b`)
	leakWords     = regexp.MustCompile(`(?i)(leaked marker|stray file|untracked file)`)
	leakedPathRe  = regexp.MustCompile(`(?i)(?:leaked marker|stray file|untracked file)s?\s*[:=]\s*` + "`?" + `([^\s` + "`" + `'",;]+)`)
	multiLeakWord = regexp.MustCompile(`(?i)(leaked markers|stray files|untracked files)`)
)

func mentionsDocsMirror(text string) bool { return strings.Contains(text, docsMirrorDir) }

// ---- (b) stale docs mirror at the final gate ----

type staleDocsGateRepair struct{ chainRepairBase }

func (*staleDocsGateRepair) Group() string { return RepairGroupTaskChain }

func (r *staleDocsGateRepair) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, t := range r.ops.ChainTasks(ctx) {
		if t.Status != TaskFailed || t.Kind != TaskKindFinalGate {
			continue
		}
		text := t.failureText()
		if !mentionsDocsMirror(text) {
			continue
		}
		f := RepairFinding{Subject: chainSubject(t),
			Summary: fmt.Sprintf("the final gate %q failed on the embedded docs mirror", t.Title)}
		switch {
		case !staleWords.MatchString(text):
			f.Unsafe, f.UnsafeReason = true, "the failure names the docs mirror but does not say it is stale, so the cause is ambiguous"
		case leakWords.MatchString(text):
			f.Unsafe, f.UnsafeReason = true, "the failure names both a stale docs mirror and a stray file, so the cause is ambiguous"
		case t.Worktree == "":
			f.Unsafe, f.UnsafeReason = true, "the Automaton worktree is unknown"
		case t.PriorAttempts >= 1 || r.wasTried(f.Subject):
			f.Unsafe, f.UnsafeReason = true, "the gate was already retried once, so a second retry would be needed"
		}
		out = append(out, f)
	}
	return out
}

func (r *staleDocsGateRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	aid, tid, ok := splitChainSubject(f.Subject)
	if !ok {
		return "", errors.New("unrecognised subject " + f.Subject)
	}
	t, ok := r.find(ctx, aid, tid)
	if !ok {
		return "", errors.New("the task is gone")
	}
	r.markTried(f.Subject)
	if out, err := r.ops.SyncDocs(ctx, t.Worktree); err != nil {
		return "", fmt.Errorf("docs sync failed: %w (%s)", err, strings.TrimSpace(out))
	}
	if err := r.ops.ResetTask(ctx, aid, tid); err != nil {
		return "", err
	}
	return "ran the docs sync command (" + strings.Join(docsSyncCommand, " ") + ") and retried the gate once", nil
}

func (r *staleDocsGateRepair) find(ctx context.Context, aid, tid string) (ChainTask, bool) {
	for _, t := range r.ops.ChainTasks(ctx) {
		if t.AutomatonID == aid && t.TaskID == tid {
			return t, true
		}
	}
	return ChainTask{}, false
}

func (*staleDocsGateRepair) UndoHint(RepairFinding) string {
	return "the sync only refreshes the gitignored docs mirror from docs/; cancel the retried gate to stop it, nothing else changed"
}

// ---- (c) leaked marker or stray file ----

type leakedFileRepair struct{ chainRepairBase }

func (*leakedFileRepair) Group() string { return RepairGroupTaskChain }

// leakedPath extracts the one path the failure names. ok is false when it
// names none or more than one.
func leakedPath(text string) (rel string, ok bool) {
	if multiLeakWord.MatchString(text) && !leakedPathRe.MatchString(text) {
		return "", false
	}
	ms := leakedPathRe.FindAllStringSubmatch(text, -1)
	seen := map[string]bool{}
	for _, m := range ms {
		seen[m[1]] = true
	}
	if len(seen) != 1 {
		return "", false
	}
	return ms[0][1], true
}

// cleanRelPath returns rel as a clean path strictly inside the worktree, or
// false when it is absolute, empty or climbs out.
func cleanRelPath(rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	c := filepath.Clean(rel)
	if c == "." || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", false
	}
	return c, true
}

func (r *leakedFileRepair) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, t := range r.ops.ChainTasks(ctx) {
		if t.Status != TaskFailed && t.Status != TaskBlocked {
			continue
		}
		text := t.failureText()
		if !leakWords.MatchString(text) {
			continue
		}
		if t.Kind == TaskKindFinalGate && mentionsDocsMirror(text) {
			continue // the stale docs gate repair owns a final gate that names the mirror, so one alert covers it
		}
		f := RepairFinding{Subject: chainSubject(t),
			Summary: fmt.Sprintf("task %q is blocked from finishing by a leaked marker or stray file", t.Title)}
		r.judge(ctx, t, text, &f)
		out = append(out, f)
	}
	return out
}

func (r *leakedFileRepair) judge(ctx context.Context, t ChainTask, text string, f *RepairFinding) {
	unsafe := func(reason string) { f.Unsafe, f.UnsafeReason = true, reason }
	if mentionsDocsMirror(text) && staleWords.MatchString(text) {
		unsafe("the failure names both a stray file and a stale docs mirror, so the cause is ambiguous")
		return
	}
	raw, ok := leakedPath(text)
	if !ok {
		unsafe("the failure does not name exactly one file, so the cause is ambiguous")
		return
	}
	rel, ok := cleanRelPath(raw)
	if !ok {
		unsafe("the named path " + raw + " is not inside the Automaton worktree")
		return
	}
	f.Summary += ": " + rel
	if t.Worktree == "" {
		unsafe("the Automaton worktree is unknown")
		return
	}
	if t.PriorAttempts >= 1 || r.wasTried(f.Subject) {
		unsafe("the task was already retried once, so a second retry would be needed")
		return
	}
	st, err := r.ops.PathState(ctx, t.Worktree, rel)
	switch {
	case err != nil:
		unsafe("could not tell whether " + rel + " is tracked: " + err.Error())
	case st.Tracked:
		unsafe(rel + " is tracked by git, so it is work, not a leak")
	case !st.Exists:
		unsafe(rel + " is not in the worktree, so the cause is ambiguous")
	}
}

func (r *leakedFileRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	aid, tid, ok := splitChainSubject(f.Subject)
	if !ok {
		return "", errors.New("unrecognised subject " + f.Subject)
	}
	var task ChainTask
	found := false
	for _, t := range r.ops.ChainTasks(ctx) {
		if t.AutomatonID == aid && t.TaskID == tid {
			task, found = t, true
			break
		}
	}
	if !found {
		return "", errors.New("the task is gone")
	}
	raw, ok := leakedPath(task.failureText())
	if !ok {
		return "", errors.New("the failure no longer names exactly one file")
	}
	rel, ok := cleanRelPath(raw)
	if !ok {
		return "", errors.New("the named path is not inside the worktree")
	}
	// Re-check right before acting: the file may have been committed since.
	st, err := r.ops.PathState(ctx, task.Worktree, rel)
	if err != nil {
		return "", err
	}
	if st.Tracked || !st.Exists {
		return "", errors.New(rel + " is tracked or gone; nothing was cleaned")
	}
	r.markTried(f.Subject)
	if out, err := r.ops.Cleanup(ctx, task.Worktree, rel); err != nil {
		return "", fmt.Errorf("cleanup failed: %w (%s)", err, strings.TrimSpace(out))
	}
	if err := r.ops.ResetTask(ctx, aid, tid); err != nil {
		return "", err
	}
	return "removed the untracked file " + rel + " with git clean and retried the task once", nil
}

func (*leakedFileRepair) UndoHint(RepairFinding) string {
	return "the removed file was untracked and is not in git; restore it from the worker's session output if it is needed, and cancel the retried task to stop it"
}

// ---- command-backed helpers for a real TaskChainOps ----

// ChainCommandRunner runs argv in dir and returns its combined output.
type ChainCommandRunner func(ctx context.Context, dir string, argv []string) (string, error)

// ExecChainCommand is the ChainCommandRunner that runs a real process, with
// the terminal-multiplexer environment removed so nothing can reach the
// operator's sessions.
func ExecChainCommand(ctx context.Context, dir string, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- argv is built from fixed commands and a validated relative path
	cmd.Dir = dir
	for _, kv := range cmd.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// CommandChainOps implements the command half of TaskChainOps over a runner:
// path state, docs sync and cleanup. Task listing and reset come from the
// embedding type.
type CommandChainOps struct {
	Run ChainCommandRunner
}

func (c CommandChainOps) run() ChainCommandRunner {
	if c.Run != nil {
		return c.Run
	}
	return ExecChainCommand
}

// PathState reports existence and whether git tracks rel.
func (c CommandChainOps) PathState(ctx context.Context, worktree, rel string) (ChainPathState, error) {
	clean, ok := cleanRelPath(rel)
	if !ok {
		return ChainPathState{}, errors.New("path is outside the worktree")
	}
	out, err := c.run()(ctx, worktree, []string{"git", "ls-files", "--", clean})
	if err != nil {
		return ChainPathState{}, err
	}
	tracked := strings.TrimSpace(out) != ""
	exists, err := c.exists(ctx, worktree, clean)
	if err != nil {
		return ChainPathState{}, err
	}
	return ChainPathState{Exists: exists, Tracked: tracked}, nil
}

func (c CommandChainOps) exists(ctx context.Context, worktree, rel string) (bool, error) {
	out, err := c.run()(ctx, worktree, []string{"git", "ls-files", "--others", "--", rel})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// SyncDocs runs the documented docs sync command.
func (c CommandChainOps) SyncDocs(ctx context.Context, worktree string) (string, error) {
	return c.run()(ctx, worktree, append([]string(nil), docsSyncCommand...))
}

// Cleanup runs git clean on the one untracked path.
func (c CommandChainOps) Cleanup(ctx context.Context, worktree, rel string) (string, error) {
	clean, ok := cleanRelPath(rel)
	if !ok {
		return "", errors.New("path is outside the worktree")
	}
	return c.run()(ctx, worktree, append(append([]string(nil), cleanupCommandPrefix...), clean))
}

// ManagerChainOps is the TaskChainOps backed by the Automaton store: it lists
// tasks of Automata that are not finished and resets with the plain reset (no
// force). Path state, docs sync and cleanup come from the embedded
// CommandChainOps.
type ManagerChainOps struct {
	CommandChainOps
	API *API
}

// ChainTasks lists the tasks of every Automaton that is not finished.
func (o ManagerChainOps) ChainTasks(_ context.Context) []ChainTask {
	if o.API == nil || o.API.M == nil {
		return nil
	}
	st := o.API.M.Store()
	var out []ChainTask
	for _, p := range st.ListPRDs() {
		prd, ok := st.SnapshotPRD(p.ID)
		if !ok {
			continue
		}
		switch prd.Status {
		case PRDCompleted, PRDRejected, PRDCancelled, PRDArchived:
			continue
		case PRDHeld:
			// The tasks of a held Automaton are never reset or repaired.
			continue
		}
		for _, s := range prd.Story {
			for _, t := range s.Tasks {
				ct := ChainTask{AutomatonID: prd.ID, TaskID: t.ID, Title: t.Title, Status: t.Status,
					Kind: t.Kind, DependsOn: t.DependsOn, PriorAttempts: t.PriorAttemptCount, Worktree: prd.ProjectDir}
				if t.WorktreePath != "" {
					ct.Worktree = t.WorktreePath
				}
				if fr := t.FailureReason; fr != nil {
					ct.FailureSource, ct.FailureCategory = fr.Source, fr.Category
					ct.FailureDetail, ct.FailureEvidence = fr.Detail, fr.Evidence
				}
				out = append(out, ct)
			}
		}
	}
	return out
}

// ResetTask is a plain reset (force off) that also resumes the run.
func (o ManagerChainOps) ResetTask(_ context.Context, automatonID, taskID string) error {
	if o.API == nil {
		return errors.New("automaton API not wired")
	}
	_, err := o.API.ResetTaskWithOptions(automatonID, taskID, "housekeeping-repair", false, false)
	return err
}
