package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestIsDaemonMarkerPath(t *testing.T) {
	cases := map[string]bool{
		"DATAWATCH_COMPLETE":        true,
		"./DATAWATCH_COMPLETE":      true,
		"CHECKPOINT.md":             true,
		treeLockRelPath:             true,
		"sub/DATAWATCH_COMPLETE":    false,
		"sub/../DATAWATCH_COMPLETE": true,
		"main.go":                   false,
		"sub/CHECKPOINT.md":         false,
		"":                          false,
		"DATAWATCH_COMPLETE.bak":    false,
	}
	for p, want := range cases {
		if got := isDaemonMarkerPath(p); got != want {
			t.Errorf("isDaemonMarkerPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestDaemonMarkerFilesIncludeCompletionMarker(t *testing.T) {
	found := false
	for _, m := range daemonMarkerFiles() {
		if m == completionMarkerFile {
			found = true
		}
		if !isDaemonMarkerPath(m) {
			t.Errorf("listed marker %q not recognised", m)
		}
	}
	if !found {
		t.Fatal("completion marker missing from daemonMarkerFiles")
	}
}

func TestMarkerRemoveRegularFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, completionMarkerFile), "x")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "keep")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "sub", completionMarkerFile), "keep")

	removed, err := removeCompletionMarker(dir)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v, want true,nil", removed, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, completionMarkerFile)); !os.IsNotExist(err) {
		t.Fatalf("marker still present: %v", err)
	}
	for _, keep := range []string{"notes.txt", filepath.Join("sub", completionMarkerFile)} {
		if _, err := os.Lstat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s should remain: %v", keep, err)
		}
	}
}

func TestMarkerRemoveMissing(t *testing.T) {
	removed, err := removeCompletionMarker(t.TempDir())
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v, want false,nil", removed, err)
	}
}

func TestMarkerRemoveRefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, completionMarkerFile)
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(d, "inner.txt"), "keep")
	removed, err := removeCompletionMarker(dir)
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v, want false,nil", removed, err)
	}
	if _, err := os.Stat(filepath.Join(d, "inner.txt")); err != nil {
		t.Errorf("directory contents lost: %v", err)
	}
}

func TestMarkerRemoveRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.txt")
	mustWrite(t, target, "keep")
	link := filepath.Join(dir, completionMarkerFile)
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	removed, err := removeCompletionMarker(dir)
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v, want false,nil", removed, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("symlink target lost: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink removed: %v", err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// finishMarkerManager builds a Manager whose uncommitted-work policy is set
// and whose git runner reports a clean tree.
func finishMarkerManager(t *testing.T, policy string) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Automation.UncommittedWorkPolicy = policy
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.driftGit = func(ctx context.Context, dir string, args ...string) (string, error) { return "", nil }
	return m
}

func TestUncommittedRollupRemovesMarkerAtFinish(t *testing.T) {
	cases := []struct {
		name     string
		policy   string
		mutate   func(p *PRD)
		wantGone bool
	}{
		{"policy hold", config.UncommittedWorkPolicyHold, nil, true},
		{"policy off still removes", config.UncommittedWorkPolicyOff, nil, true},
		{"no branch", config.UncommittedWorkPolicyHold, func(p *PRD) { p.Git.Branch = "" }, false},
		{"cluster profile", config.UncommittedWorkPolicyHold, func(p *PRD) { p.ClusterProfile = "c1" }, false},
		{"empty project dir", config.UncommittedWorkPolicyHold, func(p *PRD) { p.ProjectDir = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, completionMarkerFile)
			mustWrite(t, marker, "x")
			prd := &PRD{ID: "p1", ProjectDir: dir}
			prd.Git.Branch = "work"
			if tc.mutate != nil {
				tc.mutate(prd)
			}
			m := finishMarkerManager(t, tc.policy)
			if blocked, err := m.checkWorktreeUncommittedAtFinish(context.Background(), prd); err != nil || blocked {
				t.Fatalf("blocked=%v err=%v, want false,nil", blocked, err)
			}
			_, err := os.Lstat(marker)
			if gone := os.IsNotExist(err); gone != tc.wantGone {
				t.Fatalf("marker gone=%v, want %v (err=%v)", gone, tc.wantGone, err)
			}
		})
	}
}
