// Tests for the supervisor watch worktree checks
// (supervisor_watch_worktree.go): one test per finding kind, plus the
// healthy case, that a not-running Automaton's worktree is ignored,
// and that a non-Go project never raises worktree_no_gomod. Every
// view — the filesystem, the clock and the session records' log
// files — is injected, no real worktree is touched.
package autonomous

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// fakeSupervisorWatchWorktreeFS implements
// SupervisorWatchWorktreeFS over fixed tables. A key that is absent
// from a table reads the "unknown" value for that method (no go.mod,
// no registered worktrees, no observed change, no such log file) so
// a test only writes down what it asserts exists.
type fakeSupervisorWatchWorktreeFS struct {
	// gomod maps a cleaned directory to "holds a go.mod".
	gomod map[string]bool
	// worktrees maps a cleaned repository directory to its
	// registered worktree list; worktreesErr forces an error for a
	// repository whose registry is unreadable (git missing, not a
	// repository).
	worktrees    map[string][]string
	worktreesErr map[string]error
	// newest maps a cleaned worktree directory to the newest
	// non-`.git` file modification time observed under it.
	newest map[string]time.Time
	// mtimes maps a log file path to its modification time.
	mtimes map[string]time.Time
}

func (f *fakeSupervisorWatchWorktreeFS) key(s string) string { return filepath.Clean(s) }

func (f *fakeSupervisorWatchWorktreeFS) GoModAt(dir string) (bool, error) {
	return f.gomod[f.key(dir)], nil
}

func (f *fakeSupervisorWatchWorktreeFS) RegisteredWorktrees(repo string) ([]string, error) {
	if err := f.worktreesErr[f.key(repo)]; err != nil {
		return nil, err
	}
	return f.worktrees[f.key(repo)], nil
}

func (f *fakeSupervisorWatchWorktreeFS) NewestNonGitChange(dir string) (time.Time, bool, error) {
	t, ok := f.newest[f.key(dir)]
	return t, ok, nil
}

func (f *fakeSupervisorWatchWorktreeFS) Mtime(path string) (time.Time, error) {
	t, ok := f.mtimes[path]
	if !ok {
		return time.Time{}, errors.New("no such log file")
	}
	return t, nil
}

// fixedSupervisorWatchClock pins the watch's "now".
type fixedSupervisorWatchClock struct{ now time.Time }

func (c fixedSupervisorWatchClock) Now() time.Time { return c.now }

// fakeSupervisorWatchSessions maps session ids to the log file the
// "existing session code" already knows for them.
type fakeSupervisorWatchSessions struct {
	paths map[string]string
}

func (f *fakeSupervisorWatchSessions) SessionLogPath(sessionID string) (string, bool) {
	p, ok := f.paths[sessionID]
	return p, ok
}

const (
	supervisorWatchWorktreeBase = "/base/repo"
	supervisorWatchWorktreeDir  = "/base/repo/wtrees/p-1"
)

// supervisorWatchTestSilentMinutes returns a fresh pointer to the test
// silence window (30), fresh storage per call.
func supervisorWatchTestSilentMinutes() *int { v := 30; return &v }

// supervisorWatchWorktreePRD builds one running Automaton with the
// given project directory and tasks — the same shape
// supervisorWatchRunningUnits reads from a live PRD.
func supervisorWatchWorktreePRD(id, projectDir string, tasks ...Task) *PRD {
	running := "p-" + id
	p := &PRD{
		ID:         "p-" + id,
		Title:      running,
		Status:     PRDRunning,
		ProjectDir: projectDir,
	}
	if len(tasks) == 0 {
		return p
	}
	story := Story{ID: "p-" + id + "-s1", PRDID: p.ID, Tasks: tasks}
	for i := range story.Tasks {
		story.Tasks[i].ID = p.ID + "-t" + story.Tasks[i].ID
		story.Tasks[i].StoryID = story.ID
		story.Tasks[i].PRDID = p.ID
	}
	p.Story = []Story{story}
	return p
}

func supervisorWatchWorktreeActiveTask(id, sessionID string) Task {
	return Task{ID: id, Title: "work", Status: TaskInProgress, SessionID: sessionID}
}

// TestSupervisorWatchWorktreeNoGomod — one worktree_no_gomod finding
// per running Go-project Automaton whose worktree holds no go.mod;
// none when the worktree has the file, the base repository does not
// (the not-a-Go case is covered by its own test), or the base is not
// registered.
func TestSupervisorWatchWorktreeNoGomod(t *testing.T) {
	dirMissing := "/base/repo/wtrees/p-1" // holds no go.mod (absent ⇒ false)
	dirHealthy := "/base/repo/wtrees/p-2" // does hold one
	missing := supervisorWatchWorktreePRD("1", dirMissing,
		supervisorWatchWorktreeActiveTask("1", "s-1"))
	healthy := supervisorWatchWorktreePRD("2", dirHealthy,
		supervisorWatchWorktreeActiveTask("1", "s-2"))

	fs := &fakeSupervisorWatchWorktreeFS{
		gomod: map[string]bool{
			supervisorWatchWorktreeBase: true, // the project is Go
			dirHealthy:                  true, // p-2's worktree has the file
		},
	}
	w := NewSupervisorWatchWorktree(fs, fixedSupervisorWatchClock{now: time.Unix(1_700_000_000, 0)},
		nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow},
		supervisorWatchWorktreeBase)

	got := w.CheckNoGomod([]*PRD{missing, healthy})
	if len(got) != 1 {
		t.Fatalf("expected 1 worktree_no_gomod finding, got %d: %+v", len(got), got)
	}
	if got[0].Kind != SupervisorWatchWorktreeNoGomod || got[0].Subject != "p-1" {
		t.Errorf("expected worktree_no_gomod/p-1, got %q/%q", got[0].Kind, got[0].Subject)
	}
	want, _ := SupervisorWatchText(SupervisorWatchWorktreeNoGomod, "p-1")
	if got[0].Text != want {
		t.Errorf("text = %q, want %q", got[0].Text, want)
	}

	// The healthy worktree alone never raises.
	if got := w.CheckNoGomod([]*PRD{healthy}); got != nil {
		t.Errorf("healthy worktree: expected no findings, got %+v", got)
	}

	// No base registered ⇒ unknown ⇒ nothing to raise, even from the
	// Go-project Automaton.
	wNoBase := NewSupervisorWatchWorktree(fs, fixedSupervisorWatchClock{now: time.Unix(1_700_000_000, 0)},
		nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow}, "")
	if got := wNoBase.CheckNoGomod([]*PRD{missing}); got != nil {
		t.Errorf("no base repository: expected no findings, got %+v", got)
	}

	// Nil filesystem ⇒ unknown ⇒ nothing to raise, never a panic.
	wNil := NewSupervisorWatchWorktree(nil, nil, nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow},
		supervisorWatchWorktreeBase)
	if got := wNil.CheckNoGomod([]*PRD{missing}); got != nil {
		t.Errorf("nil filesystem: expected no findings, got %+v", got)
	}
}

// TestSupervisorWatchWorktreeNotRegistered — one
// worktree_not_registered finding per running Automaton whose
// project directory is not in the registered worktree list of its
// repository; none for a directory the list holds, and none when the
// list itself cannot be read (the nil-fs case, unknown ⇒ no finding).
func TestSupervisorWatchWorktreeNotRegistered(t *testing.T) {
	stray := supervisorWatchWorktreePRD("1", "/stray/dir",
		supervisorWatchWorktreeActiveTask("1", "s-1"))
	registered := supervisorWatchWorktreePRD("2", supervisorWatchWorktreeDir,
		supervisorWatchWorktreeActiveTask("1", "s-2"))

	registeredList := []string{
		supervisorWatchWorktreeBase, // the main checkout
		supervisorWatchWorktreeDir,  // p-2's worktree is registered
		"/other/fleet/worktrees/p-9",
	}
	fs := &fakeSupervisorWatchWorktreeFS{
		worktrees: map[string][]string{
			supervisorWatchWorktreeBase: registeredList,
		},
	}
	w := NewSupervisorWatchWorktree(fs, fixedSupervisorWatchClock{now: time.Unix(1_700_000_000, 0)},
		nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow},
		supervisorWatchWorktreeBase)

	got := w.CheckNotRegistered([]*PRD{stray, registered})
	if len(got) != 1 {
		t.Fatalf("expected 1 worktree_not_registered finding, got %d: %+v", len(got), got)
	}
	if got[0].Kind != SupervisorWatchWorktreeNotRegistered || got[0].Subject != "p-1" {
		t.Errorf("expected worktree_not_registered/p-1, got %q/%q", got[0].Kind, got[0].Subject)
	}
	want, _ := SupervisorWatchText(SupervisorWatchWorktreeNotRegistered, "p-1")
	if got[0].Text != want {
		t.Errorf("text = %q, want %q", got[0].Text, want)
	}

	// The registered worktree never raises, on its own or alongside
	// the stray one (the loop already excludes it above).
	if got := w.CheckNotRegistered([]*PRD{registered}); got != nil {
		t.Errorf("registered worktree: expected no findings, got %+v", got)
	}

	// Unreadable list ⇒ unknown ⇒ nothing to raise.
	wUnreadable := NewSupervisorWatchWorktree(
		&fakeSupervisorWatchWorktreeFS{worktreesErr: map[string]error{"/unregistered/base": errors.New("git worktree list failed")}},
		fixedSupervisorWatchClock{now: time.Unix(1_700_000_000, 0)},
		nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow}, "/unregistered/base")
	if got := wUnreadable.CheckNotRegistered([]*PRD{stray}); got != nil {
		t.Errorf("unreadable registry: expected no findings, got %+v", got)
	}
}

// TestSupervisorWatchWorktreeSilent — one worktree_silent finding
// per running Automaton with a task in progress or verifying whose
// newest activity is older than the silence window. The newest
// activity is the newer of the worktree's newest non-`.git` file
// change and the newest worker log file the session records already
// know for the session a task is working on: a stale worktree with a
// fresh session log does not raise, while a log is sufficient on its
// own when the worktree offers no readable file at all.
func TestSupervisorWatchWorktreeSilent(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fs := fixedSupervisorWatchClock{now: now}
	cfg := SupervisorWatchConfig{Mode: SupervisorWatchModeShadow, SilentMinutes: supervisorWatchTestSilentMinutes()}

	dirSilent := "/base/repo/wtrees/p-silent"   // p-silent's worktree
	dirLogOnly := "/base/repo/wtrees/p-logonly" // p-logonly's worktree: no readable file
	dirFreshLog := "/base/repo/wtrees/p-freshlog"
	dirFreshWork := "/base/repo/wtrees/p-freshwork"

	// p-silent: working; worktree moved 2h ago, session log 80m ago —
	// both older than the 30m window ⇒ raises.
	stalePRD := supervisorWatchWorktreePRD("silent", dirSilent,
		supervisorWatchWorktreeActiveTask("1", "s-stale"))
	// p-logonly: working; the worktree offers no readable file at all,
	// so its session log (90m ago) is the newest activity ⇒ raises.
	logOnlyPRD := supervisorWatchWorktreePRD("logonly", dirLogOnly,
		supervisorWatchWorktreeActiveTask("1", "s-logonly"))
	// p-freshlog: working; worktree stale (2h) but its session log
	// moved 5m ago ⇒ fresh activity, no silence.
	freshLogPRD := supervisorWatchWorktreePRD("freshlog", dirFreshLog,
		supervisorWatchWorktreeActiveTask("1", "s-freshlog"))
	// p-freshwork: working; worktree moved 2m ago, no known session
	// log ⇒ fresh on its own, no silence.
	freshWorkPRD := supervisorWatchWorktreePRD("freshwork", dirFreshWork,
		supervisorWatchWorktreeActiveTask("1", "s-nowhere"))

	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{
			dirSilent: now.Add(-2 * time.Hour),
			// dirLogOnly absent: no readable file under it
			dirFreshLog:  now.Add(-2 * time.Hour),
			dirFreshWork: now.Add(-2 * time.Minute),
		},
		mtimes: map[string]time.Time{
			"/logs/s-stale":    now.Add(-80 * time.Minute),
			"/logs/s-logonly":  now.Add(-90 * time.Minute),
			"/logs/s-freshlog": now.Add(-5 * time.Minute),
		},
	}
	sessions := &fakeSupervisorWatchSessions{paths: map[string]string{
		"s-stale":    "/logs/s-stale",
		"s-logonly":  "/logs/s-logonly",
		"s-freshlog": "/logs/s-freshlog",
	}}

	w := NewSupervisorWatchWorktree(fsys, fs, sessions, cfg, supervisorWatchWorktreeBase)
	got := w.CheckSilent([]*PRD{stalePRD, logOnlyPRD, freshLogPRD, freshWorkPRD})
	if len(got) != 2 {
		t.Fatalf("expected 2 worktree_silent findings, got %d: %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, f := range got {
		if f.Kind != SupervisorWatchWorktreeSilent {
			t.Errorf("expected kind worktree_silent, got %q", f.Kind)
		}
		if f.Text == "" {
			t.Errorf("finding %q/%q has no operator-facing text", f.Kind, f.Subject)
		}
		seen[f.Subject] = true
	}
	if !seen["p-silent"] || !seen["p-logonly"] {
		t.Errorf("expected findings for p-silent and p-logonly, saw %+v", seen)
	}

	// No task in progress or verifying ⇒ silence is expected ⇒ no
	// finding even for an old worktree with an old log.
	dormant := supervisorWatchWorktreePRD("dormant", dirSilent,
		Task{ID: "d1", Title: "done", Status: TaskCompleted, SessionID: "s-stale"})
	if got := w.CheckSilent([]*PRD{dormant}); got != nil {
		t.Errorf("no active work: expected no findings, got %+v", got)
	}

	// No activity observable at all (unknown) ⇒ no finding either:
	// the check must not report an absence it cannot see.
	wUnknown := NewSupervisorWatchWorktree(&fakeSupervisorWatchWorktreeFS{}, fs, sessions,
		cfg, supervisorWatchWorktreeBase)
	unknownPRD := supervisorWatchWorktreePRD("unknown", "/never/seen",
		supervisorWatchWorktreeActiveTask("1", "s-none"))
	if got := wUnknown.CheckSilent([]*PRD{unknownPRD}); got != nil {
		t.Errorf("no observable activity: expected no findings, got %+v", got)
	}
}

// TestSupervisorWatchWorktreeHealthy — the healthy case: a running,
// Go-project Automaton whose worktree holds a go.mod, sits in the
// registered worktree list, and shows activity within the silence
// window raises none of the three kinds.
func TestSupervisorWatchWorktreeHealthy(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	cfg := SupervisorWatchConfig{Mode: SupervisorWatchModeShadow, SilentMinutes: supervisorWatchTestSilentMinutes()}
	fsys := &fakeSupervisorWatchWorktreeFS{
		gomod: map[string]bool{
			supervisorWatchWorktreeBase: true,
			supervisorWatchWorktreeDir:  true,
		},
		worktrees: map[string][]string{
			supervisorWatchWorktreeBase: {
				supervisorWatchWorktreeBase,
				supervisorWatchWorktreeDir,
			},
		},
		newest: map[string]time.Time{
			supervisorWatchWorktreeDir: now.Add(-2 * time.Minute),
		},
	}
	w := NewSupervisorWatchWorktree(fsys, fixedSupervisorWatchClock{now: now},
		nil, cfg, supervisorWatchWorktreeBase)
	healthy := []*PRD{
		supervisorWatchWorktreePRD("healthy", supervisorWatchWorktreeDir,
			supervisorWatchWorktreeActiveTask("1", "s-healthy")),
	}
	if got := w.CheckNoGomod(healthy); got != nil {
		t.Errorf("healthy worktree: worktree_no_gomod = %+v", got)
	}
	if got := w.CheckNotRegistered(healthy); got != nil {
		t.Errorf("healthy worktree: worktree_not_registered = %+v", got)
	}
	if got := w.CheckSilent(healthy); got != nil {
		t.Errorf("healthy worktree: worktree_silent = %+v", got)
	}
}

// TestSupervisorWatchWorktreeNotRunningIgnored — every check, on a
// worktree that would trip all of them, raises nothing for an
// Automaton that is not running: the kinds are about a work item's
// worktree, and Automata outside its state are simply not checked.
func TestSupervisorWatchWorktreeNotRunningIgnored(t *testing.T) {
	for _, status := range []PRDStatus{PRDDraft, PRDApproved, PRDCompleted, PRDRejected, PRDPaused} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			p := supervisorWatchWorktreePRD("x", "/stray/dir",
				supervisorWatchWorktreeActiveTask("1", "s-x"))
			p.Status = status
			fsys := &fakeSupervisorWatchWorktreeFS{
				gomod:     map[string]bool{supervisorWatchWorktreeBase: true},
				worktrees: map[string][]string{supervisorWatchWorktreeBase: {"/base/other"}},
				newest:    map[string]time.Time{"/stray/dir": time.Now().Add(-24 * time.Hour)},
			}
			w := NewSupervisorWatchWorktree(fsys, fixedSupervisorWatchClock{now: time.Unix(1_700_000_600, 0)},
				nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow, SilentMinutes: supervisorWatchTestSilentMinutes()},
				supervisorWatchWorktreeBase)
			if got := w.CheckNoGomod([]*PRD{p}); got != nil {
				t.Errorf("status %s: worktree_no_gomod = %+v", status, got)
			}
			if got := w.CheckNotRegistered([]*PRD{p}); got != nil {
				t.Errorf("status %s: worktree_not_registered = %+v", status, got)
			}
			if got := w.CheckSilent([]*PRD{p}); got != nil {
				t.Errorf("status %s: worktree_silent = %+v", status, got)
			}
		})
	}
}

// TestSupervisorWatchWorktreeNonGoProject — a non-Go project (no
// go.mod at the registered base) never raises worktree_no_gomod,
// regardless of what its worktree holds.
func TestSupervisorWatchWorktreeNonGoProject(t *testing.T) {
	p := supervisorWatchWorktreePRD("nongo", supervisorWatchWorktreeDir,
		supervisorWatchWorktreeActiveTask("1", "s-nongo"))

	// Base holds no go.mod: the project is not a Go project.
	fs := &fakeSupervisorWatchWorktreeFS{
		gomod: map[string]bool{}, // base and worktree both absent
	}
	w := NewSupervisorWatchWorktree(fs, fixedSupervisorWatchClock{now: time.Unix(1_700_000_000, 0)},
		nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow},
		supervisorWatchWorktreeBase)
	if got := w.CheckNoGomod([]*PRD{p}); got != nil {
		t.Fatalf("non-Go project: expected no worktree_no_gomod findings, got %+v", got)
	}

	// Even with an explicit "false" on both, the gate still excludes.
	fsNonGo := &fakeSupervisorWatchWorktreeFS{
		gomod: map[string]bool{
			supervisorWatchWorktreeBase: false,
			supervisorWatchWorktreeDir:  false,
		},
	}
	wNonGo := NewSupervisorWatchWorktree(fsNonGo, fixedSupervisorWatchClock{now: time.Unix(1_700_000_000, 0)},
		nil, SupervisorWatchConfig{Mode: SupervisorWatchModeShadow},
		supervisorWatchWorktreeBase)
	if got := wNonGo.CheckNoGomod([]*PRD{p}); got != nil {
		t.Fatalf("non-Go project: expected no worktree_no_gomod findings, got %+v", got)
	}
}
