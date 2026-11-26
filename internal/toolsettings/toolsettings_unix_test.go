//go:build !windows

package toolsettings

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Replacing a file whole must not get past a protection that stops an ordinary
// write: a read-only file stays as it is.
func TestWriteDoesNotReplaceAFileThatCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes everything")
	}
	const body = `{"permissions":{"deny":["x"]}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o444); err != nil {
		t.Fatal(err)
	}
	err := Write(path, map[string]any{"a": 1}, 0o444)
	if err == nil || !strings.Contains(err.Error(), "left as it is") {
		t.Errorf("err = %v, want one saying the file was left as it is", err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Errorf("rewritten: %s", got)
	}
}

// A new file is no more open than an ordinary write would have made it.
func TestWriteOfANewFileKeepsTheUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Write(path, map[string]any{"a": 1}, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 under umask 077", info.Mode().Perm())
	}
}

// An existing file keeps its own mode whatever the umask, and no temporary
// file stays behind.
func TestWriteOfAnExistingFileKeepsItsMode(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, map[string]any{"a": 1}, 0o664); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o664 {
		t.Errorf("mode = %v, want 0664", info.Mode().Perm())
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("folder holds %v, want the settings file only", left)
	}
}
