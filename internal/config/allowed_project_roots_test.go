package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowedProjectRootsNilConfigStillAllowsWorktreeBase(t *testing.T) {
	var c *Config
	got := c.AllowedProjectRoots()
	if len(got) != 1 || got[0] != DefaultPRDWorktreeDir() {
		t.Fatalf("nil config roots = %v", got)
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
