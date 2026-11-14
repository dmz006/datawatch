package planrecords

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func releasePlan(t *testing.T, s *Store, title, release string) Record {
	t.Helper()
	return mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: title, PlanKind: PlanKindRelease, Release: release})
}

// themedPlan is a release plan holding three Automata and two Automations.
func themedPlan(t *testing.T, s *Store) Record {
	t.Helper()
	p := releasePlan(t, s, "The 10.0 release", "10.0")
	for i, id := range []string{"aaaa0001", "aaaa0002", "aaaa0004"} {
		if _, err := s.AttachAutomaton(p.ID, id, LinkInfo{Title: fmt.Sprintf("Auto %d", i+1), State: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"bbbb0001", "bbbb0002"} {
		if _, err := s.AttachAutomation(p.ID, id, LinkInfo{Title: "Automation " + id, State: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func themeNames(r Record) []string {
	var out []string
	for _, th := range r.Themes {
		out = append(out, th.Name)
	}
	return out
}

func mustTheme(t *testing.T, s *Store, plan, name string) Theme {
	t.Helper()
	_, th, err := s.CreateTheme(plan, name)
	if err != nil {
		t.Fatalf("create theme %q: %v", name, err)
	}
	return th
}

func isInvalid(err error) bool {
	var inv *ErrInvalid
	return errors.As(err, &inv)
}

func TestPlanKindMarkerValidatedAndRoundTripped(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	rel := releasePlan(t, s, "Ten", "  10.0 ")
	if rel.PlanKind != PlanKindRelease || rel.Release != "10.0" {
		t.Fatalf("marker not kept: %+v", rel)
	}
	ord := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Ordinary"})
	if ord.PlanKind != PlanKindOrdinary || ord.Release != "" || ord.Themes != nil {
		t.Fatalf("ordinary plan carries release fields: %+v", ord)
	}

	long := strings.Repeat("r", MaxReleaseRunes+1)
	cases := []struct {
		name string
		rec  Record
	}{
		{"unknown marker", Record{Kind: KindPlan, Project: "datawatch", Title: "a", PlanKind: "weird"}},
		{"label on an ordinary plan", Record{Kind: KindPlan, Project: "datawatch", Title: "b", Release: "9.0"}},
		{"label over the cap", Record{Kind: KindPlan, Project: "datawatch", Title: "d", PlanKind: PlanKindRelease, Release: long}},
		{"marker on a registry row", Record{Kind: KindRegistryRow, Project: "datawatch", Title: "e", PlanKind: PlanKindRelease}},
		{"marker on a finding", Record{Kind: KindFinding, Project: "datawatch", Title: "f", PlanKind: PlanKindRelease}},
		{"themes at creation", Record{Kind: KindPlan, Project: "datawatch", Title: "g", PlanKind: PlanKindRelease,
			Themes: []Theme{{ID: "t-1", Name: "x"}}}},
	}
	for _, c := range cases {
		if _, err := s.Create(c.rec); !isInvalid(err) {
			t.Errorf("%s: want ErrInvalid, got %v", c.name, err)
		}
	}
	// A blank label is no label: it is trimmed away, so the plan has none yet.
	if blank, err := s.Create(Record{Kind: KindPlan, Project: "datawatch", Title: "c", PlanKind: PlanKindRelease, Release: "  "}); err != nil || blank.Release != "" {
		t.Fatalf("blank label: %+v %v", blank, err)
	}
	if err := (&Record{ID: "x", UID: "u", Kind: KindPlan, Project: "datawatch", State: StateOpen, Title: "c", PlanKind: PlanKindRelease, Release: "  "}).validate(); !isInvalid(err) {
		t.Fatalf("a stored blank label must fail validation: %v", err)
	}
	// A label at the cap is accepted: reject, never truncate.
	if _, err := s.Create(Record{Kind: KindPlan, Project: "datawatch", Title: "cap", PlanKind: PlanKindRelease,
		Release: strings.Repeat("r", MaxReleaseRunes)}); err != nil {
		t.Fatalf("label at the cap refused: %v", err)
	}

	// The marker survives a reopen and the file keeps the JSON names.
	re, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := re.Get(rel.ID)
	if err != nil || got.PlanKind != PlanKindRelease || got.Release != "10.0" {
		t.Fatalf("reopen lost the marker: %+v %v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "plan-records", "plan", rel.UID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["plan_kind"] != "release" || m["release"] != "10.0" {
		t.Fatalf("file lacks plan_kind/release: %s", raw)
	}
}

func TestPlanKindPatch(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Becomes a release"})
	kind, label := PlanKindRelease, " 11.0 "
	got, err := s.Update(p.ID, Patch{PlanKind: &kind, Release: &label, Actor: "operator"})
	if err != nil || got.PlanKind != PlanKindRelease || got.Release != "11.0" {
		t.Fatalf("patch marker: %+v %v", got, err)
	}
	last := (*evs)[len(*evs)-1]
	if last.action != AuditUpdated || !reflect.DeepEqual(last.details["changed"], []string{"plan_kind", "release"}) {
		t.Fatalf("audit lacks the changed marker fields: %+v", last)
	}
	if found, ok := s.ReleasePlan("datawatch", "11.0"); !ok || found.UID != p.UID {
		t.Fatalf("ReleasePlan: %+v %v", found, ok)
	}
	if sums := s.ReleasePlans("datawatch"); len(sums) != 1 || sums[0].Release != "11.0" || sums[0].PlanKind != PlanKindRelease {
		t.Fatalf("ReleasePlans: %+v", sums)
	}
	if _, ok := s.ReleasePlan("datawatch", "  "); ok {
		t.Fatal("a blank release must find nothing")
	}

	bad := PlanKind("bogus")
	if _, err := s.Update(p.ID, Patch{PlanKind: &bad}); !isInvalid(err) {
		t.Fatalf("bad marker patch: %v", err)
	}
	// A label with no marker change, on a release plan, is fine; clearing the
	// marker while a label is set is refused, clearing both works.
	ord := PlanKindOrdinary
	if _, err := s.Update(p.ID, Patch{PlanKind: &ord}); !isInvalid(err) {
		t.Fatalf("clearing the marker but keeping the label must fail: %v", err)
	}
	empty := ""
	got, err = s.Update(p.ID, Patch{PlanKind: &ord, Release: &empty})
	if err != nil || got.PlanKind != PlanKindOrdinary || got.Release != "" {
		t.Fatalf("clear marker and label: %+v %v", got, err)
	}
	if _, ok := s.ReleasePlan("datawatch", "11.0"); ok {
		t.Fatal("an ordinary plan must not be found as a release plan")
	}
	if got2, err := s.Update(p.ID, Patch{PlanKind: &ord}); err != nil || got2.UpdatedAt != got.UpdatedAt {
		t.Fatalf("an unchanged marker must be a no-op: %v", err)
	}
}

func TestOneReleasePlanPerRelease(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	first := releasePlan(t, s, "Ten", "10.0")
	_, err := s.Create(Record{Kind: KindPlan, Project: "datawatch", Title: "Ten again", PlanKind: PlanKindRelease, Release: " 10.0"})
	var rc *ErrReleaseConflict
	if !errors.As(err, &rc) || rc.Existing.UID != first.UID {
		t.Fatalf("second release plan for 10.0 must conflict, got %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, `"10.0"`) || !strings.Contains(msg, first.ID) {
		t.Fatalf("conflict message: %s", msg)
	}
	// Another project, another label and an ordinary plan are all fine.
	if _, err := s.Create(Record{Kind: KindPlan, Project: "other", Title: "Ten", PlanKind: PlanKindRelease, Release: "10.0"}); err != nil {
		t.Fatalf("other project: %v", err)
	}
	other := releasePlan(t, s, "Eleven", "11.0")
	label := "10.0"
	if _, err := s.Update(other.ID, Patch{Release: &label}); !errors.As(err, &rc) {
		t.Fatalf("patching onto a taken label must conflict, got %v", err)
	}
	same := "10.0"
	if _, err := s.Update(first.ID, Patch{Release: &same}); err != nil {
		t.Fatalf("keeping its own label: %v", err)
	}
}

func TestThemeLifecycle(t *testing.T) {
	s, dir, evs, _ := newTestStore(t, nil)
	p := themedPlan(t, s)

	core := mustTheme(t, s, p.ID, "Core engine")
	ui := mustTheme(t, s, p.ID, "  UI polish ")
	ops := mustTheme(t, s, p.ID, "Operations")
	if core.ID == ui.ID || core.Name != "Core engine" || ui.Name != "UI polish" {
		t.Fatalf("themes: %+v %+v", core, ui)
	}
	got, _ := s.Get(p.ID)
	if !reflect.DeepEqual(themeNames(got), []string{"Core engine", "UI polish", "Operations"}) ||
		got.Themes[0].Order != 1 || got.Themes[2].Order != 3 {
		t.Fatalf("themes after create: %+v", got.Themes)
	}
	if _, _, err := s.CreateTheme(p.ID, "core ENGINE"); !isInvalid(err) {
		t.Fatalf("duplicate name must be refused: %v", err)
	}

	// Rename by id and by name; a taken name is refused; same name is a no-op.
	if _, err := s.RenameTheme(p.ID, ui.ID, "Interface"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameTheme(p.ID, "operations", "Ops"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameTheme(p.ID, ui.ID, "core engine"); !isInvalid(err) {
		t.Fatalf("rename onto a taken name: %v", err)
	}
	if _, err := s.RenameTheme(p.ID, "nope", "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename of an unknown theme: %v", err)
	}
	before, _ := s.Get(p.ID)
	if again, err := s.RenameTheme(p.ID, core.ID, "Core engine"); err != nil || again.UpdatedAt != before.UpdatedAt {
		t.Fatalf("same-name rename must change nothing: %v", err)
	}

	// Place.
	for _, pl := range []struct{ theme, kind, id string }{
		{core.ID, ItemAutomaton, "aaaa0001"},
		{core.ID, ItemAutomaton, "aaaa0002"},
		{core.ID, ItemAutomation, "bbbb0001"},
		{ui.ID, ItemAutomaton, "aaaa0004"},
	} {
		if _, err := s.PlaceInTheme(p.ID, pl.theme, pl.kind, pl.id); err != nil {
			t.Fatalf("place %+v: %v", pl, err)
		}
	}
	v, err := s.ThemesOf(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Themes) != 3 || v.Themes[0].Name != "Core engine" || len(v.Themes[0].Items) != 3 {
		t.Fatalf("themes view: %+v", v.Themes)
	}
	if it := v.Themes[0].Items[0]; it.Kind != ItemAutomaton || it.ID != "aaaa0001" || it.Title != "Auto 1" || it.State != "running" {
		t.Fatalf("item title/state from link info: %+v", it)
	}
	if it := v.Themes[0].Items[2]; it.Kind != ItemAutomation || it.ID != "bbbb0001" || it.State != "active" {
		t.Fatalf("automation item: %+v", it)
	}
	if len(v.Unplaced) != 1 || v.Unplaced[0].ID != "bbbb0002" || v.Unplaced[0].Title != "Automation bbbb0002" {
		t.Fatalf("unplaced: %+v", v.Unplaced)
	}
	if v.Release != "10.0" || v.PlanKind != PlanKindRelease || v.Plan.ID != p.ID {
		t.Fatalf("view header: %+v", v)
	}

	// Moving an item places it in one theme only.
	moved, err := s.PlaceInTheme(p.ID, ui.ID, ItemAutomaton, "aaaa0001")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(moved.Themes[0].Automata, []string{"aaaa0002"}) ||
		!reflect.DeepEqual(moved.Themes[1].Automata, []string{"aaaa0004", "aaaa0001"}) {
		t.Fatalf("move: %+v", moved.Themes)
	}
	if err := moved.validate(); err != nil {
		t.Fatalf("moved plan must validate: %v", err)
	}
	if again, err := s.PlaceInTheme(p.ID, ui.ID, ItemAutomaton, "aaaa0001"); err != nil || again.UpdatedAt != moved.UpdatedAt {
		t.Fatalf("placing where it already is must change nothing: %v", err)
	}

	// Reorder items in a theme.
	re, err := s.ReorderThemeItems(p.ID, "interface", ItemAutomaton, []string{"aaaa0001", "aaaa0004"})
	if err != nil || !reflect.DeepEqual(re.Themes[1].Automata, []string{"aaaa0001", "aaaa0004"}) {
		t.Fatalf("reorder items: %+v %v", re.Themes, err)
	}
	for name, ids := range map[string][]string{
		"missing one": {"aaaa0001"}, "foreign item": {"aaaa0001", "aaaa0002"}, "duplicate": {"aaaa0001", "aaaa0001"},
	} {
		if _, err := s.ReorderThemeItems(p.ID, ui.ID, ItemAutomaton, ids); !isInvalid(err) {
			t.Errorf("reorder items (%s): want ErrInvalid, got %v", name, err)
		}
	}

	// Reorder themes.
	rt, err := s.ReorderThemes(p.ID, []string{ops.ID, core.ID, "Interface"})
	if err != nil || !reflect.DeepEqual(themeNames(rt), []string{"Ops", "Core engine", "Interface"}) ||
		rt.Themes[0].Order != 1 || rt.Themes[2].Order != 3 {
		t.Fatalf("reorder themes: %+v %v", rt.Themes, err)
	}
	for name, refs := range map[string][]string{
		"partial": {ops.ID, core.ID}, "duplicate": {ops.ID, ops.ID, core.ID}, "unknown": {ops.ID, core.ID, "ghost"},
	} {
		if _, err := s.ReorderThemes(p.ID, refs); err == nil {
			t.Errorf("reorder themes (%s) must fail", name)
		}
	}
	if same, err := s.ReorderThemes(p.ID, []string{ops.ID, core.ID, ui.ID}); err != nil || same.UpdatedAt != rt.UpdatedAt {
		t.Fatalf("same order must change nothing: %v", err)
	}

	// Unplace keeps the item in the plan.
	un, err := s.UnplaceFromTheme(p.ID, ItemAutomaton, "aaaa0002")
	if err != nil || len(un.Themes[1].Automata) != 0 || indexOf(un.Automata, "aaaa0002") < 0 {
		t.Fatalf("unplace: %+v %v", un, err)
	}
	if _, err := s.UnplaceFromTheme(p.ID, ItemAutomaton, "aaaa0002"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unplacing an unplaced item: %v", err)
	}

	// Removing a theme leaves its items in the plan, unplaced.
	rm, err := s.RemoveTheme(p.ID, "interface")
	if err != nil || !reflect.DeepEqual(themeNames(rm), []string{"Ops", "Core engine"}) || rm.Themes[1].Order != 2 {
		t.Fatalf("remove theme: %+v %v", rm.Themes, err)
	}
	if !reflect.DeepEqual(rm.Automata, p.Automata) || !reflect.DeepEqual(rm.Automations, p.Automations) {
		t.Fatalf("removing a theme detached items: %+v", rm)
	}
	v, _ = s.ThemesOf(p.ID)
	var unplaced []string
	for _, it := range v.Unplaced {
		unplaced = append(unplaced, it.ID)
	}
	if !reflect.DeepEqual(unplaced, []string{"aaaa0001", "aaaa0002", "aaaa0004", "bbbb0002"}) {
		t.Fatalf("unplaced after remove: %v", unplaced)
	}
	if _, err := s.RemoveTheme(p.ID, "interface"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing twice: %v", err)
	}
	for _, th := range []string{ops.ID, core.ID} {
		if _, err := s.RemoveTheme(p.ID, th); err != nil {
			t.Fatal(err)
		}
	}
	if last, _ := s.Get(p.ID); last.Themes != nil {
		t.Fatalf("no themes left, want nil: %+v", last.Themes)
	}

	// Every write sent one theme audit event with its operation.
	ops2 := map[string]int{}
	for _, e := range *evs {
		if e.action == AuditThemeChanged {
			ops2[e.details["theme_op"].(string)]++
			if e.details["actor"] != "system" || e.details["kind"] != "plan" {
				t.Errorf("audit details: %+v", e.details)
			}
		}
	}
	want := map[string]int{"create": 3, "rename": 2, "place": 5, "reorder_items": 1, "reorder": 1, "unplace": 1, "remove": 3}
	if !reflect.DeepEqual(ops2, want) {
		t.Fatalf("theme audit counts %v, want %v", ops2, want)
	}
	if _, err := Open(dir, nil); err != nil {
		t.Fatal(err)
	}
}

func TestThemesSurviveReopen(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := themedPlan(t, s)
	th := mustTheme(t, s, p.ID, "Core")
	if _, err := s.PlaceInTheme(p.ID, th.ID, ItemAutomaton, "aaaa0001"); err != nil {
		t.Fatal(err)
	}
	re, err := Open(dir, nil)
	if err != nil || len(re.Skipped()) != 0 {
		t.Fatalf("reopen: %v skipped %v", err, re.Skipped())
	}
	got, _ := re.Get(p.ID)
	if len(got.Themes) != 1 || !reflect.DeepEqual(got.Themes[0].Automata, []string{"aaaa0001"}) {
		t.Fatalf("themes after reopen: %+v", got.Themes)
	}
}

func TestThemeOperationsRefuseOrdinaryPlansAndBadInput(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	ord := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Ordinary"})
	row := mustCreate(t, s, row("A row"))
	if _, _, err := s.CreateTheme(ord.ID, "X"); !isInvalid(err) {
		t.Fatalf("theme on an ordinary plan: %v", err)
	}
	if _, _, err := s.CreateTheme(row.ID, "X"); !isInvalid(err) {
		t.Fatalf("theme on a registry row: %v", err)
	}
	if _, _, err := s.CreateTheme("ghost", "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("theme on an unknown plan: %v", err)
	}
	if _, err := s.RemoveTheme(ord.ID, "x"); !isInvalid(err) {
		t.Fatalf("remove on an ordinary plan: %v", err)
	}

	p := themedPlan(t, s)
	if _, _, err := s.CreateTheme(p.ID, "   "); !isInvalid(err) {
		t.Fatalf("blank name: %v", err)
	}
	if _, _, err := s.CreateTheme(p.ID, strings.Repeat("n", MaxThemeNameRunes+1)); !isInvalid(err) {
		t.Fatalf("name over the cap: %v", err)
	}
	if _, _, err := s.CreateTheme(p.ID, strings.Repeat("n", MaxThemeNameRunes)); err != nil {
		t.Fatalf("name at the cap: %v", err)
	}
	th := mustTheme(t, s, p.ID, "Core")
	if _, err := s.PlaceInTheme(p.ID, th.ID, ItemAutomaton, "not-in-plan"); !isInvalid(err) {
		t.Fatalf("placing an item the plan does not hold: %v", err)
	}
	if _, err := s.PlaceInTheme(p.ID, th.ID, ItemAutomation, "aaaa0001"); !isInvalid(err) {
		t.Fatalf("an Automaton placed as an Automation: %v", err)
	}
	if _, err := s.PlaceInTheme(p.ID, th.ID, "finding", "x"); !isInvalid(err) {
		t.Fatalf("bad item kind: %v", err)
	}
	if _, err := s.PlaceInTheme(p.ID, "ghost", ItemAutomaton, "aaaa0001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown theme: %v", err)
	}
	if _, err := s.UnplaceFromTheme(p.ID, "weird", "aaaa0001"); !isInvalid(err) {
		t.Fatalf("unplace bad kind: %v", err)
	}
	if _, err := s.ReorderThemeItems(p.ID, "ghost", ItemAutomaton, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reorder items of an unknown theme: %v", err)
	}
	if _, err := s.ReorderThemeItems(p.ID, th.ID, "weird", nil); !isInvalid(err) {
		t.Fatalf("reorder items bad kind: %v", err)
	}
	if _, err := s.RenameTheme(p.ID, th.ID, " "); !isInvalid(err) {
		t.Fatalf("rename to blank: %v", err)
	}
	// An ordinary plan lists every item as unplaced and has no themes.
	if _, err := s.AttachAutomaton(ord.ID, "aaaa0003", LinkInfo{Title: "Three"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.ThemesOf(ord.ID)
	if err != nil || len(v.Themes) != 0 || len(v.Unplaced) != 1 || v.Unplaced[0].Title != "Three" {
		t.Fatalf("themes view of an ordinary plan: %+v %v", v, err)
	}
	if _, err := s.ThemesOf(row.ID); !isInvalid(err) {
		t.Fatalf("themes view of a row: %v", err)
	}
	if _, err := s.ThemesOf("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("themes view of an unknown plan: %v", err)
	}
}

func TestThemeCountCap(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := releasePlan(t, s, "Many", "1.0")
	for i := 0; i < MaxThemes; i++ {
		mustTheme(t, s, p.ID, fmt.Sprintf("Theme %d", i))
	}
	if _, _, err := s.CreateTheme(p.ID, "One too many"); !isInvalid(err) {
		t.Fatalf("theme over the cap: %v", err)
	}
	if got, _ := s.Get(p.ID); len(got.Themes) != MaxThemes {
		t.Fatalf("a refused theme must not be stored: %d", len(got.Themes))
	}
}

func TestItemInTwoThemesRefused(t *testing.T) {
	plan := Record{
		ID: "p", UID: "u", Kind: KindPlan, Project: "datawatch", State: StateOpen, Title: "P",
		PlanKind: PlanKindRelease, Release: "1.0", Automata: []string{"a1", "a2"}, Automations: []string{"n1"},
	}
	ok := plan
	ok.Themes = []Theme{{ID: "t-1", Name: "One", Order: 1, Automata: []string{"a1"}}, {ID: "t-2", Name: "Two", Order: 2, Automata: []string{"a2"}, Automations: []string{"n1"}}}
	if err := ok.validate(); err != nil {
		t.Fatalf("valid themes refused: %v", err)
	}
	cases := map[string][]Theme{
		"automaton in two themes":  {{ID: "t-1", Name: "One", Automata: []string{"a1"}}, {ID: "t-2", Name: "Two", Automata: []string{"a1"}}},
		"automation in two themes": {{ID: "t-1", Name: "One", Automations: []string{"n1"}}, {ID: "t-2", Name: "Two", Automations: []string{"n1"}}},
		"twice in one theme":       {{ID: "t-1", Name: "One", Automata: []string{"a1", "a1"}}},
		"item the plan lacks":      {{ID: "t-1", Name: "One", Automata: []string{"ghost"}}},
		"duplicate theme id":       {{ID: "t-1", Name: "One"}, {ID: "t-1", Name: "Two"}},
		"duplicate theme name":     {{ID: "t-1", Name: "One"}, {ID: "t-2", Name: " one "}},
		"bad theme id":             {{ID: "Bad Id", Name: "One"}},
		"blank theme name":         {{ID: "t-1", Name: " "}},
		"name over the cap":        {{ID: "t-1", Name: strings.Repeat("x", MaxThemeNameRunes+1)}},
		"order out of range":       {{ID: "t-1", Name: "One", Order: MaxOrder + 1}},
	}
	for name, ts := range cases {
		bad := plan
		bad.Themes = ts
		if err := bad.validate(); !isInvalid(err) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	tooMany := plan
	for i := 0; i <= MaxThemes; i++ {
		tooMany.Themes = append(tooMany.Themes, Theme{ID: fmt.Sprintf("t-%d", i), Name: fmt.Sprintf("T%d", i)})
	}
	if err := tooMany.validate(); !isInvalid(err) {
		t.Errorf("too many themes: %v", err)
	}
	ordinary := ok
	ordinary.PlanKind, ordinary.Release = PlanKindOrdinary, ""
	if err := ordinary.validate(); !isInvalid(err) {
		t.Errorf("themes on an ordinary plan: %v", err)
	}

	// A file holding an item in two themes is skipped on open, never half-loaded.
	s, dir, _, _ := newTestStore(t, nil)
	p := themedPlan(t, s)
	th1, th2 := mustTheme(t, s, p.ID, "One"), mustTheme(t, s, p.ID, "Two")
	cur, _ := s.Get(p.ID)
	cur.Themes[0].Automata = []string{"aaaa0001"}
	cur.Themes[1].Automata = []string{"aaaa0001"}
	if th1.ID == th2.ID {
		t.Fatal("theme ids collide")
	}
	if err := s.writeFile(cur); err != nil {
		t.Fatal(err)
	}
	re, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(re.Skipped()) != 1 {
		t.Fatalf("a duplicate placement must be skipped on open, skipped %v", re.Skipped())
	}
}

func TestDetachingAnItemRemovesItFromItsTheme(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := themedPlan(t, s)
	th := mustTheme(t, s, p.ID, "Core")
	for _, pl := range []struct{ kind, id string }{{ItemAutomaton, "aaaa0001"}, {ItemAutomaton, "aaaa0002"}, {ItemAutomation, "bbbb0001"}} {
		if _, err := s.PlaceInTheme(p.ID, th.ID, pl.kind, pl.id); err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.DetachAutomaton("aaaa0001")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Themes[0].Automata, []string{"aaaa0002"}) || indexOf(after.Automata, "aaaa0001") >= 0 {
		t.Fatalf("detached Automaton still in the theme or plan: %+v", after)
	}
	after, err = s.DetachAutomation("bbbb0001")
	if err != nil || len(after.Themes[0].Automations) != 0 {
		t.Fatalf("detached Automation still in the theme: %+v %v", after, err)
	}
	v, _ := s.ThemesOf(p.ID)
	if len(v.Themes[0].Items) != 1 || v.Themes[0].Items[0].ID != "aaaa0002" {
		t.Fatalf("view after detach: %+v", v.Themes)
	}
	for _, it := range v.Unplaced {
		if it.ID == "aaaa0001" || it.ID == "bbbb0001" {
			t.Fatalf("a detached item is not unplaced, it is gone: %+v", v.Unplaced)
		}
	}
	// The detach reached the file, and the plan still validates on reopen.
	re, err := Open(dir, nil)
	if err != nil || len(re.Skipped()) != 0 {
		t.Fatalf("reopen: %v %v", err, re.Skipped())
	}
	if got, _ := re.Get(p.ID); !reflect.DeepEqual(got.Themes[0].Automata, []string{"aaaa0002"}) {
		t.Fatalf("file kept the detached item in the theme: %+v", got.Themes)
	}
	// Detaching from a plan with no themes is unchanged.
	if _, err := s.DetachAutomaton("aaaa0004"); err != nil {
		t.Fatal(err)
	}
}

func TestOldRecordWithoutNewFieldsLoadsAsOrdinaryPlan(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Older plan", Goal: "g"})
	if _, err := s.AttachAutomaton(p.ID, "aaaa0001", LinkInfo{Title: "One"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan-records", "plan", p.UID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"plan_kind", "release", "themes"} {
		if _, has := m[k]; has {
			t.Fatalf("an ordinary plan must not write %q: %s", k, raw)
		}
	}
	re, err := Open(dir, nil)
	if err != nil || len(re.Skipped()) != 0 {
		t.Fatalf("reopen: %v %v", err, re.Skipped())
	}
	got, err := re.Get(p.ID)
	if err != nil || got.PlanKind != PlanKindOrdinary || got.Release != "" || got.Themes != nil || got.Goal != "g" {
		t.Fatalf("old record: %+v %v", got, err)
	}
	if _, ok := re.ReleasePlan("datawatch", "10.0"); ok || len(re.ReleasePlans("")) != 0 {
		t.Fatal("an old plan is not a release plan")
	}
	if v, err := re.ThemesOf(p.ID); err != nil || len(v.Themes) != 0 || len(v.Unplaced) != 1 {
		t.Fatalf("themes view of an old plan: %+v %v", v, err)
	}
}

func TestReleasePlanFileReadableByPreviousReader(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := themedPlan(t, s)
	th := mustTheme(t, s, p.ID, "Core")
	if _, err := s.PlaceInTheme(p.ID, th.ID, ItemAutomaton, "aaaa0001"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "plan-records", "plan", p.UID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	// previousRecord is the plan record as it was before the marker, release
	// label and themes were added: it has none of the three fields.
	type previousRecord struct {
		ID          string              `json:"id"`
		UID         string              `json:"uid"`
		Kind        Kind                `json:"kind"`
		Project     string              `json:"project"`
		State       State               `json:"state"`
		Title       string              `json:"title"`
		Goal        string              `json:"goal"`
		Order       int                 `json:"order"`
		Automations []string            `json:"automations"`
		Automata    []string            `json:"automata"`
		Findings    []string            `json:"findings"`
		LinkInfo    map[string]LinkInfo `json:"link_info"`
	}
	var prev previousRecord
	if err := json.Unmarshal(raw, &prev); err != nil {
		t.Fatalf("previous reader cannot parse the file: %v", err)
	}
	if prev.UID != p.UID || prev.Title != p.Title || len(prev.Automata) != 3 || len(prev.Automations) != 2 || prev.LinkInfo["automaton:aaaa0001"].Title != "Auto 1" {
		t.Fatalf("previous reader lost data: %+v", prev)
	}
}

func TestThemeWritesWorkInSecureMode(t *testing.T) {
	sealer, err := KeySealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	s, dir, _, _ := newTestStore(t, sealer)
	p := themedPlan(t, s)
	th := mustTheme(t, s, p.ID, "Secret theme")
	if _, err := s.PlaceInTheme(p.ID, th.ID, ItemAutomaton, "aaaa0001"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "plan-records", "plan", p.UID+".json"))
	if strings.Contains(string(raw), "Secret theme") || strings.Contains(string(raw), "10.0") {
		t.Fatal("theme name and release label must exist only inside the sealed payload")
	}
	re, err := Open(dir, sealer)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := re.Get(p.ID); len(got.Themes) != 1 || got.Themes[0].Name != "Secret theme" {
		t.Fatalf("sealed reopen: %+v", got.Themes)
	}
}

// TestThemesOnFixturePlan runs the themes over the shared plans fixture: the
// engine plan becomes a release plan, and the titles and states in the view
// are the ones the fixture reported when it attached each Automaton.
func TestThemesOnFixturePlan(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	engine := plans["engine"]
	kind, label := PlanKindRelease, "10.0"
	if _, err := s.Update(engine.ID, Patch{PlanKind: &kind, Release: &label}); err != nil {
		t.Fatal(err)
	}
	// The other fixture plan stays ordinary and refuses themes.
	if _, _, err := s.CreateTheme(plans["browser"].ID, "X"); !isInvalid(err) {
		t.Fatalf("themes on the ordinary fixture plan: %v", err)
	}
	var wantIDs []string
	for _, p := range f.Plans {
		if p.Key == "engine" {
			wantIDs = p.Automata
		}
	}
	half := len(wantIDs) / 2
	first, second := mustTheme(t, s, engine.ID, "First half"), mustTheme(t, s, engine.ID, "Second half")
	for i, id := range wantIDs {
		th := first
		if i >= half {
			th = second
		}
		if _, err := s.PlaceInTheme(engine.ID, th.ID, ItemAutomaton, id); err != nil {
			t.Fatalf("place %s: %v", id, err)
		}
	}
	v, err := s.ThemesOf(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tv := range v.Themes {
		for _, it := range tv.Items {
			got = append(got, it.ID)
			if want := f.automaton(it.ID); it.Title != want.Title || it.State != want.State {
				t.Errorf("%s: title/state %q/%q, fixture says %q/%q", it.ID, it.Title, it.State, want.Title, want.State)
			}
		}
	}
	if !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("themes hold %v, want %v", got, wantIDs)
	}
	if len(v.Unplaced) != 1 || v.Unplaced[0].Kind != ItemAutomation || v.Unplaced[0].ID != "bbbb0001" {
		t.Fatalf("the plan's Automation is the only unplaced item: %+v", v.Unplaced)
	}
	if rp, ok := s.ReleasePlan(f.Project, "10.0"); !ok || rp.UID != engine.UID {
		t.Fatalf("ReleasePlan: %v %v", rp.ID, ok)
	}
}
