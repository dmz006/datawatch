package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func startEmptyProjectDir(t *testing.T, cfg *config.Config) string {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	if cfg != nil {
		mgr.SetConfig(cfg)
	}
	var gotDir string
	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		gotDir = projectDir
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})
	if _, err := mgr.Start(context.Background(), "echo hi", "", "", nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return gotDir
}

// MCP and other non-REST callers pass an empty project_dir straight to the
// manager; it must honour session.default_project_dir rather than $HOME.
func TestStart_EmptyProjectDir_UsesConfiguredDefault(t *testing.T) {
	want := filepath.Join(t.TempDir(), "projects")
	cfg := &config.Config{}
	cfg.Session.DefaultProjectDir = want
	if got := startEmptyProjectDir(t, cfg); got != want {
		t.Fatalf("project dir = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("default project dir not created: %v", err)
	}
}

func TestStart_EmptyProjectDir_ExpandsTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg := &config.Config{}
	cfg.Session.DefaultProjectDir = "~/dw-b108-projects"
	if got, want := startEmptyProjectDir(t, cfg), filepath.Join(home, "dw-b108-projects"); got != want {
		t.Fatalf("project dir = %q, want %q", got, want)
	}
}

func TestStart_EmptyProjectDir_NoConfigFallsBackToHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := startEmptyProjectDir(t, nil); got != home {
		t.Fatalf("project dir = %q, want home %q", got, home)
	}
}
