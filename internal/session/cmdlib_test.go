package session

import (
	"path/filepath"
	"testing"
)

func tempCmdLib(t *testing.T) *CmdLibrary {
	t.Helper()
	lib, err := NewCmdLibrary(filepath.Join(t.TempDir(), "commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestCmdLibrary_New(t *testing.T) {
	lib := tempCmdLib(t)
	if lib == nil {
		t.Fatal("expected non-nil")
	}
}

func TestCmdLibrary_AddAndList(t *testing.T) {
	lib := tempCmdLib(t)
	_, err := lib.Add("test-cmd", "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	cmds := lib.List()
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].Name != "test-cmd" {
		t.Errorf("expected 'test-cmd', got %q", cmds[0].Name)
	}
}

func TestCmdLibrary_Get(t *testing.T) {
	lib := tempCmdLib(t)
	lib.Add("find-me", "ls -la")

	cmd, ok := lib.Get("find-me")
	if !ok {
		t.Fatal("expected to find command")
	}
	if cmd.Command != "ls -la" {
		t.Errorf("expected 'ls -la', got %q", cmd.Command)
	}

	_, ok = lib.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestCmdLibrary_Delete(t *testing.T) {
	lib := tempCmdLib(t)
	lib.Add("del-me", "rm -rf")

	if err := lib.Delete("del-me"); err != nil {
		t.Fatal(err)
	}
	if _, ok := lib.Get("del-me"); ok {
		t.Error("expected deleted")
	}
}

func TestCmdLibrary_Delete_NotFound(t *testing.T) {
	lib := tempCmdLib(t)
	if err := lib.Delete("nope"); err == nil {
		t.Error("expected error for nonexistent")
	}
}

func TestCmdLibrary_Update(t *testing.T) {
	lib := tempCmdLib(t)
	lib.Add("upd", "old command")

	_, err := lib.Update("upd", "upd-new", "new command")
	if err != nil {
		t.Fatal(err)
	}
	cmd, ok := lib.Get("upd-new")
	if !ok {
		t.Fatal("expected renamed command")
	}
	if cmd.Command != "new command" {
		t.Errorf("expected 'new command', got %q", cmd.Command)
	}
}

func TestCmdLibrary_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmds.json")

	lib1, _ := NewCmdLibrary(path)
	lib1.Add("persist", "echo saved")

	lib2, _ := NewCmdLibrary(path)
	cmds := lib2.List()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 persisted, got %d", len(cmds))
	}
}

func TestCmdLibrary_Seed(t *testing.T) {
	lib := tempCmdLib(t)
	defaults := []SavedCommand{
		{Name: "approve", Command: "yes"},
		{Name: "reject", Command: "no"},
	}
	lib.Seed(defaults)
	if len(lib.List()) < 2 {
		t.Errorf("expected at least 2 after seed, got %d", len(lib.List()))
	}
}
