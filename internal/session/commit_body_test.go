package session

import (
	"fmt"
	"strings"
	"testing"
)

func TestCommitBodyListsFiles(t *testing.T) {
	got := commitBody([]string{"a.go", "  b/c.go  ", "", "d.md"}, "go test ./... passed")
	want := "Files changed:\n- a.go\n- b/c.go\n- d.md\n\nVerification: go test ./... passed"
	if got != want {
		t.Fatalf("commitBody() =\n%q\nwant\n%q", got, want)
	}
}

func TestCommitBodyCapsLongFileList(t *testing.T) {
	var files []string
	for i := 0; i < 52; i++ {
		files = append(files, fmt.Sprintf("f%02d.go", i))
	}
	got := commitBody(files, "ok")

	lines := strings.Split(got, "\n")
	// header + 50 entries + "...and 2 more" + blank + verification
	if len(lines) != 54 {
		t.Fatalf("got %d lines, want 54:\n%s", len(lines), got)
	}
	if lines[50] != "- f49.go" {
		t.Errorf("last listed entry = %q, want %q", lines[50], "- f49.go")
	}
	if lines[51] != "- ...and 2 more" {
		t.Errorf("overflow line = %q, want %q", lines[51], "- ...and 2 more")
	}
	if strings.Contains(got, "f50.go") {
		t.Errorf("entry beyond cap was listed:\n%s", got)
	}
}

func TestCommitBodyNoFiles(t *testing.T) {
	want := "Files changed: none\n\nVerification: ok"
	if got := commitBody(nil, "ok"); got != want {
		t.Errorf("nil files: got %q, want %q", got, want)
	}
	// Entries that are only whitespace are skipped, so they count as no files.
	if got := commitBody([]string{"", "   "}, "ok"); got != want {
		t.Errorf("blank files: got %q, want %q", got, want)
	}
}

func TestCommitBodyEmptyVerification(t *testing.T) {
	want := "Files changed:\n- a.go\n\nVerification: not run before this commit"
	if got := commitBody([]string{"a.go"}, ""); got != want {
		t.Errorf("empty verification: got %q, want %q", got, want)
	}
	if got := commitBody([]string{"a.go"}, "   "); got != want {
		t.Errorf("whitespace verification: got %q, want %q", got, want)
	}
}
