// load_step_rules_test.go — tests for the bounded CPU-load / stress-step
// rule: the built-in template that carries it, the decompose-prompt rules
// text, the warn-severity decompose-lint finding, and a fixture that a
// bounded load step (its own session, niced -n 19, a trap on EXIT TERM INT
// that kills its child, terminated by its recorded PID) leaves no orphaned
// load behind and never outlives its owning task.
//
// Fixture safety (per AGENT.md): the fixture is a harmless sleeping script
// in a temp dir; nothing is ever killed by name (only by the PID we
// recorded), and TMUX / TMUX_TMPDIR are unset so the fixture can never pull
// in the operator's tmux server. No real LLM is started, no operator config
// is read.
package autonomous

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// --- the rule text ---------------------------------------------------------

// TestLoadStepRuleTextInTemplate pins the bounded load-step rule in the
// built-in bounded_load_step template: the template is present, renders
// with the feature (no slot survives), and carries every bound the rule
// demands — niced, a quarter of the cores, a hard timeout, a trap-cleanup,
// and never outlive the owning task — with Automaton wording.
func TestLoadStepRuleTextInTemplate(t *testing.T) {
	defs := BuiltInTaskTemplates()
	var got *config.TaskTemplateDef
	for i := range defs {
		if defs[i].ID == TemplateBoundedLoadStep {
			got = &defs[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("built-in templates: bounded_load_step def not found (ids: %v)", taskTemplateIDs(defs))
	}

	for _, want := range []string{
		"nice -n 19",            // run it niced
		"quarter of the cores",  // at most a quarter of the cores
		"hard timeout",          // its own hard timeout
		"trap on EXIT INT TERM", // clean up children with a trap
		"never outlive its owning task",
		"Automaton", // user-visible wording
	} {
		if !strings.Contains(got.Spec, want) {
			t.Errorf("bounded_load_step spec missing %q:\n%s", want, got.Spec)
		}
	}

	// Renders with the feature and leaves no slot behind.
	slots := config.TaskTemplateSlots{
		Feature:   "bounded-load-step-rule",
		Files:     "docs/howto/task-templates.md",
		FilesList: "- docs/howto/task-templates.md",
	}
	out := RenderTaskTemplateDef(*got, slots)
	if out.ID != TemplateBoundedLoadStep {
		t.Errorf("render id = %q, want %q", out.ID, TemplateBoundedLoadStep)
	}
	if strings.Contains("||"+out.Title+"|"+out.Spec+"|", "{{") {
		t.Errorf("rendered bounded_load_step still has slots: %q", out.Spec)
	}
}

// --- the decompose-prompt rules text --------------------------------------

// TestLoadStepSuffixGatesOnProjectDir checks that the decompose-prompt
// rules block is present for a PRD with a project (and carries the rule)
// and absent for a headless PRD (empty ProjectDir), mirroring
// verificationScriptSuffix and rulesContextSuffix.
func TestLoadStepSuffixGatesOnProjectDir(t *testing.T) {
	m := &Manager{}
	headless := &PRD{ID: "p", ProjectDir: ""}
	if got := m.loadStepSuffix(headless); got != "" {
		t.Fatalf("headless: loadStepSuffix = %q, want %q (gated on ProjectDir)", got, "")
	}

	withProj := &PRD{ID: "p", ProjectDir: "/tmp/proj"}
	suffix := m.loadStepSuffix(withProj)
	if !strings.Contains(suffix, "CPU-load and stress steps") {
		t.Fatalf("with project: suffix missing the heading: %q", suffix)
	}
	for _, want := range []string{"nice -n 19", "quarter of the cores", "hard timeout", "trap on EXIT INT TERM", "never outlive its owning task"} {
		if !strings.Contains(suffix, want) {
			t.Errorf("with project: suffix missing %q", want)
		}
	}
}

// --- the decompose-lint rule ----------------------------------------------

// TestLoadStepLintRuleWarnsOnRequest checks the unbounded_load_step rule:
// it is warn severity, fires when a task spec requests a CPU-load or stress
// step, and does not fire for a task spec with no load step.
func TestLoadStepLintRuleWarnsOnRequest(t *testing.T) {
	rules := EffectiveRules(config.DecomposeLintConfig{})
	if got := ruleSeverity(rules, "unbounded_load_step"); got != "warn" {
		t.Fatalf("unbounded_load_step severity = %q, want warn", got)
	}

	// positive: a spec that requests a load step.
	pos := LintTasks([]Story{mkDecomposeLintStory("s", mkDecomposeLintTask(
		"c1", "CPU load run",
		"Run a CPU load stress test that pins all of the cores for a soak window and report the throughput.",
		nil, false, "sh -c 'true'"))}, rules)
	finding, found := findDecomposeLintFinding(pos, "unbounded_load_step")
	if !found {
		t.Fatalf("unbounded request: no unbounded_load_step finding (got %v)", findingRuleIDs(pos))
	}
	if finding.Severity != "warn" {
		t.Errorf("unbounded request: severity = %q, want warn", finding.Severity)
	}
	m := strings.ToLower(finding.Matched)
	if !strings.Contains(m, "load") && !strings.Contains(m, "stress") && !strings.Contains(m, "cpu") {
		t.Errorf("unbounded request: matched %q should name the load step", finding.Matched)
	}

	// negative: no load step requested.
	neg := LintTasks([]Story{mkDecomposeLintStory("s", mkDecomposeLintTask(
		"c2", "Refactor the scheduler",
		"Reduce preemption overhead in internal/core/sched.go and add a regression test.",
		[]string{"internal/core/sched.go"}, false, "go test ./internal/core/..."))}, rules)
	if f, found := findDecomposeLintFinding(neg, "unbounded_load_step"); found {
		_ = f
		t.Errorf("no load step: unbounded_load_step fired, want none (all findings: %v)", findingRuleIDs(neg))
	}
}

// --- the functional fixture ------------------------------------------------

// TestLoadStepBoundedFixtureCleansUpChildren is the functional proof: a
// bounded load step — its own session (setsid, when available), niced -n
// 19, a trap on EXIT TERM INT that kills its child, and terminated by its
// recorded PID — leaves no orphaned load behind and does not outlive its
// owning task. The child is a harmless sleeping script in a temp dir;
// nothing is ever killed by name.
func TestLoadStepBoundedFixtureCleansUpChildren(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skipf("testbed lacks /proc (fixture needs Linux /proc): %v", err)
	}
	setEnvVars(t)

	workdir := t.TempDir() // the fake worktree the workload runs inside
	script := filepath.Join(workdir, "workload.sh")
	childPIDFile := filepath.Join(workdir, "child.pid")
	workloadPIDFile := filepath.Join(workdir, "workload.pid")
	body := `
if command -v nice >/dev/null 2>&1; then
	nice -n 19 sleep 300 &
else
	sleep 300 &
fi
CHILD=$!
printf '%s\n' "$CHILD" > "` + childPIDFile + `"
printf '%s\n' "$$" > "` + workloadPIDFile + `"
cleanup() { kill "$CHILD" 2>/dev/null; }
trap cleanup EXIT
trap 'cleanup; exit 143' TERM INT
wait "$CHILD"
`
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\n"+body), 0o755); err != nil {
		t.Fatalf("write workload script: %v", err)
	}

	// Launch: its own session (setsid, when present) so the workload is
	// isolated from anything in this shell. We do NOT trust the coprocess
	// PID (a session wrapper may fork); instead the script records its own
	// $$ and its child's PID into pidfiles, and we target those exact PIDs.
	var cmdArgs []string
	if _, err := exec.LookPath("setsid"); err == nil {
		cmdArgs = append(cmdArgs, "setsid")
	}
	if niceAvailable() {
		cmdArgs = append(cmdArgs, "nice", "-n", "19")
	}
	cmdArgs = append(cmdArgs, "bash", script)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = workdir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start workload fixture (%v): %v", cmdArgs, err)
	}

	// Bounded teardown, strictly by the PIDs the workload recorded (never by
	// name): kill the child, then the workload, then reap the coprocess.
	stop := func() {
		if c := readIntFile(childPIDFile); c > 0 && procAlive(c) {
			_ = syscall.Kill(c, syscall.SIGKILL)
		}
		if w := readIntFile(workloadPIDFile); w > 0 && procAlive(w) {
			_ = syscall.Kill(w, syscall.SIGHUP)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	t.Cleanup(stop)

	// (1) the workload recorded itself and its niced child spawned.
	workloadPID, okW := pollReadPIDFile(t, workloadPIDFile, 5*time.Second)
	if !okW {
		t.Fatalf("workload did not record its own PID in time (%q)", workloadPIDFile)
	}
	childPID, okC := pollReadPIDFile(t, childPIDFile, 5*time.Second)
	if !okC {
		t.Fatalf("workload did not spawn its child in time (%q)", childPIDFile)
	}

	// (2) niced: the workload was launched under nice -n 19, so it and its
	//     load-generator child both carry a positive nice value (yielding to
	//     real host work).
	if niceAvailable() {
		if nice := procNice(workloadPID); nice <= 0 {
			t.Errorf("workload not niced: %d nice = %d, want a positive niced value", workloadPID, nice)
		}
		if nice := procNice(childPID); nice <= 0 {
			t.Errorf("load generator not niced: child %d nice = %d, want a positive niced value (inherited from the niced workload)", childPID, nice)
		}
	}

	// (3) terminate the workload by its recorded PID (never by name); the
	//     trap on TERM must reap the child — no orphaned load survives.
	if err := syscall.Kill(workloadPID, syscall.SIGTERM); err != nil {
		t.Fatalf("signal workload by PID %d: %v", workloadPID, err)
	}
	if !pollProcGone(t, childPID, 5*time.Second) {
		stop()
		t.Fatalf("orphaned load: child %d still alive after the workload was terminated", childPID)
	}
	// The workload itself must not outlive the owning task either.
	if !pollProcGone(t, workloadPID, 5*time.Second) {
		stop()
		t.Fatalf("workload %d still alive after TERM (should exit via its trap)", workloadPID)
	}
	_ = cmd.Wait()
}

// --- small helpers ---------------------------------------------------------

func taskTemplateIDs(defs []config.TaskTemplateDef) []string {
	ids := make([]string, 0, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
	}
	return ids
}

func ruleSeverity(rules []config.DecomposeLintRule, id string) string {
	for _, r := range rules {
		if r.ID == id {
			return r.Severity
		}
	}
	return ""
}

func niceAvailable() bool {
	if os.Getenv("SKIP_LOADSTEP_NICE") != "" {
		return false
	}
	_, err := exec.LookPath("nice")
	return err == nil
}

// setEnvVars unsets anything that could pull an operator tmux server into
// the fixture, restoring the original values on cleanup.
func setEnvVars(t *testing.T) {
	t.Helper()
	for _, v := range []string{"TMUX", "TMUX_TMPDIR"} {
		orig := os.Getenv(v)
		_ = os.Unsetenv(v)
		t.Cleanup(func() {
			if orig != "" {
				_ = os.Setenv(v, orig)
			}
		})
	}
}

func procNice(pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return -1
	}
	s := string(b)
	// comm (field 2) may in principle contain spaces; anchor on the last ')'
	// to drop pid+comm, then the remainder starts at field 3 (state).
	ri := strings.LastIndex(s, ")")
	if ri < 0 {
		return -1
	}
	fields := strings.Fields(s[ri+1:])
	// nice is overall field 19; field 3 is fields[0], so nice is fields[16].
	if len(fields) <= 16 {
		return -1
	}
	n, err := strconv.Atoi(fields[16])
	if err != nil {
		return -1
	}
	return n
}

func procAlive(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

// procRunning reports whether the pid is in a running/sleeping state — still
// doing work. A zombie (Z) or dead (X) process has exited and is not
// running, even though its /proc entry lingers until reaped. That is exactly
// the bound we are proving: the load is gone, not merely reparented.
func procRunning(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(b)
	j := strings.LastIndexByte(s, ')')
	if j < 0 || j+2 > len(s) || s[j+1] != ' ' {
		return true // cannot parse state — be conservative, treat as running
	}
	state := s[j+2 : j+3] // the byte after ") " is the process state
	switch state {
	case "Z", "X", "x": // zombie / dead → not running
		return false
	default:
		return true
	}
}

func readIntFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return n
}

func pollReadPIDFile(t *testing.T, path string, deadline time.Duration) (int, bool) {
	t.Helper()
	deadlineAt := time.Now().Add(deadline)
	for time.Now().Before(deadlineAt) {
		if n := readIntFile(path); n > 0 {
			return n, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, false
}

func pollProcGone(t *testing.T, pid int, deadline time.Duration) bool {
	t.Helper()
	deadlineAt := time.Now().Add(deadline)
	for time.Now().Before(deadlineAt) {
		// A zombie/dead state counts as gone (the load is off, even if the
		// /proc entry lingers until the parent reaps it).
		if !procRunning(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !procRunning(pid)
}
