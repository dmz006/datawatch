package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/testhome"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

func TestDeriveSessionUUID_Deterministic(t *testing.T) {
	// Same input must always produce the same UUID.
	name := "peer-d-f7ec"
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
	a := deriveSessionUUID("peer-d-f7ec")
	b := deriveSessionUUID("peer-d-a1b2")
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
	//   fullID     = "peer-d-f7ec"
	//   tmuxSession = "cs-peer-d-f7ec"
	fullID := "peer-d-f7ec"
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
		{"peer-d-f7ec", false},
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

// ---- Worker session profile launch ----

// backendWithProfile builds a claude-code backend with the named builtin
// worker profile applied. The datawatch MCP server is offered so profiles
// that select it (MCPDatawatch) can name it in their per-session config.
func backendWithProfile(t *testing.T, name string, starting autonomous.StartingContextInput) *Backend {
	t.Helper()
	p, ok := workerprofile.Builtin(name)
	if !ok {
		t.Fatalf("builtin worker profile %q not available", name)
	}
	b := backendOf()
	b.SetWorkerProfile(WorkerProfileLaunch{
		Profile: p,
		MCPServers: map[string]channel.MCPServerSpec{
			channel.ProjectMCPEntryName: {Command: "datawatch", Args: []string{"mcp"}},
		},
		Starting: starting,
	})
	return b
}

// TestLaunch_WorkerProfileMinimal: the minimal profile (no MCP servers,
// tools allow-list, skills off, minimal context) must map onto the launch
// command line as:
//   - the strict per-session MCP config (pinning, no servers at all);
//   - the allow-list as --allowedTools — never any disallowed-tools flag;
//   - "skills": false inside the --settings JSON;
//   - the starting context written to the working directory and passed via
//     --append-system-prompt.
func TestLaunch_WorkerProfileMinimal(t *testing.T) {
	d := t.TempDir()
	argvFile := startFakeTmux(t, false)

	starting := autonomous.StartingContextInput{
		Role:        "worker",
		TaskSpec:    "implement the task",
		Constraints: []string{"work within the task scope"},
	}
	if err := backendWithProfile(t, "minimal", starting).Launch(context.Background(), "", "cs-minprofile", d, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch with minimal worker profile: %v", err)
	}
	cmd := recordedCmd(t, argvFile)

	// MCP: strict per-session config with zero servers.
	mcpPath := filepath.Join(d, channel.WorkerMCPConfigFileName)
	if want := "--strict-mcp-config --mcp-config '" + mcpPath + "'"; !strings.Contains(cmd, want) {
		t.Errorf("command must pin the session MCP config strictly (want %q), got:\n%s", want, cmd)
	}
	mcpRaw, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("per-session MCP config missing in %s: %v", d, err)
	}
	var mcpCfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpRaw, &mcpCfg); err != nil {
		t.Fatalf("per-session MCP config is not valid JSON: %v\n%s", err, mcpRaw)
	}
	if len(mcpCfg.MCPServers) != 0 {
		t.Errorf("minimal profile keeps no MCP servers, config has %d entries: %s", len(mcpCfg.MCPServers), mcpRaw)
	}

	// Tools: the allow-list maps to --allowedTools; no deny-list anywhere.
	if want := "--allowedTools 'Read,Edit,Write,Bash,Glob,Grep'"; !strings.Contains(cmd, want) {
		t.Errorf("command must carry the allow-list (want %q), got:\n%s", want, cmd)
	}
	if strings.Contains(cmd, "disallowedTools") || strings.Contains(cmd, "--disallowed") {
		t.Errorf("a worker profile must never pass a disallowed-tools list, got:\n%s", cmd)
	}

	// Skills: off in the profile → "skills": false in the --settings JSON.
	if want := `--settings '{"skills":false}'`; !strings.Contains(cmd, want) {
		t.Errorf("command must turn skills off (want %s), got:\n%s", want, cmd)
	}

	// Starting context: file in the working directory, appended to the
	// system prompt by reading the file at launch time.
	ctxPath := filepath.Join(d, WorkerStartingContextFileName)
	if want := `--append-system-prompt "$(cat '` + ctxPath + `')"`; !strings.Contains(cmd, want) {
		t.Errorf("command must append the starting context file (want %s), got:\n%s", want, cmd)
	}
	ctxRaw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("starting context file missing in %s: %v", d, err)
	}
	for _, want := range []string{
		"Role\nworker\n",
		`profile="minimal" context=minimal`,
		`skills=false`,
		"Constraints\n- work within the task scope\n",
		"Task\nimplement the task\n",
	} {
		if !strings.Contains(string(ctxRaw), want) {
			t.Errorf("starting context missing %q, got:\n%s", want, ctxRaw)
		}
	}
	if strings.Contains(string(ctxRaw), "Rules digest\n") || strings.Contains(string(ctxRaw), "Rules\n") {
		t.Errorf("minimal context level must not carry rules sections, got:\n%s", ctxRaw)
	}

	// The typed line stays short (context delivered via cat-file), and the
	// completion marker is untouched.
	if len(cmd) >= llm.MaxSendKeysCommand {
		t.Errorf("profile launch command is %d bytes, must stay under the %d-byte send limit", len(cmd), llm.MaxSendKeysCommand)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: claude done'") {
		t.Errorf("completion marker echo missing at the tail, got: %s", cmd)
	}
}

// TestLaunch_WorkerProfileFull: the full profile (the datawatch MCP server,
// all tools, skills on, full context with rules digest + full rules text)
// must pin the session MCP config to exactly the datawatch server, add no
// allowed/disallowed tools flags, leave skills enabled, and carry the full
// starting context — digest and rules included.
func TestLaunch_WorkerProfileFull(t *testing.T) {
	d := t.TempDir()
	argvFile := startFakeTmux(t, false)

	starting := autonomous.StartingContextInput{
		Role:        "worker",
		TaskSpec:    "land the PRD change",
		Constraints: []string{"stay in scope", "run the verification"},
		RulesDigest: "only touch this task",
		RulesFull:   "full rules text from the rules file",
	}
	if err := backendWithProfile(t, "full", starting).Launch(context.Background(), "", "cs-fullprofile", d, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch with full worker profile: %v", err)
	}
	cmd := recordedCmd(t, argvFile)

	// MCP: the single allowed server, pinned strictly.
	mcpPath := filepath.Join(d, channel.WorkerMCPConfigFileName)
	if want := "--strict-mcp-config --mcp-config '" + mcpPath + "'"; !strings.Contains(cmd, want) {
		t.Errorf("command must pin the session MCP config strictly (want %q), got:\n%s", want, cmd)
	}
	mcpRaw, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("per-session MCP config missing in %s: %v", d, err)
	}
	var mcpCfg struct {
		MCPServers map[string]channel.MCPServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpRaw, &mcpCfg); err != nil {
		t.Fatalf("per-session MCP config is not valid JSON: %v\n%s", err, mcpRaw)
	}
	if len(mcpCfg.MCPServers) != 1 {
		t.Fatalf("full profile names exactly 1 server, config has %d entries: %s", len(mcpCfg.MCPServers), mcpRaw)
	}
	spec, ok := mcpCfg.MCPServers[channel.ProjectMCPEntryName]
	if !ok {
		t.Errorf("per-session MCP config must name the %q server: %s", channel.ProjectMCPEntryName, mcpRaw)
	}
	if spec.Command != "datawatch" {
		t.Errorf("datawatch server command = %q, want %q", spec.Command, "datawatch")
	}

	// Tools: the full profile allows everything, so no allowed/disallowed
	// tools flags at all.
	if strings.Contains(cmd, "--allowedTools") || strings.Contains(cmd, "--disallowedTools") || strings.Contains(cmd, "--disallowed") {
		t.Errorf("full profile allows all tools: no tools flags expected, got:\n%s", cmd)
	}

	// Skills: on in the profile → no --settings at all (nothing to hide and
	// skills untouched).
	if strings.Contains(cmd, "--settings") {
		t.Errorf("skills stay on with the full profile: --settings not expected, got:\n%s", cmd)
	}

	// Starting context: full level carries the rules digest and the full
	// rules text, with the profile line reflecting the profile itself.
	ctxPath := filepath.Join(d, WorkerStartingContextFileName)
	if !strings.Contains(cmd, `--append-system-prompt "$(cat '`+ctxPath+`')"`) {
		t.Fatalf("command must append the starting context file, got:\n%s", cmd)
	}
	ctxRaw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("starting context file missing in %s: %v", d, err)
	}
	for _, want := range []string{
		`profile="full" context=full`,
		`mcp=datawatch servers="" tools=all`,
		"skills=true",
		"Rules digest\nonly touch this task\n",
		"Rules\nfull rules text from the rules file\n",
		"Task\nland the PRD change\n",
	} {
		if !strings.Contains(string(ctxRaw), want) {
			t.Errorf("starting context missing %q, got:\n%s", want, ctxRaw)
		}
	}
	if len(cmd) >= llm.MaxSendKeysCommand {
		t.Errorf("profile launch command is %d bytes, must stay under the %d-byte send limit", len(cmd), llm.MaxSendKeysCommand)
	}
}

// TestLaunch_NoProfileUnchanged: with no worker profile applied the launch
// command line and the working directory must show no trace of the profile
// machinery — the command is the non-profile command, byte for byte.
func TestLaunch_NoProfileUnchanged(t *testing.T) {
	d := t.TempDir()
	argvFile := startFakeTmux(t, false)

	b := backendOf()
	if err := b.Launch(context.Background(), "", "cs-noprofile", d, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch without a worker profile: %v", err)
	}
	cmd := recordedCmd(t, argvFile)

	for _, marker := range []string{
		"--strict-mcp-config",
		"--mcp-config",
		channel.WorkerMCPConfigFileName,
		"--allowedTools",
		"disallowedTools",
		"--disallowed",
		"--settings",
		"--append-system-prompt",
		WorkerStartingContextFileName,
	} {
		if strings.Contains(cmd, marker) {
			t.Errorf("no-profile launch must not carry %q, got:\n%s", marker, cmd)
		}
	}
	if _, err := os.Stat(filepath.Join(d, channel.WorkerMCPConfigFileName)); !os.IsNotExist(err) {
		t.Errorf("no per-session MCP config expected in %s", d)
	}
	if _, err := os.Stat(filepath.Join(d, WorkerStartingContextFileName)); !os.IsNotExist(err) {
		t.Errorf("no starting context file expected in %s", d)
	}
	// The base launch shape is intact.
	if !strings.Contains(cmd, "--add-dir '"+d+"'") || !strings.Contains(cmd, "--session-id ") {
		t.Errorf("no-profile launch must keep its base shape, got:\n%s", cmd)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: claude done'") {
		t.Errorf("completion marker echo missing at the tail, got: %s", cmd)
	}
}

// TestLaunchResume_WorkerProfileApplied: a profiled resume keeps the whole
// non-profile resume shape (the --resume || --session-id fallback) and both
// halves of it must apply the same profile flags against the same files.
func TestLaunchResume_WorkerProfileApplied(t *testing.T) {
	d := t.TempDir()
	argvFile := startFakeTmux(t, false)

	starting := autonomous.StartingContextInput{Role: "worker", TaskSpec: "carry on the task"}
	if err := backendWithProfile(t, "minimal", starting).LaunchResume(context.Background(), "", "cs-minresume", d, "/nonexistent/log", "minsess-0001"); err != nil {
		t.Fatalf("LaunchResume with minimal worker profile: %v", err)
	}
	cmd := recordedCmd(t, argvFile)

	orIdx := strings.Index(cmd, " || ")
	if orIdx < 0 {
		t.Fatalf("resume/fallback halves not joined by ||, got:\n%s", cmd)
	}
	for name, half := range map[string]string{"resume": cmd[:orIdx], "fallback": cmd[orIdx:]} {
		for _, want := range []string{
			"--strict-mcp-config",
			"--mcp-config '" + filepath.Join(d, channel.WorkerMCPConfigFileName) + "'",
			"--allowedTools",
			`--settings '{"skills":false}'`,
			`--append-system-prompt "$(cat '` + filepath.Join(d, WorkerStartingContextFileName) + `')"`,
		} {
			if !strings.Contains(half, want) {
				t.Errorf("%s half missing %q, got:\n%s", name, want, half)
			}
		}
	}
	if strings.Contains(cmd, "disallowedTools") || strings.Contains(cmd, "--disallowed") {
		t.Errorf("a worker profile must never pass a disallowed-tools list, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--resume ") || !strings.Contains(cmd, "--session-id ") {
		t.Errorf("profiled resume must keep the --resume || --session-id shape, got:\n%s", cmd)
	}
	for _, p := range []string{filepath.Join(d, channel.WorkerMCPConfigFileName), filepath.Join(d, WorkerStartingContextFileName)} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("profile file %s must exist after a profiled resume: %v", p, err)
		}
	}
}

func launchOf(t *testing.T, name string) WorkerProfileLaunch {
	t.Helper()
	p, ok := workerprofile.Builtin(name)
	if !ok {
		t.Fatalf("builtin worker profile %q not available", name)
	}
	return WorkerProfileLaunch{
		Profile: p,
		MCPServers: map[string]channel.MCPServerSpec{
			channel.ProjectMCPEntryName: {Command: "datawatch", Args: []string{"mcp"}},
		},
		Starting: autonomous.StartingContextInput{Role: "worker", TaskSpec: "task for " + name},
	}
}

// TestLaunchWithProfile_PerCallIsolation: two launches on ONE shared backend
// with different profiles each carry their own arguments, and the backend
// keeps no profile afterwards.
func TestLaunchWithProfile_PerCallIsolation(t *testing.T) {
	b := backendOf()

	dMin := t.TempDir()
	argvMin := startFakeTmux(t, false)
	if err := b.LaunchWithProfile(context.Background(), launchOf(t, "minimal"), "", "cs-percall-a", dMin, "/nonexistent/log"); err != nil {
		t.Fatalf("minimal launch: %v", err)
	}
	cmdMin := recordedCmd(t, argvMin)

	dFull := t.TempDir()
	argvFull := startFakeTmux(t, false)
	if err := b.LaunchWithProfile(context.Background(), launchOf(t, "full"), "", "cs-percall-b", dFull, "/nonexistent/log"); err != nil {
		t.Fatalf("full launch: %v", err)
	}
	cmdFull := recordedCmd(t, argvFull)

	if !strings.Contains(cmdMin, "--allowedTools 'Read,Edit,Write,Bash,Glob,Grep'") || !strings.Contains(cmdMin, `--settings '{"skills":false}'`) {
		t.Errorf("minimal launch lost its own arguments:\n%s", cmdMin)
	}
	if strings.Contains(cmdFull, "--allowedTools") || strings.Contains(cmdFull, "--settings") {
		t.Errorf("full launch picked up the minimal profile's arguments:\n%s", cmdFull)
	}
	if !strings.Contains(cmdMin, filepath.Join(dMin, WorkerStartingContextFileName)) || !strings.Contains(cmdFull, filepath.Join(dFull, WorkerStartingContextFileName)) {
		t.Errorf("each launch must reference its own starting context file:\nmin: %s\nfull: %s", cmdMin, cmdFull)
	}
	if got := b.WorkerProfileName(); got != "" {
		t.Errorf("per-call launch must not store a profile on the shared backend, got %q", got)
	}
}

// TestLaunchWithProfile_ZeroValueMatchesLaunch: a zero WorkerProfileLaunch
// builds exactly the command line the plain Launch builds.
func TestLaunchWithProfile_ZeroValueMatchesLaunch(t *testing.T) {
	d := t.TempDir()
	b := backendOf()

	argvA := startFakeTmux(t, false)
	if err := b.Launch(context.Background(), "do it", "cs-zero", d, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	want := recordedCmd(t, argvA)

	argvB := startFakeTmux(t, false)
	if err := b.LaunchWithProfile(context.Background(), WorkerProfileLaunch{}, "do it", "cs-zero", d, "/nonexistent/log"); err != nil {
		t.Fatalf("LaunchWithProfile zero: %v", err)
	}
	got := recordedCmd(t, argvB)

	// The task scratch file path is random per call; mask it before comparing.
	re := regexp.MustCompile(`datawatch-claudecode-task-[^']*\.txt`)
	if re.ReplaceAllString(got, "TASK") != re.ReplaceAllString(want, "TASK") {
		t.Errorf("zero-value launch differs from Launch:\nwant: %s\ngot:  %s", want, got)
	}
	if strings.Contains(got, "--strict-mcp-config") || strings.Contains(got, "--settings") {
		t.Errorf("zero-value launch carries profile flags: %s", got)
	}
}

// TestLaunchWithProfileFallback_NothingConfiguredArgsUnchanged: with nothing
// configured, a worker, verifier or judge launch through the profile-aware
// path builds the zero-profile argument line byte for byte, which is also the
// plain Launch line. No profile flag appears and no per-session file is written.
func TestLaunchWithProfileFallback_NothingConfiguredArgsUnchanged(t *testing.T) {
	for _, role := range []string{"worker", "verifier", "judge"} {
		t.Run(role, func(t *testing.T) {
			d := t.TempDir()
			b := backendOf()
			session := "cs-identity-" + role
			taskText := role + " task"
			// The task scratch file path is random per call; mask it before comparing.
			mask := func(s string) string {
				return regexp.MustCompile(`datawatch-claudecode-task-[^']*\.txt`).ReplaceAllString(s, "TASK")
			}

			argvPlain := startFakeTmux(t, false)
			if err := b.Launch(context.Background(), taskText, session, d, "/nonexistent/log"); err != nil {
				t.Fatalf("Launch: %v", err)
			}
			want := mask(recordedCmd(t, argvPlain))

			argvZero := startFakeTmux(t, false)
			if err := b.LaunchWithProfile(context.Background(), WorkerProfileLaunch{}, taskText, session, d, "/nonexistent/log"); err != nil {
				t.Fatalf("zero-profile launch: %v", err)
			}
			if got := mask(recordedCmd(t, argvZero)); got != want {
				t.Errorf("zero-profile argument line differs from Launch:\nwant: %s\ngot:  %s", want, got)
			}

			argvNew := startFakeTmux(t, false)
			reason, err := b.LaunchWithProfileFallback(context.Background(), WorkerProfileLaunch{}, taskText, session, d, "/nonexistent/log")
			if err != nil {
				t.Fatalf("profile-aware launch: %v", err)
			}
			if reason != "" {
				t.Errorf("fallback reason %q, want none", reason)
			}
			got := mask(recordedCmd(t, argvNew))
			if got != want {
				t.Errorf("profile-aware argument line differs from the zero-profile launch:\nwant: %s\ngot:  %s", want, got)
			}
			for _, marker := range []string{"--strict-mcp-config", "--mcp-config", "--allowedTools", "--settings", "--append-system-prompt", channel.WorkerMCPConfigFileName, WorkerStartingContextFileName} {
				if strings.Contains(got, marker) {
					t.Errorf("%s launch carries %q with nothing configured: %s", role, marker, got)
				}
			}
			for _, name := range []string{channel.WorkerMCPConfigFileName, WorkerStartingContextFileName} {
				if _, err := os.Stat(filepath.Join(d, name)); !os.IsNotExist(err) {
					t.Errorf("%s: %s written with nothing configured", role, name)
				}
			}
		})
	}
}
