package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

func TestContainedUnderAnyRefusesPathOutsideItsBase(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	// A sibling that only shares the base's name as a prefix is outside it.
	sibling := base + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sibling) })
	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"unrelated directory":    outside,
		"prefix-sharing sibling": sibling,
		"dot-dot escape":         filepath.Join(base, "..", filepath.Base(outside)),
		"symlink out of base":    filepath.Join(link, "x"),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := containedUnderAny([]string{base}, path)
			if !errors.Is(err, gitguard.ErrPathEscapes) {
				t.Fatalf("want ErrPathEscapes, got %v", err)
			}
			if got != "" {
				t.Fatalf("a refused path must not be handed back, got %q", got)
			}
		})
	}

	t.Run("no usable root", func(t *testing.T) {
		got, err := containedUnderAny([]string{"", "  "}, base)
		if err == nil || got != "" {
			t.Fatalf("want refusal with empty result, got %q, %v", got, err)
		}
	})
	t.Run("inside the base", func(t *testing.T) {
		in := filepath.Join(base, "a", "b")
		got, err := containedUnderAny([]string{base}, in)
		if err != nil || got != in {
			t.Fatalf("want %q accepted, got %q, %v", in, got, err)
		}
	})
}

func TestCheckExistingWorktreeRefusesPathOutsideRootsUntouched(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// A linked-worktree look-alike outside the root: it would pass the
	// worktree check if the path were inspected before the roots check.
	if err := os.WriteFile(filepath.Join(outside, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{outside, filepath.Join(root, "..", filepath.Base(outside))} {
		err := checkExistingWorktree([]string{root}, dir)
		if !errors.Is(err, ErrProjectDirNotWorktree) {
			t.Fatalf("checkExistingWorktree(%q): want ErrProjectDirNotWorktree, got %v", dir, err)
		}
		if !strings.Contains(err.Error(), "outside the allowed project roots") || !strings.Contains(err.Error(), dir) {
			t.Fatalf("error should name the refused directory and the reason: %v", err)
		}
	}
}

func TestStartRefusesDotDotAndSymlinkEscapes(t *testing.T) {
	base := filepath.Join(t.TempDir(), "worktrees")
	workspace := filepath.Join(t.TempDir(), "workspace")
	outside := t.TempDir()
	for _, d := range []string{base, workspace} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	mgr := rootsManager(t, base, workspace)

	for name, dir := range map[string]string{
		"absolute dot-dot":     filepath.Join(workspace, "..", filepath.Base(outside), "proj"),
		"relative dot-dot":     filepath.Join("..", filepath.Base(outside), "proj"),
		"symlink in workspace": filepath.Join(workspace, "escape", "proj"),
		"relative via symlink": filepath.Join("escape", "proj"),
	} {
		t.Run(name, func(t *testing.T) {
			requireRefused(t, startIn(mgr, dir), dir)
			if _, err := os.Stat(filepath.Join(outside, "proj")); !os.IsNotExist(err) {
				t.Fatalf("a refused start wrote outside the roots: %v", err)
			}
		})
	}
}
