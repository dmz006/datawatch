package autonomous

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// storedPRD returns the stored record of an Automaton.
func storedPRD(t *testing.T, st *Store, id string) *PRD {
	t.Helper()
	p, ok := st.GetPRD(id)
	if !ok {
		t.Fatalf("prd %q not stored", id)
	}
	return p
}

func TestPrimaryTagValidationAndClearing(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	b := mustTag(t, m, "b", "")
	mustPRD(t, st, "p1")

	primA := a.ID
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID, b.ID}, &primA); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	if got := storedPRD(t, st, "p1").PrimaryTagID; got != a.ID {
		t.Fatalf("primary = %q, want %q", got, a.ID)
	}

	// A primary that is not one of the Automaton's tags is refused, and the
	// stored primary and tags stay as they were.
	outside := b.ID
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID}, &outside); !errors.Is(err, ErrTagInvalid) {
		t.Fatalf("primary outside tags: err = %v, want ErrTagInvalid", err)
	}
	if got := storedPRD(t, st, "p1"); got.PrimaryTagID != a.ID || !reflect.DeepEqual(got.TagIDs, []string{a.ID, b.ID}) {
		t.Fatalf("refused primary changed the stored Automaton: primary=%q tags=%v", got.PrimaryTagID, got.TagIDs)
	}

	// An empty primary clears it and keeps the tags.
	empty := ""
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID, b.ID}, &empty); err != nil {
		t.Fatalf("clear primary: %v", err)
	}
	if got := storedPRD(t, st, "p1").PrimaryTagID; got != "" {
		t.Errorf("primary after empty = %q, want none", got)
	}

	// Removing the primary tag from the Automaton clears the primary.
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID, b.ID}, &primA); err != nil {
		t.Fatalf("set primary again: %v", err)
	}
	if err := m.SetPRDTags("p1", []string{b.ID}); err != nil {
		t.Fatalf("remove primary tag: %v", err)
	}
	if got := storedPRD(t, st, "p1").PrimaryTagID; got != "" {
		t.Errorf("primary after removing its tag = %q, want none", got)
	}

	// With no primary argument the primary survives while it stays a tag.
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID, b.ID}, &primA); err != nil {
		t.Fatalf("set primary third time: %v", err)
	}
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID, b.ID}, nil); err != nil {
		t.Fatalf("nil primary: %v", err)
	}
	if got := storedPRD(t, st, "p1").PrimaryTagID; got != a.ID {
		t.Errorf("nil primary kept %q, want %q", got, a.ID)
	}
}

func TestPrimaryTagDefaultsToNoneAndIsOmittedWhenEmpty(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	mustPRD(t, st, "p1")
	if err := m.SetPRDTags("p1", []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if got := storedPRD(t, st, "p1").PrimaryTagID; got != "" {
		t.Fatalf("default primary = %q, want none", got)
	}
	raw, err := json.Marshal(storedPRD(t, st, "p1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "primary_tag_id") {
		t.Errorf("empty primary is serialised: %s", raw)
	}

	primary := a.ID
	if err := m.SetPRDTagsAndPrimary("p1", []string{a.ID}, &primary); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(storedPRD(t, st, "p1"))
	if !strings.Contains(string(raw), `"primary_tag_id":"`+a.ID+`"`) {
		t.Errorf("set primary is not serialised as primary_tag_id: %s", raw)
	}
}

func TestSummaryViewCarriesTagsPrimaryAndCounts(t *testing.T) {
	prd := &PRD{
		ID:           "p1",
		TagIDs:       []string{"tag-a", "tag-b"},
		PrimaryTagID: "tag-a",
		Story: []Story{{ID: "s1", Tasks: []Task{
			{ID: "t1", Status: TaskPending},
			{ID: "t2", Status: TaskInProgress},
		}}},
	}
	view := BuildSummaryView(prd)
	if !reflect.DeepEqual(view.Tags, []string{"tag-a", "tag-b"}) {
		t.Errorf("summary tags = %v, want the tag IDs", view.Tags)
	}
	if view.PrimaryTagID != "tag-a" {
		t.Errorf("summary primary = %q, want tag-a", view.PrimaryTagID)
	}
	if view.StoryCount != 1 || view.TaskCount != 2 {
		t.Errorf("story_count=%d task_count=%d, want 1 and 2", view.StoryCount, view.TaskCount)
	}
	want := map[string]int{"pending": 1, "in_progress": 1}
	if !reflect.DeepEqual(view.TaskStatusCounts, want) {
		t.Errorf("task_status_counts = %v, want %v", view.TaskStatusCounts, want)
	}

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"tags", "primary_tag_id", "story_count", "task_count", "task_status_counts"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("summary JSON lacks %q: %s", k, raw)
		}
	}
}

// attentionFixture stores one Automaton per case and returns the ID of each.
func attentionFixture(t *testing.T, st *Store) {
	t.Helper()
	save := func(p *PRD) {
		if err := st.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	save(&PRD{ID: "p-blocked", Status: PRDStatus("blocked"), BlockedReason: "waiting on a token"})
	save(&PRD{ID: "p-paused", Status: PRDStatus("paused")})
	save(&PRD{ID: "p-approved", Status: PRDApproved})
	save(&PRD{ID: "p-running", Status: PRDRunning})
	save(&PRD{ID: "p-running-review", Status: PRDRunning, Story: []Story{{ID: "s1",
		Tasks: []Task{{ID: "t1", Status: TaskReviewNeeded}}}}})
}

// attentionIDs returns the IDs of the attention items, sorted.
func attentionIDs(t *testing.T, items []any) []string {
	t.Helper()
	var ids []string
	for _, x := range items {
		v, ok := x.(SummaryView)
		if !ok {
			t.Fatalf("attention item is %T, want SummaryView", x)
		}
		ids = append(ids, v.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestTagAttentionDefaultStatusesAndReviewNeeded(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	attentionFixture(t, st)

	statuses := ResolveAttentionStatuses(nil)
	if !reflect.DeepEqual(statuses, []string{"blocked", "needs_review", "paused"}) {
		t.Fatalf("default statuses = %v", statuses)
	}
	items := NewAPI(m).AttentionAutomata(statuses)
	want := []string{"p-blocked", "p-paused", "p-running-review"}
	if got := attentionIDs(t, items); !reflect.DeepEqual(got, want) {
		t.Errorf("attention items = %v, want %v", got, want)
	}
	for _, x := range items {
		if v := x.(SummaryView); v.ID == "p-blocked" && v.BlockedReason != "waiting on a token" {
			t.Errorf("blocked item blocked_reason = %q", v.BlockedReason)
		}
	}
}

func TestTagAttentionCustomStatusesSetting(t *testing.T) {
	var asked []string
	lookup := func(key string) ([]string, bool) {
		asked = append(asked, key)
		if key == AttentionStatusesKey {
			return []string{" Approved ", "approved", "", "running"}, true
		}
		return nil, false
	}
	got := ResolveAttentionStatuses(lookup)
	if want := []string{"approved", "running"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("custom statuses = %v, want %v", got, want)
	}
	if len(asked) != 1 || asked[0] != "autonomous.tags.attention_statuses" {
		t.Errorf("lookup keys = %v, want the attention statuses key", asked)
	}

	m, st, _ := newTagTestManager(t)
	attentionFixture(t, st)
	items := NewAPI(m).AttentionAutomata(got)
	want := []string{"p-approved", "p-running", "p-running-review"}
	if ids := attentionIDs(t, items); !reflect.DeepEqual(ids, want) {
		t.Errorf("custom attention items = %v, want %v", ids, want)
	}
}

func TestTagAttentionFallsBackToDefaultWhenKeyMissing(t *testing.T) {
	missing := func(string) ([]string, bool) { return nil, false }
	empty := func(string) ([]string, bool) { return []string{"  ", ""}, true }
	for name, lookup := range map[string]SettingLookup{"missing key": missing, "empty list": empty} {
		if got := ResolveAttentionStatuses(lookup); !reflect.DeepEqual(got, DefaultAttentionStatuses) {
			t.Errorf("%s: statuses = %v, want the default", name, got)
		}
	}
	// The returned slice is a copy: changing it leaves the default alone.
	got := ResolveAttentionStatuses(nil)
	got[0] = "changed"
	if DefaultAttentionStatuses[0] != "blocked" {
		t.Errorf("default statuses were modified through the returned slice")
	}
}

func TestTagAutomationTagsAreMemberUnion(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := mustTag(t, m, "a", "")
	b := mustTag(t, m, "b", "")
	c := mustTag(t, m, "c", "")
	for _, p := range []*PRD{
		{ID: "p1", TagIDs: []string{b.ID, a.ID}},
		{ID: "p2", TagIDs: []string{b.ID, c.ID}},
		{ID: "p3"},
	} {
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	auto := &Automation{ID: "auto1", Members: []AutomationMember{
		{ID: "m1", PRDID: "p1"}, {ID: "m2", PRDID: "p2"}, {ID: "m3", PRDID: "p3"},
		{ID: "m4", PRDID: "missing"},
	}}
	view := m.AutomationViewOf(auto)
	want := []string{a.ID, b.ID, c.ID}
	sort.Strings(want)
	if !reflect.DeepEqual(view.Tags, want) {
		t.Errorf("automation tags = %v, want the sorted union %v", view.Tags, want)
	}

	// The stored Automation carries no tags field of its own.
	raw, err := json.Marshal(auto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"tags"`) {
		t.Errorf("stored automation serialises tags: %s", raw)
	}

	untagged := m.AutomationViewOf(&Automation{ID: "auto2", Members: []AutomationMember{{ID: "m5", PRDID: "p3"}}})
	if untagged.Tags != nil {
		t.Errorf("automation with no tagged member tags = %v, want none", untagged.Tags)
	}
}
