package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowedProjectRootsNilConfigAllowsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var c *Config
	got := strings.Join(c.AllowedProjectRoots(), ",")
	want := DefaultPRDWorktreeDir() + "," + filepath.Join(home, "workspace")
	if got != want {
		t.Fatalf("nil config roots = %s, want %s", got, want)
	}
}

func TestAllowedProjectRootsDefaultWorkspaceWhenUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := DefaultConfig()
	got := c.AllowedProjectRoots()
	want := []string{
		filepath.Join(home, ".datawatch", "prd-worktrees"),
		filepath.Join(home, "workspace"),
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v, want %v", got, want)
	}
	if DefaultWorkspaceRoot() != want[1] {
		t.Fatalf("DefaultWorkspaceRoot = %q, want %q", DefaultWorkspaceRoot(), want[1])
	}
}

func TestAllowedProjectRootsConfiguredWorkspaceReplacesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := DefaultConfig()
	c.Session.WorkspaceRoot = "/elsewhere"
	got := c.AllowedProjectRoots()
	if len(got) != 2 || got[1] != "/elsewhere" {
		t.Fatalf("roots = %v", got)
	}
	for _, r := range got {
		if r == filepath.Join(home, "workspace") {
			t.Fatalf("default workspace must not be added when session.workspace_root is set: %v", got)
		}
	}
}

func TestAllowedProjectRootsNoHomeAddsNoDefaultWorkspace(t *testing.T) {
	t.Setenv("HOME", "")
	if DefaultWorkspaceRoot() != "" {
		t.Skip("home directory still resolvable without HOME on this platform")
	}
	c := DefaultConfig()
	got := c.AllowedProjectRoots()
	if len(got) != 1 || got[0] != DefaultPRDWorktreeDir() {
		t.Fatalf("roots = %v, want the worktree base alone", got)
	}
	var nilCfg *Config
	if got := nilCfg.AllowedProjectRoots(); len(got) != 1 {
		t.Fatalf("nil config roots = %v, want the worktree base alone", got)
	}
}

func TestAllowedProjectRootsOrderAndHomeExpansion(t *testing.T) {
	t.Setenv("HOME", "/home/op")
	c := DefaultConfig()
	c.Autonomous.WorktreeDir = "~/wt"
	c.Session.WorkspaceRoot = " /workspace "
	c.Server.AllowedProjectRoots = []string{"~/projects", "", "/srv/x"}
	want := "/home/op/wt,/workspace,/home/op/projects,/srv/x"
	if got := strings.Join(c.AllowedProjectRoots(), ","); got != want {
		t.Fatalf("roots = %s, want %s", got, want)
	}
}

func TestAllowedProjectRootsAlwaysEnforcedRegardlessOfToken(t *testing.T) {
	c := DefaultConfig()
	for _, token := range []string{"", "secret"} {
		c.Server.Token = token
		roots := c.AllowedProjectRoots()
		if len(roots) == 0 || roots[0] != DefaultPRDWorktreeDir() {
			t.Fatalf("token %q: PRD worktree base missing from %v", token, roots)
		}
	}
}

func TestValidateAllowedProjectRoots(t *testing.T) {
	t.Setenv("HOME", "/home/op")
	ok := [][]string{nil, {}, {"/srv/a"}, {"~/work", "/srv/b/../c"}}
	for _, roots := range ok {
		if err := ValidateAllowedProjectRoots(roots); err != nil {
			t.Errorf("%v refused: %v", roots, err)
		}
	}
	bad := map[string][]string{
		"relative":        {"work/dir"},
		"dot":             {"."},
		"filesystem root": {"/"},
		"root via dots":   {"/srv/.."},
		"empty":           {"  "},
		"NUL":             {"/srv/a\x00"},
	}
	for name, roots := range bad {
		if err := ValidateAllowedProjectRoots(roots); err == nil {
			t.Errorf("%s: %v accepted", name, roots)
		}
	}
}

func TestDefaultPRDWorktreeDirMatchesDocumentedPath(t *testing.T) {
	t.Setenv("HOME", "/home/op")
	if got, want := DefaultPRDWorktreeDir(), filepath.Join("/home/op", ".datawatch", "prd-worktrees"); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
