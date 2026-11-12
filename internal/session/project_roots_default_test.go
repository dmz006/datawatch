package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStartDefaultProjectRoots proves the guard with the roots a daemon has
// when nothing is configured: the PRD worktree base under the home directory
// and the default workspace folder, HOME/workspace.
func TestStartDefaultProjectRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".datawatch", "prd-worktrees")
	workspace := filepath.Join(home, "workspace")
	repo := filepath.Join(workspace, "repo")
	for _, d := range []string{base, repo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	extra := t.TempDir()
	otherWorkspace := t.TempDir()
	// A link inside the default workspace that leads out of every root.
	link := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		// mgr builds the manager under test; workspace "" means unset.
		mgr    func(t *testing.T) *Manager
		dir    string
		refuse bool
		// wantInErr lists strings a refusal must mention.
		wantInErr []string
		// mustNotExist is a path that must still be absent afterwards.
		mustNotExist string
	}{
		{
			name: "directory under the default workspace folder",
			mgr:  func(t *testing.T) *Manager { return rootsManager(t, base, "") },
			dir:  repo,
		},
		{
			name: "new directory under the default workspace folder",
			mgr:  func(t *testing.T) *Manager { return rootsManager(t, base, "") },
			dir:  filepath.Join(workspace, "fresh"),
		},
		{
			name: "directory under the worktree base",
			mgr:  func(t *testing.T) *Manager { return rootsManager(t, base, "") },
			dir:  filepath.Join(base, "prd1"),
		},
		{
			name:         "directory outside both roots",
			mgr:          func(t *testing.T) *Manager { return rootsManager(t, base, "") },
			dir:          filepath.Join(outside, "proj"),
			refuse:       true,
			wantInErr:    []string{base, workspace, "server.allowed_project_roots"},
			mustNotExist: filepath.Join(outside, "proj"),
		},
		{
			name:         "symlink in the workspace pointing outside every root",
			mgr:          func(t *testing.T) *Manager { return rootsManager(t, base, "") },
			dir:          filepath.Join(link, "created-through-link"),
			refuse:       true,
			mustNotExist: filepath.Join(outside, "created-through-link"),
		},
		{
			name: "directory under an extra configured root",
			mgr:  func(t *testing.T) *Manager { return rootsManager(t, base, "", extra) },
			dir:  filepath.Join(extra, "proj"),
		},
		{
			name:      "default workspace is not added when workspace_root is explicit",
			mgr:       func(t *testing.T) *Manager { return rootsManager(t, base, otherWorkspace) },
			dir:       repo,
			refuse:    true,
			wantInErr: []string{base, otherWorkspace},
		},
		{
			name: "explicit workspace_root still allows its own directories",
			mgr:  func(t *testing.T) *Manager { return rootsManager(t, base, otherWorkspace) },
			dir:  filepath.Join(otherWorkspace, "proj"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.dir
			err := startIn(tc.mgr(t), dir)
			if !tc.refuse {
				if err != nil {
					t.Fatalf("Start(%q): want accepted, got %v", dir, err)
				}
				return
			}
			requireRefused(t, err, dir)
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should mention %q: %v", want, err)
				}
			}
			if tc.mustNotExist != "" {
				if _, serr := os.Stat(tc.mustNotExist); !os.IsNotExist(serr) {
					t.Fatalf("a refused start must not create %q: %v", tc.mustNotExist, serr)
				}
			}
		})
	}
}
