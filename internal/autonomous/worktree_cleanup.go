// Worktree cleanup and release.
//
// An Automaton works in a dedicated work tree (a work branch checked out
// under the operator's base repository) and keeps it while it runs: the
// branch is not force-pushed, the work tree is never deleted out from
// under a live session. When the Automaton has finished and its work is
// on the base project — completed or cancelled, with nothing left to
// salvage — the work branch and work tree should be releasable, but
// never by force:
//
//   - the branch must belong to an Automaton that is finished or
//     cancelled, and finished means NOT restarted: the current state of
//     the store decides, never a remembered one;
//   - no live session and no live lease may be using the work tree, so
//     that no running or resumable work is deleted out from under
//     itself;
//   - the work tree must be clean (re-verifying with `git status
//     --porcelain` at removal time, not trusting the earlier report), so
//     uncommitted work is never destroyed;
//   - the branch must be at least as old as the retention window, so
//     freshly finished work stays reachable while an operator still
//     looks at it;
//   - the branch must be the main checkout of no working tree, so the
//     primary checkout is never touched;
//   - its commits must be all on the base project (re-verifying with
//     rev-list at removal time), so nothing unique is lost.
//
// Removal itself is the strict pair `git worktree remove` (never with
// --force) and `git branch -d` (never -D): if either one refuses, the
// entry is left in place and the refusal is recorded with its error, so
// work is never silently destroyed.
//
// Three modes are honored from the config accessors:
//
//   - off    does nothing and does not even call git;
//   - report runs every safety check, records what would be removed and
//     removes nothing;
//   - on     removes, at most the per-pass removal limit.
//
// Two entry points share the same path and the same safety criteria:
//
//   - ReleaseAutomatonWorktree is called from an ended Automaton's end
//     paths (completed, cancelled) and from the merge-pending pass when
//     the work has landed on the base project, releasing that Automaton's
//     work branch and work tree;
//   - CleanupWorktreesPass is the periodic safety net, called by the
//     coordinator's idle cycle, removing up to the per-pass limit across
//     every Automaton branch.
//
// Every removal is logged to the Automaton's triage record (a note when
// one exists, a record minted otherwise) and to the audit log, so an
// operator can reconstruct what was removed, when and why, from the
// store alone.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// worktreeReleaseGitTimeout bounds the git round-trips of one end-path
// release so a stuck repository can never hang the executor.
const worktreeReleaseGitTimeout = 60 * time.Second

// worktreeCleanupPassTimeout bounds the periodic pass.
const worktreeCleanupPassTimeout = 120 * time.Second

// worktreeCleanupAuditAction is the audit event recorded for every
// work branch + work tree removal.
const worktreeCleanupAuditAction = "autonomous_worktree_remove"

// Skip reasons reported by CleanupWorktrees. Each one is a single fact
// about why an entry was not removed.
const (
	// cleanupSkipNotFinished: the branch's Automaton is running (or the
	// store no longer owns it), never a finished or cancelled one.
	cleanupSkipNotFinished = "not_finished"
	// cleanupSkipLiveSession: a live session is in or bound to the
	// work tree, so deleting it would destroy running work.
	cleanupSkipLiveSession = "live_session"
	// cleanupSkipLiveLease: the work tree is held by a live lease
	// (tree lock) whose holder task is not finished.
	cleanupSkipLiveLease = "live_lease"
	// cleanupSkipRetained: the entry is younger than the retention
	// window (or its age could not be determined — treated as young).
	cleanupSkipRetained = "retained"
	// cleanupSkipPrimary: the entry is the main checkout, which is
	// never touched.
	cleanupSkipPrimary = "primary"
	// cleanupSkipDirty: the work tree has uncommitted changes.
	cleanupSkipDirty = "dirty"
	// cleanupSkipCommits: commits on the branch are not all on the
	// base branch.
	cleanupSkipCommits = "commits_not_on_base"
	// cleanupSkipNoBase: the base branch the branch must be on could
	// not be resolved, so it cannot be verified — the entry is left.
	cleanupSkipNoBase = "no_base_branch"
	// cleanupSkipHeld: the safe-to-delete report itself held the
	// branch (commits not on base or a failed check).
	cleanupSkipHeld = "held"
	// cleanupSkipOverLimit: the per-pass removal limit was reached.
	cleanupSkipOverLimit = "over_limit"
)

// Stage names recorded on a failed entry: the step at which removal
// stopped and the entry was left in place.
const (
	cleanupStageStatus = "status_check"
	cleanupStageSize   = "size_lookup"
	cleanupStageAudit  = "audit_write"
	cleanupStageRemove = "worktree_remove"
	cleanupStageBranch = "branch_delete"
	cleanupStageRecord = "record"
)

// WorktreeCleanupProbe is the small interface CleanupWorktrees needs to
// know about one Automaton branch: whether its Automaton is finished or
// cancelled (and not restarted), the base branch the branch's work
// must be on, whether a live session or lease uses the work tree, how
// old the entry is, and how big the work tree is. Manager-level
// implementations back these with the store and the session lister;
// tests supply fakes.
type WorktreeCleanupProbe interface {
	// AutomatonFinished reports whether the branch belongs to an
	// Automaton whose current state is completed or cancelled, and not
	// restarted.
	AutomatonFinished(branch string) bool
	// BaseBranch returns the base branch the branch's commits must be
	// on for removal to be safe; empty when it cannot be resolved.
	BaseBranch(branch string) string
	// WorktreeInUse reports whether a live session or a live lease is
	// using the work tree; kind is "session" or "lease".
	WorktreeInUse(branch, worktree string) (inUse bool, kind string)
	// EntryAge returns how old the branch's Automaton entry is (since
	// it finished); ok is false when no age can be determined.
	EntryAge(branch string) (age time.Duration, ok bool)
	// WorktreeSize returns the work tree's byte size (sum of regular
	// file sizes), for the "bytes freed" accounting.
	WorktreeSize(worktree string) (int64, error)
}

// WorktreeCleanupAudit records one removal in the audit log; a non-nil
// error leaves the entry untouched and reported as failed.
type WorktreeCleanupAudit func(action string, details map[string]any) error

// WorktreeCleanupResult is the outcome of one cleanup or release pass.
// Every input entry lands in exactly one of the four lists, so an
// operator can always tell what happened to every branch.
type WorktreeCleanupResult struct {
	// Mode echoes the cleanup mode the pass ran in.
	Mode string
	// Removed holds the entries removed, with what was freed and why it
	// was deemed safe.
	Removed []WorktreeCleanupRemoval
	// WouldRemove holds the entries that passed every check in report
	// mode (nothing was removed).
	WouldRemove []WorktreeCleanupRemoval
	// Skipped holds the entries left in place with the reason, per
	// entry (not_finished, live_session, live_lease, retained,
	// primary, dirty, commits_not_on_base, no_base_branch, held).
	Skipped []WorktreeCleanupSkipped
	// Failed holds the entries whose removal hit an error at a named
	// stage, each left in place with its error so no work is
	// destroyed and the cause is recorded.
	Failed []WorktreeCleanupFailed
}

// WorktreeCleanupRemoval is one removed (or, in report mode,
// would-be-removed) branch + work tree.
type WorktreeCleanupRemoval struct {
	Branch string `json:"branch"`
	// Worktree is the work tree path, "" for a branch without one.
	Worktree string `json:"worktree"`
	// BytesFreed is the work tree's size measured before removal.
	BytesFreed int64 `json:"bytes_freed"`
	// Reason is the safety verdict that qualified it (e.g. "all
	// commits on trunk", "no commits ahead of trunk": the project's own
	// default branch).
	Reason string `json:"reason"`
}

// WorktreeCleanupSkipped is one entry left in place, with the reason.
type WorktreeCleanupSkipped struct {
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
	// Reason is one of the cleanupSkip* constants.
	Reason string `json:"reason"`
	// Detail is the human-facing explanation.
	Detail string `json:"detail"`
}

// WorktreeCleanupFailed is one entry whose removal stopped at a stage,
// left in place with its error.
type WorktreeCleanupFailed struct {
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
	// Stage is one of the cleanupStage* constants: where the removal
	// stopped.
	Stage string `json:"stage"`
	// Error carries the cause.
	Error string `json:"error"`
}

// WorktreeCleanupOptions is one CleanupWorktrees call: the safe-to-
// delete report to act on, the run parameters from the config
// accessors, the clock, the git runner, and the small probe. All
// fields are required except Audit and Record (nil = no-op).
type WorktreeCleanupOptions struct {
	Ctx         context.Context
	Repo        string // the base repository (home of the work branches)
	Report      SafeDeleteReport
	Mode        string // config.CleanupModeOff / Report / On
	Retention   time.Duration
	MaxRemovals int
	Git         MergeGitRunner
	Now         func() time.Time
	Probe       WorktreeCleanupProbe
	Audit       WorktreeCleanupAudit
	// Record logs one removal for the branch (e.g. a triage record
	// note); a non-nil error is surfaced in the result's Failed list
	// but never un-removes the entry: the removal happened.
	Record func(branch, text string) error
}

// CleanupWorktrees acts on one safe-to-delete report under the
// cleanup's safety rules and mode. It never touches a Held or
// HeldDirty entry; it re-verifies dirty state, base-branch containment
// and the work tree's identity itself (the report is an input, not a
// guarantee); and in on mode it removes at most MaxRemovals entries —
// `git worktree remove` and then `git branch -d`, neither with force.
// A failed removal is recorded with its stage and error and the pass
// moves on to the next entry.
func CleanupWorktrees(opts WorktreeCleanupOptions) WorktreeCleanupResult {
	res := WorktreeCleanupResult{Mode: opts.Mode}
	if opts.Mode == config.CleanupModeOff {
		return res // off does nothing and does not even call git
	}
	if opts.Probe == nil || opts.Git == nil {
		return res // no probe or runner: nothing can be verified, nothing is done
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Audit == nil {
		opts.Audit = func(string, map[string]any) error { return nil }
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	primary := ""
	primaryKnown := false
	if out, err := opts.Git.Run(ctx, opts.Repo, "worktree", "list", "--porcelain"); err == nil {
		primary = worktreePrimaryPath(out)
		primaryKnown = true
	}

	removed := 0
	for _, e := range opts.Report.Safe {
		skip := func(reason, detail string) {
			res.Skipped = append(res.Skipped, WorktreeCleanupSkipped{Branch: e.Branch, Worktree: e.Worktree, Reason: reason, Detail: detail})
		}
		failed := func(stage, format string, a ...any) {
			res.Failed = append(res.Failed, WorktreeCleanupFailed{Branch: e.Branch, Worktree: e.Worktree, Stage: stage, Error: fmt.Sprintf(format, a...)})
		}

		if !opts.Probe.AutomatonFinished(e.Branch) {
			skip(cleanupSkipNotFinished, "the branch's Automaton is not finished or cancelled (and not a finished one that was restarted)")
			continue
		}
		if inUse, kind := opts.Probe.WorktreeInUse(e.Branch, e.Worktree); inUse {
			if kind == "lease" {
				skip(cleanupSkipLiveLease, "a live lease (tree lock with a not-finished holder) is on the work tree")
			} else {
				skip(cleanupSkipLiveSession, "a live session is in or bound to the work tree")
			}
			continue
		}
		age, ok := opts.Probe.EntryAge(e.Branch)
		if !ok {
			skip(cleanupSkipRetained, "the entry's age could not be determined; treated as inside the retention window")
			continue
		}
		if age < opts.Retention {
			skip(cleanupSkipRetained, fmt.Sprintf("the entry is %s old, inside the %s retention window", age.Round(time.Second), opts.Retention.Round(time.Second)))
			continue
		}
		if e.Worktree != "" {
			if !primaryKnown {
				skip(cleanupSkipPrimary, "the main checkout could not be identified from the work tree list; the entry may be it, so it is left")
				continue
			}
			if sameDirectory(e.Worktree, primary) {
				skip(cleanupSkipPrimary, "the entry is the main checkout, which is never touched")
				continue
			}
			status, serr := opts.Git.Run(ctx, e.Worktree, "status", "--porcelain")
			if serr != nil {
				failed(cleanupStageStatus, "git status: %v: %s", serr, strings.TrimSpace(status))
				continue
			}
			if strings.TrimSpace(status) != "" {
				skip(cleanupSkipDirty, "the work tree has uncommitted changes: "+firstLine(status))
				continue
			}
		}
		base := opts.Probe.BaseBranch(e.Branch)
		if base == "" {
			skip(cleanupSkipNoBase, "the base branch could not be resolved (no per-Automaton base and no recorded remote default)")
			continue
		}
		cmp := CompareBranchToMain(ctx, opts.Git, opts.Repo, e.Branch, base)
		if cmp.State != MergeStateMerged && cmp.State != MergeStateEmpty {
			detail := fmt.Sprintf("commits on the branch are not all on the base branch %q (comparison state %q)", base, cmp.State)
			if cmp.Error != "" {
				detail += ": " + cmp.Error
			}
			skip(cleanupSkipCommits, detail)
			continue
		}

		if opts.Mode == config.CleanupModeReport {
			w := WorktreeCleanupRemoval{Branch: e.Branch, Worktree: e.Worktree, Reason: e.Reason}
			if e.Worktree != "" {
				if size, serr := opts.Probe.WorktreeSize(e.Worktree); serr == nil {
					w.BytesFreed = size
				}
			}
			res.WouldRemove = append(res.WouldRemove, w)
			continue
		}

		if removed >= opts.MaxRemovals {
			skip(cleanupSkipOverLimit, fmt.Sprintf("the per-pass removal limit of %d was reached", opts.MaxRemovals))
			continue
		}

		var size int64
		if e.Worktree != "" {
			s, serr := opts.Probe.WorktreeSize(e.Worktree)
			if serr != nil {
				failed(cleanupStageSize, "measuring work tree size: %v", serr)
				continue
			}
			size = s
		}
		details := map[string]any{
			"branch":      e.Branch,
			"worktree":    e.Worktree,
			"bytes_freed": size,
			"base_branch": base,
			"reason":      e.Reason,
			"age_seconds": int64(age / time.Second),
			"at":          opts.Now().Format(time.RFC3339),
		}
		if err := opts.Audit(worktreeCleanupAuditAction, details); err != nil {
			failed(cleanupStageAudit, "audit log write: %v", err)
			continue
		}

		if e.Worktree != "" {
			out, rerr := opts.Git.Run(ctx, opts.Repo, "worktree", "remove", e.Worktree) // never --force
			if rerr != nil {
				failed(cleanupStageRemove, "git worktree remove: %v: %s", rerr, strings.TrimSpace(out))
				continue
			}
		}
		out, derr := opts.Git.Run(ctx, opts.Repo, "branch", "-d", e.Branch) // never -D
		if derr != nil {
			failed(cleanupStageBranch, "git branch -d: %v: %s", derr, strings.TrimSpace(out))
			continue
		}
		removed++
		text := fmt.Sprintf("work branch %s and its work tree removed (%d bytes freed): %s", e.Branch, size, e.Reason)
		if opts.Record != nil {
			if err := opts.Record(e.Branch, text); err != nil {
				log.Printf("[autonomous] worktree-cleanup: recording removal of %s: %v", e.Branch, err)
				res.Failed = append(res.Failed, WorktreeCleanupFailed{Branch: e.Branch, Worktree: e.Worktree, Stage: cleanupStageRecord, Error: err.Error()})
			}
		}
		res.Removed = append(res.Removed, WorktreeCleanupRemoval{Branch: e.Branch, Worktree: e.Worktree, BytesFreed: size, Reason: e.Reason})
	}

	for _, h := range opts.Report.Held {
		reason := cleanupSkipHeld
		detail := "the safe-to-delete report held the branch"
		if h.Dirty {
			reason = cleanupSkipDirty
			detail = "the work tree has uncommitted changes"
		}
		if h.Note != "" {
			detail += ": " + h.Note
		}
		res.Skipped = append(res.Skipped, WorktreeCleanupSkipped{Branch: h.Branch, Worktree: h.Worktree, Reason: reason, Detail: detail})
	}
	return res
}

// worktreePrimaryPath returns the first work tree path of a `git worktree
// list --porcelain` listing — the main checkout.
func worktreePrimaryPath(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree ")
		}
	}
	return ""
}

// sameDirectory reports whether two paths are the same directory,
// resolving symlinks where possible and falling back to cleaned
// comparison when a path no longer exists.
func sameDirectory(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if fa, err := filepath.Abs(a); err == nil {
		a = fa
	}
	if fb, err := filepath.Abs(b); err == nil {
		b = fb
	}
	if a == b {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	if ea == nil && eb == nil {
		return ra == rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return strings.TrimSpace(s)
}

// dirSize sums the sizes of the regular files under dir.
func dirSize(dir string) (int64, error) {
	var total int64
	werr := fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, werr
}

// --------------------------------------------------------------------
// Manager wiring: the two entry points and the store-backed probe
// --------------------------------------------------------------------

// managerWorktreeProbe backs WorktreeCleanupProbe with the store, the
// session lister and the tree locks; the default base branch (from the
// recorded remote) is resolved once per pass.
type managerWorktreeProbe struct {
	m            *Manager
	repo         string
	git          MergeGitRunner
	ctx          context.Context
	baseResolved bool
	baseName     string
}

func (p *managerWorktreeProbe) prdFor(branch string) *PRD {
	if branch == "" || p.m.store == nil {
		return nil
	}
	for _, prd := range p.m.store.ListPRDs() {
		if prd != nil && prd.Git.Branch == branch {
			return prd
		}
	}
	return nil
}

// AutomatonFinished applies the finished/cancelled rule against the
// store's current state.
func (p *managerWorktreeProbe) AutomatonFinished(branch string) bool {
	prd := p.prdFor(branch)
	return prd != nil && (prd.Status == PRDCompleted || prd.Status == PRDCancelled)
}

// BaseBranch honors the per-Automaton base branch, falling back to the
// repository's recorded default branch — never an assumed name.
func (p *managerWorktreeProbe) BaseBranch(branch string) string {
	prd := p.prdFor(branch)
	if prd != nil && prd.Git.BaseBranch != "" {
		return prd.Git.BaseBranch
	}
	return p.defaultBase()
}

// defaultBase is the remote's default branch (symbolic-ref
// refs/remotes/<remote>/HEAD), resolved once: an operator's project may
// have any default branch name ("main", "master", "trunk", ...) or one
// recorded only by the remote.
func (p *managerWorktreeProbe) defaultBase() string {
	if p.baseResolved {
		return p.baseName
	}
	p.baseResolved = true
	var remotes []string
	if out, err := p.git.Run(p.ctx, p.repo, "remote"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				remotes = append(remotes, l)
			}
		}
	}
	for _, remote := range append([]string{"origin"}, remotes...) {
		out, err := p.git.Run(p.ctx, p.repo, "symbolic-ref", "refs/remotes/"+remote+"/HEAD")
		if err != nil {
			continue
		}
		ref := strings.TrimSpace(out)
		const prefix = "refs/remotes/"
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := strings.TrimPrefix(ref, prefix)
		if i := strings.Index(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name != "" {
			p.baseName = name
			return name
		}
	}
	return ""
}

// WorktreeInUse checks the session lister (a live session in the work
// tree directory, or bound to the Automaton that is running) and the
// tree lock (a lease whose holder task is not finished — a stale lock
// of a finished task does not hold the work tree).
func (p *managerWorktreeProbe) WorktreeInUse(branch, worktree string) (bool, string) {
	prd := p.prdFor(branch)
	p.m.mu.Lock()
	listFn := p.m.sessionListerFn
	p.m.mu.Unlock()
	if listFn != nil {
		wt := worktree
		if wt != "" {
			if aw, err := filepath.Abs(wt); err == nil {
				wt = aw
			}
		}
		for _, s := range listFn() {
			if !s.Alive {
				continue
			}
			if wt != "" && s.ProjectDir != "" {
				sd := s.ProjectDir
				if as, err := filepath.Abs(sd); err == nil {
					sd = as
				}
				if sd == wt {
					return true, "session"
				}
			}
			if prd != nil && s.PRDID == prd.ID && (prd.Status == PRDRunning || prd.Status == PRDActive) {
				return true, "session"
			}
		}
	}
	if worktree == "" {
		return false, ""
	}
	lockPath := treeLockPath(treeKeyPrefix+worktree, worktree)
	rec, err := readLockRecord(lockPath)
	if err != nil {
		return false, "" // no lock file (or unreadable): nothing is leased here
	}
	if rec.HolderTaskID != "" {
		if t, ok := p.m.store.GetTask(rec.HolderTaskID); ok && t != nil && isTerminalTaskStatus(t.Status) {
			return false, "" // stale lock of a finished holder; the work tree is not leased
		}
	}
	return true, "lease"
}

// EntryAge is the time since the Automaton's store entry was last
// updated — for a finished or cancelled Automaton, when it finished.
func (p *managerWorktreeProbe) EntryAge(branch string) (time.Duration, bool) {
	prd := p.prdFor(branch)
	if prd == nil || prd.UpdatedAt.IsZero() {
		return 0, false
	}
	return time.Since(prd.UpdatedAt), true
}

func (p *managerWorktreeProbe) WorktreeSize(worktree string) (int64, error) {
	return dirSize(worktree)
}

// worktreeCleanupAudit returns the audit hook from the manager's audit
// sink; a nil sink records nowhere and never fails.
func (m *Manager) worktreeCleanupAudit() WorktreeCleanupAudit {
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn == nil {
		return func(string, map[string]any) error { return nil }
	}
	return func(action string, details map[string]any) error {
		fn(action, details)
		return nil
	}
}

// recordWorktreeRemoval logs one removal to the Automaton's triage
// record: an appended note when the record exists, a new record
// otherwise. It never fails the removal.
func (m *Manager) recordWorktreeRemoval(branch, text string) error {
	prd := (&managerWorktreeProbe{m: m}).prdFor(branch)
	if prd == nil {
		return nil
	}
	err := m.AppendTriageNote(prd.ID, "worktree-cleanup", text)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrTriageNoRecord) {
		return err
	}
	now := time.Now().UTC()
	rec := &TriageRecord{
		AutomationID: prd.AutomationID,
		PRDID:        prd.ID,
		PRDTitle:     prd.Title,
		Status:       "completed",
		Branch:       branch,
		FirstSeenAt:  now,
		UpdatedAt:    now,
		Notes: []TriageNote{
			{At: now, Source: "worktree-cleanup", Text: text},
		},
	}
	return m.SaveTriageRecord(prd.ID, rec)
}

// worktreeOutcomeLog records the pass outcome: every removal is already
// audited; here the aggregate lands in the log and the audit stream.
func (m *Manager) worktreeOutcomeLog(scope string, r WorktreeCleanupResult) {
	log.Printf("[autonomous] worktree-cleanup %s: mode=%q removed=%d would_remove=%d skipped=%d failed=%d",
		scope, r.Mode, len(r.Removed), len(r.WouldRemove), len(r.Skipped), len(r.Failed))
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn("autonomous_worktree_cleanup_"+scope, map[string]any{
			"mode":         r.Mode,
			"removed":      len(r.Removed),
			"would_remove": len(r.WouldRemove),
			"skipped":      skippedReasonDigest(r.Skipped),
			"failed":       len(r.Failed),
		})
	}
}

func skippedReasonDigest(skipped []WorktreeCleanupSkipped) map[string]int {
	d := map[string]int{}
	for _, s := range skipped {
		d[s.Reason]++
	}
	return d
}

// worktreeCleanupRun wires the manager's hooks onto one
// CleanupWorktrees call.
func (m *Manager) worktreeCleanupRun(opts WorktreeCleanupOptions) WorktreeCleanupResult {
	opts.Audit = m.worktreeCleanupAudit()
	opts.Record = m.recordWorktreeRemoval
	opts.Now = func() time.Time { return time.Now().UTC() }
	return CleanupWorktrees(opts)
}

// ReleaseAutomatonWorktree attempts to release one finished (completed
// or cancelled) Automaton's work branch and work tree from an end path,
// under the cleanup's safety criteria (all but the retention window). If anything fails — a
// safety check holds the entry, a command refuses — the entry is left
// alone and the reason is recorded (log + audit); the periodic
// CleanupWorktreesPass retries it later. off mode does nothing; report
// mode records what would be removed and removes nothing.
//
// The retention window does not apply here: the entry's age is measured
// from the moment it finished, so at the end path it is always young and
// a window of at least an hour would make this path release nothing. Only
// work whose every commit is already on the base branch, with a clean work
// tree and no live user, is released, so nothing an operator could still
// want is lost; the periodic pass keeps the retention window for whatever
// this path held back.
func (m *Manager) ReleaseAutomatonWorktree(prd *PRD) {
	if prd == nil || prd.ID == "" || prd.Git.Branch == "" {
		return
	}
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	mode := m.cfg.Automation.EffectiveCleanupMode()
	m.mu.Unlock()
	if mode == config.CleanupModeOff {
		return // off does nothing
	}
	if repo == "" {
		log.Printf("[autonomous] worktree-cleanup: cannot release %s: no base repository configured", prd.Git.Branch)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), worktreeReleaseGitTimeout)
	defer cancel()
	git := NewMergeGitRunner()
	probe := &managerWorktreeProbe{m: m, repo: repo, git: git, ctx: ctx}

	base := prd.Git.BaseBranch
	if base == "" {
		base = probe.defaultBase()
	}
	if base == "" {
		log.Printf("[autonomous] worktree-cleanup: cannot release %s: no base branch could be resolved (per-Automaton base unset and no recorded remote default)", prd.Git.Branch)
		m.worktreeOutcomeLog("release_skipped", WorktreeCleanupResult{Mode: mode,
			Skipped: []WorktreeCleanupSkipped{{Branch: prd.Git.Branch, Reason: cleanupSkipNoBase, Detail: "no base branch could be resolved"}}})
		return
	}
	report, err := SafeToDeleteReport(ctx, git, repo, base)
	if err != nil {
		log.Printf("[autonomous] worktree-cleanup: safe-to-delete report for %s: %v", prd.Git.Branch, err)
		return
	}

	// Act only on this Automaton's branch; the report may name other
	// Automata's branches, and end-path release is per-Automaton.
	single := SafeDeleteReport{Safe: []SafeDeleteEntry{}, Held: []SafeDeleteHeld{}}
	for _, e := range report.Safe {
		if e.Branch == prd.Git.Branch {
			single.Safe = append(single.Safe, e)
		}
	}
	for _, h := range report.Held {
		if h.Branch == prd.Git.Branch {
			single.Held = append(single.Held, h)
		}
	}
	if len(single.Safe) == 0 && len(single.Held) == 0 {
		return // the branch ref is gone: already released, nothing to do
	}
	if prd.Status != PRDCompleted && prd.Status != PRDCancelled {
		// The store moved on (restarted); the probe's rule then holds
		// the entry too — surface the reason without a git round-trip.
		res := WorktreeCleanupResult{Mode: mode, Skipped: []WorktreeCleanupSkipped{
			{Branch: prd.Git.Branch, Reason: cleanupSkipNotFinished, Detail: "the Automaton is no longer finished or cancelled"},
		}}
		m.worktreeOutcomeLog("release", res)
		return
	}

	res := m.worktreeCleanupRun(WorktreeCleanupOptions{
		Ctx:         ctx,
		Repo:        repo,
		Report:      single,
		Mode:        mode,
		Retention:   0, // the owner releasing its own just-ended work: see below
		MaxRemovals: 1,
		Git:         git,
		Probe:       probe,
	})
	m.worktreeOutcomeLog("release", res)
	// cleanup status: this is the owner releasing its own work at its
	// end of work (completed or cancelled); the net count in the status
	// stays separate so the operator can see when the safety net is the
	// one doing the work (cleanup_status.go).
	if res.Mode != config.CleanupModeOff {
		m.recordCleanupWorktree(res, cleanupSourceOwner)
	}
}

// CleanupWorktreesPass is the periodic safety net: one cleanup pass
// across every Automaton branch of the base repository, removing up to
// the per-pass limit. It is called from the coordinator's idle cycle.
// off mode does nothing (not even a git read); report mode records what
// would be removed; on mode removes. The result is recorded into the
// cleanup status under the net bucket (cleanup_status.go); an
// off-mode pass does not touch the status.
func (m *Manager) CleanupWorktreesPass() WorktreeCleanupResult {
	res := m.cleanupWorktreesPassRun()
	if res.Mode != config.CleanupModeOff {
		m.recordCleanupWorktree(res, cleanupSourceNet)
	}
	return res
}

func (m *Manager) cleanupWorktreesPassRun() WorktreeCleanupResult {
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	mode := m.cfg.Automation.EffectiveCleanupMode()
	retention := time.Duration(m.cfg.Automation.EffectiveCleanupRetentionHours()) * time.Hour
	maxRemovals := m.cfg.Automation.EffectiveCleanupMaxRemovalsPerPass()
	m.mu.Unlock()
	if mode == config.CleanupModeOff {
		return WorktreeCleanupResult{Mode: mode}
	}
	if repo == "" {
		return WorktreeCleanupResult{Mode: mode}
	}

	ctx, cancel := context.WithTimeout(context.Background(), worktreeCleanupPassTimeout)
	defer cancel()
	git := NewMergeGitRunner()
	probe := &managerWorktreeProbe{m: m, repo: repo, git: git, ctx: ctx}

	base := probe.defaultBase()
	if base == "" {
		log.Printf("[autonomous] worktree-cleanup: pass skipped, no default base branch could be resolved for %s", repo)
		return WorktreeCleanupResult{Mode: mode}
	}
	report, err := SafeToDeleteReport(ctx, git, repo, base)
	if err != nil {
		log.Printf("[autonomous] worktree-cleanup: safe-to-delete report: %v", err)
		return WorktreeCleanupResult{Mode: mode}
	}

	res := m.worktreeCleanupRun(WorktreeCleanupOptions{
		Ctx:         ctx,
		Repo:        repo,
		Report:      report,
		Mode:        mode,
		Retention:   retention,
		MaxRemovals: maxRemovals,
		Git:         git,
		Probe:       probe,
	})
	m.worktreeOutcomeLog("pass", res)
	return res
}
