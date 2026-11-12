package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

func TestProtectedPaths_Exact(t *testing.T) {
	got := MatchProtectedPaths([]string{"AGENT.md", "docs/AGENT.md", "other.md"}, []string{"AGENT.md"})
	if want := []string{"AGENT.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestProtectedPaths_Directory(t *testing.T) {
	for _, pat := range []string{"docs/", "docs/**"} {
		got := MatchProtectedPaths([]string{"docs/a.md", "docs", "docsx/a.md", "src/docs/a.md"}, []string{pat})
		if want := []string{"docs/a.md"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("pattern %q: got %v want %v", pat, got, want)
		}
	}
}

func TestProtectedPaths_NestedUnderDirectory(t *testing.T) {
	got := MatchProtectedPaths([]string{"docs/a/b/c.md"}, []string{"docs/"})
	if want := []string{"docs/a/b/c.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestProtectedPaths_Glob(t *testing.T) {
	changed := []string{"src/a.go", "src/b.txt", "src/sub/c.go", "x.go"}
	got := MatchProtectedPaths(changed, []string{"src/*.go"})
	if want := []string{"src/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := MatchProtectedPaths([]string{"a/b/c.go"}, []string{"*.go"}); len(got) != 0 {
		t.Fatalf("* must not cross a slash, got %v", got)
	}
	got = MatchProtectedPaths([]string{"f1.md", "f2.md", "fa.md"}, []string{"f[12].md", "f?x.md"})
	if want := []string{"f1.md", "f2.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestProtectedPaths_CaseSensitive(t *testing.T) {
	if got := MatchProtectedPaths([]string{"agent.md"}, []string{"AGENT.md"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if got := MatchProtectedPaths([]string{"Docs/a.md"}, []string{"docs/"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestProtectedPaths_NormalisationAndDedupe(t *testing.T) {
	got := MatchProtectedPaths(
		[]string{"./docs/a.md", "docs/a.md", "docs//b.md", "z.md", "./z.md"},
		[]string{"./docs/", "z.md", "z.md"})
	if want := []string{"docs/a.md", "docs/b.md", "z.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestProtectedPaths_OutsideAnyPattern(t *testing.T) {
	if got := MatchProtectedPaths([]string{"src/main.go"}, []string{"docs/", "AGENT.md", "*.yaml"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if got := MatchProtectedPaths([]string{"a.go"}, nil); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestProtectedPaths_EscapingAndEmptyNeverMatch(t *testing.T) {
	changed := []string{"../docs/a.md", "docs/../../x", "", "  ", "."}
	if got := MatchProtectedPaths(changed, []string{"docs/", "*", "**", "../docs/", ""}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestProtectedPaths_SplitAuthorized(t *testing.T) {
	hits := []string{"AGENT.md", "docs/a.md", "scripts/run.sh"}

	need, allowed := SplitAuthorized(hits, []string{"AGENT.md"})
	if !reflect.DeepEqual(allowed, []string{"AGENT.md"}) || !reflect.DeepEqual(need, []string{"docs/a.md", "scripts/run.sh"}) {
		t.Fatalf("file: need=%v allowed=%v", need, allowed)
	}

	need, allowed = SplitAuthorized(hits, []string{"docs/"})
	if !reflect.DeepEqual(allowed, []string{"docs/a.md"}) || !reflect.DeepEqual(need, []string{"AGENT.md", "scripts/run.sh"}) {
		t.Fatalf("dir: need=%v allowed=%v", need, allowed)
	}

	need, allowed = SplitAuthorized(hits, nil)
	if len(allowed) != 0 || !reflect.DeepEqual(need, hits) {
		t.Fatalf("none: need=%v allowed=%v", need, allowed)
	}
}

func TestProtectedPaths_SetterStoresNormalisedList(t *testing.T) {
	m, prd := newBL416Manager(t)
	rec := &driftAuditRecorder{}
	m.SetAuditFn(rec.fn)

	got, err := m.SetAuthorizedProtectedPaths(prd.ID,
		[]string{" ./AGENT.md ", "docs/**", "docs/", "scripts//", "", "AGENT.md", "./"}, "operator")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	want := []string{"AGENT.md", "docs/", "scripts/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if !reflect.DeepEqual(stored.AuthorizedProtectedPaths, want) {
		t.Fatalf("stored %v want %v", stored.AuthorizedProtectedPaths, want)
	}

	last := stored.Decisions[len(stored.Decisions)-1]
	if last.Kind != "protected_paths_authorised_set" || last.Actor != "operator" ||
		!strings.Contains(last.Note, "AGENT.md") || !strings.Contains(last.Note, "docs/") {
		t.Fatalf("decision %+v", last)
	}
	if len(rec.acts) != 1 || rec.acts[0] != "autonomous_protected_paths_authorised_set" {
		t.Fatalf("audit %v", rec.acts)
	}
	if rec.calls[0]["prd_id"] != prd.ID || rec.calls[0]["actor"] != "operator" {
		t.Fatalf("audit details %v", rec.calls[0])
	}
}

func TestProtectedPaths_SetterRejects(t *testing.T) {
	m, prd := newBL416Manager(t)
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "f" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + "/"
	}
	cases := []struct {
		name  string
		paths []string
		named string
	}{
		{"absolute", []string{"ok.md", "/etc/passwd"}, "/etc/passwd"},
		{"dotdot", []string{"docs/../../x"}, "docs/../../x"},
		{"leading dotdot", []string{"../x"}, "../x"},
		{"too long", []string{strings.Repeat("a", 257)}, "longer than 256"},
		{"too many", tooMany, "too many"},
	}
	for _, tc := range cases {
		_, err := m.SetAuthorizedProtectedPaths(prd.ID, tc.paths, "operator")
		if err == nil || !strings.Contains(err.Error(), tc.named) {
			t.Fatalf("%s: err %v, want it to mention %q", tc.name, err, tc.named)
		}
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if len(stored.AuthorizedProtectedPaths) != 0 {
		t.Fatalf("rejected input must not store: %v", stored.AuthorizedProtectedPaths)
	}
	// Boundary values are accepted.
	if _, err := m.SetAuthorizedProtectedPaths(prd.ID, []string{strings.Repeat("a", 256)}, "operator"); err != nil {
		t.Fatalf("256 bytes: %v", err)
	}
	hundred := make([]string, 100)
	for i := range hundred {
		hundred[i] = "d" + strings.Repeat("y", i) + "/"
	}
	if _, err := m.SetAuthorizedProtectedPaths(prd.ID, hundred, "operator"); err != nil {
		t.Fatalf("100 entries: %v", err)
	}
}

func TestProtectedPaths_SetterClearsOnEmpty(t *testing.T) {
	m, prd := newBL416Manager(t)
	rec := &driftAuditRecorder{}
	m.SetAuditFn(rec.fn)
	if _, err := m.SetAuthorizedProtectedPaths(prd.ID, []string{"docs/"}, "operator"); err != nil {
		t.Fatal(err)
	}
	got, err := m.SetAuthorizedProtectedPaths(prd.ID, nil, "operator")
	if err != nil || len(got) != 0 {
		t.Fatalf("clear: %v %v", got, err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if len(stored.AuthorizedProtectedPaths) != 0 {
		t.Fatalf("not cleared: %v", stored.AuthorizedProtectedPaths)
	}
	last := stored.Decisions[len(stored.Decisions)-1]
	if last.Kind != "protected_paths_authorised_set" || !strings.Contains(last.Note, "cleared") {
		t.Fatalf("decision %+v", last)
	}
	if len(rec.acts) != 2 {
		t.Fatalf("audit %v", rec.acts)
	}
}

func TestProtectedPaths_SetterUnknownID(t *testing.T) {
	m, _ := newBL416Manager(t)
	if _, err := m.SetAuthorizedProtectedPaths("nope", []string{"docs/"}, "operator"); err == nil {
		t.Fatal("want an error for an unknown Automaton id")
	}
}

func TestProtectedPaths_SetterNilAuditSafe(t *testing.T) {
	m, prd := newBL416Manager(t)
	if _, err := m.SetAuthorizedProtectedPaths(prd.ID, []string{"docs/"}, "operator"); err != nil {
		t.Fatalf("no audit sink: %v", err)
	}
}
