package autonomous

import (
	"strings"
	"testing"
)

// A depends_on entry that names a title two or more stories or tasks of the
// plan share must reject the plan: picking one of them would give the plan a
// dependency nobody asked for.
func TestResolveDependsOnRejectsSharedTitle(t *testing.T) {
	cases := []struct {
		name    string
		stories []Story
		owner   string
	}{
		{
			name:  "two tasks of one story",
			owner: `task "Go"`,
			stories: []Story{{ID: "s-a", Title: "S", Tasks: []Task{
				{ID: "t-a", Title: "Setup"}, {ID: "t-b", Title: "Setup"},
				{ID: "t-c", Title: "Go", DependsOn: []string{"Setup"}},
			}}},
		},
		{
			name:  "tasks of two stories",
			owner: `task "Go"`,
			stories: []Story{
				{ID: "s-a", Title: "One", Tasks: []Task{{ID: "t-a", Title: "Setup"}}},
				{ID: "s-b", Title: "Two", Tasks: []Task{
					{ID: "t-b", Title: "Setup"},
					{ID: "t-c", Title: "Go", DependsOn: []string{" Setup "}},
				}},
			},
		},
		{
			name:  "two stories",
			owner: `story "Last"`,
			stories: []Story{
				{ID: "s-a", Title: "Setup"}, {ID: "s-b", Title: "Setup"},
				{ID: "s-c", Title: "Last", DependsOn: []string{"Setup"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolveDependsOn(tc.stories, map[string]*Story{}, map[string]*Task{})
			if err == nil {
				t.Fatal("the plan was accepted; a shared title must reject it")
			}
			for _, want := range []string{tc.owner, "Setup", "ambiguous"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q must contain %q", err, want)
				}
			}
		})
	}
}

// Shared titles are only a fault when an entry has to choose between them: an
// entry by id or position still resolves, and a title nobody else carries does.
func TestResolveDependsOnSharedTitlesStillResolvableOtherWays(t *testing.T) {
	stories := []Story{
		{ID: "s-a", Title: "Setup", Tasks: []Task{{ID: "t-a", Title: "Setup"}}},
		{ID: "s-b", Title: "Setup", DependsOn: []string{"s-a"}, Tasks: []Task{
			{ID: "t-b", Title: "Setup"},
			{ID: "t-c", Title: "Unique", DependsOn: []string{"t-a", "s1-t1"}},
			{ID: "t-d", Title: "Go", DependsOn: []string{"Unique"}},
		}},
	}
	if err := resolveDependsOn(stories, map[string]*Story{}, map[string]*Task{}); err != nil {
		t.Fatalf("resolveDependsOn: %v", err)
	}
	if got := stories[1].DependsOn; len(got) != 1 || got[0] != "s-a" {
		t.Errorf("story depends_on = %v, want [s-a]", got)
	}
	if got := stories[1].Tasks[1].DependsOn; len(got) != 2 || got[0] != "t-a" || got[1] != "t-a" {
		t.Errorf("id and positional entries = %v, want [t-a t-a]", got)
	}
	if got := stories[1].Tasks[2].DependsOn; len(got) != 1 || got[0] != "t-c" {
		t.Errorf("unique title = %v, want [t-c]", got)
	}
}

// The store path persists nothing of a rejected plan.
func TestSetStoriesRejectsSharedTitleAndStoresNothing(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prd, err := st.CreatePRD("spec", "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	err = st.SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "Setup"}, {Title: "Setup"}, {Title: "Go", DependsOn: []string{"Setup"}},
	}}})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("SetStories error = %v, want an ambiguous rejection", err)
	}
	got, ok := st.GetPRD(prd.ID)
	if !ok || len(got.Story) != 0 || len(st.tasks) != 0 || len(st.stories) != 0 {
		t.Fatalf("a rejected plan was stored: stories %d, task index %d, story index %d", len(got.Story), len(st.tasks), len(st.stories))
	}
}
