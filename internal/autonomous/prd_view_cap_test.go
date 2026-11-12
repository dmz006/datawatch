package autonomous

import (
	"strings"
	"testing"
)

// TestCapPRDTextKeepsWriteBackFieldsWhole is the read-then-save-back
// guarantee: the fields a write accepts verbatim (PRD spec, story
// description, task spec) come back whole and unmarked from CapPRDText,
// while the heavy output fields keep the cap and land in the truncated list.
func TestCapPRDTextKeepsWriteBackFieldsWhole(t *testing.T) {
	spec := strings.Repeat("0123456789", 1000) // 10000 bytes, comfortably over the 4096 cap
	if len(spec) != 10000 {
		t.Fatalf("fixture: spec is %d bytes, want 10000", len(spec))
	}
	heavy := strings.Repeat("heavy-output-", 900)
	prd := &PRD{
		ID: "w00001", Spec: spec, MemoryReport: heavy,
		Story: []Story{{
			ID: "s1", PRDID: "w00001", Description: strings.Repeat("story-desc-", 900),
			Tasks: []Task{{
				ID: "t1", StoryID: "s1", PRDID: "w00001", Status: TaskCompleted,
				Spec: spec, Error: heavy,
			}},
		}},
	}
	got := CapPRDText(prd, 4096)
	if got == prd {
		t.Fatal("CapPRDText must return a copy")
	}

	// Write-back fields: whole, no marker, no truncated entry for them.
	for name, pair := range map[string][2]string{
		"prd spec":          {prd.Spec, got.Spec},
		"story description": {prd.Story[0].Description, got.Story[0].Description},
		"task spec":         {prd.Story[0].Tasks[0].Spec, got.Story[0].Tasks[0].Spec},
	} {
		if gotSpec, want := pair[1], pair[0]; gotSpec != want {
			t.Errorf("%s: %d bytes, want the whole %d bytes", name, len(gotSpec), len(want))
		}
		if strings.Contains(got.Spec, truncationMarker) {
			t.Error("prd spec: carries a truncation marker")
		}
	}
	if got.Story[0].Tasks[0].Spec != spec || strings.Contains(got.Story[0].Tasks[0].Spec, truncationMarker) {
		t.Error("task spec must be returned whole with no marker")
	}
	for _, f := range got.Truncated {
		if f.Field == "spec" || f.Field == "description" {
			t.Errorf("write-back field %q in prd truncated list", f.Field)
		}
	}
	if len(got.Story[0].Truncated) != 0 {
		t.Errorf("story carries truncated notes for a write-back field: %+v", got.Story[0].Truncated)
	}

	// Heavy fields: still capped, still listed.
	if got.MemoryReport == heavy || !strings.Contains(got.MemoryReport, truncationMarker) {
		t.Error("memory report must keep the cap")
	}
	if len(got.Truncated) != 1 || got.Truncated[0].Field != "memory_report" || got.Truncated[0].Ref != OutputsRef("w00001", "") {
		t.Errorf("prd truncated notes: %+v", got.Truncated)
	}
	tk := got.Story[0].Tasks[0]
	if tk.Error == heavy || !strings.Contains(tk.Error, truncationMarker) {
		t.Error("task error must keep the cap")
	}
	if len(tk.Truncated) != 1 || tk.Truncated[0].Field != "error" || tk.Truncated[0].Ref != OutputsRef("w00001", "t1") {
		t.Errorf("task truncated notes: %+v", tk.Truncated)
	}

	// The stored record is untouched.
	if prd.Spec != spec || prd.MemoryReport != heavy || len(prd.Truncated) != 0 ||
		len(prd.Story[0].Truncated) != 0 || len(prd.Story[0].Tasks[0].Truncated) != 0 {
		t.Error("CapPRDText mutated the stored PRD")
	}
}

// TestCapPRDTextMaxOff keeps the escape hatches: 0 and negative leave the
// record (and its capped fields) unchanged.
func TestCapPRDTextMaxOff(t *testing.T) {
	prd := &PRD{ID: "w00002", Spec: strings.Repeat("s", 10000), MemoryReport: strings.Repeat("m", 10000),
		Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Error: strings.Repeat("e", 10000)}}}}}
	for _, max := range []int{0, -1} {
		got := CapPRDText(prd, max)
		if got.MemoryReport != prd.MemoryReport || len(got.Truncated) != 0 ||
			got.Story[0].Tasks[0].Error != prd.Story[0].Tasks[0].Error {
			t.Errorf("max %d: record must be left unchanged", max)
		}
	}
}

// TestHasTruncationMarker covers the marker at the end (the shape capText
// appends), the marker followed by more text (a copy of a capped view with
// notes added after the cut), and clean text that must not trip it.
func TestHasTruncationMarker(t *testing.T) {
	capped, cut := capText("abcdefghijklmnopqrst", 10)
	if !cut {
		t.Fatal("expected capText to cut")
	}
	if !strings.Contains(capped, truncationMarker) {
		t.Fatal("capped text must carry the marker")
	}
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"marker at end", capped, true},
		{"marker mid-text", capped + "\noperator notes added after the cut", true},
		{"marker buried", "line one\n" + capped + "\nline three", true},
		{"clean text long enough to pass the cap", strings.Repeat("a", 5000), false},
		{"empty", "", false},
		{"lookalike without the ellipsis prefix", "[truncated: kept 10 of 20 bytes]", false},
	}
	for _, tc := range cases {
		if got := HasTruncationMarker(tc.in); got != tc.want {
			t.Errorf("%s: HasTruncationMarker = %v, want %v", tc.name, got, tc.want)
		}
	}
}
