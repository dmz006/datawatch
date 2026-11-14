package planrecords

import (
	"errors"
	"strings"
	"testing"
)

// relFixture is a store, a fake resolver and a counting audit hook.
type relFixture struct {
	t      *Store
	facts  []ReleaseFact
	marks  map[string]ReleaseMark
	audits []map[string]any
	calls  int
	store  *[]auditEv
}

func newRelFixture(t *testing.T) *relFixture {
	t.Helper()
	s, _, evs, _ := newTestStore(t, nil)
	return &relFixture{
		t: s, store: evs,
		facts: []ReleaseFact{
			{ID: "aa000001", Kind: "automaton", Title: "Encrypt the vault at rest", State: "completed", Release: "9.150.0"},
			{ID: "aa000002", Kind: "automaton", Title: "Android push notices", State: "draft", Tags: []string{"android"}},
			{ID: "aa000003", Kind: "automaton", Title: "Tidy the odds and ends", State: "approved"},
			{ID: "aa000004", Kind: "automaton", Title: "Evaluate the models", State: "draft", Held: true, Tags: []string{"core"}},
			{ID: "aa000005", Kind: "automaton", Title: "Abandoned idea", State: "cancelled"},
			{ID: "aa000006", Kind: "automaton", Title: "Old shipped thing", State: "completed", Release: "8.2.0"},
			{ID: "bb000001", Kind: "automation", Title: "Projects and plans", State: "active"},
		},
	}
}

func (f *relFixture) opts() ReleaseMigrateOptions {
	return ReleaseMigrateOptions{
		Store: f.t, Project: "datawatch", Marks: f.marks, Actor: "tester",
		Resolve: func() ([]ReleaseFact, error) { f.calls++; return append([]ReleaseFact(nil), f.facts...), nil },
		Audit: func(action, subject string, d map[string]any) {
			if action != AuditReleaseMigrated {
				panic("unexpected audit action " + action)
			}
			f.audits = append(f.audits, d)
		},
	}
}

// preview runs the preview.
func (f *relFixture) preview(t *testing.T) ReleaseMigrateReport {
	t.Helper()
	rep, err := ReleaseMigrate(f.opts())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// apply previews, then writes with that preview's id.
func (f *relFixture) apply(t *testing.T) ReleaseMigrateReport {
	t.Helper()
	pre := f.preview(t)
	o := f.opts()
	o.Apply, o.ApprovalToken = true, pre.PreviewID
	rep, err := ReleaseMigrate(o)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return rep
}

func itemIDs(l []ReleaseMigrateItem) []string {
	var out []string
	for _, it := range l {
		out = append(out, it.ID)
	}
	return out
}

func findItem(l []ReleaseMigrateItem, id string) (ReleaseMigrateItem, bool) {
	for _, it := range l {
		if it.ID == id {
			return it, true
		}
	}
	return ReleaseMigrateItem{}, false
}

func TestReleaseMigratePreviewWritesNothing(t *testing.T) {
	f := newRelFixture(t)
	before := f.t.Len()
	storeEvents := len(*f.store)
	rep := f.preview(t)
	if rep.Applied || rep.PreviewID == "" {
		t.Fatalf("preview: applied=%v id=%q", rep.Applied, rep.PreviewID)
	}
	if f.t.Len() != before || len(*f.store) != storeEvents || len(f.audits) != 0 {
		t.Fatalf("preview wrote: len %d -> %d, store events %d -> %d, audits %d", before, f.t.Len(), storeEvents, len(*f.store), len(f.audits))
	}
	var plans, themes int
	for _, c := range rep.Creates {
		switch c.Kind {
		case "plan":
			plans++
		case "theme":
			themes++
		}
	}
	if plans != 2 || themes != 4 {
		t.Fatalf("creates = %+v", rep.Creates)
	}
	if got := strings.Join(itemIDs(rep.Attaches), ","); got != "bb000001,aa000001,aa000002,aa000003,aa000004" {
		t.Fatalf("attaches = %s", got)
	}
	vault, _ := findItem(rep.Attaches, "aa000001")
	if vault.Release != DefaultCurrentRelease || vault.Theme != ThemeSecurity || vault.ThemeSource != ThemeFromRule {
		t.Fatalf("vault = %+v", vault)
	}
	if _, ok := findItem(rep.Skipped, "aa000005"); !ok {
		t.Fatalf("cancelled item not skipped: %+v", rep.Skipped)
	}
	if it, ok := findItem(rep.Skipped, "aa000006"); !ok || !strings.Contains(it.Reason, "before the last major release") {
		t.Fatalf("old shipped item: %+v", rep.Skipped)
	}
	// The preview id is stable.
	if again := f.preview(t); again.PreviewID != rep.PreviewID {
		t.Fatalf("preview id changed: %s vs %s", rep.PreviewID, again.PreviewID)
	}
	// Facts in another order give the same id.
	for i, j := 0, len(f.facts)-1; i < j; i, j = i+1, j-1 {
		f.facts[i], f.facts[j] = f.facts[j], f.facts[i]
	}
	if again := f.preview(t); again.PreviewID != rep.PreviewID {
		t.Fatal("preview id depends on the order of the facts")
	}
}

func TestReleaseMigrateWrittenRunNeedsMatchingPreviewID(t *testing.T) {
	f := newRelFixture(t)
	before := f.t.Len()
	o := f.opts()
	o.Apply = true
	if _, err := ReleaseMigrate(o); !errors.Is(err, ErrPreviewMissing) {
		t.Fatalf("empty token: %v", err)
	}
	o.ApprovalToken = "deadbeef"
	if _, err := ReleaseMigrate(o); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("wrong token: %v", err)
	}
	pre := f.preview(t)
	// The data changes after the preview: the approved id is stale.
	f.facts = append(f.facts, ReleaseFact{ID: "aa000009", Kind: "automaton", Title: "Late arrival", State: "draft"})
	o.ApprovalToken = pre.PreviewID
	if _, err := ReleaseMigrate(o); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("stale token: %v", err)
	}
	if f.t.Len() != before || len(f.audits) != 0 {
		t.Fatalf("a refused run wrote: len %d -> %d, audits %d", before, f.t.Len(), len(f.audits))
	}
}

func TestReleaseMigrateWrittenRunIsIdempotent(t *testing.T) {
	f := newRelFixture(t)
	rep := f.apply(t)
	if !rep.Applied || len(rep.Conflicts) != 0 {
		t.Fatalf("apply: %+v", rep.Counts())
	}
	cur, ok := f.t.ReleasePlan("datawatch", DefaultCurrentRelease)
	if !ok || cur.PlanKind != PlanKindRelease || len(cur.Automata) != 3 || len(cur.Automations) != 1 {
		t.Fatalf("current plan = %+v %v", cur, ok)
	}
	next, ok := f.t.ReleasePlan("datawatch", DefaultNextRelease)
	if !ok || len(next.Automata) != 1 || next.Automata[0] != "aa000004" {
		t.Fatalf("next plan = %+v %v", next, ok)
	}
	if got := rep.Changes(); len(f.audits) != got {
		t.Fatalf("audit entries = %d, changes = %d", len(f.audits), got)
	}
	for _, a := range f.audits {
		if a["actor"] != "tester" {
			t.Fatalf("audit without the actor: %+v", a)
		}
	}
	n, audits, storeEvents := f.t.Len(), len(f.audits), len(*f.store)

	again := f.apply(t)
	if again.Changes() != 0 || len(again.Attaches) != 0 || len(again.Creates) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("second run changed things: %+v", again.Counts())
	}
	// Two release plans, five items filed; the rest were skipped before and still are.
	if len(again.Unchanged) != 2+5 {
		t.Fatalf("unchanged = %+v", again.Unchanged)
	}
	if f.t.Len() != n || len(f.audits) != audits || len(*f.store) != storeEvents {
		t.Fatalf("second run wrote: len %d -> %d, audits %d -> %d", n, f.t.Len(), audits, len(f.audits))
	}
	// The first run's id is stale now.
	o := f.opts()
	o.Apply, o.ApprovalToken = true, rep.PreviewID
	if _, err := ReleaseMigrate(o); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("old id after the write: %v", err)
	}
}

func TestReleaseMigrateRefusesItemOfAnotherPlan(t *testing.T) {
	f := newRelFixture(t)
	mine := mustCreate(t, f.t, plan("Mine"))
	if _, err := f.t.AttachAutomaton(mine.ID, "aa000002", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	pre := f.preview(t)
	c, ok := findItem(pre.Conflicts, "aa000002")
	if !ok || c.HoldingPlan != "Mine" || c.HoldingPlanID != mine.ID || !strings.Contains(c.Reason, "already linked to "+mine.ID) {
		t.Fatalf("conflicts = %+v", pre.Conflicts)
	}
	rep := f.apply(t)
	if _, ok := findItem(rep.Conflicts, "aa000002"); !ok {
		t.Fatalf("apply conflicts = %+v", rep.Conflicts)
	}
	if p, _ := f.t.PlanOf("aa000002"); p.ID != mine.ID {
		t.Fatalf("the item moved to %+v", p)
	}
	cur, _ := f.t.ReleasePlan("datawatch", DefaultCurrentRelease)
	if indexOf(cur.Automata, "aa000002") >= 0 {
		t.Fatal("the conflicting item is on the release plan")
	}
	// Still a conflict, never unchanged, on the next run.
	if again := f.preview(t); len(again.Conflicts) != 1 {
		t.Fatalf("again = %+v", again.Counts())
	}
}

func TestReleaseMigrateHeldGoesToNextRelease(t *testing.T) {
	f := newRelFixture(t)
	rep := f.apply(t)
	held, ok := findItem(rep.Attaches, "aa000004")
	if !ok || held.Release != DefaultNextRelease {
		t.Fatalf("held item = %+v", held)
	}
	next, _ := f.t.ReleasePlan("datawatch", DefaultNextRelease)
	if p, ok := f.t.PlanOf("aa000004"); !ok || p.UID != next.UID {
		t.Fatalf("held item is on %+v", p)
	}
	// A held item that is also shipped still goes to the next release plan.
	f.facts[3].State, f.facts[3].Release = "completed", "9.170.0"
	if again := f.preview(t); len(again.Conflicts) != 0 || len(again.Attaches) != 0 {
		t.Fatalf("held and shipped: %+v", again.Counts())
	}
}

func TestReleaseMigrateMarksOverrideReleaseAndTheme(t *testing.T) {
	f := newRelFixture(t)
	f.marks = map[string]ReleaseMark{
		"aa000002": {Release: "11.0"},                         // planned item pushed to the next release
		"aa000001": {Theme: ThemeReleaseTooling},              // theme override, release stays
		"aa000003": {Release: "11.0", Theme: "Odds and ends"}, // unmatched item gets an operator theme
		"aa000009": {Release: "11.0"},                         // names nothing
	}
	rep := f.apply(t)
	android, _ := findItem(rep.Attaches, "aa000002")
	if android.Release != DefaultNextRelease || android.Theme != ThemeMobile {
		t.Fatalf("android = %+v", android)
	}
	vault, _ := findItem(rep.Attaches, "aa000001")
	if vault.Release != DefaultCurrentRelease || vault.Theme != ThemeReleaseTooling || vault.ThemeSource != ThemeFromMark {
		t.Fatalf("vault = %+v", vault)
	}
	tidy, _ := findItem(rep.Attaches, "aa000003")
	if tidy.Release != DefaultNextRelease || tidy.Theme != "Odds and ends" {
		t.Fatalf("tidy = %+v", tidy)
	}
	if len(rep.Unplaced) != 0 {
		t.Fatalf("unplaced = %+v", rep.Unplaced)
	}
	if it, ok := findItem(rep.Skipped, "aa000009"); !ok || it.Kind != "mark" {
		t.Fatalf("stray mark: %+v", rep.Skipped)
	}
	v, err := f.t.ThemesOf(vault.PlanID)
	if err != nil || len(v.Themes) == 0 {
		t.Fatalf("themes of current = %+v %v", v, err)
	}
	next, _ := f.t.ReleasePlan("datawatch", DefaultNextRelease)
	if th := themeIndex(next.Themes, "Odds and ends"); th < 0 || indexOf(next.Themes[th].Automata, "aa000003") < 0 {
		t.Fatalf("operator theme not placed: %+v", next.Themes)
	}
}

func TestReleaseMigrateMarkNamingAnUnmanagedReleaseIsAConflict(t *testing.T) {
	f := newRelFixture(t)
	f.marks = map[string]ReleaseMark{"aa000002": {Release: "12.0"}}
	rep := f.apply(t)
	if it, ok := findItem(rep.Conflicts, "aa000002"); !ok || !strings.Contains(it.Reason, "12.0") {
		t.Fatalf("conflicts = %+v", rep.Conflicts)
	}
	if _, ok := f.t.PlanOf("aa000002"); ok {
		t.Fatal("the item was attached")
	}
}

func TestReleaseMigrateUnmatchedItemIsUnplacedButAttached(t *testing.T) {
	f := newRelFixture(t)
	pre := f.preview(t)
	if got := itemIDs(pre.Unplaced); len(got) != 1 || got[0] != "aa000003" {
		t.Fatalf("unplaced = %+v", pre.Unplaced)
	}
	f.apply(t)
	p, ok := f.t.PlanOf("aa000003")
	if !ok {
		t.Fatal("the unplaced item is not attached")
	}
	v, err := f.t.ThemesOf(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, u := range v.Unplaced {
		if u.ID == "aa000003" {
			found = true
		}
	}
	if !found {
		t.Fatalf("not in the plan's unplaced list: %+v", v.Unplaced)
	}
	// Placing it by hand sticks: the next run leaves it alone.
	if _, _, err := f.t.CreateTheme(p.ID, "By hand"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.t.PlaceInTheme(p.ID, "By hand", "automaton", "aa000003"); err != nil {
		t.Fatal(err)
	}
	if again := f.preview(t); again.Changes() != 0 || len(again.Unplaced) != 0 {
		t.Fatalf("again = %+v", again.Counts())
	}
}

func TestReleaseMigrateCreatesBothPlansWhenAbsent(t *testing.T) {
	f := newRelFixture(t)
	rep := f.apply(t)
	cur, ok1 := f.t.ReleasePlan("datawatch", DefaultCurrentRelease)
	next, ok2 := f.t.ReleasePlan("datawatch", DefaultNextRelease)
	if !ok1 || !ok2 || cur.UID == next.UID {
		t.Fatalf("plans: %v %v", ok1, ok2)
	}
	for _, c := range rep.Creates {
		if c.Kind == "plan" && c.PlanID == "" {
			t.Fatalf("created plan without an id in the written report: %+v", c)
		}
	}
	if len(f.t.ReleasePlans("datawatch")) != 2 {
		t.Fatalf("release plans = %+v", f.t.ReleasePlans("datawatch"))
	}
}

func TestReleaseMigrateReusesExistingPlansFoundByLabel(t *testing.T) {
	f := newRelFixture(t)
	// Plans with titles the engine would never pick; only the label identifies them.
	cur := plan("The big one")
	cur.PlanKind, cur.Release = PlanKindRelease, DefaultCurrentRelease
	cur = mustCreate(t, f.t, cur)
	next := plan("After the big one")
	next.PlanKind, next.Release = PlanKindRelease, " "+DefaultNextRelease+" "
	next = mustCreate(t, f.t, next)
	n := f.t.Len()

	pre := f.preview(t)
	for _, c := range pre.Creates {
		if c.Kind == "plan" {
			t.Fatalf("a release plan would be created: %+v", c)
		}
	}
	f.apply(t)
	if f.t.Len() != n || len(f.t.ReleasePlans("datawatch")) != 2 {
		t.Fatalf("plans created: len %d -> %d", n, f.t.Len())
	}
	got, _ := f.t.ReleasePlan("datawatch", DefaultCurrentRelease)
	if got.UID != cur.UID || len(got.Automata) != 3 {
		t.Fatalf("current plan = %+v", got)
	}
	got, _ = f.t.ReleasePlan("datawatch", DefaultNextRelease)
	if got.UID != next.UID || len(got.Automata) != 1 {
		t.Fatalf("next plan = %+v", got)
	}
}

func TestReleaseMigrateTitleOfAnotherRecordBlocksPlanCreation(t *testing.T) {
	f := newRelFixture(t)
	mustCreate(t, f.t, plan("Release "+DefaultNextRelease)) // an ordinary plan with that title
	rep := f.apply(t)
	if it, ok := findItem(rep.Conflicts, "aa000004"); !ok || !strings.Contains(it.Reason, "cannot be created") {
		t.Fatalf("conflicts = %+v", rep.Conflicts)
	}
	if _, ok := f.t.ReleasePlan("datawatch", DefaultNextRelease); ok {
		t.Fatal("a release plan was made over another record")
	}
	if _, ok := f.t.ReleasePlan("datawatch", DefaultCurrentRelease); !ok {
		t.Fatal("the current release plan was not made")
	}
}

func TestReleaseMigrateReleaseLabelsAreInputs(t *testing.T) {
	f := newRelFixture(t)
	f.facts = []ReleaseFact{
		{ID: "cc000001", Kind: "automaton", Title: "Shipped last year", State: "completed", Release: "3.2.0"},
		{ID: "cc000002", Kind: "automaton", Title: "Planned", State: "draft", Release: "5.0"},
	}
	o := f.opts()
	o.CurrentRelease, o.NextRelease = "4.0", "5.0"
	pre, err := ReleaseMigrate(o)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := findItem(pre.Attaches, "cc000001")
	b, _ := findItem(pre.Attaches, "cc000002")
	if a.Release != "4.0" || b.Release != "5.0" {
		t.Fatalf("attaches = %+v", pre.Attaches)
	}
	o.NextRelease = "4.0"
	if _, err := ReleaseMigrate(o); err == nil {
		t.Fatal("equal labels must be refused")
	}
}

func TestReleaseMigrateNeedsStoreResolverAndProject(t *testing.T) {
	f := newRelFixture(t)
	o := f.opts()
	o.Store = nil
	if _, err := ReleaseMigrate(o); err == nil {
		t.Fatal("expected an error without a store")
	}
	o = f.opts()
	o.Resolve = nil
	if _, err := ReleaseMigrate(o); err == nil {
		t.Fatal("expected an error without a resolver")
	}
	o = f.opts()
	o.Project = "not a project"
	if _, err := ReleaseMigrate(o); err == nil {
		t.Fatal("expected an error for a bad project")
	}
	o = f.opts()
	o.Resolve = func() ([]ReleaseFact, error) { return nil, errors.New("boom") }
	if _, err := ReleaseMigrate(o); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("resolver error: %v", err)
	}
}

func TestReleaseThemeRulesFirstMatchWins(t *testing.T) {
	if len(ReleaseThemeRules) == 0 {
		t.Fatal("no rules")
	}
	known := map[string]bool{}
	for _, n := range ReleaseThemeNames() {
		known[n] = true
	}
	for i, r := range ReleaseThemeRules {
		if !known[r.Theme] || len(r.Any) == 0 {
			t.Fatalf("rule %d is malformed: %+v", i, r)
		}
		if r.Field != RuleFieldTag && r.Field != RuleFieldAutomation && r.Field != RuleFieldTitle {
			t.Fatalf("rule %d has field %q", i, r.Field)
		}
	}
	cases := []struct {
		name string
		f    ReleaseFact
		want string
	}{
		{"tag beats title", ReleaseFact{Kind: "automaton", Title: "Android release notes", Tags: []string{"Security"}}, ThemeSecurity},
		{"automation title", ReleaseFact{Kind: "automaton", Title: "Fix the thing", AutomationTitle: "Mobile parity work"}, ThemeMobile},
		{"own title as automation", ReleaseFact{Kind: "automation", Title: "Security hardening"}, ThemeSecurity},
		{"title words", ReleaseFact{Kind: "automaton", Title: "Cut the changelog"}, ThemeReleaseTooling},
		{"whole words only", ReleaseFact{Kind: "automaton", Title: "Happening now"}, ""},
		{"phrase", ReleaseFact{Kind: "automaton", Title: "A companion app screen"}, ThemeMobile},
		{"no rule", ReleaseFact{Kind: "automaton", Title: "Tidy the odds and ends"}, ""},
	}
	for _, c := range cases {
		got, ok := MatchThemeRule(c.f)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: got %q (%v), want %q", c.name, got, ok, c.want)
		}
	}
}
