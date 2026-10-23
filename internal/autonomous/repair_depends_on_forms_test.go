// RepairDependsOn full-forms coverage: positional codes ("sN-tM", "sN"),
// bare numbers, sibling titles, and the rejection paths (unresolvable
// entry / dependency cycle) must name the exact bad entry and leave the
// store untouched — the same resolution + validation + message shapes as
// SetStories (they share resolveDependsOn).

package autonomous

import (
	"strings"
	"testing"
)

// persistedPRDFile returns the raw bytes of the store's prds.jsonl — the
// source of truth for "nothing was persisted": RepairDependsOn resolves
// entries on the shared in-memory slice before it can detect a cycle, so a
// rejected call may legitimately have left the in-memory copy touched; the
// file must not have changed at all (no persist() call on the error path).
func persistedPRDFile(t *testing.T, dir string) string {
	t.Helper()
	return persistedPRDRecords(t, dir)
}

// seedReprSeedPRD builds a PRD with explicit story/task IDs and raw
// (unresolved) DependsOn entries exactly as a pre-fix PRD would carry
// them, seeds the store maps, and persists. Returns the PRD id and the
// stable IDs of the first story/task for later assertions.
func seedRepairSeedPRD(t *testing.T, st *Store, stories []Story) string {
	t.Helper()
	prd, err := st.CreatePRD("spec", "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = stories
	for i := range prd.Story {
		st.stories[prd.Story[i].ID] = &prd.Story[i]
		for j := range prd.Story[i].Tasks {
			st.tasks[prd.Story[i].Tasks[j].ID] = &prd.Story[i].Tasks[j]
		}
	}
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd.ID
}

// TestStore_RepairDependsOn_ResolvesPositionalStoryAndTaskCodes seeds a
// PRD whose later story depends on the earlier one via "s1" and whose
// later task depends on the earlier story's task via "s1-t1" (1-based
// positional codes), then confirms repair rewrites them to the real IDs.
func TestStore_RepairDependsOn_ResolvesPositionalStoryAndTaskCodes(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	s1 := Story{ID: newID(), PRDID: "", Title: "S1"}
	s1.Tasks = []Task{{ID: newID(), Title: "T1", Spec: "do thing 1"}}
	s2 := Story{ID: newID(), PRDID: "", Title: "S2", DependsOn: []string{"s1"}}
	s2.Tasks = []Task{{ID: newID(), Title: "T2", Spec: "do thing 2", DependsOn: []string{"s1-t1"}}}
	prdID := seedRepairSeedPRD(t, st, []Story{s1, s2})

	got, err := st.RepairDependsOn(prdID)
	if err != nil {
		t.Fatalf("RepairDependsOn: %v", err)
	}
	if len(got.Story[1].DependsOn) != 1 || got.Story[1].DependsOn[0] != s1.ID {
		t.Fatalf("story S2's DependsOn = %v, want the s1 story's ID %q", got.Story[1].DependsOn, s1.ID)
	}
	if len(got.Story[1].Tasks[0].DependsOn) != 1 || got.Story[1].Tasks[0].DependsOn[0] != s1.Tasks[0].ID {
		t.Fatalf("task T2's DependsOn = %v, want the s1 task's ID %q", got.Story[1].Tasks[0].DependsOn, s1.Tasks[0].ID)
	}
}

// TestStore_RepairDependsOn_ResolvesPlainNumberCodes covers the bare
// numeric form: a bare N is the Nth story at both the story and the task
// level (here: "1" is the 1st story, and story 2's task depends on it).
func TestStore_RepairDependsOn_ResolvesPlainNumberCodes(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	s1 := Story{ID: newID(), PRDID: "", Title: "S1"}
	s1.Tasks = []Task{{ID: newID(), Title: "T1", Spec: "do thing 1"}}
	s2 := Story{ID: newID(), PRDID: "", Title: "S2"}
	s2.Tasks = []Task{{ID: newID(), Title: "T2", Spec: "do thing 2", DependsOn: []string{"1"}}}
	prdID := seedRepairSeedPRD(t, st, []Story{s1, s2})

	got, err := st.RepairDependsOn(prdID)
	if err != nil {
		t.Fatalf("RepairDependsOn: %v", err)
	}
	if len(got.Story[1].Tasks[0].DependsOn) != 1 || got.Story[1].Tasks[0].DependsOn[0] != s1.ID {
		t.Fatalf("task T2's DependsOn = %v, want plain '1' resolved to S1's ID %q", got.Story[1].Tasks[0].DependsOn, s1.ID)
	}
}

// TestStore_RepairDependsOn_StillResolvesTitles keeps the original
// pre-fix shape (raw sibling titles) covered alongside the positional
// forms — both must repair to real IDs.
func TestStore_RepairDependsOn_StillResolvesTitles(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	s1 := Story{ID: newID(), PRDID: "", Title: "S1"}
	s1.Tasks = []Task{{ID: newID(), Title: "T1", Spec: "do thing 1"}}
	s2 := Story{ID: newID(), PRDID: "", Title: "S2", DependsOn: []string{"S1"}}
	s2.Tasks = []Task{{ID: newID(), Title: "T2", Spec: "do thing 2", DependsOn: []string{"T1"}}}
	prdID := seedRepairSeedPRD(t, st, []Story{s1, s2})

	got, err := st.RepairDependsOn(prdID)
	if err != nil {
		t.Fatalf("RepairDependsOn: %v", err)
	}
	if len(got.Story[1].DependsOn) != 1 || got.Story[1].DependsOn[0] != s1.ID {
		t.Fatalf("story S2's DependsOn = %v, want S1's ID %q", got.Story[1].DependsOn, s1.ID)
	}
	if len(got.Story[1].Tasks[0].DependsOn) != 1 || got.Story[1].Tasks[0].DependsOn[0] != s1.Tasks[0].ID {
		t.Fatalf("task T2's DependsOn = %v, want T1's ID %q", got.Story[1].Tasks[0].DependsOn, s1.Tasks[0].ID)
	}
}

// TestStore_RepairDependsOn_UnresolvableEntryErrorsAndPersistsNothing
// plants an out-of-range positional code ("s9-t9") and confirms the call
// fails naming the exact raw entry while the persisted PRD is byte-for-
// byte what it was before: nothing half-resolved, raw entry still in
// place.
func TestStore_RepairDependsOn_UnresolvableEntryErrorsAndPersistsNothing(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	s1 := Story{ID: newID(), PRDID: "", Title: "S1"}
	s1.Tasks = []Task{{ID: newID(), Title: "T1", Spec: "do thing 1"}}
	s2 := Story{ID: newID(), PRDID: "", Title: "S2", DependsOn: []string{"s9-t9"}}
	prdID := seedRepairSeedPRD(t, st, []Story{s1, s2})
	beforeFile := persistedPRDFile(t, dir)
	before, _ := st.GetPRD(prdID)
	beforeRev := before.Rev

	_, err := st.RepairDependsOn(prdID)
	if err == nil {
		t.Fatalf("RepairDependsOn: want error for unresolvable entry %q, got nil", "s9-t9")
	}
	if !strings.Contains(err.Error(), "s9-t9") {
		t.Fatalf("error %q does not name the bad entry", err)
	}
	after, ok := st.GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD disappeared after a rejected repair")
	}
	if len(after.Story) != 2 || len(after.Story[1].DependsOn) != 1 || after.Story[1].DependsOn[0] != "s9-t9" {
		t.Fatalf("stored PRD changed after a rejected repair: %+v", after.Story)
	}
	if after.Rev != beforeRev {
		t.Fatalf("persisted state moved (Rev %d -> %d) even though repair rejected the plan", beforeRev, after.Rev)
	}
	if got := persistedPRDFile(t, dir); got != beforeFile {
		t.Fatalf("prds.jsonl changed after a rejected repair:\nbefore: %s\nafter:  %s", beforeFile, got)
	}
}

// TestStore_RepairDependsOn_CycleErrorsAndPersistsNothing plants two
// tasks that depend on each other (a genuine in-plan cycle that resolves
// fine to real IDs) and confirms the call fails naming the cycle with the
// stored state untouched.
func TestStore_RepairDependsOn_CycleErrorsAndPersistsNothing(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	s1 := Story{ID: newID(), PRDID: "", Title: "S1"}
	a := Task{ID: newID(), Title: "A", Spec: "task a", DependsOn: []string{"B"}} // B is the sibling task's title
	b := Task{ID: newID(), Title: "B", Spec: "task b", DependsOn: []string{"A"}}
	s1.Tasks = []Task{a, b}
	prdID := seedRepairSeedPRD(t, st, []Story{s1})
	beforeFile := persistedPRDFile(t, dir)
	beforeRev, ok := st.PRDRev(prdID)
	if !ok {
		t.Fatalf("PRD not in store after seeding")
	}

	_, err := st.RepairDependsOn(prdID)
	if err == nil {
		t.Fatalf("RepairDependsOn: want cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error %q does not say the plan is cyclic", err)
	}
	// Both real IDs must appear: the error names the cycle members.
	if !strings.Contains(err.Error(), a.ID) || !strings.Contains(err.Error(), b.ID) {
		t.Fatalf("error %q does not name the cycle members %q / %q", err, a.ID, b.ID)
	}
	after, ok := st.GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD disappeared after a rejected repair")
	}
	if len(after.Story) != 1 || len(after.Story[0].Tasks) != 2 {
		t.Fatalf("stored structure changed after a rejected repair: %+v", after.Story)
	}
	if rev, _ := st.PRDRev(prdID); rev != beforeRev {
		t.Fatalf("persisted state moved (Rev %d -> %d) even though repair rejected the plan", beforeRev, rev)
	}
	if got := persistedPRDFile(t, dir); got != beforeFile {
		t.Fatalf("prds.jsonl changed after a rejected cycle repair:\nbefore: %s\nafter:  %s", beforeFile, got)
	}
}
