package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// fakeSupervisorWatchHostProbe is the injected probe for the
// SupervisorWatchHost tests: a full fake, so no test in this file ever
// runs a real command or touches the real host. Every method's error is
// independently scripted; methods that never fail return their value.
type fakeSupervisorWatchHostProbe struct {
	restartCount   int
	restartErr     error
	activeSince    string
	activeSinceErr error

	// daemonActiveByNode maps node -> active; a node in daemonErrByNode
	// errors regardless. A node in neither returns (false, nil) for a
	// daemon that reads inactive.
	daemonActiveByNode map[string]bool
	daemonErrByNode    map[string]error

	// residentModelsByNode maps node -> loaded models; a node in
	// modelsErrByNode errors regardless. A node in neither returns
	// (nil, nil): an empty resident list.
	residentModelsByNode map[string][]string
	modelsErrByNode      map[string]error

	gitStatus map[string][]string
	gitErr    map[string]error

	fileModes map[string]os.FileMode
	fileErr   map[string]error
}

func (f *fakeSupervisorWatchHostProbe) ServiceRestartCount() (int, error) {
	return f.restartCount, f.restartErr
}

func (f *fakeSupervisorWatchHostProbe) ActiveSince() (string, error) {
	return f.activeSince, f.activeSinceErr
}

func (f *fakeSupervisorWatchHostProbe) DaemonActive(node string) (bool, error) {
	if err, ok := f.daemonErrByNode[node]; ok {
		return false, err
	}
	return f.daemonActiveByNode[node], nil
}

func (f *fakeSupervisorWatchHostProbe) ResidentModels(node string) ([]string, error) {
	if err, ok := f.modelsErrByNode[node]; ok {
		return nil, err
	}
	return f.residentModelsByNode[node], nil
}

func (f *fakeSupervisorWatchHostProbe) GitStatus(dir string) ([]string, error) {
	if err, ok := f.gitErr[dir]; ok {
		return nil, err
	}
	return f.gitStatus[dir], nil
}

func (f *fakeSupervisorWatchHostProbe) FileMode(path string) (os.FileMode, error) {
	if err, ok := f.fileErr[path]; ok {
		return 0, err
	}
	mode, ok := f.fileModes[path]
	if !ok {
		return 0, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
	}
	return mode, nil
}

// fakeSupervisorWatchNodes is the injected compute node registry view.
type fakeSupervisorWatchNodes struct{ names []string }

func (f fakeSupervisorWatchNodes) List() []*compute.Node {
	out := make([]*compute.Node, 0, len(f.names))
	for _, n := range f.names {
		out = append(out, &compute.Node{Name: n})
	}
	return out
}

var errSupervisorWatchProbe = errors.New("probe: unknown (tool missing)")

func TestSupervisorWatchHostTmuxRestartHealthy(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		restartCount: 3,
		activeSince:  "Tue 2026-10-20 18:46:38 EDT",
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{
		TmuxRestartBaseline: "3" + supervisorWatchBaselineSep + "Tue 2026-10-20 18:46:38 EDT",
	}, fakeSupervisorWatchNodes{})
	if got := h.CheckTmuxRestart(); len(got) != 0 {
		t.Fatalf("no restart since the stored baseline: want no findings, got %v", got)
	}
}

func TestSupervisorWatchHostTmuxRestartMismatch(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		restartCount: 5, // the stored baseline says 3
		activeSince:  "Thu 2026-10-22 09:12:04 EDT",
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{
		TmuxRestartBaseline: "3" + supervisorWatchBaselineSep + "Tue 2026-10-20 18:46:38 EDT",
	}, fakeSupervisorWatchNodes{})

	got := h.CheckTmuxRestart()
	if len(got) != 1 || got[0].Kind != SupervisorWatchTmuxRestart || got[0].Subject != SupervisorWatchTmuxUnitName {
		t.Fatalf("restart count or active-since differs from the baseline: want exactly one tmux_restart finding on the tmux unit, got %+v", got)
	}
	if got[0].Text == "" {
		t.Fatalf("finding must carry an operator-facing one line")
	}
	// The new state is stored, so a repeat of the same state raises
	// nothing on the next poll.
	if got := h.CheckTmuxRestart(); len(got) != 0 {
		t.Fatalf("baseline is refreshed after the finding: a repeat of the same state must not re-raise, got %v", got)
	}
}

func TestSupervisorWatchHostTmuxRestartFirstSight(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		restartCount: 7,
		activeSince:  "Fri 2026-11-06 06:00:00 EDT",
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})

	// First sight: no baseline stored yet: store it, raise nothing.
	if got := h.CheckTmuxRestart(); len(got) != 0 {
		t.Fatalf("first sight stores the baseline and raises nothing: got %v", got)
	}
	if got, want := h.StoredTmuxBaseline(), "7"+supervisorWatchBaselineSep+"Fri 2026-11-06 06:00:00 EDT"; got != want {
		t.Fatalf("stored baseline = %q, want %q", got, want)
	}
	// A later change of either field raises once.
	probe.restartCount = 8
	if got := h.CheckTmuxRestart(); len(got) != 1 || got[0].Kind != SupervisorWatchTmuxRestart {
		t.Fatalf("a later restart difference must raise one tmux_restart finding, got %v", got)
	}
}

func TestSupervisorWatchHostTmuxRestartProbeError(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		restartErr:     errSupervisorWatchProbe,
		activeSince:    "Tue 2026-10-20 18:46:38 EDT",
		activeSinceErr: errSupervisorWatchProbe,
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{
		TmuxRestartBaseline: "3" + supervisorWatchBaselineSep + "Tue 2026-10-20 18:46:38 EDT",
	}, fakeSupervisorWatchNodes{})

	// Probe error: unknown fact, no finding, baseline untouched.
	if got := h.CheckTmuxRestart(); len(got) != 0 {
		t.Fatalf("probe error degrades to unknown, no finding: got %v", got)
	}
	if got, want := h.StoredTmuxBaseline(), "3"+supervisorWatchBaselineSep+"Tue 2026-10-20 18:46:38 EDT"; got != want {
		t.Fatalf("probe error must not touch the stored baseline: now %q, want %q", got, want)
	}
	// A recovered probe that matches the baseline stays quiet.
	probe.restartErr, probe.activeSinceErr = nil, nil
	probe.restartCount = 3
	if got := h.CheckTmuxRestart(); len(got) != 0 {
		t.Fatalf("a healthy probe matching the stored baseline must raise nothing, got %v", got)
	}
}

func TestSupervisorWatchHostTmuxRestartNilProbe(t *testing.T) {
	h := NewSupervisorWatchHost(nil, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	if got := h.CheckTmuxRestart(); got != nil {
		t.Fatalf("a nil probe degrades to no findings, got %v", got)
	}
}

func TestSupervisorWatchHostDaemonInactive(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		daemonActiveByNode: map[string]bool{"local": true, "remote": false},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{names: []string{"remote", "local"}})

	got := h.CheckDaemonInactive()
	if len(got) != 1 || got[0].Kind != SupervisorWatchDaemonInactive || got[0].Subject != "remote" {
		t.Fatalf("one node inactive: want exactly one daemon_inactive finding on that node, got %+v", got)
	}
}

func TestSupervisorWatchHostDaemonInactiveHealthy(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		daemonActiveByNode: map[string]bool{"local": true, "remote": true},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{names: []string{"local", "remote"}})
	if got := h.CheckDaemonInactive(); len(got) != 0 {
		t.Fatalf("every node active: want no findings, got %v", got)
	}
}

func TestSupervisorWatchHostDaemonInactiveProbeError(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		daemonErrByNode: map[string]error{"remote": errSupervisorWatchProbe},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{names: []string{"remote"}})
	if got := h.CheckDaemonInactive(); len(got) != 0 {
		t.Fatalf("probe error degrades to unknown, no finding: got %v", got)
	}
}

func TestSupervisorWatchHostModelNotResident(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		residentModelsByNode: map[string][]string{
			"local":  {"llama3.1:8b"},
			"remote": {"qwen3.8:64k"}, // the working model, under its base tag with the context suffix
			"broken": {"gemma3:27b"},
		},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{WorkingModel: "qwen3.8"}, fakeSupervisorWatchNodes{names: []string{"local", "remote", "broken"}})

	got := h.CheckModelNotResident()
	if len(got) != 2 {
		t.Fatalf("two nodes lack the working model: want two model_not_resident findings, got %+v", got)
	}
	byNode := map[string]SupervisorWatchKind{}
	for _, f := range got {
		if f.Kind != SupervisorWatchModelNotResident {
			t.Fatalf("wrong kind on %s: %v", f.Subject, f.Kind)
		}
		byNode[f.Subject] = f.Kind
	}
	if _, ok := byNode["local"]; !ok {
		t.Fatalf("local lacks the working model and must be named: %v", got)
	}
	if _, ok := byNode["broken"]; !ok {
		t.Fatalf("broken lacks the working model and must be named: %v", got)
	}
	if _, ok := byNode["remote"]; ok {
		t.Fatalf("the context variant of the base name counts as resident: %v must not flag remote", got)
	}
}

func TestSupervisorWatchHostModelNotResidentHealthy(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		residentModelsByNode: map[string][]string{
			"local":  {"qwen3.8"},
			"remote": {"qwen3.8:64k"},
		},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{WorkingModel: "qwen3.8"}, fakeSupervisorWatchNodes{names: []string{"local", "remote"}})
	if got := h.CheckModelNotResident(); len(got) != 0 {
		t.Fatalf("every registered node carries the working model: want no findings, got %v", got)
	}
}

func TestSupervisorWatchHostModelNotResidentProbeError(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		modelsErrByNode: map[string]error{"remote": errSupervisorWatchProbe},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{WorkingModel: "qwen3.8"}, fakeSupervisorWatchNodes{names: []string{"remote"}})
	if got := h.CheckModelNotResident(); len(got) != 0 {
		t.Fatalf("probe error degrades to unknown, no finding: got %v", got)
	}
}

func TestSupervisorWatchHostModelNotResidentUnsetModel(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{} // no node has any model
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{names: []string{"local"}})
	if got := h.CheckModelNotResident(); len(got) != 0 {
		t.Fatalf("no working model configured: the check is disabled, got %v", got)
	}

	// No registered nodes either: nothing to check.
	h2 := NewSupervisorWatchHost(probe, SupervisorWatchConfig{WorkingModel: "qwen3.8"}, fakeSupervisorWatchNodes{})
	if got := h2.CheckModelNotResident(); len(got) != 0 {
		t.Fatalf("no registered nodes: want no findings, got %v", got)
	}
}

func TestSupervisorWatchHostVerifyScriptNotExecutable(t *testing.T) {
	worktree := "/tmp/automata/worktrees/a1b2c3d4"
	script := filepath.Join(worktree, "scripts", "check.sh")
	probe := &fakeSupervisorWatchHostProbe{
		fileModes: map[string]os.FileMode{script: 0o644}, // exists, no exec bit
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	units := []SupervisorWatchHostUnit{{
		AutomatonID: "a1b2c3d4",
		Worktree:    worktree,
		ScriptPaths: []string{"scripts/check.sh", "../outside.sh", "/abs/esc.sh", "missing.sh"},
	}}

	got := h.CheckVerifyScriptNotExecutable(units)
	if len(got) != 1 || got[0].Kind != SupervisorWatchVerifyScriptNotExecutable || got[0].Subject != "a1b2c3d4" {
		t.Fatalf("one script exists without the exec bit: want exactly one verify_script_not_executable finding on that Automaton, got %+v", got)
	}
	if got[0].Text == "" {
		t.Fatalf("finding must carry an operator-facing one line")
	}
}

func TestSupervisorWatchHostVerifyScriptNotExecutableHealthy(t *testing.T) {
	worktree := "/tmp/automata/worktrees/a1b2c3d4"
	good := filepath.Join(worktree, "scripts", "check.sh")
	probe := &fakeSupervisorWatchHostProbe{
		fileModes: map[string]os.FileMode{good: 0o755}, // exec bit set
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	units := []SupervisorWatchHostUnit{{
		AutomatonID: "a1b2c3d4",
		Worktree:    worktree,
		ScriptPaths: []string{"scripts/check.sh", "missing.sh"}, // a missing script is not this finding
	}}
	if got := h.CheckVerifyScriptNotExecutable(units); len(got) != 0 {
		t.Fatalf("every present script is executable: want no findings, got %v", got)
	}
}

func TestSupervisorWatchHostVerifyScriptProbeError(t *testing.T) {
	worktree := "/tmp/automata/worktrees/a1b2c3d4"
	script := filepath.Join(worktree, "scripts", "check.sh")
	probe := &fakeSupervisorWatchHostProbe{
		fileErr: map[string]error{script: errSupervisorWatchProbe},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	units := []SupervisorWatchHostUnit{{
		AutomatonID: "a1b2c3d4",
		Worktree:    worktree,
		ScriptPaths: []string{"scripts/check.sh"},
	}}
	if got := h.CheckVerifyScriptNotExecutable(units); len(got) != 0 {
		t.Fatalf("probe error degrades to unknown, no finding: got %v", got)
	}
}

func TestSupervisorWatchHostVerifyScriptStaysInsideWorktree(t *testing.T) {
	outside := "/tmp/automata/other/bad.sh"
	probe := &fakeSupervisorWatchHostProbe{
		fileModes: map[string]os.FileMode{outside: 0o644}, // bad, but it is not inside any registered worktree
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	units := []SupervisorWatchHostUnit{{
		AutomatonID: "a1b2c3d4",
		Worktree:    "/tmp/automata/worktrees/a1b2c3d4",
		ScriptPaths: []string{"../../other/bad.sh"},
	}}
	if got := h.CheckVerifyScriptNotExecutable(units); len(got) != 0 {
		t.Fatalf("paths that climb out of the registered worktree are never inspected: got %v", got)
	}
}

func TestSupervisorWatchHostVerifyScriptOneFindingPerAutomaton(t *testing.T) {
	wtA := "/tmp/automata/worktrees/aaaaaaaa"
	wtB := "/tmp/automata/worktrees/bbbbbbbb"
	probe := &fakeSupervisorWatchHostProbe{
		fileModes: map[string]os.FileMode{
			filepath.Join(wtA, "check.sh"): 0o644,
			filepath.Join(wtB, "check.sh"): 0o644,
		},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	units := []SupervisorWatchHostUnit{
		{AutomatonID: "bbbbbbbb", Worktree: wtB, ScriptPaths: []string{"check.sh", "also/bad.sh"}},
		{AutomatonID: "aaaaaaaa", Worktree: wtA, ScriptPaths: []string{"check.sh"}},
		{AutomatonID: "bbbbbbbb", Worktree: wtB, ScriptPaths: []string{"check.sh"}}, // duplicate unit
	}
	got := h.CheckVerifyScriptNotExecutable(units)
	if len(got) != 2 {
		t.Fatalf("one finding per Automaton with a bad script, in deterministic order: got %+v", got)
	}
	if got[0].Subject != "aaaaaaaa" || got[1].Subject != "bbbbbbbb" {
		t.Fatalf("findings must be ordered by Automaton id: %v", got)
	}
}

func TestSupervisorWatchHostMainCheckoutDirty(t *testing.T) {
	dir := "checkouts/main"
	probe := &fakeSupervisorWatchHostProbe{
		gitStatus: map[string][]string{dir: {" M internal/autonomous/manager.go", "?? notes.md", "A\tnew.go"}},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{MainCheckout: dir}, fakeSupervisorWatchNodes{})

	got := h.CheckMainCheckoutDirty()
	if len(got) != 1 || got[0].Kind != SupervisorWatchMainCheckoutDirty || got[0].Subject != dir {
		t.Fatalf("tracked files modified (untracked do not count): want exactly one main_checkout_dirty finding on the checkout, got %+v", got)
	}
	if got[0].Text == "" {
		t.Fatalf("finding must carry an operator-facing one line")
	}
}

func TestSupervisorWatchHostMainCheckoutHealthy(t *testing.T) {
	dir := "checkouts/main"
	probe := &fakeSupervisorWatchHostProbe{
		gitStatus: map[string][]string{dir: {"?? scratch.md", "?? logs/"}}, // untracked only: does not count
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{MainCheckout: dir}, fakeSupervisorWatchNodes{})
	if got := h.CheckMainCheckoutDirty(); len(got) != 0 {
		t.Fatalf("untracked files do not count: want no findings, got %v", got)
	}
}

func TestSupervisorWatchHostMainCheckoutProbeError(t *testing.T) {
	dir := "checkouts/main"
	probe := &fakeSupervisorWatchHostProbe{
		gitErr: map[string]error{dir: errSupervisorWatchProbe},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{MainCheckout: dir}, fakeSupervisorWatchNodes{})
	if got := h.CheckMainCheckoutDirty(); len(got) != 0 {
		t.Fatalf("probe error degrades to unknown, no finding: got %v", got)
	}
}

func TestSupervisorWatchHostMainCheckoutUnset(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		gitStatus: map[string][]string{"/elsewhere": {" M x.go"}},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	if got := h.CheckMainCheckoutDirty(); len(got) != 0 {
		t.Fatalf("no main checkout configured: the check is disabled, got %v", got)
	}
}

func TestSupervisorWatchHostRunningUnits(t *testing.T) {
	running := &PRD{ID: "a1b2c3d4", Status: PRDRunning, ProjectDir: "/wt/a1b2c3d4"}
	running.Story = []Story{{ID: "s1", Tasks: []Task{
		{ID: "t1", VerificationScript: "scripts/check.sh"},
		{ID: "t2"},
		{ID: "t3", VerificationScript: "  "},
	}}}
	notRunning := &PRD{ID: "e5f6a7b8", Status: PRDCompleted, ProjectDir: "/wt/e5f6a7b8"}
	notRunning.Story = []Story{{ID: "s2", Tasks: []Task{{ID: "t9", VerificationScript: "bad.sh"}}}}
	noWorktree := &PRD{ID: "c9d0e1f2", Status: PRDRunning} // registered without a worktree

	got := SupervisorWatchRunningUnits([]*PRD{notRunning, running, noWorktree})
	if len(got) != 1 {
		t.Fatalf("only the running Automaton with a worktree yields a unit, got %+v", got)
	}
	u := got[0]
	if u.AutomatonID != "a1b2c3d4" || u.Worktree != "/wt/a1b2c3d4" {
		t.Fatalf("unit identity: %+v", u)
	}
	if len(u.ScriptPaths) != 1 || u.ScriptPaths[0] != "scripts/check.sh" {
		t.Fatalf("blank scripts dropped, kept scripts in task order: %+v", u.ScriptPaths)
	}
}

// TestSupervisorWatchHostVerifyScriptAbsolutePathSkipped — a script path
// that is absolute is never resolved against the worktree, even when a
// file at that path is not executable.
func TestSupervisorWatchHostVerifyScriptAbsolutePathSkipped(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{
		fileModes: map[string]os.FileMode{"/abs/esc.sh": 0o644},
	}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{}, fakeSupervisorWatchNodes{})
	units := []SupervisorWatchHostUnit{{
		AutomatonID: "a1b2c3d4",
		Worktree:    "/tmp/automata/worktrees/a1b2c3d4",
		ScriptPaths: []string{"/abs/esc.sh"},
	}}
	if got := h.CheckVerifyScriptNotExecutable(units); len(got) != 0 {
		t.Fatalf("an absolute path is never inspected: got %v", got)
	}
}

// TestSupervisorWatchRunningUnitsOrder — two running Automata come back
// in id order, whatever order the store listed them in.
func TestSupervisorWatchRunningUnitsOrder(t *testing.T) {
	mk := func(id string) *PRD {
		return &PRD{ID: id, Status: PRDRunning, ProjectDir: "/tmp/automata/worktrees/" + id}
	}
	units := SupervisorWatchRunningUnits([]*PRD{mk("b"), mk("a")})
	if len(units) != 2 || units[0].AutomatonID != "a" || units[1].AutomatonID != "b" {
		t.Fatalf("units must be in id order, got %+v", units)
	}
}
