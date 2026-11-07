package platformcontext

import "testing"

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
