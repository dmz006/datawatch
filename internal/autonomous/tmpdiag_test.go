package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTmpDiag(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("t.TempDir()           = %q", base)
	t.Logf("os.TempDir()          = %q", os.TempDir())
	t.Logf("os.Getwd()            = %q", os.Getwd())
	v, err := filepath.Abs(base)
	t.Logf("Abs(base)             = %q err=%v", v, err)
	v, err = filepath.EvalSymlinks(base)
	t.Logf("EvalSymlinks(base)    = %q err=%v", v, err)
	v, err = filepath.EvalSymlinks(repo)
	t.Logf("EvalSymlinks(repo)    = %q err=%v", v, err)
	st, err := os.Lstat(base)
	t.Logf("Lstat(base)           = %v err=%v", st, err)
	out, _ := exec.Command("sh", "-c", "readlink -f '"+base+"'").CombinedOutput()
	t.Logf("child readlink -f     = %q", string(out))
	out, _ = exec.Command("sh", "-c", "ls -ld '"+base+"' /tmp | head -4").CombinedOutput()
	t.Logf("child ls -ld          =\n%s", string(out))
}
