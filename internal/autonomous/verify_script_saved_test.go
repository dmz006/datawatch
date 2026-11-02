package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func writeVerifyTestFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readVerifyTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestVerifyScriptStore_SaveCopiesAllFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeVerifyTestFile(t, src, "a.sh", "A")
	writeVerifyTestFile(t, src, "b.sh", "B")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := savedVerifyScriptsDir(root, "prd1")
	n, err := saveVerifyScripts(src, saved)
	if err != nil || n != 2 {
		t.Fatalf("got %d, %v; want 2, nil", n, err)
	}
	// overwrite existing copies
	writeVerifyTestFile(t, src, "a.sh", "A2")
	if n, err = saveVerifyScripts(src, saved); err != nil || n != 2 {
		t.Fatalf("resave got %d, %v", n, err)
	}
	if got := readVerifyTestFile(t, filepath.Join(saved, "a.sh")); got != "A2" {
		t.Fatalf("a.sh = %q, want A2", got)
	}
	if _, err := os.Stat(filepath.Join(saved, "sub")); err == nil {
		t.Fatal("subdirectory must not be copied")
	}
}

func TestVerifyScriptStore_SaveMissingSource(t *testing.T) {
	root := t.TempDir()
	n, err := saveVerifyScripts(filepath.Join(root, "nope"), filepath.Join(root, "saved"))
	if n != 0 || err != nil {
		t.Fatalf("got %d, %v; want 0, nil", n, err)
	}
}

func TestVerifyScriptStore_RestoreFillsOnlyMissing(t *testing.T) {
	root := t.TempDir()
	src1 := filepath.Join(root, "s1")
	src2 := filepath.Join(root, "s2")
	writeVerifyTestFile(t, src1, "a.sh", "S1A")
	writeVerifyTestFile(t, src2, "a.sh", "S2A")
	writeVerifyTestFile(t, src2, "b.sh", "S2B")
	wt := filepath.Join(root, "wt")
	dst := filepath.Join(wt, ".datawatch", "verify")
	writeVerifyTestFile(t, dst, "b.sh", "EXISTING")
	n, err := restoreVerifyScripts([]string{src1, src2}, wt, nil)
	if err != nil || n != 1 {
		t.Fatalf("got %d, %v; want 1, nil", n, err)
	}
	if got := readVerifyTestFile(t, filepath.Join(dst, "a.sh")); got != "S1A" {
		t.Fatalf("a.sh = %q, want S1A (first srcDir wins)", got)
	}
	if got := readVerifyTestFile(t, filepath.Join(dst, "b.sh")); got != "EXISTING" {
		t.Fatalf("b.sh = %q, must not be overwritten", got)
	}
}

func TestVerifyScriptStore_RestoreOnlyNamed(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "s")
	writeVerifyTestFile(t, src, "a.sh", "A")
	writeVerifyTestFile(t, src, "b.sh", "B")
	wt := filepath.Join(root, "wt")
	n, err := restoreVerifyScripts([]string{src}, wt, []string{"b.sh"})
	if err != nil || n != 1 {
		t.Fatalf("got %d, %v; want 1, nil", n, err)
	}
	dst := filepath.Join(wt, ".datawatch", "verify")
	if _, err := os.Stat(filepath.Join(dst, "a.sh")); err == nil {
		t.Fatal("a.sh must not be copied")
	}
	if got := readVerifyTestFile(t, filepath.Join(dst, "b.sh")); got != "B" {
		t.Fatalf("b.sh = %q", got)
	}
}

func TestVerifyScriptStore_RejectsBadBasename(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "s")
	writeVerifyTestFile(t, src, "a.sh", "A")
	for _, bad := range []string{"../a.sh", "x/a.sh", ".", ".."} {
		if _, err := restoreVerifyScripts([]string{src}, filepath.Join(root, "wt"), []string{bad}); err == nil {
			t.Fatalf("basename %q accepted, want error", bad)
		}
	}
}

func TestVerifyScriptStore_RemoveDeletesSavedFolder(t *testing.T) {
	root := t.TempDir()
	if err := removeSavedVerifyScripts(root, "prd1"); err != nil {
		t.Fatalf("absent folder: %v", err)
	}
	saved := savedVerifyScriptsDir(root, "prd1")
	writeVerifyTestFile(t, saved, "a.sh", "A")
	if err := removeSavedVerifyScripts(root, "prd1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Fatalf("saved folder still present: %v", err)
	}
}

func TestRemoveSavedVerifyScriptsRefusesUnsafeIDs(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, ".verify-scripts", "keep0001")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", ".", "..", "../x", "../../etc"} {
		if err := removeSavedVerifyScripts(root, id); err == nil {
			t.Errorf("id %q: want an error", id)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("saved folder of another Automaton was touched: %v", err)
	}
	if err := removeSavedVerifyScripts(root, "keep0001"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); !os.IsNotExist(err) {
		t.Fatalf("saved folder should be gone, got %v", err)
	}
}
