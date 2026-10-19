// SetStories depends_on resolution and rejection tests: positional codes
// (sN-tM / sN / bare number), titles, unresolvable entries, and cycles.
package autonomous

import (
	"strings"
	"testing"
)

// basePlan is the shared shape for the resolution table: two stories, the
// first with two tasks. Stored order is authoritative for positional codes
// (1-based).
func basePlan() []Story {
	return []Story{
		{
			Title: "S1",
			Tasks: []Task{
				{Title: "A1", Spec: "first task"},
				{Title: "A2", Spec: "second task"},
			},
		},
		{
			Title: "S2",
			Tasks: []Task{{Title: "B1", Spec: "third task"}},
		},
	}
}

// TestSetStoriesPositionalDependsOnResolution covers the positional-code
// interpretation: "sN-tM" resolves to the Mth task of the Nth story, "sN"
// to the Nth story, and a bare integer N to the Nth story at BOTH the story
// and the task level (a task may depend on an earlier story). Titles keep
// resolving too. Whitespace around entries is trimmed before matching.
func TestSetStoriesPositionalDependsOnResolution(t *testing.T) {
	tests := []struct {
		name      string
		owner     string // "S2" (story level) or "B1" (task level)
		value     string // the raw DependsOn entry under test
		wantOwner string // title of the entity the entry must resolve to
	}{
		{name: "story level sN-tM", owner: "S2", value: "s1-t1", wantOwner: "A1"},
		{name: "task level sN-tM", owner: "B1", value: "s1-t2", wantOwner: "A2"},
		{name: "story level sN", owner: "S2", value: "s1", wantOwner: "S1"},
		{name: "task level sN", owner: "B1", value: "s1", wantOwner: "S1"},
		{name: "story level bare number", owner: "S2", value: "1", wantOwner: "S1"},
		{name: "task level bare number", owner: "B1", value: "1", wantOwner: "S1"},
		{name: "story level title", owner: "S2", value: "S1", wantOwner: "S1"},
		{name: "task level title", owner: "B1", value: "A2", wantOwner: "A2"},
		{name: "entry with surrounding whitespace", owner: "B1", value: "  s1-t1  ", wantOwner: "A1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := NewStore(dir)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			prd, err := st.CreatePRD("spec", "/p", "", "", "")
			if err != nil {
				t.Fatalf("CreatePRD: %v", err)
			}
			stories := basePlan()
			if tc.owner == "S2" {
				stories[1].DependsOn = []string{tc.value}
			} else {
				stories[1].Tasks[0].DependsOn = []string{tc.value}
			}
			if err := st.SetStories(prd.ID, stories); err != nil {
				t.Fatalf("SetStories: %v", err)
			}
			got, ok := st.GetPRD(prd.ID)
			if !ok {
				t.Fatalf("PRD missing: %s", prd.ID)
			}

			want := ""
			for i := range got.Story {
				if got.Story[i].Title == tc.wantOwner {
					want = got.Story[i].ID
				}
				for j := range got.Story[i].Tasks {
					if got.Story[i].Tasks[j].Title == tc.wantOwner {
						want = got.Story[i].Tasks[j].ID
					}
				}
			}
			if want == "" {
				t.Fatalf("expected resolved target %q not found in persisted plan", tc.wantOwner)
			}

			var actual []string
			if tc.owner == "S2" {
				actual = got.Story[1].DependsOn
			} else {
				actual = got.Story[1].Tasks[0].DependsOn
			}
			if len(actual) != 1 || actual[0] != want {
				t.Fatalf("%q on %s resolved to %v, want %q", tc.value, tc.owner, actual, want)
			}
		})
	}
}

// TestSetStoriesUnresolvableDependsOnRejected covers the rejection cases:
// positional codes out of range for this plan and unknown titles both
// reject the whole plan with the raw entry named, and nothing is persisted.
func TestSetStoriesUnresolvableDependsOnRejected(t *testing.T) {
	tests := []struct {
		name  string
		owner string // "S2" (story level) or "B1" (task level)
		value string
	}{
		{name: "out-of-range sN at story level", owner: "S2", value: "s5"},
		{name: "out-of-range sN-tM story number", owner: "B1", value: "s5-t2"},
		{name: "out-of-range sN-tM task number", owner: "B1", value: "s1-t9"},
		{name: "out-of-range bare number", owner: "S2", value: "7"},
		{name: "unknown title at task level", owner: "B1", value: "such a task"},
		{name: "unknown title at story level", owner: "S2", value: "no such story"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := NewStore(dir)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			prd, err := st.CreatePRD("spec", "/p", "", "", "")
			if err != nil {
				t.Fatalf("CreatePRD: %v", err)
			}
			stories := basePlan()
			if tc.owner == "S2" {
				stories[1].DependsOn = []string{tc.value}
			} else {
				stories[1].Tasks[0].DependsOn = []string{tc.value}
			}

			err = st.SetStories(prd.ID, stories)
			if err == nil {
				t.Fatalf("SetStories: want rejection for %q on %s, got nil", tc.value, tc.owner)
			}
			if !strings.Contains(err.Error(), tc.value) {
				t.Fatalf("error %q does not name the raw entry %q", err, tc.value)
			}
			got, ok := st.GetPRD(prd.ID)
			if !ok {
				t.Fatalf("PRD missing after rejection: %s", prd.ID)
			}
			if len(got.Story) != 0 {
				t.Fatalf("nothing may be persisted after rejection, got %d stories: %+v", len(got.Story), got.Story)
			}
		})
	}
}

// TestSetStoriesDependsOnCycleRejected covers cycle rejection. IDs are
// pre-assigned (SetStories only fills empty IDs) so the test can assert the
// cycle members are named in the error.
func TestSetStoriesDependsOnCycleRejected(t *testing.T) {
	t.Run("two tasks each depend on the other", func(t *testing.T) {
		dir := t.TempDir()
		st, _ := NewStore(dir)
		prd, _ := st.CreatePRD("spec", "/p", "", "", "")
		stories := []Story{{
			Title: "S1",
			Tasks: []Task{
				{ID: "task-circle-a", Title: "TA", Spec: "a", DependsOn: []string{"TB"}},
				{ID: "task-circle-b", Title: "TB", Spec: "b", DependsOn: []string{"TA"}},
			},
		}}
		err := st.SetStories(prd.ID, stories)
		if err == nil {
			t.Fatalf("SetStories: want cycle rejection, got nil")
		}
		for _, member := range []string{"task-circle-a", "task-circle-b"} {
			if !strings.Contains(err.Error(), member) {
				t.Fatalf("cycle error %q does not name member %q", err, member)
			}
		}
		got, _ := st.GetPRD(prd.ID)
		if len(got.Story) != 0 {
			t.Fatalf("nothing may be persisted after cycle rejection: %+v", got.Story)
		}
	})

	t.Run("story depends on task which depends back on the story", func(t *testing.T) {
		dir := t.TempDir()
		st, _ := NewStore(dir)
		prd, _ := st.CreatePRD("spec", "/p", "", "", "")
		stories := []Story{
			{
				ID: "story-first",
				// task T1 depends on story S2 (a task may depend on an
				// earlier/later story); the first story itself has no deps.
				Title: "S1",
				Tasks: []Task{{
					ID:        "task-first",
					Title:     "T1",
					Spec:      "a",
					DependsOn: []string{"2"}, // -> story S2
				}},
			},
			{
				ID:        "story-loop",
				Title:     "S2",
				DependsOn: []string{"s2-t1"}, // -> its own sibling task T2
				Tasks: []Task{{
					ID:        "task-loop",
					Title:     "T2",
					Spec:      "b",
					DependsOn: []string{"2"}, // -> story S2, closing the loop
				}},
			},
		}
		err := st.SetStories(prd.ID, stories)
		if err == nil {
			t.Fatalf("SetStories: want cycle rejection, got nil")
		}
		for _, member := range []string{"story-loop", "task-loop"} {
			if !strings.Contains(err.Error(), member) {
				t.Fatalf("cycle error %q does not name member %q", err, member)
			}
		}
		got, _ := st.GetPRD(prd.ID)
		if len(got.Story) != 0 {
			t.Fatalf("nothing may be persisted after cycle rejection: %+v", got.Story)
		}
	})
}
