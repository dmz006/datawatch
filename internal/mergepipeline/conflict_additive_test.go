package mergepipeline

import (
	"errors"
	"strings"
	"testing"
)

// diff3 text of one hunk, as git writes it.
func hunkText(before, ours, base, theirs, after string) string {
	return before + "<<<<<<< HEAD\n" + ours + "||||||| base\n" + base + "=======\n" + theirs + ">>>>>>> feature\n" + after
}

func TestResolveAdditiveAppendedEntriesKeepBothSides(t *testing.T) {
	text := hunkText("a\n", "m\n", "", "f\n", "z\n")
	got, resolved, remaining, err := ResolveAdditive("list.txt", text)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nm\nf\nz\n" {
		t.Fatalf("text = %q", got)
	}
	if len(remaining) != 0 || len(resolved) != 1 {
		t.Fatalf("resolved=%v remaining=%v", resolved, remaining)
	}
	r := resolved[0]
	if r.File != "list.txt" || r.Index != 1 || r.Line != 2 || r.OursLines != 1 || r.TheirsLines != 1 || r.KeptLines != 2 || r.Method != MethodAdditive {
		t.Fatalf("record = %+v", r)
	}
}

func TestResolveAdditiveIsDeterministic(t *testing.T) {
	text := hunkText("", "m1\nm2\n", "", "f1\n", "")
	first, _, _, _ := ResolveAdditive("x", text)
	for i := 0; i < 20; i++ {
		again, _, _, _ := ResolveAdditive("x", text)
		if again != first {
			t.Fatalf("run %d differs: %q vs %q", i, again, first)
		}
	}
	if first != "m1\nm2\nf1\n" {
		t.Fatalf("order = %q, want ours then theirs", first)
	}
}

func TestResolveAdditiveKeepsAdditionsAroundSharedBaseLines(t *testing.T) {
	// Both sides kept the two base lines and each added around them.
	text := hunkText("", "o1\nb1\nb2\n", "b1\nb2\n", "b1\nt1\nb2\nt2\n", "")
	got, _, remaining, err := ResolveAdditive("x", text)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("err=%v remaining=%v", err, remaining)
	}
	if got != "o1\nb1\nt1\nb2\nt2\n" {
		t.Fatalf("text = %q", got)
	}
}

func TestResolveAdditiveKeepsAnIdenticalAdditionOnce(t *testing.T) {
	text := hunkText("", "same\n", "", "same\n", "")
	got, resolved, _, _ := ResolveAdditive("x", text)
	if got != "same\n" || len(resolved) != 1 {
		t.Fatalf("text=%q resolved=%v", got, resolved)
	}
}

func TestResolveAdditiveLeavesOverlappingEditAlone(t *testing.T) {
	text := hunkText("pre\n", "x=2\n", "x=1\n", "x=3\n", "post\n")
	got, resolved, remaining, err := ResolveAdditive("cfg", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 0 || len(remaining) != 1 {
		t.Fatalf("resolved=%v remaining=%v", resolved, remaining)
	}
	h := remaining[0]
	if h.File != "cfg" || h.Index != 1 || strings.Join(h.Base, "") != "x=1\n" || strings.Join(h.Ours, "") != "x=2\n" || strings.Join(h.Theirs, "") != "x=3\n" {
		t.Fatalf("hunk = %+v", h)
	}
	if !strings.Contains(got, "<<<<<<<") || !strings.HasPrefix(got, "pre\n") || !strings.HasSuffix(got, "post\n") {
		t.Fatalf("unresolved hunk must keep its markers: %q", got)
	}
}

func TestResolveAdditiveNotAdditiveWhenASideRemovesABaseLine(t *testing.T) {
	// Ours removed b1 while theirs added after it.
	text := hunkText("", "", "b1\n", "b1\nt1\n", "")
	_, resolved, remaining, _ := ResolveAdditive("x", text)
	if len(resolved) != 0 || len(remaining) != 1 {
		t.Fatalf("a removal must not be treated as additive: resolved=%v remaining=%v", resolved, remaining)
	}
}

func TestResolveAdditiveMixedHunksRenumbersRemaining(t *testing.T) {
	text := hunkText("", "m\n", "", "f\n", "mid\n") + hunkText("", "x=2\n", "x=1\n", "x=3\n", "")
	got, resolved, remaining, err := ResolveAdditive("f", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Index != 1 {
		t.Fatalf("resolved = %v", resolved)
	}
	if len(remaining) != 1 || remaining[0].Index != 1 {
		t.Fatalf("remaining must be numbered by the returned text: %v", remaining)
	}
	if !strings.HasPrefix(got, "m\nf\nmid\n<<<<<<<") {
		t.Fatalf("text = %q", got)
	}
}

func TestParseConflictsRejectsWhatCannotBeJudged(t *testing.T) {
	if _, err := ParseHunks("f", "plain text\n"); !errors.Is(err, ErrNoConflictMarkers) {
		t.Fatalf("no markers: err = %v", err)
	}
	merge2 := "<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n"
	if _, err := ParseHunks("f", merge2); err == nil || errors.Is(err, ErrNoConflictMarkers) {
		t.Fatalf("a hunk with no base must be an error, got %v", err)
	}
	if _, err := ParseHunks("f", "<<<<<<< HEAD\na\n||||||| b\n=======\nb\n"); err == nil {
		t.Fatal("an unclosed hunk must be an error")
	}
	if _, _, _, err := ResolveAdditive("f", merge2); err == nil {
		t.Fatal("ResolveAdditive must not guess without a base")
	}
}
