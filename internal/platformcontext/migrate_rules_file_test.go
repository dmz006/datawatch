package platformcontext

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRulesFile creates a file under t.TempDir with the given content and
// returns its path.
func writeRulesFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRulesFileMigrationRemovesBlock(t *testing.T) {
	base := "# Rules\n\nuser line one\nuser line two\n"
	before := base + "\n" + block("This session runs on datawatch.", 1)
	want := base

	path := writeRulesFile(t, before)
	changed, err := MigrateRulesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := readFile(t, path); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// Permission bits are preserved.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode = %v, want 0640", got)
	}
	// Idempotent: a second call is a no-op and does not rewrite.
	changed, err = MigrateRulesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second call: changed = true, want false")
	}
	if got := readFile(t, path); got != want {
		t.Fatalf("after second call: got %q\nwant %q", got, want)
	}
}

func TestRulesFileMigrationCRLF(t *testing.T) {
	// A CRLF-flavoured file: the block is located and removed despite the
	// \r line endings. Note that RemoveSection only strips a separator blank
	// line ending in "\n\n", so a CRLF blank line ("\r\n\r\n") is left in
	// place — the assertion below pins that exact byte-for-byte result.
	before := "line one\r\n\r\n" +
		"<!-- datawatch:platform-context v1 -->\r\n" +
		"This session runs on datawatch.\r\n" +
		"<!-- /datawatch:platform-context -->\r\n" +
		"tail\r\n"
	want := "line one\r\n\r\ntail\r\n"

	path := writeRulesFile(t, before)
	changed, err := MigrateRulesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := readFile(t, path); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestRulesFileMigrationBlockAtEOFNoTrailingNewline(t *testing.T) {
	before := "text\n\n" +
		"<!-- datawatch:platform-context v1 -->\n" +
		"body line\n" +
		"<!-- /datawatch:platform-context -->"
	want := "text\n"

	path := writeRulesFile(t, before)
	changed, err := MigrateRulesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := readFile(t, path); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestRulesFileMigrationTwoBlocksBothRemoved(t *testing.T) {
	base := "text\n"
	before := base + "\n" + block("body v1", 1) + "\n" + block("body v2", 2)
	want := base

	path := writeRulesFile(t, before)
	changed, err := MigrateRulesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := readFile(t, path); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if changed, err = MigrateRulesFile(path); err != nil || changed {
		t.Fatalf("second call: changed=%v err=%v, want false,nil", changed, err)
	}
}

func TestRulesFileMigrationNoBlockUnchangedAndNotRewritten(t *testing.T) {
	content := "# Rules\n\nno platform context here\n"
	path := writeRulesFile(t, content)
	m1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateRulesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
	if got := readFile(t, path); got != content {
		t.Fatalf("got %q\nwant %q", got, content)
	}
	m2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !m1.ModTime().Equal(m2.ModTime()) {
		t.Fatalf("file was rewritten: modtime %v -> %v", m1.ModTime(), m2.ModTime())
	}
}

func TestRulesFileMigrationMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.md")
	changed, err := MigrateRulesFile(path)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
}
