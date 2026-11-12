package claudecode

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// lockedDir returns a directory the launch cannot write into. It is restored
// to writable at cleanup so t.TempDir can remove it. Skips when the process can
// still write there (root ignores permission bits).
func lockedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // test fixture: read-only dir
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // restore for TempDir cleanup
	probe, err := os.CreateTemp(dir, "probe-*")
	if err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("directory stays writable for this user (root?); cannot force a write failure")
	}
	return dir
}

// TestLaunchWithProfileFallback_WriteFailureLaunchesWithoutProfile: the
// profile's worker-mcp.json cannot be written, so the launch starts with no
// profile. Its command equals a launch with nothing configured, and the apply
// error is returned as the reason instead of failing the launch.
func TestLaunchWithProfileFallback_WriteFailureLaunchesWithoutProfile(t *testing.T) {
	d := lockedDir(t)
	b := backendOf()

	argvNone := startFakeTmux(t, false)
	if err := b.Launch(context.Background(), "", "cs-fallback-same", d, "/nonexistent/log"); err != nil {
		t.Fatalf("no-profile Launch: %v", err)
	}
	want := recordedCmd(t, argvNone)

	argvFell := startFakeTmux(t, false)
	reason, err := b.LaunchWithProfileFallback(context.Background(), launchOf(t, "full"), "", "cs-fallback-same", d, "/nonexistent/log")
	if err != nil {
		t.Fatalf("fallback launch failed instead of starting without a profile: %v", err)
	}
	got := recordedCmd(t, argvFell)

	if reason == "" {
		t.Fatal("fallback reason is empty; the write failure must be reported")
	}
	if !strings.Contains(reason, `apply worker profile "full"`) {
		t.Errorf("reason %q does not name the profile", reason)
	}
	if got != want {
		t.Errorf("fallback command differs from a no-profile launch:\nwant: %s\ngot:  %s", want, got)
	}
	if strings.Contains(got, "--strict-mcp-config") || strings.Contains(got, WorkerStartingContextFileName) {
		t.Errorf("fallback command carries profile flags: %s", got)
	}
}

// TestLaunchResumeWithProfileFallback_WriteFailureLaunchesWithoutProfile is the
// resume-path counterpart.
func TestLaunchResumeWithProfileFallback_WriteFailureLaunchesWithoutProfile(t *testing.T) {
	d := lockedDir(t)
	b := backendOf()

	argvNone := startFakeTmux(t, false)
	if err := b.LaunchResume(context.Background(), "", "cs-fallback-resume", d, "/nonexistent/log", "peer-x"); err != nil {
		t.Fatalf("no-profile LaunchResume: %v", err)
	}
	want := recordedCmd(t, argvNone)

	argvFell := startFakeTmux(t, false)
	reason, err := b.LaunchResumeWithProfileFallback(context.Background(), launchOf(t, "full"), "", "cs-fallback-resume", d, "/nonexistent/log", "peer-x")
	if err != nil {
		t.Fatalf("resume fallback failed: %v", err)
	}
	if reason == "" {
		t.Fatal("resume fallback reason is empty")
	}
	if got := recordedCmd(t, argvFell); got != want {
		t.Errorf("resume fallback command differs from a no-profile resume:\nwant: %s\ngot:  %s", want, got)
	}
}

// TestLaunchWithProfileFallback_MissingMCPServerFallsBack: a profile that names
// an MCP server that is not configured falls back the same way.
func TestLaunchWithProfileFallback_MissingMCPServerFallsBack(t *testing.T) {
	d := t.TempDir()
	b := backendOf()

	p, ok := workerprofile.Builtin("full")
	if !ok {
		t.Fatal("builtin full profile missing")
	}
	p.MCP = workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"no-such-server"}}
	w := WorkerProfileLaunch{Profile: p, MCPServers: map[string]channel.MCPServerSpec{}}

	argvNone := startFakeTmux(t, false)
	if err := b.Launch(context.Background(), "", "cs-fallback-missing", d, "/nonexistent/log"); err != nil {
		t.Fatalf("no-profile Launch: %v", err)
	}
	want := recordedCmd(t, argvNone)

	argvFell := startFakeTmux(t, false)
	reason, err := b.LaunchWithProfileFallback(context.Background(), w, "", "cs-fallback-missing", d, "/nonexistent/log")
	if err != nil {
		t.Fatalf("fallback launch failed: %v", err)
	}
	if !strings.Contains(reason, "no-such-server") {
		t.Errorf("reason %q does not name the missing server", reason)
	}
	if got := recordedCmd(t, argvFell); got != want {
		t.Errorf("fallback command differs from a no-profile launch:\nwant: %s\ngot:  %s", want, got)
	}
}

// TestLaunchWithProfileFallback_AppliedProfileHasNoReason: when the profile
// applies there is no fallback reason and the profile flags are present.
func TestLaunchWithProfileFallback_AppliedProfileHasNoReason(t *testing.T) {
	d := t.TempDir()
	b := backendOf()

	argv := startFakeTmux(t, false)
	reason, err := b.LaunchWithProfileFallback(context.Background(), launchOf(t, "full"), "", "cs-fallback-ok", d, "/nonexistent/log")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty when the profile applied", reason)
	}
	if got := recordedCmd(t, argv); !strings.Contains(got, "--strict-mcp-config") {
		t.Errorf("applied profile missing its flags: %s", got)
	}
}
