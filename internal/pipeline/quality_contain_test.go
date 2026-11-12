package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A project directory outside every allowed root is refused and nothing runs.
func TestRunTests_RefusesDirOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	marker := filepath.Join(outside, "ran")
	cmd := "touch " + marker

	for name, dir := range map[string]string{
		"sibling":   outside,
		"dotdot":    filepath.Join(root, "..", filepath.Base(outside)),
		"traversal": root + "/sub/../../" + filepath.Base(outside),
	} {
		r := RunTests(cmd, dir, 5, []string{root})
		if r.ExitCode != -1 || !strings.Contains(r.Output, "project directory refused") {
			t.Errorf("%s: want a refusal, got exit=%d output=%q", name, r.ExitCode, r.Output)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("command ran in a directory outside the root")
	}
}

// A symlink inside the root that points out of it is refused.
func TestRunTests_RefusesSymlinkOutOfRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	r := RunTests("true", link, 5, []string{root})
	if r.ExitCode != -1 || !strings.Contains(r.Output, "project directory refused") {
		t.Errorf("want a refusal, got exit=%d output=%q", r.ExitCode, r.Output)
	}
}

// A directory inside the root still runs.
func TestRunTests_AcceptsDirInsideRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := RunTests("true", dir, 5, []string{root})
	if r.ExitCode != 0 {
		t.Errorf("want exit 0, got exit=%d output=%q", r.ExitCode, r.Output)
	}
}

// containedProjectDir returns the checked directory, the root itself included,
// and refuses a sibling that only shares the root's name as a prefix.
func TestContainedProjectDir(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work")
	sibling := filepath.Join(base, "work-evil")
	inside := filepath.Join(root, "proj")
	for _, d := range []string{inside, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{root, inside, inside + "/"} {
		got, err := containedProjectDir(d, []string{root})
		if err != nil {
			t.Errorf("containedProjectDir(%q) refused: %v", d, err)
			continue
		}
		if got != filepath.Clean(d) {
			t.Errorf("containedProjectDir(%q) = %q, want %q", d, got, filepath.Clean(d))
		}
	}
	for _, d := range []string{sibling, base, filepath.Join(root, "..", "work-evil"), ""} {
		if got, err := containedProjectDir(d, []string{root}); err == nil || got != "" {
			t.Errorf("containedProjectDir(%q) = %q, %v; want a refusal and no path", d, got, err)
		}
	}
	if got, err := containedProjectDir(inside, nil); err == nil || got != "" {
		t.Errorf("no roots: got %q, %v; want a refusal", got, err)
	}
}
