// v5.26.26 — verify Manager.Delete reaps daemon-owned workspaces
// (sess.EphemeralWorkspace=true under <data_dir>/workspaces/) and
// leaves operator-supplied project_dirs alone.

package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDelete_ReapsEphemeralWorkspace(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.WithFakeTmux()

	// Simulate a daemon-cloned workspace under <data_dir>/workspaces/
	wsRoot := filepath.Join(dataDir, "workspaces")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	clonePath := filepath.Join(wsRoot, "smoke-prof-aabbccdd")
	if err := os.MkdirAll(clonePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop a marker file so we can detect removal.
	if err := os.WriteFile(filepath.Join(clonePath, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		ID: "ee01", FullID: "testhost-ee01", TmuxSession: "cs-ee01",
		ProjectDir:         clonePath,
		EphemeralWorkspace: true,
		State:              StateComplete,
		CreatedAt:          time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Delete("testhost-ee01", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(clonePath); !os.IsNotExist(err) {
		t.Fatalf("expected ephemeral workspace to be reaped, but %s still exists (err=%v)", clonePath, err)
	}
}

func TestDelete_LeavesOperatorProjectDirAlone(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.WithFakeTmux()

	// Operator-supplied dir outside <data_dir>/workspaces/. Reaper
	// must NOT touch this — only sessions with EphemeralWorkspace=true
	// get reaped, AND the safety guard double-checks the path is under
	// the workspace root.
	operatorDir := t.TempDir()
	marker := filepath.Join(operatorDir, "MYWORK.md")
	if err := os.WriteFile(marker, []byte("important"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		ID: "ee02", FullID: "testhost-ee02", TmuxSession: "cs-ee02",
		ProjectDir:         operatorDir,
		EphemeralWorkspace: false, // operator-supplied
		State:              StateComplete,
		CreatedAt:          time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Delete("testhost-ee02", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("operator project_dir should not be touched, but marker is gone: %v", err)
	}
}

func TestDelete_RefusesReapOutsideWorkspaceRoot(t *testing.T) {
	// Defense-in-depth: even if EphemeralWorkspace is set, the path
	// safety guard refuses to remove anything outside <data_dir>/workspaces/.
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.WithFakeTmux()

	// Outside-root dir (an attacker-set ProjectDir, or stored-state
	// drift after a data-dir move).
	outsideDir := t.TempDir()
	marker := filepath.Join(outsideDir, "do-not-delete")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		ID: "ee03", FullID: "testhost-ee03", TmuxSession: "cs-ee03",
		ProjectDir:         outsideDir,
		EphemeralWorkspace: true, // claimed ephemeral, but path is out of bounds
		State:              StateComplete,
		CreatedAt:          time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Delete("testhost-ee03", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("safety guard should prevent reap of outside-root path, but marker is gone: %v", err)
	}
}
