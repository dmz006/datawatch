// BL428 (design docs/plans/2026-10-14-concurrency-and-budget-design.md,
// decision 3a) — canonical project-tree key for the advisory write lock.
//
// The key identifies *which tree a lock protects*, not which PRD/task
// holds it, so that every concurrent holder of one tree agrees on a
// single advisory file an operator can find and inspect:
//
//   - worktree-mode PRDs (prd.Git.Branch != "", BL407) run in their own
//     git worktree and key per worktree;
//   - direct-mode PRDs write to the base checkout directly — a main
//     checkout and its linked worktrees (or any subdir of a checkout)
//     all resolve to the one shared base via the git common dir;
//   - non-git project directories fall back to a per-directory key.
//
// This file is the key-resolution half only; lock acquisition/release
// lands with the executor wiring.
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// treeKeyPrefix marks a key as a canonical project-tree key; the
// remainder is an absClean'd absolute path.
const treeKeyPrefix = "tree:"

// absClean resolves p to an absolute, symlink-resolved, cleaned path.
// Symlink errors (e.g. a directory not created yet) fall back to a
// plain Abs so a PRD whose project dir is pending still resolves; an
// Abs failure is defensive and returns the cleaned input unchanged.
func absClean(p string) string {
	ap, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if rp, err := filepath.EvalSymlinks(ap); err == nil {
		ap = rp
	}
	return filepath.Clean(ap)
}

// gitExecTimeout bounds a single git probe during key resolution: a wedged
// index or a hung git process must be able to stall a task, never the whole
// resolution of every PRD sharing the helper.
const gitExecTimeout = 10 * time.Second

// gitCommonDir resolves repoRoot's shared git common dir, e.g.
// <base>/.git for a main checkout or linked worktree, or
// <base>/.git/modules/<sub> for a submodule. It runs
// `git -C <root> rev-parse --git-common-dir` and absolutizes the
// output — git prints a path relative to the repo root when run at
// the root (".git") or from a subdir ("../.git"), and an absolute
// path from elsewhere.
func gitCommonDir(repoRoot string) (string, error) {
	dir, err := gitProbe(context.Background(), repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot, dir)
	}
	return absClean(dir), nil
}

// gitProbe runs one bounded git probe in dir and returns its stdout.
// Wrapped separately (like runGitIn in worktree.go) with a deadline so
// tree key resolution can't hang on a wedged index. Only stdout is
// captured: on failure git writes its diagnostics to stderr, which
// surfaces in the returned error instead of polluting the value.
func gitProbe(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git subcommand; dir comes from operator-controlled config and PRD fields, same trust model as every other git shell-out in this codebase (e.g. runGitIn)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if line := strings.TrimSpace(string(out)); line != "" {
		return line, nil
	}
	return "", fmt.Errorf("git %s in %s: empty output", strings.Join(args, " "), dir)
}

// sharedBase returns the working-tree root that owns gitDir: the
// directory containing the .git path component. A main checkout, a
// linked worktree's common dir (<base>/.git) and a submodule
// (<base>/.git/modules/<sub>) all map back to <base>. Falls back to
// filepath.Dir for unusual layouts that carry no .git element (bare
// repos, custom GIT_DIR). Absolute input stays absolute: dropping the
// leading empty path element would otherwise yield a relative dir.
func sharedBase(gitDir string) string {
	abs := filepath.IsAbs(gitDir)
	parts := strings.Split(filepath.ToSlash(gitDir), "/")
	for i, part := range parts {
		if part == ".git" {
			b := filepath.Join(parts[:i]...)
			if abs && !filepath.IsAbs(b) {
				b = "/" + filepath.Clean(b)
			}
			return filepath.ToSlash(b)
		}
	}
	return filepath.Dir(gitDir)
}

// ResolveTreeKey returns the canonical key the project-tree write lock
// is keyed on for this PRD/task, plus treeRoot — the absClean'd
// directory whose files the task actually writes.
//
//   - worktree mode (prd.Git.Branch != ""): per-worktree key,
//     "tree:" + absClean(root);
//   - direct mode on a git repo: key "tree:" + absClean(base) where
//     base is the shared base of the git common dir — main checkout
//     and linked worktrees of the same repo therefore share one key —
//     while treeRoot stays the actual write root;
//   - non-git fallback: per-directory key "tree:" + absClean(root).
//
// Symlinked, relative and redundant path spellings of the same
// directory all resolve to the same key (absClean). Errors when there
// is no project dir to key on.
func ResolveTreeKey(prd *PRD, task *Task) (string, string, error) {
	root := ResolveTaskProjectDir(prd, task)
	if root == "" {
		prdID, taskID := "<nil>", "<nil>"
		if prd != nil {
			prdID = prd.ID
		}
		if task != nil {
			taskID = task.ID
		}
		return "", "", fmt.Errorf("tree key: no project dir to key on (prd %s, task %s)", prdID, taskID)
	}
	treeRoot := absClean(root)

	// BL407 worktree-mode PRDs own their worktree — each worktree is
	// an independent tree, so no base canonicalization here.
	if prd != nil && prd.Git.Branch != "" {
		return treeKeyPrefix + treeRoot, treeRoot, nil
	}

	if commonDir, err := gitCommonDir(root); err == nil {
		base := absClean(sharedBase(commonDir))
		return treeKeyPrefix + base, treeRoot, nil
	}

	// Not a git repo (or git unavailable) — key per directory.
	return treeKeyPrefix + treeRoot, treeRoot, nil
}

// treeLockRelPath is the advisory lock file's slash path inside its tree, as
// git reports it.
const treeLockRelPath = ".datawatch/tree.lock"

// treeLockPath returns the advisory lock-file location for a resolved
// tree key:
//
//	<canonical tree>/.datawatch/tree.lock
//
// The canonical tree is the path carried by the key — the shared base
// for direct-mode keys (so an operator inspects one file in the main
// checkout even when a linked worktree holds the lock) and the
// worktree itself for worktree-mode keys. treeRoot is only used as a
// fallback when the key carries no usable absolute path.
func treeLockPath(key, treeRoot string) string {
	dir := strings.TrimPrefix(key, treeKeyPrefix)
	if dir == key || !filepath.IsAbs(dir) {
		dir = absClean(treeRoot)
	}
	return filepath.Join(dir, filepath.FromSlash(treeLockRelPath))
}

// ── Advisory acquisition machinery (BL428 decision 3a, operator (i)) ──
//
// The lock is ADVISORY BY CONTRACT: it only protects against datawatch
// writers that check it. A session (or operator, or any external tool)
// that never looks at the file is not a bug this package fixes — BL425's
// foreign-file-drift check is the after-the-fact detector for exactly
// that (see the BL428 design's "Operator manual edits cannot be
// enforced"). All operations here are unprivileged plain file ops on a
// git-ignored path, and no other lock/file is held while the record is
// read (NeverBlockOnRead): a slow reader only delays that one
// acquisition, never another holder or a releaser.

// LockRecord is the JSON payload of the advisory tree lock file
// (.datawatch/tree.lock, git-ignored so a held lock never dirties
// `git status`). It names the holder so an operator — or a reclaiming
// acquirer — can tell whose lock it is looking at.
type LockRecord struct {
	HolderTaskID string `json:"holder_task_id"`
	HolderPRDID  string `json:"holder_prd_id,omitempty"`
	AcquiredAt   int64  `json:"acquired_at"` // unix seconds
	PID          int    `json:"pid"`
	Note         string `json:"note,omitempty"`
}

// defaultTreeLockPoll is the short default wait-loop poll interval;
// production wiring passes a Manager-controlled value via AcquireTreeLock.
const defaultTreeLockPoll = 5 * time.Second

// maxStaleReclaimAttempts bounds consecutive best-effort delete attempts
// against one stale lock before an acquirer falls through to the wait
// loop (bounded per spec: a losing reclaim race degrades to waiting, it
// never corrupts — a removal only ever targets a lock re-verified as
// stale in the same pass).
const maxStaleReclaimAttempts = 3

// staleLockGrace gates reclaim of an unreadable or empty lock record.
// AcquireTreeLock creates the file and only then writes the record
// (documented window in AcquireTreeLock), so an empty record shortly
// after creation is most likely a LIVE acquirer two steps in — deleting
// it would let two holders coexist. Anything still empty after the grace
// crashed in the window; its holder is unknown by definition, and
// unknown holders are stale, so reclaim then is safe.
const staleLockGrace = 30 * time.Second

// lockRecordPayload renders rec as JSON with a trailing newline.
func lockRecordPayload(rec LockRecord) []byte {
	b, err := json.Marshal(rec)
	if err != nil {
		b = []byte(`{}`) // unmarshalable-fields-free struct; cannot happen
	}
	return append(b, '\n')
}

// writeLockRecord writes rec (JSON + trailing newline) to path: parent
// dir mkdir'd 0755, file 0644. Overwrite-safe (idempotent re-acquire).
func writeLockRecord(path string, rec LockRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, lockRecordPayload(rec), 0o644)
}

// readLockRecord returns the current record at path, or an error when the
// file is missing or not a parseable record.
func readLockRecord(path string) (LockRecord, error) {
	var rec LockRecord
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, fmt.Errorf("tree lock %s: unreadable record: %w", path, err)
	}
	return rec, nil
}

// isTerminalTaskStatus reports whether a task in this state will never
// release its lock itself — failed, cancelled and completed tasks end
// rather than retry — so their lock is stale and reclaimable. Everything
// else (pending, queued, in_progress, waiting_capacity, ...) is a live
// or soon-live holder and must be waited on.
func isTerminalTaskStatus(st TaskStatus) bool {
	switch st {
	case TaskFailed, TaskCancelled, TaskCompleted:
		return true
	}
	return false
}

// treeLockStale decides whether the lock at path is held by a dead holder
// and may be reclaimed:
//   - unreadable record or empty holder_task_id (crash in the
//     create→write window): stale only after staleLockGrace, so a live
//     acquirer mid-write is never destroyed;
//   - taskStatus hook nil, or holder not found in the store: UNKNOWN →
//     stale;
//   - holder in a TERMINAL state (failed/cancelled/completed): stale;
//   - otherwise: live holder → NOT stale.
func treeLockStale(path string, rec *LockRecord, readErr error, taskStatus func(string) (TaskStatus, bool)) bool {
	if readErr != nil || rec == nil || rec.HolderTaskID == "" {
		fi, serr := os.Stat(path)
		if serr != nil { // gone in the meantime — the next create retry wins
			return true
		}
		return time.Since(fi.ModTime()) > staleLockGrace
	}
	if taskStatus == nil {
		return true
	}
	st, ok := taskStatus(rec.HolderTaskID)
	if !ok {
		return true // unknown holder
	}
	return isTerminalTaskStatus(st)
}

// reclaimMu serializes stale-lock removal within this process. A waiter
// decides "stale" from a record it read earlier; without re-verifying
// under a lock, its delayed os.Remove can delete the FRESH lock a
// faster waiter has since claimed (read stale → other waiter reclaims
// and claims → our Remove deletes their live lock → two holders).
var reclaimMu sync.Mutex

// removeStaleLock deletes the lock at path only if it still carries the
// stale record the caller observed (seen=true) — a changed holder means
// a successor already reclaimed and claimed it, and that live lock must
// survive; the caller then just loops and waits on it. With seen=false
// (unreadable/empty record) the file is removed as before. Because new
// claims are O_EXCL, nothing can replace the file between the re-read
// and the Remove except another remover, and removers are serialized.
func removeStaleLock(path string, observed LockRecord, seen bool) error {
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	if seen {
		cur, err := readLockRecord(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if cur.HolderTaskID != observed.HolderTaskID || cur.AcquiredAt != observed.AcquiredAt || cur.PID != observed.PID {
			return fmt.Errorf("tree lock %s: lock changed hands (now task %q); not removing", path, cur.HolderTaskID)
		}
	}
	return os.Remove(path)
}

// interruptibleSleep sleeps d, returning false as soon as ctx cancels or
// isCancelled() reports true. Checked in fine chunks so cancellation
// during a long Manager-controlled poll is honored promptly.
func interruptibleSleep(ctx context.Context, d time.Duration, isCancelled func() bool) bool {
	cancelled := func() bool {
		if ctx != nil && ctx.Err() != nil {
			return true
		}
		return isCancelled != nil && isCancelled()
	}
	if ctx == nil || ctx.Done() == nil {
		// No ctx to select on — pure polling loop.
		remain := d
		for remain > 0 {
			if cancelled() {
				return false
			}
			step := 100 * time.Millisecond
			if remain < step {
				step = remain
			}
			time.Sleep(step)
			remain -= step
		}
		return true
	}
	const chunk = 100 * time.Millisecond
	remain := d
	for remain > 0 {
		if cancelled() {
			return false
		}
		step := chunk
		if remain < step {
			step = remain
		}
		select {
		case <-time.After(step):
			remain -= step
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// newTreeLockRelease returns release() for the lock at path held by
// holder:
//   - Holder-guarded: it deletes the file ONLY if the current record
//     still names this holder — after a reclaim race a successor may
//     hold it, and that fresh lock must survive our release.
//   - Idempotent: the second and later calls are a silent no-op.
//   - Never returns an error: read/delete failures are logged, because a
//     release must never abort the task that is finishing.
func newTreeLockRelease(path, holder string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			rec, err := readLockRecord(path)
			if err != nil {
				if os.IsNotExist(err) {
					return // already gone — nothing to do
				}
				log.Printf("tree lock: release %s: cannot read record: %v", path, err)
				return
			}
			if rec.HolderTaskID != holder {
				log.Printf("tree lock: release %s: refusing — now held by task %q, not %q", path, rec.HolderTaskID, holder)
				return
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("tree lock: release %s: remove failed: %v", path, err)
			}
		})
	}
}

// treeWaitReason is the human-readable reason waitHook receives while
// blocked on a live holder.
func treeWaitReason(key string, rec *LockRecord, readErr error) string {
	if readErr != nil || rec == nil {
		return TreeLockWaitText(key, "", "", 0)
	}
	return TreeLockWaitText(key, rec.HolderTaskID, rec.HolderPRDID, rec.AcquiredAt)
}

// AcquireTreeLock acquires the advisory project-tree write lock for key
// (see ResolveTreeKey) on behalf of holder (a task ID) running under
// prdID, returning release() and nil. Semantics (BL428 3a, operator
// option (i)):
//
//  1. TRY-ONCE-THEN-WAIT: the first attempt is an atomic
//     O_CREATE|O_EXCL lock create, immediately followed by the record
//     write. Documented window: between the two steps the file exists
//     with an empty record. A concurrent acquirer reading it in that
//     moment treats an unknown holder as stale-BUT-recent: treeLockStale
//     refuses reclaim until staleLockGrace has elapsed, so the live
//     writer's claim survives. An acquirer that crashes inside the window
//     leaves an empty record that is reclaimed after the grace, so a
//     holder-less file can never wedge the tree forever.
//
//  2. IDEMPOTENT RE-ACQUIRE: if the record already names holder, the
//     lock is re-returned — no double-ownership, and the original
//     acquired_at is preserved.
//
//  3. STALE RECLAIM: if the holder is UNKNOWN (taskStatus hook missing
//     or not finding the task — a nil hook means every non-self holder
//     is unknown) or its task is TERMINAL (failed/cancelled/completed),
//     the file is best-effort deleted, a warning naming the dead holder
//     is logged, and acquisition is retried. The retry is bounded: after
//     maxStaleReclaimAttempts consecutive lost delete races the acquirer
//     falls through to the wait loop instead of hammering.
//
//  4. WAIT: for a live, non-stale holder, waitHook (if set) is called
//     with a human-readable reason, then the acquirer sleeps
//     pollInterval (≤0 selects the ~5s default; production wiring passes
//     the Manager-controlled value) and re-checks. Cancellation via ctx
//     or isCancelled aborts with an error and NO release: a cancelling
//     waiter never deletes another holder's lock.
//
// NeverBlockOnRead: no lock is held while the record is read, and all
// file ops are unprivileged plain files. Adversarial behavior
// (a session that never checks the lock) is explicitly out of scope by
// design — BL425's drift detection covers it. On error, release is
// nil and nothing is left for the caller to undo.
func AcquireTreeLock(
	ctx context.Context,
	key, treeRoot string,
	holder, prdID string,
	taskStatus func(holderID string) (TaskStatus, bool),
	waitHook func(reason string),
	isCancelled func() bool,
	pollInterval time.Duration,
) (func(), error) {
	if holder == "" {
		return nil, fmt.Errorf("tree lock: empty holder task ID for key %q", key)
	}
	if pollInterval <= 0 {
		pollInterval = defaultTreeLockPoll
	}
	path := treeLockPath(key, treeRoot)
	rec := LockRecord{
		HolderTaskID: holder,
		HolderPRDID:  prdID,
		AcquiredAt:   time.Now().Unix(),
		PID:          os.Getpid(),
	}

	claim := func() error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, werr := f.Write(lockRecordPayload(rec)); werr != nil {
			_ = f.Close()
			_ = os.Remove(path) // best effort — never leave our own record-less claim
			return fmt.Errorf("tree lock: write record to %s: %w", path, werr)
		}
		if cerr := f.Close(); cerr != nil {
			_ = os.Remove(path)
			return fmt.Errorf("tree lock: close %s: %w", path, cerr)
		}
		return nil
	}

	reclaimCount := 0
	for {
		if ctx != nil && ctx.Err() != nil {
			return nil, fmt.Errorf("tree lock: cancelled while waiting for %q", key)
		}
		if isCancelled != nil && isCancelled() {
			return nil, fmt.Errorf("tree lock: cancelled while waiting for %q", key)
		}

		err := claim()
		if err == nil {
			log.Printf("tree lock: acquired %s holder task=%s prd=%s", path, holder, prdID)
			return newTreeLockRelease(path, holder), nil
		}
		if !os.IsExist(err) {
			// Not "already held" — most likely the lazily-created
			// .datawatch dir (or a transient FS error). Mkdir the parent
			// once, retry the claim, and give up if it still fails.
			if merr := os.MkdirAll(filepath.Dir(path), 0o755); merr != nil {
				return nil, fmt.Errorf("tree lock: %v", merr)
			}
			retryErr := claim()
			if retryErr == nil {
				log.Printf("tree lock: acquired %s holder task=%s prd=%s", path, holder, prdID)
				return newTreeLockRelease(path, holder), nil
			}
			if !os.IsExist(retryErr) {
				return nil, fmt.Errorf("tree lock: create %s: %w", path, retryErr)
			}
		}

		// The lock exists (either already on first pass, or re-appearing
		// after a lost reclaim race). NeverBlockOnRead: a plain read with
		// nothing else held.
		existing, rerr := readLockRecord(path)
		if rerr == nil && existing.HolderTaskID == holder {
			log.Printf("tree lock: re-acquired %s holder task=%s (idempotent)", path, holder)
			return newTreeLockRelease(path, holder), nil
		}

		if treeLockStale(path, &existing, rerr, taskStatus) {
			if rerr == nil {
				log.Printf("tree lock: reclaiming stale lock %s — holder task %q (PRD %q) is unknown or terminal", path, existing.HolderTaskID, existing.HolderPRDID)
			} else {
				log.Printf("tree lock: reclaiming stale lock %s — record unreadable or empty (grace expired)", path)
			}
			derr := removeStaleLock(path, existing, rerr == nil) // best effort per spec
			if derr == nil || os.IsNotExist(derr) {
				if reclaimCount < maxStaleReclaimAttempts {
					reclaimCount++
					continue // immediate retry — the stale file is gone
				}
				// Lost the delete race maxStaleReclaimAttempts times
				// running: fall through to the wait loop (bounded; no
				// corruption — we only ever removed locks re-verified
				// stale in the same pass).
			} else {
				log.Printf("tree lock: best-effort reclaim of %s failed: %v", path, derr)
			}
		}

		// Live holder (or a bounded reclaim loop): notify, sleep, retry.
		reason := treeWaitReason(key, &existing, rerr)
		if waitHook != nil {
			waitHook(reason)
		}
		if !interruptibleSleep(ctx, pollInterval, isCancelled) {
			return nil, fmt.Errorf("tree lock: cancelled while waiting: %s", reason)
		}
	}
}

// --- Executor wiring (BL428 §3a) --------------------------------------
//
// The acquireTreeWritersLock method is the Manager-level admission hook
// Run's executeOne calls before a task touches its tree. It resolves the
// per-(PRD, task) canonical tree, runs the refuse-dirty-tree gate, then
// acquires the advisory file lock with the daemon's wait/cancel
// conventions: visible as TaskWaitingCapacity with a "tree lock" wait
// reason, bounded by the capacity wait timeout, interruptible on task
// cancellation, holding the same lock for the rest of the task's run
// (spawn, verify, retries) via the release closure it stores.

// resolveRefuseDirtyTree returns the effective refuse-dirty-tree gate
// for this PRD: the PRD-level override when set, else the daemon-wide
// config default (false).
func (m *Manager) resolveRefuseDirtyTree(prd *PRD) bool {
	if prd != nil && prd.RefuseDirtyTreeStart != nil {
		return *prd.RefuseDirtyTreeStart
	}
	m.mu.Lock()
	v := m.cfg.RefuseDirtyTreeStart
	m.mu.Unlock()
	return v
}

// dirtyTreeRefused implements admitTree's dirty-tree check: when the
// gate is on and the task's canonical tree has uncommitted changes
// (git status --porcelain non-empty), it returns an error hard-blocking
// the task. A tree where git can't report status (not a repo, git
// missing) is passed through — the gate only reasons about dirt it can
// observe.
func (m *Manager) dirtyTreeRefused(ctx context.Context, prd *PRD, task *Task) error {
	if !m.resolveRefuseDirtyTree(prd) {
		return nil
	}
	_, treeRoot, err := ResolveTreeKey(prd, task)
	if err != nil {
		return nil // no resolvable tree — nothing to refuse on
	}
	out, err := gitProbe(ctx, treeRoot, "status", "--porcelain")
	if err != nil {
		return nil
	}
	dirt := strings.TrimSpace(out)
	if dirt == "" {
		return nil
	}
	n := len(strings.Split(dirt, "\n"))
	unit := "entries"
	if n == 1 {
		unit = "entry"
	}
	snip := dirt
	if i := strings.IndexByte(snip, '\n'); i > 0 {
		snip = strings.TrimSpace(snip[:i])
	}
	if len(snip) > 64 {
		snip = snip[:64] + "…"
	}
	return fmt.Errorf("refuse_dirty_tree_start: tree %s has %d uncommitted %s (%s); commit, stash, `git clean`, or set autonomous.refuse_dirty_tree_start: false", treeRoot, n, unit, snip)
}

// acquireTreeWritersLock acquires the per-canonical-tree advisory write
// lock for one task and returns its release closure. The caller (one
// executeOne run) keeps the closure for the rest of the task's run
// (spawn, verify, retries) and releases it through its own single
// deferred call, so two concurrent tasks each own and release their own
// acquisition. Waiting is visible through setWaiting with a
// "tree lock: ..." reason; on success the wait state is cleared again.
func (m *Manager) acquireTreeWritersLock(ctx context.Context, prd *PRD, task *Task) (func(), error) {
	if gerr := m.dirtyTreeRefused(ctx, prd, task); gerr != nil {
		return nil, gerr
	}
	key, treeRoot, err := ResolveTreeKey(prd, task)
	if err != nil {
		// No project dir to key on (e.g. cluster-dispatched PRDs run in
		// the worker's own container, not on this host's tree): there is
		// no local canonical tree to serialize or gate — nothing to lock.
		return func() {}, nil
	}
	if st, serr := os.Stat(treeRoot); serr != nil || !st.IsDir() {
		// The canonical tree is not (yet) present on disk — a task may
		// create its project directory as part of the work, and there
		// is nothing to serialize or gate on until it exists. Real
		// operator project dirs exist before workers write to them, so
		// this skip only affects not-yet-created trees (and test
		// fakes); once the tree exists, every later admission re-checks
		// the gate and the lock normally.
		return func() {}, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, m.capacityWait())
	defer cancel()
	waited := false
	release, err := AcquireTreeLock(waitCtx, key, treeRoot, task.ID, task.PRDID,
		func(holderID string) (TaskStatus, bool) {
			// TaskSnapshot (value copy under the store lock), not GetTask's shared
			// pointer + out-of-lock .Status read: that read raced a concurrent
			// store-locked Status write (executeOne advancing a holder to
			// TaskVerifying) and failed -race in concurrent same-tree runs.
			snap, ok := m.store.TaskSnapshot(holderID)
			if !ok {
				return "", false
			}
			return snap.Status, true
		},
		func(reason string) {
			waited = true
			m.setWaiting(task, "tree lock: "+reason)
		},
		func() bool { return m.taskCancelled(task.ID) },
		capRetryPause,
	)
	if waited {
		m.clearWaiting(task)
	}
	if err != nil {
		if m.taskCancelled(task.ID) {
			return nil, errCancelledWhileWaiting
		}
		return nil, err
	}
	return release, nil
}
