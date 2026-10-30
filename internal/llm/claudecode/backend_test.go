package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/testhome"
)

func TestDeriveSessionUUID_Deterministic(t *testing.T) {
	// Same input must always produce the same UUID.
	name := "ralfthewise-f7ec"
	a := deriveSessionUUID(name)
	b := deriveSessionUUID(name)
	if a != b {
		t.Fatalf("deriveSessionUUID not deterministic: %q != %q", a, b)
	}
	if !isUUID(a) {
		t.Fatalf("deriveSessionUUID did not produce a valid UUID: %q", a)
	}
}

func TestDeriveSessionUUID_DifferentInputs(t *testing.T) {
	a := deriveSessionUUID("ralfthewise-f7ec")
	b := deriveSessionUUID("ralfthewise-a1b2")
	if a == b {
		t.Fatalf("different inputs produced the same UUID: %q", a)
	}
}

// TestLaunchAndResume_SameSessionID verifies that the --session-id set during
// Launch matches the --resume ID used during LaunchResume for the same session.
// This is the core invariant that makes resume work: the conversation ID Claude
// stores at launch time must equal the ID we ask it to resume by.
func TestLaunchAndResume_SameSessionID(t *testing.T) {
	// Simulate the datawatch identifiers for a session:
	//   fullID     = "ralfthewise-f7ec"
	//   tmuxSession = "cs-ralfthewise-f7ec"
	fullID := "ralfthewise-f7ec"
	tmuxSession := "cs-" + fullID

	// Launch derives UUID from tmuxSession minus "cs-" prefix.
	launchFullID := strings.TrimPrefix(tmuxSession, "cs-")
	launchUUID := deriveSessionUUID(launchFullID)

	// LaunchResume receives fullID as resumeID (from the UI dropdown).
	// Since fullID is not a UUID, it derives the same deterministic UUID.
	resumeID := fullID
	if isUUID(resumeID) {
		t.Fatalf("fullID %q should not be detected as UUID", resumeID)
	}
	resumeUUID := deriveSessionUUID(resumeID)

	if launchUUID != resumeUUID {
		t.Fatalf("session ID mismatch: Launch would set --session-id %q but Resume would use --resume %q",
			launchUUID, resumeUUID)
	}
	t.Logf("session ID match: %s", launchUUID)
}

func TestIsUUID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"a1085917-b6b3-5fa9-b8a9-513c7ed4c8c3", true},
		{"ralfthewise-f7ec", false},
		{"not-a-uuid", false},
		{"", false},
		{"00000000-0000-0000-0000-000000000000", true},
	}
	for _, tc := range tests {
		got := isUUID(tc.input)
		if got != tc.want {
			t.Errorf("isUUID(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// ---- Task delivery via scratch file + shared llm.SendKeys ----

var scratchFileRe = regexp.MustCompile(`\$\(cat '([^']+)'`)

// startFakeTmux puts a PATH stub named "tmux" (first on PATH) that records
// its argv NUL-separated into argvFile and, if fail is true, additionally
// writes "command too long" to stderr and exits 1. No real tmux server is
// ever touched — the stub is the only "tmux" the test resolves.
func startFakeTmux(t *testing.T, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "#!/bin/sh\n"
	if fail {
		script += "echo 'command too long' >&2\n"
	}
	script += "printf '%s\\0' \"$@\" > '" + argvFile + "'\n"
	if fail {
		script += "exit 1\n"
	}
	fakeTmux := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

// recordedCmd returns the send-keys payload (second-to-last argv) recorded by
// the fake tmux: tmux send-keys -t <session> <cmd> Enter
func recordedCmd(t *testing.T, argvFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if len(args) < 2 {
		t.Fatalf("recorded tmux argv too short: %q", raw)
	}
	return args[len(args)-2]
}

func backendOf() *Backend {
	return New("claude").(*Backend)
}

// TestResumeKeepsTask_Interface pins the optional-interface contract from
// internal/llm/tmuxsend.go: a resumed claude-code conversation keeps the
// original task in its context.
func TestResumeKeepsTask_Interface(t *testing.T) {
	var b llm.Backend = backendOf()
	k, ok := b.(llm.ResumeKeepsTask)
	if !ok {
		t.Fatal("claudecode.Backend must satisfy llm.ResumeKeepsTask")
	}
	if !k.ResumeKeepsTask() {
		t.Error("ResumeKeepsTask() = false, want true")
	}
}

// TestLaunchResume_BigTaskSucceedsViaScratchFile: a 40 KB task (the class
// of task that made live `tmux send-keys` fail with "command too long")
// must succeed, and the recorded command must be short and carry only the
// cat-file expression — with the scratch file on disk holding the full text.
func TestLaunchResume_BigTaskSucceedsViaScratchFile(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	argvFile := startFakeTmux(t, false)
	task := strings.Repeat("a", 40*1024)

	if err := backendOf().LaunchResume(context.Background(), task, "cs-bigtasks", t.TempDir(), "/nonexistent/log", "bigsess-1234"); err != nil {
		t.Fatalf("LaunchResume with 40 KB task: %v", err)
	}

	cmd := recordedCmd(t, argvFile)
	if len(cmd) >= 15000 {
		t.Errorf("recorded command is %d bytes, want < 15000", len(cmd))
	}
	if strings.Contains(cmd, task) {
		t.Error("recorded command must not embed the raw task text")
	}
	m := scratchFileRe.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("recorded command missing the cat-file expression: %q", cmd)
	}
	fileContent, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatalf("scratch file %q does not exist on disk: %v", m[1], err)
	}
	if string(fileContent) != task {
		t.Fatalf("scratch file = %d bytes, want the full 40 KB task (%d bytes)", len(fileContent), len(task))
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: claude done'") {
		t.Errorf("completion marker echo missing at the tail, got: %s", cmd)
	}
}

// TestLaunchResume_EmptyTaskCarriesNoTaskText: a no-override restart
// (empty task — safe because ResumeKeepsTask) must produce a command with
// no task delivery at all: no cat-file expression, no rm -f cleanup.
func TestLaunchResume_EmptyTaskCarriesNoTaskText(t *testing.T) {
	argvFile := startFakeTmux(t, false)
	marker := "SHOULD-NEVER-APPEAR-IN-COMMAND"

	if err := backendOf().LaunchResume(context.Background(), "", "cs-emptytask", t.TempDir(), "/nonexistent/log", "emptysess-abcd"); err != nil {
		t.Fatalf("LaunchResume with empty task: %v", err)
	}

	cmd := recordedCmd(t, argvFile)
	if strings.Contains(cmd, "$(cat") {
		t.Errorf("empty task must not reference a task file, got: %s", cmd)
	}
	if strings.Contains(cmd, "rm -f") {
		t.Errorf("empty task must not add a scratch-file cleanup, got: %s", cmd)
	}
	if strings.Contains(cmd, marker) {
		t.Errorf("recorded command carries unexpected text, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--resume ") || !strings.Contains(cmd, "||") || !strings.Contains(cmd, "--session-id ") {
		t.Errorf("empty-task resume command must keep the --resume || --session-id fallback shape, got: %s", cmd)
	}
}

// TestLaunchResume_NineKBTaskIncident: the incident case — a ~9 KB task (just
// under the inline limit) must still succeed with the typed command well
// below the limit, and both the --resume and the --session-id halves must
// reference the SAME scratch file.
func TestLaunchResume_NineKBTaskIncident(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	argvFile := startFakeTmux(t, false)
	task := strings.Repeat("x", 9000)

	if err := backendOf().LaunchResume(context.Background(), task, "cs-incident", t.TempDir(), "/nonexistent/log", "incidentsess-99"); err != nil {
		t.Fatalf("LaunchResume with ~9 KB task: %v", err)
	}

	cmd := recordedCmd(t, argvFile)
	if len(cmd) > llm.MaxSendKeysCommand/2 {
		t.Errorf("recorded command is %d bytes, want well under the %d-byte limit", len(cmd), llm.MaxSendKeysCommand)
	}
	path := cmdPath(t, cmd)

	orIdx := strings.Index(cmd, " || ")
	if orIdx < 0 {
		t.Fatalf("resume/fallback halves not joined by ||, got: %s", cmd)
	}
	resumeHalf, fallbackHalf := cmd[:orIdx], cmd[orIdx:]
	for name, half := range map[string]string{"resume": resumeHalf, "fallback": fallbackHalf} {
		want := "$(cat '" + path + "')"
		if !strings.Contains(half, want) {
			t.Errorf("%s half must reference the same scratch file %q (want %q), got: %s", name, path, want, half)
		}
	}
	if got := strings.Count(cmd, "$(cat '"+path+"')"); got != 2 {
		t.Errorf("scratch file reference count = %d, want exactly 2 (resume + fallback), in: %s", got, cmd)
	}
	if c, _ := os.ReadFile(path); string(c) != task {
		t.Errorf("scratch file content mismatch (got %d bytes, want %d)", len(c), len(task))
	}
}

// TestLaunchResume_TmuxFailureSurfacesStderr: a tmux that fails with
// "command too long" on stderr must surface that exact text in the returned
// error (via llm.SendKeys' stderr capture), not a bare "exit status 1".
func TestLaunchResume_TmuxFailureSurfacesStderr(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	startFakeTmux(t, true)
	err := backendOf().LaunchResume(context.Background(), "small task", "cs-failcase", t.TempDir(), "/nonexistent/log", "failsess-0001")
	if err == nil {
		t.Fatal("LaunchResume with failing tmux must return an error")
	}
	if !strings.Contains(err.Error(), "command too long") {
		t.Errorf("error must contain tmux's stderr text %q, got: %v", "command too long", err)
	}
}

// TestLaunch_ShortTaskDeliveredViaFile: a short (< 4 KB) task still arrives
// at claude with its exact text — read back from the scratch file — and the
// DATAWATCH_COMPLETE echo is preserved at the tail.
func TestLaunch_ShortTaskDeliveredViaFile(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	argvFile := startFakeTmux(t, false)
	task := "it's a short task\nwith quotes and a newline"
	if len(task) >= 4096 {
		t.Fatalf("test task must be under 4 KB, got %d", len(task))
	}

	if err := backendOf().Launch(context.Background(), task, "cs-short", t.TempDir(), "/nonexistent/log"); err != nil {
		t.Fatalf("Launch with short task: %v", err)
	}

	cmd := recordedCmd(t, argvFile)
	path := cmdPath(t, cmd)
	fileContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("scratch file %q not on disk: %v", path, err)
	}
	if string(fileContent) != task {
		t.Errorf("scratch file content = %q, want the full task text %q", string(fileContent), task)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: claude done'") {
		t.Errorf("completion marker echo missing at the tail, got: %s", cmd)
	}
}

func cmdPath(t *testing.T, cmd string) string {
	t.Helper()
	m := scratchFileRe.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("recorded command missing the cat-file expression: %q", cmd)
	}
	return m[1]
}
