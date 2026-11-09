package autonomous_test

// Byte-identity guard for the default launch: with no worker profile named at
// any tier (task, Automaton, configured role or kind default, daemon-wide name)
// and no user profiles, a worker, verifier or judge launch through the
// profile-aware path must produce exactly the command, MCP handling and
// starting context the zero-profile call path produces.
//
// This is an external test package because the launch under test lives in
// internal/llm/claudecode, which imports internal/autonomous.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/llm/claudecode"
)

// identityScratchRe masks the per-call task scratch file name, which is random.
var identityScratchRe = regexp.MustCompile(`datawatch-claudecode-task-[^']*\.txt`)

// identityProfileMarkers are the flags and file names a profiled launch adds.
// None may appear in a launch with nothing configured.
var identityProfileMarkers = []string{
	"--strict-mcp-config",
	"--mcp-config",
	"--allowedTools",
	"--settings",
	"--append-system-prompt",
	channel.WorkerMCPConfigFileName,
	claudecode.WorkerStartingContextFileName,
}

// installIdentityFakeTmux puts a "tmux" stub first on PATH that records its
// argv NUL-separated and exits 0. No real tmux server is touched.
func installIdentityFakeTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + argvFile + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

// recordedIdentityCmd returns the send-keys payload the fake tmux recorded
// (tmux send-keys -t <session> <cmd> Enter), with the scratch file masked.
func recordedIdentityCmd(t *testing.T, argvFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if len(args) < 2 {
		t.Fatalf("recorded tmux argv too short: %q", raw)
	}
	return identityScratchRe.ReplaceAllString(args[len(args)-2], "TASK")
}

// TestNothingConfiguredLaunchIsByteIdenticalPerRole: for each of worker,
// verifier and judge, the launch resolved with nothing configured carries no
// profile, and its full launch (arguments, MCP handling, starting context)
// equals the zero-profile launch byte for byte.
func TestNothingConfiguredLaunchIsByteIdenticalPerRole(t *testing.T) {
	for _, role := range []string{"worker", "verifier", "judge"} {
		t.Run(role, func(t *testing.T) {
			m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			// No SetWorkerProfileSettings call: no daemon-wide name, no role or
			// kind default, no user profiles. The PRD names no profile either.
			prd, err := m.CreatePRD("spec", "", "pi-prompt", "qwen3", "")
			if err != nil {
				t.Fatalf("CreatePRD: %v", err)
			}
			if err := m.Store().SetStories(prd.ID, []autonomous.Story{{Title: "S", Tasks: []autonomous.Task{{
				ID: "tp1", Title: "t", Spec: "do it", Kind: "implementation",
			}}}}); err != nil {
				t.Fatalf("SetStories: %v", err)
			}
			prd, _ = m.Store().GetPRD(prd.ID)
			task := &prd.Story[0].Tasks[0]

			// The profile-aware path: the resolved profile for this role.
			p, applied := m.ResolveSessionProfile(role, prd, task)
			if applied {
				t.Fatalf("%s: profile %q applied with nothing configured", role, p.Name)
			}
			w := claudecode.WorkerProfileLaunch{}

			b := claudecode.New("claude").(*claudecode.Backend)
			dir := t.TempDir()
			const session = "cs-identity-default"
			const taskText = "do it"
			const logFile = "/nonexistent/log"

			// Pre-change bytes: the zero-profile call path.
			argvZero := installIdentityFakeTmux(t)
			if err := b.LaunchWithProfile(context.Background(), claudecode.WorkerProfileLaunch{}, taskText, session, dir, logFile); err != nil {
				t.Fatalf("zero-profile launch: %v", err)
			}
			want := recordedIdentityCmd(t, argvZero)

			// Profile-aware path with nothing configured.
			argvNew := installIdentityFakeTmux(t)
			reason, err := b.LaunchWithProfileFallback(context.Background(), w, taskText, session, dir, logFile)
			if err != nil {
				t.Fatalf("profile-aware launch: %v", err)
			}
			if reason != "" {
				t.Errorf("fallback reason %q, want none with nothing configured", reason)
			}
			got := recordedIdentityCmd(t, argvNew)

			if got != want {
				t.Errorf("%s launch differs from the zero-profile launch:\nwant: %s\ngot:  %s", role, want, got)
			}
			for _, marker := range identityProfileMarkers {
				if strings.Contains(got, marker) {
					t.Errorf("%s launch carries %q with nothing configured: %s", role, marker, got)
				}
			}
			for _, name := range []string{channel.WorkerMCPConfigFileName, claudecode.WorkerStartingContextFileName} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Errorf("%s: %s written into the session directory with nothing configured", role, name)
				}
			}
		})
	}
}
