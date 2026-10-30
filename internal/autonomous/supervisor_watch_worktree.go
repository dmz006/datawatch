// Supervisor watch worktree — the daemon-side checks over an
// Automaton's project worktree that the supervisor watch raises on
// each coordinator poll, with no model call:
//
//   - the worktree has no go.mod (worktree_no_gomod), applied only
//     when the Automaton's project is a Go project — detected by the
//     repository's own go.mod at the registered base — so non-Go
//     projects never raise it;
//
//   - the worktree directory is not in the registered worktree list
//     of its repository (worktree_not_registered);
//
//   - an Automaton with a task in progress or verifying has gone
//     silent (worktree_silent): its newest activity — the newest
//     modification time of any non-`.git` file in the worktree, plus
//     the newest worker transcript or log file the existing session
//     code already knows for that session — is older than the watch's
//     silence window (SupervisorWatchConfig.EffectiveSilentMinutes).
//
// Like the host checks (supervisor_watch_host.go) and the work
// checks (supervisor_watch_work.go), the checks are pure over their
// inputs: they read facts through the injected filesystem, clock and
// session views and mint findings from them, and they never act. A
// fact the views cannot read raises nothing (unknown, no finding),
// and no literal absolute path is used other than the one taken from
// the configuration (the registered base repository) or the registry:
// every worktree directory comes from the Automaton's registered
// project directory.
package autonomous

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// SupervisorWatchWorktreeFS is the injected filesystem view the
// worktree checks read through, so the checks (and their tests) run
// against fakes and never touch a real worktree. Every method returns
// an error when the fact is unknown (directory gone, git missing,
// path unreadable); the checks treat that as "unknown, no finding"
// and never mint a finding from a value they could not read.
type SupervisorWatchWorktreeFS interface {
	// GoModAt reports whether dir holds a go.mod file. A missing
	// directory or file is (false, nil), not an error; a directory
	// unreadable for any other reason is an error.
	GoModAt(dir string) (bool, error)
	// RegisteredWorktrees returns the registered worktree
	// directories of the repository at repo — its `git worktree
	// list`. An error means the list is unreadable (git missing,
	// repo no repository): the check then finds nothing.
	RegisteredWorktrees(repo string) ([]string, error)
	// NewestNonGitChange returns the newest modification time of any
	// non-`.git` file under dir; ok is false when the directory
	// holds no readable file at all.
	NewestNonGitChange(dir string) (time.Time, bool, error)
	// Mtime returns the modification time of the file at path. A
	// missing file is an error, kept distinct from an old timestamp.
	Mtime(path string) (time.Time, error)
}

// SupervisorWatchClock is the injected watch clock, so the silence
// check's "now" is testable.
type SupervisorWatchClock interface {
	Now() time.Time
}

// realSupervisorWatchClock — the production clock: time.Now.
type realSupervisorWatchClock struct{}

func (realSupervisorWatchClock) Now() time.Time { return time.Now() }

// SupervisorWatchSessionLogger is the view of the existing session
// code's own records: the newest worker transcript or log file it
// already knows for a session.
type SupervisorWatchSessionLogger interface {
	// SessionLogPath returns the log file the session with id
	// sessionID already has, and true when the session is known and
	// carries one; false when the session is unknown, has no log
	// yet, or the view is not wired.
	SessionLogPath(sessionID string) (string, bool)
}

// SessionSupervisorWatchSessionLogger adapts the existing session
// manager (internal/session) to SupervisorWatchSessionLogger: the
// log file the session code already knows for a session is the
// session record's own LogFile field (session.Session.LogFile), the
// same field the per-session monitors and the worker nudge clock
// read. A session looked up by full or short id is both handled by
// the manager's own lookup; a nil GetSession or a session without a
// log reads as "no known log", never an error.
type SessionSupervisorWatchSessionLogger struct {
	// GetSession is *session.Manager's GetSession (full id or short
	// id → session record); tests use a fixed map.
	GetSession func(id string) (*session.Session, bool)
}

// SessionLogPath satisfies SupervisorWatchSessionLogger.
func (a SessionSupervisorWatchSessionLogger) SessionLogPath(sessionID string) (string, bool) {
	if a.GetSession == nil {
		return "", false
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", false
	}
	sess, ok := a.GetSession(id)
	if !ok || sess == nil {
		return "", false
	}
	if p := strings.TrimSpace(sess.LogFile); p != "" {
		return p, true
	}
	return "", false
}

// SupervisorWatchWorktree runs the worktree checks. It holds the
// injected filesystem and clock views, the view of the session
// records' log files, the watch config (the only place the silence
// window's default is applied, via EffectiveSilentMinutes), and the
// registered base repository: the base of the repository this fleet
// runs in, taken from the configuration — the one absolute path the
// checks may use, the same base repository the manager's worktree
// integrity check reads (cfg.WorktreeBaseRepo).
type SupervisorWatchWorktree struct {
	FS       SupervisorWatchWorktreeFS
	Clock    SupervisorWatchClock
	Sessions SupervisorWatchSessionLogger
	Config   SupervisorWatchConfig
	BaseRepo string
}

// NewSupervisorWatchWorktree wires the worktree checks over the given
// views. A nil filesystem degrades every check to "unknown, no
// finding" rather than panicking; a nil clock defaults to the real
// clock. Sessions may be nil: the silence check then reads only
// worktree file activity. config's Effective* accessors are the only
// place defaults exist, so callers pass the config as-is.
func NewSupervisorWatchWorktree(
	fsys SupervisorWatchWorktreeFS,
	clock SupervisorWatchClock,
	sessions SupervisorWatchSessionLogger,
	config SupervisorWatchConfig,
	baseRepo string,
) *SupervisorWatchWorktree {
	if clock == nil {
		clock = realSupervisorWatchClock{}
	}
	return &SupervisorWatchWorktree{
		FS:       fsys,
		Clock:    clock,
		Sessions: sessions,
		Config:   config,
		BaseRepo: strings.TrimSpace(baseRepo),
	}
}

// supervisorWatchWorktreePRDs returns the Automata the worktree checks
// run over, in stable id order: running Automata (PRDRunning) with a
// registered project directory (PRD.ProjectDir) — the same filter
// SupervisorWatchRunningUnits applies for the host checks. Automata
// that are not running, have no id, or have no registered worktree are
// never checked: the worktree findings are about a running work
// item's worktree, and an unregistered worktree directory is
// exactly the worktree_not_registered finding, not a fact the checks
// may inspect.
func supervisorWatchWorktreePRDs(prds []*PRD) []*PRD {
	seen := map[string]bool{}
	var out []*PRD
	for _, p := range prds {
		if p == nil || p.Status != PRDRunning {
			continue
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.ProjectDir) == "" {
			continue
		}
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// supervisorWatchWorktreeFinding mints one finding of the given kind
// for the given subject, with its operator-facing text built in the
// single wording home (supervisor_watch_text.go); the kinds used here
// are all known, so the empty fallback cannot bite. FirstSeenAt and
// LastSeenAt are left zero: the shared finding store sets and
// preserves them on record (see SupervisorWatchStore.Upsert).
func supervisorWatchWorktreeFinding(kind SupervisorWatchKind, subject string) SupervisorWatchFinding {
	text, _ := SupervisorWatchText(kind, subject)
	return SupervisorWatchFinding{
		Kind:    kind,
		Subject: subject,
		Text:    text,
	}
}

// CheckNoGomod returns one worktree_no_gomod finding per running
// Automaton whose registered worktree holds no go.mod, applied only
// when the Automaton's project is a Go project: the repository's own
// go.mod must be present at the registered base (BaseRepo), so a
// non-Go project never raises this finding. An unset base, an
// unreadable base, or an unreadable worktree raises nothing:
// unknown, no finding. The finding's subject is the Automaton id,
// the same subject the finding text inlines.
func (w *SupervisorWatchWorktree) CheckNoGomod(prds []*PRD) []SupervisorWatchFinding {
	if w.FS == nil {
		return nil
	}
	if w.BaseRepo == "" {
		return nil // no registered base: it cannot be shown to be Go
	}
	baseIsGo, err := w.FS.GoModAt(w.BaseRepo)
	if err != nil || !baseIsGo {
		return nil // non-Go project (or unknown): never raise
	}
	var out []SupervisorWatchFinding
	for _, p := range supervisorWatchWorktreePRDs(prds) {
		has, err := w.FS.GoModAt(p.ProjectDir)
		if err != nil || has {
			continue
		}
		out = append(out, supervisorWatchWorktreeFinding(SupervisorWatchWorktreeNoGomod, p.ID))
	}
	return out
}

// CheckNotRegistered returns one worktree_not_registered finding per
// running Automaton whose project directory is not in the registered
// worktree list of its repository — the repository at the registered
// base (BaseRepo). A nil view or an unreadable list raises nothing
// (unknown, no finding); a path matches when it is one of the
// list's entries after cleaning (the list is compared by
// symlink-resolved path, the same convention the manager's worktree
// integrity check uses, see evalOrClean).
func (w *SupervisorWatchWorktree) CheckNotRegistered(prds []*PRD) []SupervisorWatchFinding {
	if w.FS == nil {
		return nil
	}
	if w.BaseRepo == "" {
		return nil // no repository to register against
	}
	list, err := w.FS.RegisteredWorktrees(w.BaseRepo)
	if err != nil {
		return nil
	}
	registered := map[string]bool{}
	for _, d := range list {
		if strings.TrimSpace(d) != "" {
			registered[evalOrClean(d)] = true
		}
	}
	var out []SupervisorWatchFinding
	for _, p := range supervisorWatchWorktreePRDs(prds) {
		if registered[evalOrClean(p.ProjectDir)] {
			continue
		}
		out = append(out, supervisorWatchWorktreeFinding(SupervisorWatchWorktreeNotRegistered, p.ID))
	}
	return out
}

// CheckSilent returns one worktree_silent finding per running
// Automaton that has a task in progress or verifying whose newest
// activity is older than the silence window in effect
// (config.EffectiveSilentMinutes — the only place the 60-minute
// default is applied). "Activity" is the newest of: the newest
// modification time of any non-`.git` file in the worktree, and the
// newest worker transcript or log file the existing session code
// already knows for the session of a task the Automaton is working
// on (SessionSupervisorWatchSessionLogger). An Automaton with no
// task in progress or verifying is not expected to be active and
// never raises; so does the check when it could observe no activity
// at all (unknown, no finding).
//
// The comparison is strictly "older than the window": activity
// exactly at the window's edge is still expected, so a poll at the
// boundary never reports a silence its window does not cover.
func (w *SupervisorWatchWorktree) CheckSilent(prds []*PRD) []SupervisorWatchFinding {
	if w.FS == nil || w.Clock == nil {
		return nil
	}
	window := time.Duration(w.Config.EffectiveSilentMinutes()) * time.Minute
	now := w.Clock.Now()
	var out []SupervisorWatchFinding
	for _, p := range supervisorWatchWorktreePRDs(prds) {
		if !supervisorWatchWorktreeHasActiveWork(p) {
			continue // no task in progress or verifying: silence is expected
		}
		dir := filepath.Clean(p.ProjectDir)
		var newest time.Time
		if t, ok, err := w.FS.NewestNonGitChange(dir); err != nil {
			continue // unreadable worktree directory: unknown, no finding
		} else if ok {
			newest = t
		}
		for i := range p.Story {
			for j := range p.Story[i].Tasks {
				t := &p.Story[i].Tasks[j]
				if t.Status != TaskInProgress && t.Status != TaskVerifying {
					continue
				}
				sid := strings.TrimSpace(t.SessionID)
				sidv := strings.TrimSpace(t.VerifierSessionID)
				for _, known := range []string{sid, sidv} {
					if known == "" || w.Sessions == nil {
						continue
					}
					logFile, ok := w.Sessions.SessionLogPath(known)
					if !ok {
						continue
					}
					mt, err := w.FS.Mtime(logFile)
					if err != nil {
						continue // no readable log: not activity, not a fact
					}
					if mt.After(newest) {
						newest = mt
					}
				}
			}
		}
		if newest.IsZero() {
			continue // no activity observed at all: unknown, no finding
		}
		if now.Sub(newest) > window {
			out = append(out, supervisorWatchWorktreeFinding(SupervisorWatchWorktreeSilent, p.ID))
		}
	}
	return out
}

// supervisorWatchWorktreeHasActiveWork reports whether p has at least
// one task in progress or verifying — the "expected to be active"
// half of the worktree_silent condition.
func supervisorWatchWorktreeHasActiveWork(p *PRD) bool {
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			t := &p.Story[i].Tasks[j]
			if t != nil && (t.Status == TaskInProgress || t.Status == TaskVerifying) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------
//
// Production filesystem view: ShellSupervisorWatchWorktreeFS.
//
// Reads the facts the checks need straight from the filesystem and
// one `git worktree list` per registered base. Every failure path is
// an error the checks degrade to "unknown, no finding"; the one
// absolute path this reads is the base repository, taken from the
// configuration — never a literal.

// ShellSupervisorWatchWorktreeFS — the production
// SupervisorWatchWorktreeFS.
type ShellSupervisorWatchWorktreeFS struct{}

// NewShellSupervisorWatchWorktreeFS returns the production filesystem
// view. It carries no configuration of its own: the base repository
// and every worktree directory arrive from the caller (the
// SupervisorWatchWorktree it is wired into), so no path is baked in
// here.
func NewShellSupervisorWatchWorktreeFS() *ShellSupervisorWatchWorktreeFS {
	return &ShellSupervisorWatchWorktreeFS{}
}

// GoModAt reports whether dir holds a go.mod: a file (or a
// broken symlink standing in for one) named go.mod directly in dir.
// A missing directory or a directory without the file is (false, nil);
// a directory unreadable for another reason (permission denied) is an
// error.
func (f *ShellSupervisorWatchWorktreeFS) GoModAt(dir string) (bool, error) {
	fi, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.IsDir() {
		return false, nil // a directory named go.mod is not a go.mod
	}
	return true, nil
}

// RegisteredWorktrees returns the repository at repo's registered
// worktree directories — its `git worktree list --porcelain` output.
// git missing or the list unreadable is an error: the check degrades
// to "unknown, no finding".
func (f *ShellSupervisorWatchWorktreeFS) RegisteredWorktrees(repo string) ([]string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("supervisor watch worktree: git not found on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, git, "-C", repo, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("supervisor watch worktree: git worktree list in %s: %v (output: %s)",
			repo, err, strings.TrimSpace(string(out)))
	}
	var list []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			if p = strings.TrimSpace(p); p != "" {
				list = append(list, p)
			}
		}
	}
	return list, nil
}

// NewestNonGitChange returns the newest modification time of any
// non-`.git` file under dir; ok is false when the directory holds no
// readable file at all. The `.git` entry (a directory in a main
// checkout, a gitlink file in a linked worktree) is skipped, and so
// are its contents; a file that vanished during the walk is skipped,
// a directory unreadable at the root is an error.
func (f *ShellSupervisorWatchWorktreeFS) NewestNonGitChange(dir string) (time.Time, bool, error) {
	st, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	if !st.IsDir() {
		return time.Time{}, false, fmt.Errorf("supervisor watch worktree: %s is not a directory", dir)
	}
	var newest time.Time
	found := false
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err // the root itself is unreadable
			}
			return nil // an entry that vanished or cannot be read: skip it
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil // the gitlink file of a linked worktree: skip it too
		}
		if !d.Type().IsRegular() {
			return nil
		}
		// An entry whose info cannot be read is skipped, like an
		// unreadable one above.
		if info, ierr := d.Info(); ierr == nil && (!found || info.ModTime().After(newest)) {
			newest = info.ModTime()
			found = true
		}
		return nil
	})
	if walkErr != nil {
		return time.Time{}, false, walkErr
	}
	return newest, found, nil
}

// Mtime returns the modification time of the file at path; a missing
// file is the caller's os.IsNotExist to filter on.
func (f *ShellSupervisorWatchWorktreeFS) Mtime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
