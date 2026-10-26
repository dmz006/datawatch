package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

func TestResolveWorkspaceDir_ValidRelative(t *testing.T) {
	root := t.TempDir()
	got, err := resolveWorkspaceDir(root, "team/app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "team", "app"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveWorkspaceDir_CreatesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	if _, err := resolveWorkspaceDir(root, "app"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		t.Fatalf("root not created: %v", err)
	}
}

func TestResolveWorkspaceDir_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"..", "../outside", "a/../../outside", "../../../etc"} {
		if _, err := resolveWorkspaceDir(root, rel); !errors.Is(err, gitguard.ErrPathEscapes) {
			t.Fatalf("%q: want ErrPathEscapes, got %v", rel, err)
		}
	}
}

func TestResolveWorkspaceDir_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := resolveWorkspaceDir(root, "link/proj"); !errors.Is(err, gitguard.ErrPathEscapes) {
		t.Fatalf("want ErrPathEscapes, got %v", err)
	}
}
