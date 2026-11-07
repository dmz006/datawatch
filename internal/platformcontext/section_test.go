package platformcontext

import (
	"strings"
	"testing"
)

func TestUpsertRemoveRoundTrip(t *testing.T) {
	base := "# Title\n\nuser text\n"
	a := UpsertSection(base, "body", 1)
	if a[:len(base)] != base {
		t.Fatal("prefix changed")
	}
	if UpsertSection(a, "body", 1) != a {
		t.Fatal("not idempotent")
	}
	b := UpsertSection(a+"tail\n", "new", 2)
	if b == a+"tail\n" || b[len(b)-5:] != "tail\n" || b[:len(base)] != base {
		t.Fatalf("bad replace: %q", b)
	}
	if got := RemoveSection(a); got != base {
		t.Fatalf("remove: %q", got)
	}
	if got := RemoveSection(base); got != base {
		t.Fatal("remove without section changed input")
	}
	if got := UpsertSection("", "x", 1); got != block("x", 1) {
		t.Fatalf("empty: %q", got)
	}
}

func TestUpsertSectionTable(t *testing.T) {
	user := "# Title\n\nuser text\n"
	v1 := block("body", 1)
	tests := []struct {
		name     string
		existing string
		body     string
		version  int
		want     string
	}{
		{"absent appended", user, "body", 1, user + "\n" + v1},
		{"absent no trailing newline", "abc", "body", 1, "abc\n\n" + v1},
		{"absent blank line already", user + "\n", "body", 1, user + "\n" + v1},
		{"same version and body unchanged", user + "\n" + v1, "body", 1, user + "\n" + v1},
		{"different version replaces block", user + "\n" + v1, "body", 2, user + "\n" + block("body", 2)},
		{"different body replaces block", user + "\n" + v1, "other", 1, user + "\n" + block("other", 1)},
		{"text before and after preserved", "before\r\n  keep  \n\n" + v1 + "after\n\ttab\n", "new", 3,
			"before\r\n  keep  \n\n" + block("new", 3) + "after\n\ttab\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UpsertSection(tc.existing, tc.body, tc.version)
			if got != tc.want {
				t.Fatalf("got %q\nwant %q", got, tc.want)
			}
			if !strings.HasPrefix(tc.want, tc.existing[:min(len(tc.existing), 4)]) {
				t.Fatal("bad test table")
			}
		})
	}
}

func TestUpsertSectionAbsentKeepsExistingBytes(t *testing.T) {
	existing := "a\r\nb  \n\n\tc"
	got := UpsertSection(existing, "body", 1)
	if !strings.HasPrefix(got, existing) {
		t.Fatalf("existing bytes changed: %q", got)
	}
}

func TestUpsertSectionOnlyBlockReplaced(t *testing.T) {
	before, after := "top\n\n", "bottom\n"
	old := before + block("old", 1) + after
	got := UpsertSection(old, "new", 2)
	if !strings.HasPrefix(got, before) || !strings.HasSuffix(got, after) {
		t.Fatalf("surrounding text changed: %q", got)
	}
	if strings.Contains(got, "old") || !strings.Contains(got, "v2") {
		t.Fatalf("block not replaced: %q", got)
	}
}

func TestRemoveSectionTable(t *testing.T) {
	tests := []struct {
		name, existing, want string
	}{
		{"block at end", "user\n\n" + block("b", 1), "user\n"},
		{"block between text", "before\n\n" + block("b", 1) + "after\n", "before\nafter\n"},
		{"block only", block("b", 1), ""},
		{"no block unchanged", "user\ntext\n", "user\ntext\n"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RemoveSection(tc.existing)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if strings.Contains(got, "datawatch:platform-context") {
				t.Fatal("markers remain")
			}
		})
	}
}
