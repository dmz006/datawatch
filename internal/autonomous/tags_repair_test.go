package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// repairWorld is made-up data: two Automations with auto tags and one operator
// tag.
type repairWorld struct {
	m           *Manager
	alpha, beta *Automation
	a, b        []*PRD // members of alpha and beta
	operator    *Tag
}

// repairKinds makes an Automation whose members have the given kinds.
func repairKinds(t *testing.T, m *Manager, name string, kinds ...string) (*Automation, []*PRD) {
	t.Helper()
	var prds []*PRD
	var members []AutomationMember
	for _, k := range kinds {
		prd := autoTagsPRD(t, m, "A made-up item of "+name)
		if err := m.SetPRDKind(prd.ID, k); err != nil {
			t.Fatal(err)
		}
		prds = append(prds, prd)
		members = append(members, AutomationMember{PRDID: prd.ID})
	}
	a, err := m.CreateAutomation(AutomationInput{Name: name, Members: members}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation %q: %v", name, err)
	}
	return a, prds
}

func newRepairWorld(t *testing.T) repairWorld {
	t.Helper()
	m := autoTagsManager(t, true)
	w := repairWorld{m: m, operator: mustTag(t, m, "release:made-up", "")}
	w.alpha, w.a = repairKinds(t, m, "Alpha", "feature", "feature", "bug", "other")
	w.beta, w.b = repairKinds(t, m, "Beta", "bug", "feature", "feature")
	if err := m.SetPRDTagsAndPrimary(w.a[0].ID, append(autoTagsPRDIDs(t, m, w.a[0].ID), w.operator.ID), &w.operator.ID); err != nil {
		t.Fatal(err)
	}
	return w
}

// copyOfTags is a saved copy in the form of the tag list reply.
func copyOfTags(t *testing.T, m *Manager) []*Tag {
	t.Helper()
	data, err := json.Marshal(map[string]any{"tags": m.ListTags()})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := ParseTagCopies(data)
	if err != nil {
		t.Fatal(err)
	}
	return tags
}

func lose(t *testing.T, m *Manager, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := m.store.DeleteTag(id); err != nil {
			t.Fatal(err)
		}
	}
}

// repairSnapshot is every tag record and every Automaton's tags.
func repairSnapshot(t *testing.T, m *Manager) string {
	t.Helper()
	type rec struct {
		ID, Primary string
		Tags        []string
		Updated     time.Time
	}
	var prds []rec
	for _, p := range m.store.ListPRDs() {
		prds = append(prds, rec{p.ID, p.PrimaryTagID, p.TagIDs, p.UpdatedAt})
	}
	data, err := json.Marshal([]any{m.ListTags(), prds})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func repairItem(t *testing.T, r TagRepairReport, id string) TagRepairItem {
	t.Helper()
	for _, it := range r.Items {
		if it.TagID == id {
			return it
		}
	}
	t.Fatalf("no item for %s in %+v", id, r.Items)
	return TagRepairItem{}
}

func TestTagRepairDryRunChangesNothingAndApplyRestoresFromACopy(t *testing.T) {
	w := newRepairWorld(t)
	copies := copyOfTags(t, w.m)
	alpha := mustAutoTag(t, w.m, "automation "+w.alpha.ID)
	want := *alpha
	opWant := *w.operator
	lose(t, w.m, alpha.ID, w.operator.ID)
	before := repairSnapshot(t, w.m)

	dry, err := w.m.RepairTags(copies, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if got := repairSnapshot(t, w.m); got != before {
		t.Fatalf("the dry run changed the data:\n%s\n%s", before, got)
	}
	if !dry.DryRun || dry.Missing != 2 || dry.Restore != 2 || dry.Merge != 0 || dry.Unresolved != 0 || dry.AutomataLeft != 0 || dry.Automata != 4 {
		t.Fatalf("dry run report = %+v", dry)
	}

	done, err := w.m.RepairTags(copies, false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if done.DryRun || done.Restore != 2 {
		t.Fatalf("report = %+v", done)
	}
	for _, exp := range []Tag{want, opWant} {
		got, ok := w.m.store.GetTag(exp.ID)
		if !ok {
			t.Fatalf("tag %s was not restored", exp.ID)
		}
		if got.Name != exp.Name || got.Description != exp.Description || got.System != exp.System || !got.CreatedAt.Equal(exp.CreatedAt) {
			t.Errorf("restored %+v, want %+v", got, exp)
		}
	}
	again, err := w.m.RepairTags(copies, false)
	if err != nil || again.Missing != 0 {
		t.Fatalf("a second repair = %+v, %v; want nothing missing", again, err)
	}
}

func TestTagRepairWorksOutAutoTagsFromTheAutomations(t *testing.T) {
	w := newRepairWorld(t)
	alpha := *mustAutoTag(t, w.m, "automation "+w.alpha.ID)
	beta := *mustAutoTag(t, w.m, "automation "+w.beta.ID)
	maintenance := *mustAutoTag(t, w.m, "kind maintenance") // one member of alpha
	feature := *mustAutoTag(t, w.m, "kind feature")         // members of both
	lose(t, w.m, alpha.ID, beta.ID, maintenance.ID, feature.ID)

	r, err := w.m.RepairTags(nil, false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if r.Missing != 4 || r.Restore != 4 || r.Unresolved != 0 {
		t.Fatalf("report = %+v", r)
	}
	for _, exp := range []Tag{alpha, beta, maintenance, feature} {
		got, ok := w.m.store.GetTag(exp.ID)
		if !ok {
			t.Fatalf("tag %s %q was not restored", exp.ID, exp.Name)
		}
		if got.Name != exp.Name || got.Description != exp.Description || !got.System {
			t.Errorf("restored %+v, want %+v", got, exp)
		}
		if it := repairItem(t, r, exp.ID); it.Source != TagRepairFromAutomations {
			t.Errorf("source of %s = %q", exp.ID, it.Source)
		}
	}
}

// After a loss the daemon can make a new tag for the same slot. The repair then
// moves the old ID onto the new tag and does not make a second tag.
func TestTagRepairMergesALostIDIntoTheTagThatReplacedIt(t *testing.T) {
	w := newRepairWorld(t)
	copies := copyOfTags(t, w.m)
	lost := mustAutoTag(t, w.m, "automation "+w.alpha.ID).ID
	lose(t, w.m, lost)
	if err := w.m.syncAutoTags(w.alpha); err != nil {
		t.Fatalf("sync: %v", err)
	}
	replacement := mustAutoTag(t, w.m, "automation "+w.alpha.ID).ID
	primary := lost
	if err := w.m.setPRDTags(w.a[1].ID, autoTagsPRDIDs(t, w.m, w.a[1].ID), &primary, true); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	p, _ := w.m.store.GetPRD(w.a[1].ID)
	w.m.store.WithLock(func() { p.UpdatedAt = stamp })

	for name, saved := range map[string][]*Tag{"with a copy": copies, "without a copy": nil} {
		dry, err := w.m.RepairTags(saved, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		it := repairItem(t, dry, lost)
		if it.Action != TagRepairMerge || it.IntoTagID != replacement || len(it.AutomatonIDs) != 4 {
			t.Fatalf("%s: item = %+v, want a merge into %s", name, it, replacement)
		}
	}
	if _, err := w.m.RepairTags(nil, false); err != nil {
		t.Fatalf("repair: %v", err)
	}
	for _, prd := range w.a {
		ids := autoTagsPRDIDs(t, w.m, prd.ID)
		autoTagsLacks(t, "alpha member", ids, lost)
		autoTagsHas(t, "alpha member", ids, replacement)
	}
	got, _ := w.m.store.GetPRD(w.a[1].ID)
	if got.PrimaryTagID != replacement {
		t.Errorf("primary tag = %q, want %q", got.PrimaryTagID, replacement)
	}
	if !got.UpdatedAt.Equal(stamp) {
		t.Errorf("the repair changed updated_at to %v", got.UpdatedAt)
	}
	if _, ok := w.m.store.GetTag(lost); ok {
		t.Errorf("a merged ID also got a record")
	}
}

func TestTagRepairLeavesWhatItCannotTellApart(t *testing.T) {
	m := autoTagsManager(t, true)
	// One member: its automation tag and its kind tag are carried alike.
	_, prds := repairKinds(t, m, "Solo", "feature")
	ids := append([]string(nil), autoTagsPRDIDs(t, m, prds[0].ID)...)
	// An ID outside any Automation.
	loose := autoTagsPRD(t, m, "A made-up loose item")
	loose.TagIDs = []string{"nosuch01"}
	if err := m.store.SavePRD(loose); err != nil {
		t.Fatal(err)
	}
	lose(t, m, ids...)
	before := repairSnapshot(t, m)

	r, err := m.RepairTags(nil, false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if r.Missing != 3 || r.Unresolved != 3 || r.AutomataLeft != 2 {
		t.Fatalf("report = %+v", r)
	}
	for _, it := range r.Items {
		if it.Reason == "" {
			t.Errorf("unresolved item %s gives no reason", it.TagID)
		}
	}
	if got := repairSnapshot(t, m); got != before {
		t.Errorf("an unresolved repair changed the data")
	}
}

func TestTagRepairIgnoresACopyNothingPointsToAndReusesARemadeOperatorTag(t *testing.T) {
	w := newRepairWorld(t)
	unused := mustTag(t, w.m, "deleted on purpose", "")
	copies := copyOfTags(t, w.m)
	if err := w.m.DeleteTag(unused.ID); err != nil {
		t.Fatal(err)
	}
	lose(t, w.m, w.operator.ID)
	remade := mustTag(t, w.m, "release:made-up", "")

	r, err := w.m.RepairTags(copies, false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if it := repairItem(t, r, w.operator.ID); it.Action != TagRepairMerge || it.IntoTagID != remade.ID {
		t.Fatalf("item = %+v, want a merge into %s", it, remade.ID)
	}
	if _, ok := w.m.store.GetTag(unused.ID); ok {
		t.Errorf("a tag nothing points to was restored")
	}
	if len(r.UnusedCopies) != 1 || !strings.Contains(r.UnusedCopies[0], unused.ID) {
		t.Errorf("unused copies = %v", r.UnusedCopies)
	}
}

func TestTagRepairRestoresAParentFromACopy(t *testing.T) {
	m := autoTagsManager(t, false)
	parent := mustTag(t, m, "area", "")
	child := mustTag(t, m, "sub area", parent.ID)
	prd := autoTagsPRD(t, m, "A made-up item")
	if err := m.SetPRDTags(prd.ID, []string{child.ID}); err != nil {
		t.Fatal(err)
	}
	copies := copyOfTags(t, m)
	lose(t, m, child.ID, parent.ID)
	r, err := m.RepairTags(copies, false)
	if err != nil || r.Restore != 2 {
		t.Fatalf("repair = %+v, %v; want child and parent restored", r, err)
	}
	if got, ok := m.store.GetTag(child.ID); !ok || got.ParentID != parent.ID {
		t.Fatalf("child = %+v ok=%v", got, ok)
	}
}

func TestParseTagCopiesReadsEveryForm(t *testing.T) {
	want := []string{"t1", "t2"}
	for name, data := range map[string]string{
		"tag list reply": `{"registry":{"revision":4},"tags":[{"id":"t1","name":"one","count":3},{"id":"t2","name":"two"}]}`,
		"list":           `[{"id":"t1","name":"one"},{"id":"t2","name":"two"}]`,
		"tags file":      "{\"id\":\"t1\",\"name\":\"one\"}\n\n{\"id\":\"t2\",\"name\":\"two\"}\n",
	} {
		got, err := ParseTagCopies([]byte(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var ids []string
		for _, g := range got {
			ids = append(ids, g.ID)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("%s: ids = %v", name, ids)
		}
	}
	one, err := ParseTagCopies([]byte(`{"id":"t1","name":"one"}`))
	if err != nil || len(one) != 1 {
		t.Errorf("a one-line tags file = %v, %v", one, err)
	}
	for name, data := range map[string]string{
		"empty": "", "no tags": `{"tags":[]}`, "no name": `[{"id":"t1"}]`, "text": "not a copy",
	} {
		if _, err := ParseTagCopies([]byte(data)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
