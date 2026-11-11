package autonomous

import (
	"strings"
	"testing"
)

func TestUnlandedWorkListTextRoundTrip(t *testing.T) {
	l, err := ParseUnlandedWorkList([]byte(`{"branches":[{"automaton_id":"a1","branch":"automaton/a1","title":"Add paging","commits":3,"last_activity":"2026-11-01T12:00:00Z","recommendation":"merge","reason":"nothing conflicts"},{"automaton_id":"a2","branch":"automaton/a2","commits":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := RenderUnlandedWorkList(l)
	for _, want := range []string{"(2):", "Add paging (automaton/a1)", "3 commits, last activity 2026-11-01 12:00 UTC",
		"recommended: merge (nothing conflicts)", "a2 (automaton/a2)", "1 commit\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("text lacks %q:\n%s", want, got)
		}
	}
}

func TestUnlandedWorkListTextEmptyAndBad(t *testing.T) {
	l, err := ParseUnlandedWorkList([]byte(`{}`))
	if err != nil || l.Branches == nil {
		t.Fatalf("err=%v branches=%v", err, l.Branches)
	}
	if got := RenderUnlandedWorkList(l); !strings.Contains(got, "No archived Automaton") {
		t.Errorf("empty text = %q", got)
	}
	if _, err := ParseUnlandedWorkList([]byte(`nope`)); err == nil {
		t.Error("bad JSON must fail")
	}
}

func TestRenderUnlandedActionResult(t *testing.T) {
	for _, tc := range []struct{ route, body, want string }{
		{"keep", `{"branch":"b"}`, "Kept b"},
		{"archive-branch", `{"branch":"b","archive_ref":"r"}`, "Archived b as r."},
		{"delete-worktree", `{"branch":"b","bundle_path":"p","worktree_path":"w"}`, "backup bundle: p (worktree w)."},
		{"delete-worktree", `{"branch":"b","bundle_path":"p"}`, "backup bundle: p."},
		{"keep", `not json`, "not json"},
	} {
		if got := RenderUnlandedActionResult(tc.route, []byte(tc.body)); !strings.Contains(got, tc.want) {
			t.Errorf("%s %s = %q, want %q", tc.route, tc.body, got, tc.want)
		}
	}
}
