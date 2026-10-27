package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/gitguard"
)

// rootsManager builds a manager that allows only the roots it is given:
// the PRD worktree base, the workspace root and server.allowed_project_roots.
// Unlike the shared test helpers it does not allow the system temp directory.
func rootsManager(t *testing.T, worktreeBase, workspace string, extra ...string) *Manager {
	t.Helper()
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	fake := mgr.WithFakeTmux()
	cfg := config.DefaultConfig()
	cfg.Autonomous.WorktreeDir = worktreeBase
	cfg.Session.WorkspaceRoot = workspace
	cfg.Server.AllowedProjectRoots = extra
	mgr.SetConfig(cfg)
	mgr.SetWorkspaceRoot(workspace)
	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, _, _ string) error {
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})
	return mgr
}

func startIn(mgr *Manager, dir string) error {
	_, err := mgr.Start(context.Background(), "task", "", dir, &StartOptions{OneShot: true})
	return err
}

func requireRefused(t *testing.T, err error, dir string) {
	t.Helper()
	if !errors.Is(err, gitguard.ErrOutsideAllowedRoots) && !errors.Is(err, gitguard.ErrPathEscapes) {
		t.Fatalf("Start(%q): want a project-root refusal, got %v", dir, err)
	}
}

func TestStartEnforcesAllowedProjectRoots(t *testing.T) {
	base := filepath.Join(t.TempDir(), "worktrees")
	workspace := filepath.Join(t.TempDir(), "workspace")
	extra := t.TempDir()
	outside := t.TempDir()
	for _, d := range []string{base, workspace} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mgr := rootsManager(t, base, workspace, extra)

	t.Run("inside the PRD worktree base", func(t *testing.T) {
		if err := startIn(mgr, filepath.Join(base, "prd1")); err != nil {
			t.Fatalf("want accepted: %v", err)
		}
	})
	t.Run("inside the workspace root", func(t *testing.T) {
		if err := startIn(mgr, filepath.Join(workspace, "app")); err != nil {
			t.Fatalf("want accepted: %v", err)
		}
	})
	t.Run("inside a configured extra root", func(t *testing.T) {
		if err := startIn(mgr, filepath.Join(extra, "proj")); err != nil {
			t.Fatalf("want accepted: %v", err)
		}
	})
	t.Run("relative path lands under the workspace root", func(t *testing.T) {
		if err := startIn(mgr, "rel/app"); err != nil {
			t.Fatalf("want accepted: %v", err)
		}
		if fi, err := os.Stat(filepath.Join(workspace, "rel", "app")); err != nil || !fi.IsDir() {
			t.Fatalf("relative dir not created under the workspace root: %v", err)
		}
	})
	t.Run("outside every root", func(t *testing.T) {
		dir := filepath.Join(outside, "proj")
		requireRefused(t, startIn(mgr, dir), dir)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("a refused start must not create the directory: %v", err)
		}
	})
	t.Run("the error names the allowed roots", func(t *testing.T) {
		err := startIn(mgr, filepath.Join(outside, "proj"))
		for _, want := range []string{base, workspace, extra, "server.allowed_project_roots"} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error should mention %q: %v", want, err)
			}
		}
	})
	t.Run("traversal out of a root", func(t *testing.T) {
		for _, dir := range []string{
			filepath.Join(base, "..", filepath.Base(outside)),
			base + "/prd1/../../" + filepath.Base(outside),
			"../../../etc",
			"..",
		} {
			requireRefused(t, startIn(mgr, dir), dir)
		}
	})
	t.Run("symlink escape", func(t *testing.T) {
		link := filepath.Join(base, "escape")
		if err := os.Symlink(outside, link); err != nil {
			t.Skip(err)
		}
		for _, dir := range []string{link, filepath.Join(link, "deeper")} {
			requireRefused(t, startIn(mgr, dir), dir)
		}
		if _, err := os.Stat(filepath.Join(outside, "deeper")); !os.IsNotExist(err) {
			t.Fatal("a refused start created a directory through the link")
		}
	})
	t.Run("dangling symlink escape", func(t *testing.T) {
		missing := filepath.Join(outside, "not-created")
		link := filepath.Join(workspace, "dangling")
		if err := os.Symlink(missing, link); err != nil {
			t.Skip(err)
		}
		requireRefused(t, startIn(mgr, link), link)
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatal("a refused start created the link's missing target")
		}
	})
}

func TestStartNoRootsConfiguredRefusesOutsideDefaultBase(t *testing.T) {
	// Nothing configured: only the default PRD worktree base is allowed, so a
	// plain temp directory is refused. The check never fails open.
	mgr := rootsManager(t, "", "")
	dir := t.TempDir()
	requireRefused(t, startIn(mgr, dir), dir)
}

func TestStartConfiguredDefaultProjectDirIsDaemonConfig(t *testing.T) {
	// session.default_project_dir is the daemon's own configuration, so a
	// start that names exactly that directory (the REST layer fills it in)
	// is not request input and is not held to the roots.
	def := filepath.Join(t.TempDir(), "default-dir")
	mgr := rootsManager(t, filepath.Join(t.TempDir(), "wt"), "")
	cfg := config.DefaultConfig()
	cfg.Autonomous.WorktreeDir = filepath.Join(t.TempDir(), "wt")
	cfg.Session.DefaultProjectDir = def
	mgr.SetConfig(cfg)
	if err := startIn(mgr, def); err != nil {
		t.Fatalf("configured default dir refused: %v", err)
	}
	if err := startIn(mgr, def+"-other"); err == nil {
		t.Fatal("a different directory next to the default must still be refused")
	}
	if err := startIn(mgr, ""); err != nil {
		t.Fatalf("empty dir (daemon default) refused: %v", err)
	}
}
