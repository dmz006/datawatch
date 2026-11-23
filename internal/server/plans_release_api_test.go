// plans_release_api_test.go — the release plans API over the real plan-record
// store, with fake Automaton facts: themes, archive and restore, the release
// migration, the row-plan conversion, the unattributed list and count, and the
// bulk attach. No test here sleeps.

package server

import (
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// releaseMux is every plan route the production mux registers, with the same
// patterns, so a pattern conflict between the old and the new routes panics
// here.
func releaseMux(s *Server) *http.ServeMux {
	mux := plansWriteMux(s)
	mux.HandleFunc("GET /api/plans/{ref}/themes", s.handlePlanThemesGet)
	mux.HandleFunc("PUT /api/plans/{ref}/themes", s.handlePlanThemesPut)
	mux.HandleFunc("POST /api/plans/{ref}/themes", s.handlePlanThemesAction)
	mux.HandleFunc("POST /api/plans/{ref}/archive", s.handlePlanArchive)
	mux.HandleFunc("POST /api/plans/{ref}/restore", s.handlePlanRestore)
	mux.HandleFunc("GET /api/plans/unattributed", s.handlePlansUnattributed)
	mux.HandleFunc("/api/plans/migrate/row-plans", s.handlePlansRowPlans)
	mux.HandleFunc("POST /api/plans/migrate", s.handlePlansMigrate)
	return mux
}

type relRig struct {
	*planRecordsRig
	mux   *http.ServeMux
	facts []planItemFact
}

func relFact(id, title, state string, tags ...string) planItemFact {
	return planItemFact{
		ReleaseFact: planrecords.ReleaseFact{ID: id, Kind: planrecords.ReleaseKindAutomaton, Title: title, State: state, Tags: tags},
		Project:     "datawatch",
	}
}

// newRelRig seeds fake facts:
//
//	a-sec    planned, tagged security                    -> current release, Security
//	a-held   planned, held                               -> next release, Mobile (by title)
//	a-ship   shipped in 10.1                             -> current release
//	a-plain  planned, no rule matches its title          -> current release, unplaced
//	a-other  planned, already attached to an ordinary plan -> refused
//	a-away   planned, in another project                 -> never listed
//	au-1     an Automation                               -> current release
func newRelRig(t *testing.T) *relRig {
	t.Helper()
	r := newPlanRecordsRig(t)
	rr := &relRig{planRecordsRig: r}
	held := relFact("a-held", "Mobile app polish", "draft")
	held.Held = true
	ship := relFact("a-ship", "Changelog tidy", "completed")
	ship.Release = "10.1"
	away := relFact("a-away", "Elsewhere", "draft")
	away.Project = "other"
	au := planItemFact{
		ReleaseFact: planrecords.ReleaseFact{ID: "au-1", Kind: planrecords.ReleaseKindAutomation, Title: "Chain", State: "draft"},
		Project:     "datawatch",
	}
	rr.facts = []planItemFact{
		relFact("a-sec", "Rotate the certificate", "draft", "security"), held, ship,
		relFact("a-plain", "Zebra work", "draft"), relFact("a-other", "Already planned", "draft"), away, au,
	}
	r.s.planFactsSource = func() ([]planItemFact, error) { return rr.facts, nil }
	r.s.planItemSource = func(id string) (planrecords.MigrateRef, bool) {
		for _, f := range rr.facts {
			if f.ID == id {
				return planrecords.MigrateRef{Kind: f.Kind, Title: f.Title, State: f.State}, true
			}
		}
		return planrecords.MigrateRef{}, false
	}
	// a-other is held by an ordinary plan before any migration runs.
	old := mustPlan(t, r.store, "Old plan", 0, "")
	if _, err := r.store.AttachAutomaton(old.ID, "a-other", planrecords.LinkInfo{Title: "Already planned", State: "draft"}); err != nil {
		t.Fatal(err)
	}
	rr.mux = releaseMux(r.s)
	return rr
}

func (rr *relRig) send(t *testing.T, method, path string, body any, caps ...string) (int, map[string]any) {
	t.Helper()
	return plansSend(t, rr.mux, method, path, body, caps...)
}

func (rr *relRig) must(t *testing.T, want int, method, path string, body any) map[string]any {
	t.Helper()
	code, out := rr.send(t, method, path, body)
	if code != want {
		t.Fatalf("%s %s: want %d, got %d %v", method, path, want, code, out)
	}
	return out
}

func relItems(out map[string]any, key string) []map[string]any {
	var items []map[string]any
	if l, ok := out[key].([]any); ok {
		for _, v := range l {
			items = append(items, v.(map[string]any))
		}
	}
	return items
}

func relIDs(items []map[string]any) []string {
	ids := []string{}
	for _, it := range items {
		ids = append(ids, it["id"].(string))
	}
	sort.Strings(ids)
	return ids
}

func relInt(out map[string]any, path ...string) int {
	var cur any = out
	for _, p := range path {
		cur = cur.(map[string]any)[p]
	}
	return int(cur.(float64))
}

func (rr *relRig) preview(t *testing.T) map[string]any {
	t.Helper()
	return rr.must(t, 200, http.MethodPost, plansMigratePath, map[string]any{"mode": "release", "project": "datawatch"})
}

func (rr *relRig) apply(t *testing.T, id string) (int, map[string]any) {
	t.Helper()
	return rr.send(t, http.MethodPost, plansMigratePath, map[string]any{
		"mode": "release", "project": "datawatch", "apply": true, "preview_id": id})
}

func (rr *relRig) releasePlanID(t *testing.T, release string) string {
	t.Helper()
	p, ok := rr.store.ReleasePlan("datawatch", release)
	if !ok {
		t.Fatalf("no release plan for %s", release)
	}
	return p.ID
}

func (rr *relRig) unattributedCount(t *testing.T) int {
	t.Helper()
	out := rr.must(t, 200, http.MethodGet, "/api/projects/datawatch/overview-summary", nil)
	return relInt(out, "counts", "unattributed", "automata")
}

// --- release migration -------------------------------------------------------

func TestReleaseMigrationPreviewWritesNothing(t *testing.T) {
	rr := newRelRig(t)
	before := rr.store.Len()
	out := rr.preview(t)
	if out["applied"] != false || out["preview_id"] == "" {
		t.Fatalf("preview must not be applied and must carry an id: %v", out)
	}
	if rr.store.Len() != before {
		t.Fatalf("a preview wrote records: %d -> %d", before, rr.store.Len())
	}
	if _, ok := rr.store.ReleasePlan("datawatch", "10.0"); ok {
		t.Fatal("a preview created the current release plan")
	}
	// The default (no apply) and an explicit apply:false are both previews.
	rr.must(t, 200, http.MethodPost, plansMigratePath, map[string]any{"mode": "release", "project": "datawatch", "apply": false})
	if rr.store.Len() != before {
		t.Fatal("apply:false wrote records")
	}
	for _, e := range prAuditActions(t, rr.audit) {
		if e.Details["op"] == "create_plan" || e.Details["op"] == "attach" {
			t.Fatalf("a preview audited a write: %+v", e)
		}
	}

	// What the preview says.
	if got := relIDs(relItems(out, "attached")); !reflect.DeepEqual(got, []string{"a-held", "a-plain", "a-sec", "a-ship", "au-1"}) {
		t.Fatalf("attached = %v", got)
	}
	byID := map[string]map[string]any{}
	for _, it := range relItems(out, "attached") {
		byID[it["id"].(string)] = it
	}
	if byID["a-held"]["release"] != "11.0" {
		t.Fatalf("a held Automaton must go to the next release plan: %v", byID["a-held"])
	}
	if byID["a-sec"]["release"] != "10.0" || byID["a-sec"]["theme"] != "Security and encryption" || byID["a-sec"]["theme_source"] != "rule" {
		t.Fatalf("a-sec: %v", byID["a-sec"])
	}
	if got := relIDs(relItems(out, "unplaced")); !reflect.DeepEqual(got, []string{"a-plain", "au-1"}) {
		t.Fatalf("unplaced = %v", got)
	}
	conf := relItems(out, "conflicts")
	if len(conf) != 1 || conf[0]["id"] != "a-other" || conf[0]["holding_plan"] != "Old plan" {
		t.Fatalf("conflicts = %v", conf)
	}
	if len(relItems(out, "created")) < 2 || len(relItems(out, "unchanged")) != 0 {
		t.Fatalf("created/unchanged = %v / %v", out["created"], out["unchanged"])
	}
}

func TestReleaseMigrationWriteNeedsPreviewID(t *testing.T) {
	rr := newRelRig(t)
	before := rr.store.Len()
	if code, out := rr.apply(t, ""); code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "preview") {
		t.Fatalf("no preview id: %d %v", code, out)
	}
	if code, out := rr.apply(t, "0123456789abcdef"); code != http.StatusConflict {
		t.Fatalf("wrong preview id: %d %v", code, out)
	}
	if rr.store.Len() != before {
		t.Fatal("a refused written run wrote records")
	}

	id := rr.preview(t)["preview_id"].(string)
	// The data changes after the preview: the id is stale.
	mustPlan(t, rr.store, "Unrelated plan", 0, "")
	rr.facts = append(rr.facts, relFact("a-new", "Brand new", "draft"))
	if code, _ := rr.apply(t, id); code != http.StatusConflict {
		t.Fatalf("stale preview id: %d", code)
	}
	if _, ok := rr.store.ReleasePlan("datawatch", "10.0"); ok {
		t.Fatal("a stale written run created a plan")
	}

	id = rr.preview(t)["preview_id"].(string)
	code, out := rr.apply(t, id)
	if code != 200 || out["applied"] != true {
		t.Fatalf("apply: %d %v", code, out)
	}
	cur, ok := rr.store.ReleasePlan("datawatch", "10.0")
	if !ok || cur.PlanKind != planrecords.PlanKindRelease {
		t.Fatalf("current release plan: %+v", cur)
	}
	if ref, ok := rr.store.PlanOf("a-held"); !ok || ref.Title != "Release 11.0" {
		t.Fatalf("a-held is on %+v", ref)
	}
	if ref, ok := rr.store.PlanOf("a-other"); !ok || ref.Title != "Old plan" {
		t.Fatalf("an Automaton attached elsewhere moved: %+v", ref)
	}
	th, err := rr.store.ThemesOf(cur.ID)
	if err != nil || len(th.Themes) == 0 {
		t.Fatalf("themes: %+v %v", th, err)
	}
	// The migration audits with the operator as the actor.
	found := false
	for _, e := range prAuditActions(t, rr.audit) {
		if e.Details["op"] == "create_plan" && e.Details["actor"] == "operator" {
			found = true
		}
	}
	if !found {
		t.Fatal("no audit entry with the operator as actor for the written migration")
	}
}

func TestReleaseMigrationSecondRunChangesNothing(t *testing.T) {
	rr := newRelRig(t)
	if code, out := rr.apply(t, rr.preview(t)["preview_id"].(string)); code != 200 {
		t.Fatalf("first apply: %d %v", code, out)
	}
	snap := func() map[string]string {
		m := map[string]string{}
		for _, sm := range rr.store.Plans("") {
			rec, _ := rr.store.Get(sm.ID)
			m[sm.ID] = rec.UpdatedAt
		}
		return m
	}
	before, n := snap(), rr.store.Len()
	again := rr.preview(t)
	for _, k := range []string{"created", "attached", "unplaced"} {
		if len(relItems(again, k)) != 0 {
			t.Fatalf("second preview lists %s: %v", k, again[k])
		}
	}
	if len(relItems(again, "unchanged")) == 0 {
		t.Fatalf("second preview lists nothing unchanged: %v", again)
	}
	code, out := rr.apply(t, again["preview_id"].(string))
	if code != 200 || !strings.Contains(out["note"].(string), "Nothing to change") {
		t.Fatalf("second apply: %d %v", code, out)
	}
	if !reflect.DeepEqual(before, snap()) || rr.store.Len() != n {
		t.Fatal("a second written run changed records")
	}
}

func TestReleaseMigrationMarksAndModeErrors(t *testing.T) {
	rr := newRelRig(t)
	// An operator mark wins over the rules and over the held rule.
	out := rr.must(t, 200, http.MethodPost, plansMigratePath, map[string]any{
		"mode": "release", "project": "datawatch",
		"marks": map[string]any{"a-held": map[string]any{"release": "10.0", "theme": "Hand placed"}}})
	for _, it := range relItems(out, "attached") {
		if it["id"] == "a-held" && (it["release"] != "10.0" || it["theme"] != "Hand placed" || it["theme_source"] != "mark") {
			t.Fatalf("mark ignored: %v", it)
		}
	}
	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"unknown mode", map[string]any{"mode": "sideways"}, 400},
		{"release needs a project", map[string]any{"mode": "release"}, 400},
		{"unknown project", map[string]any{"mode": "release", "project": "nope"}, 400},
		{"bad release label", map[string]any{"mode": "release", "project": "datawatch", "current_release": "x"}, 400},
		{"release field in arcs mode", map[string]any{"project": "datawatch"}, 400},
		{"unknown field", map[string]any{"mode": "release", "project": "datawatch", "bogus": 1}, 400},
	} {
		if code, o := rr.send(t, http.MethodPost, plansMigratePath, c.body); code != c.want {
			t.Errorf("%s: want %d, got %d %v", c.name, c.want, code, o)
		}
	}
	// The original mode still works and is the default.
	if out := rr.must(t, 200, http.MethodPost, plansMigratePath, map[string]any{}); out["counts"] == nil {
		t.Fatalf("arcs mode answer lost its counts: %v", out)
	}
	// No manager: the release migration says so.
	rr.s.planFactsSource = nil
	if code, _ := rr.send(t, http.MethodPost, plansMigratePath, map[string]any{"mode": "release", "project": "datawatch"}); code != http.StatusServiceUnavailable {
		t.Fatalf("no Automata manager: %d", code)
	}
}

// --- unattributed ------------------------------------------------------------

func TestUnattributedListAndCountDropToWhatNoReleasePlanHolds(t *testing.T) {
	rr := newRelRig(t)
	// Everything of the project is unattributed before a release plan exists.
	if n := rr.unattributedCount(t); n != 5 { // a-sec a-held a-ship a-plain a-other
		t.Fatalf("count before = %d, want 5", n)
	}
	list := rr.must(t, 200, http.MethodGet, "/api/plans/unattributed?project=datawatch", nil)
	if got := relIDs(relItems(list, "automata")); !reflect.DeepEqual(got, []string{"a-held", "a-other", "a-plain", "a-sec", "a-ship"}) {
		t.Fatalf("list before = %v", got)
	}
	rows := map[string]map[string]any{}
	for _, it := range relItems(list, "automata") {
		rows[it["id"].(string)] = it
	}
	sec := rows["a-sec"]
	if sec["title"] != "Rotate the certificate" || sec["state"] != "draft" || sec["project"] != "datawatch" ||
		sec["suggested_release"] != "10.0" || sec["suggested_theme"] != "Security and encryption" ||
		!reflect.DeepEqual(sec["tags"], []any{"security"}) {
		t.Fatalf("a-sec row = %v", sec)
	}
	if rows["a-held"]["suggested_release"] != "11.0" || rows["a-held"]["suggested_theme"] != "Mobile and the app" {
		t.Fatalf("a-held suggestion = %v", rows["a-held"])
	}
	if _, ok := rows["a-plain"]["suggested_theme"]; ok || rows["a-plain"]["suggested_release"] != "10.0" {
		t.Fatalf("a-plain suggestion = %v", rows["a-plain"])
	}
	if rows["a-other"]["holding_plan"] != "Old plan" {
		t.Fatalf("a-other row = %v", rows["a-other"])
	}
	all := rr.must(t, 200, http.MethodGet, "/api/plans/unattributed", nil)
	if got := relIDs(relItems(all, "automata")); len(got) != 6 { // adds a-away of the other project
		t.Fatalf("all-project list = %v", got)
	}

	// After the migration only the Automata no release plan holds are left.
	if code, out := rr.apply(t, rr.preview(t)["preview_id"].(string)); code != 200 {
		t.Fatalf("apply: %d %v", code, out)
	}
	if n := rr.unattributedCount(t); n != 1 {
		t.Fatalf("count after = %d, want 1 (a-other)", n)
	}
	list = rr.must(t, 200, http.MethodGet, "/api/plans/unattributed?project=datawatch", nil)
	if got := relIDs(relItems(list, "automata")); !reflect.DeepEqual(got, []string{"a-other"}) {
		t.Fatalf("list after = %v", got)
	}

	// Archiving a release plan gives its Automata back to the unattributed.
	cur := rr.releasePlanID(t, "10.0")
	rr.must(t, 200, http.MethodPost, "/api/plans/"+cur+"/archive", nil)
	if n := rr.unattributedCount(t); n != 4 { // a-sec a-ship a-plain a-other
		t.Fatalf("count with the current plan archived = %d, want 4", n)
	}
	rr.must(t, 200, http.MethodPost, "/api/plans/"+cur+"/restore", nil)
	if n := rr.unattributedCount(t); n != 1 {
		t.Fatalf("count after restore = %d, want 1", n)
	}

	// Paging and validation.
	page := rr.must(t, 200, http.MethodGet, "/api/plans/unattributed?limit=1&offset=1", nil)
	if len(relItems(page, "automata")) != 1 || relInt(page, "page", "total") != 1 {
		// one project row (a-other) plus a-away: total 2
		if relInt(page, "page", "total") != 2 {
			t.Fatalf("page = %v", page)
		}
	}
	if code, _ := rr.send(t, http.MethodGet, "/api/plans/unattributed?project=nope", nil); code != 400 {
		t.Fatalf("unknown project: %d", code)
	}
	if code, _ := rr.send(t, http.MethodGet, "/api/plans/unattributed?limit=x", nil); code != 400 {
		t.Fatalf("bad limit: %d", code)
	}
	if code, _ := rr.send(t, http.MethodGet, "/api/plans/unattributed?current_release=x", nil); code != 400 {
		t.Fatalf("bad release label: %d", code)
	}
	rr.s.planFactsSource = nil
	if code, _ := rr.send(t, http.MethodGet, "/api/plans/unattributed", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: %d", code)
	}
	// The overview leaves the count out while it cannot be counted.
	ov := rr.must(t, 200, http.MethodGet, "/api/projects/datawatch/overview-summary", nil)
	if _, ok := ov["counts"].(map[string]any)["unattributed"]; ok {
		t.Fatalf("overview carries an unattributed count with no Automata manager: %v", ov["counts"])
	}
}

func TestRollupAndTimelineUnassignedLeaveOutReleaseHeld(t *testing.T) {
	dir := "/srv/acme"
	mk := func(id string) *autonomous.PRD {
		return &autonomous.PRD{ID: id, ProjectDir: dir, Status: autonomous.PRDDraft, UpdatedAt: rollupNow}
	}
	prds := []*autonomous.PRD{mk("p1"), mk("p2"), mk("p3")}
	held := func(id string) bool { return id == "p2" }

	roll := buildProjectRollup(rollupInput{Project: "acme", Dir: dir, Entries: summaryOf(prds), WindowDays: 7, Now: rollupNow, ReleaseHeld: held})
	if ids := rollupIDs(roll.Unassigned.Automata); !reflect.DeepEqual(ids, []string{"p1", "p3"}) {
		t.Fatalf("unassigned = %v, want p1 and p3 (a release plan holds p2)", ids)
	}
	roll = buildProjectRollup(rollupInput{Project: "acme", Dir: dir, Entries: summaryOf(prds), WindowDays: 7, Now: rollupNow})
	if len(roll.Unassigned.Automata) != 3 {
		t.Fatalf("with no release plans every Automaton is unassigned: %d", len(roll.Unassigned.Automata))
	}

	tl, terr := buildProjectTimeline(timelineInput{Project: "acme", Dir: dir, Entries: summaryOf(prds), Now: timelineNow, ReleaseHeld: held})
	if terr != nil {
		t.Fatal(terr)
	}
	count := -1
	for _, row := range tl.Rows {
		if row.Kind == timelineKindUnassigned {
			count = row.ChildrenCount
		}
	}
	if count != 2 {
		t.Fatalf("gantt unassigned row counts %d, want 2", count)
	}
	tl, _ = buildProjectTimeline(timelineInput{Project: "acme", Dir: dir, Entries: summaryOf(prds), Now: timelineNow})
	for _, row := range tl.Rows {
		if row.Kind == timelineKindUnassigned && row.ChildrenCount != 3 {
			t.Fatalf("with no release plans the row counts %d, want 3", row.ChildrenCount)
		}
	}
}

// --- archive and restore -----------------------------------------------------

func TestArchiveRestoreRoundTripDeletesNothing(t *testing.T) {
	rr := newRelRig(t)
	if code, _ := rr.apply(t, rr.preview(t)["preview_id"].(string)); code != 200 {
		t.Fatal("apply failed")
	}
	cur := rr.releasePlanID(t, "10.0")
	before, _ := rr.store.Get(cur)
	planIDs := func(q string) []string {
		out := rr.must(t, 200, http.MethodGet, "/api/projects/datawatch/plans"+q, nil)
		ids := []string{}
		for _, p := range relItems(out, "plans") {
			ids = append(ids, p["id"].(string))
		}
		for _, p := range relItems(out["backlog"].(map[string]any), "plans") {
			ids = append(ids, p["id"].(string))
		}
		return ids
	}
	contains := func(ids []string, id string) bool {
		for _, v := range ids {
			if v == id {
				return true
			}
		}
		return false
	}
	if !contains(planIDs(""), cur) {
		t.Fatal("the release plan is not in the default list")
	}

	out := rr.must(t, 200, http.MethodPost, "/api/plans/"+cur+"/archive", map[string]any{"reason": "shipped"})
	if out["changed"] != true || out["automata_held"].(float64) < 1 {
		t.Fatalf("archive: %v", out)
	}
	plan := out["plan"].(map[string]any)
	if plan["archived"] != true || plan["archive_reason"] != "shipped" || plan["plan_kind"] != "release" || plan["release"] != "10.0" {
		t.Fatalf("archived plan = %v", plan)
	}
	if contains(planIDs(""), cur) || contains(planIDs("?archived=exclude"), cur) {
		t.Fatal("an archived plan is in the default list")
	}
	if !contains(planIDs("?archived=include"), cur) || !contains(planIDs("?archived=only"), cur) {
		t.Fatal("archived=include and archived=only must list the archived plan")
	}
	for _, id := range planIDs("?archived=only") {
		if id != cur {
			t.Fatalf("archived=only listed %s", id)
		}
	}
	// Nothing was deleted: the record, its links and its themes are all there.
	arch, err := rr.store.Get(cur)
	if err != nil || !reflect.DeepEqual(arch.Automata, before.Automata) || !reflect.DeepEqual(arch.Themes, before.Themes) {
		t.Fatalf("archive changed links or themes: %+v %v", arch, err)
	}
	if ref, ok := rr.store.PlanOf("a-sec"); !ok || ref.ID != cur {
		t.Fatalf("a-sec lost its plan: %+v", ref)
	}
	// Archiving again changes nothing.
	if out = rr.must(t, 200, http.MethodPost, "/api/plans/"+cur+"/archive", nil); out["changed"] != false {
		t.Fatalf("second archive: %v", out)
	}

	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+cur+"/restore", nil)
	if out["changed"] != true || out["plan"].(map[string]any)["archived"] != nil {
		t.Fatalf("restore: %v", out)
	}
	if !contains(planIDs(""), cur) {
		t.Fatal("a restored plan is not back in the default list")
	}
	back, _ := rr.store.Get(cur)
	if back.Order != before.Order || back.Archived || !reflect.DeepEqual(back.Automata, before.Automata) {
		t.Fatalf("restore: %+v, was %+v", back, before)
	}
	if out = rr.must(t, 200, http.MethodPost, "/api/plans/"+cur+"/restore", nil); out["changed"] != false {
		t.Fatalf("second restore: %v", out)
	}

	// Audit entries name the operator.
	var archived, restored bool
	for _, e := range prAuditActions(t, rr.audit) {
		if e.Action == audit.ActionPlanRecordArchived && e.Details["actor"] == "operator" {
			archived = true
		}
		if e.Action == audit.ActionPlanRecordRestored && e.Details["actor"] == "operator" {
			restored = true
		}
	}
	if !archived || !restored {
		t.Fatalf("archive audited %v, restore audited %v", archived, restored)
	}
}

func TestArchiveProtectedPlanAndErrors(t *testing.T) {
	rr := newRelRig(t)
	if code, _ := rr.apply(t, rr.preview(t)["preview_id"].(string)); code != 200 {
		t.Fatal("apply failed")
	}
	cur := rr.releasePlanID(t, "10.0")
	if code, out := rr.send(t, http.MethodPost, "/api/plans/"+cur+"/archive", map[string]any{"protected_plan": "Release 10.0"}); code != http.StatusConflict {
		t.Fatalf("protected plan: %d %v", code, out)
	}
	if p, _ := rr.store.Get(cur); p.Archived {
		t.Fatal("a protected plan was archived")
	}
	// The protection applies to the named plan only.
	next := rr.releasePlanID(t, "11.0")
	rr.must(t, 200, http.MethodPost, "/api/plans/"+next+"/archive", map[string]any{"protected_plan": cur})
	for _, c := range []struct {
		name, path string
		body       any
		want       int
	}{
		{"unknown plan", "/api/plans/nope/archive", nil, 404},
		{"unknown plan restore", "/api/plans/nope/restore", nil, 404},
		{"unknown protected plan", "/api/plans/" + cur + "/archive", map[string]any{"protected_plan": "nope"}, 404},
		{"unknown field", "/api/plans/" + cur + "/archive", map[string]any{"bogus": 1}, 400},
		{"long reason", "/api/plans/" + cur + "/archive", map[string]any{"reason": strings.Repeat("x", 1001)}, 400},
	} {
		if code, out := rr.send(t, http.MethodPost, c.path, c.body); code != c.want {
			t.Errorf("%s: want %d, got %d %v", c.name, c.want, code, out)
		}
	}
	// A registry row is not a plan.
	row := rr.create(t, "registry-rows", map[string]any{"title": "A row"})
	if code, out := rr.send(t, http.MethodPost, "/api/plans/"+row["id"].(string)+"/archive", nil); code != 400 ||
		!strings.Contains(out["error"].(string), "not a plan") {
		t.Fatalf("archiving a row: %d %v", code, out)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/"+row["id"].(string)+"/restore", nil); code != 404 {
		t.Fatalf("restoring a row: %d", code)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/projects/datawatch/plans?archived=sideways", nil); code == 200 {
		t.Fatal("unexpected 200")
	}
	if code, _ := rr.send(t, http.MethodGet, "/api/projects/datawatch/plans?archived=sideways", nil); code != 400 {
		t.Fatalf("bad archived option: %d", code)
	}
}

// --- plan create, list, browse -----------------------------------------------

func TestPlanCreateReleaseFieldsAndReads(t *testing.T) {
	rr := newRelRig(t)
	out := rr.must(t, 201, http.MethodPost, "/api/plans", map[string]any{
		"project": "datawatch", "title": "Release 12.0", "plan_kind": "release", "release": "12.0"})
	plan := out["plan"].(map[string]any)
	if plan["plan_kind"] != "release" || plan["release"] != "12.0" {
		t.Fatalf("create answer = %v", plan)
	}
	id := plan["id"].(string)
	// An ordinary plan does not carry the fields.
	ord := rr.must(t, 201, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Plain", "plan_kind": "ordinary"})
	if _, ok := ord["plan"].(map[string]any)["plan_kind"]; ok {
		t.Fatalf("ordinary plan has plan_kind: %v", ord)
	}
	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"duplicate release", map[string]any{"project": "datawatch", "title": "Another", "plan_kind": "release", "release": " 12.0 "}, 409},
		{"release needs a label", map[string]any{"project": "datawatch", "title": "X", "plan_kind": "release"}, 400},
		{"label needs the kind", map[string]any{"project": "datawatch", "title": "Y", "release": "13.0"}, 400},
		{"unknown kind", map[string]any{"project": "datawatch", "title": "Z", "plan_kind": "epic"}, 400},
		{"long label", map[string]any{"project": "datawatch", "title": "W", "plan_kind": "release", "release": strings.Repeat("9", 65)}, 400},
	} {
		code, o := rr.send(t, http.MethodPost, "/api/plans", c.body)
		if code != c.want {
			t.Errorf("%s: want %d, got %d %v", c.name, c.want, code, o)
		}
		if c.name == "duplicate release" && o["existing"].(map[string]any)["id"] != id {
			t.Errorf("duplicate release names %v, want %s", o["existing"], id)
		}
	}

	// The plans list returns the fields (this plan holds no Automaton: Backlog).
	list := rr.must(t, 200, http.MethodGet, "/api/projects/datawatch/plans", nil)
	found := false
	for _, p := range relItems(list["backlog"].(map[string]any), "plans") {
		if p["id"] == id {
			found = p["plan_kind"] == "release" && p["release"] == "12.0"
		}
	}
	if !found {
		t.Fatalf("the list does not carry plan_kind and release: %v", list["backlog"])
	}
	// The plan-scoped reads carry them too.
	if th := rr.must(t, 200, http.MethodGet, "/api/plans/"+id+"/themes", nil); th["plan"].(map[string]any)["release"] != "12.0" {
		t.Fatalf("themes plan block = %v", th["plan"])
	}
	if au := rr.must(t, 200, http.MethodGet, "/api/plans/"+id+"/automata", nil); au["plan"].(map[string]any)["plan_kind"] != "release" {
		t.Fatalf("automata plan block = %v", au["plan"])
	}

	// Browse returns the kind and release of the plan that holds a finding.
	fd := rr.create(t, "findings", map[string]any{"title": "A finding"})
	if code, o := rr.send(t, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "finding", "id": fd["id"]}); code != 200 {
		t.Fatalf("attach finding: %d %v", code, o)
	}
	br := rr.must(t, 200, http.MethodGet, "/api/plan-records/browse?kind=finding", nil)
	rows := relItems(br, "records")
	if len(rows) != 1 || rows[0]["plan_kind"] != "release" || rows[0]["release"] != "12.0" {
		t.Fatalf("browse rows = %v", rows)
	}
}

// --- themes ------------------------------------------------------------------

func (rr *relRig) releasePlan(t *testing.T, release string, attach ...string) string {
	t.Helper()
	out := rr.must(t, 201, http.MethodPost, "/api/plans", map[string]any{
		"project": "datawatch", "title": "Release " + release, "plan_kind": "release", "release": release})
	id := out["plan"].(map[string]any)["id"].(string)
	for _, a := range attach {
		rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "automaton", "id": a})
	}
	return id
}

func themeNames(out map[string]any) []string {
	names := []string{}
	for _, th := range relItems(out, "themes") {
		names = append(names, th["name"].(string))
	}
	return names
}

func TestThemeActions(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0", "a-sec", "a-plain")
	post := func(body map[string]any) (int, map[string]any) {
		return rr.send(t, http.MethodPost, "/api/plans/"+id+"/themes", body)
	}
	out := rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "create", "name": "Security"})
	secID := out["theme"].(map[string]any)["id"].(string)
	rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "create", "name": "Misc"})
	if got := themeNames(rr.must(t, 200, http.MethodGet, "/api/plans/"+id+"/themes", nil)); !reflect.DeepEqual(got, []string{"Security", "Misc"}) {
		t.Fatalf("themes = %v", got)
	}

	// place (by theme name), items listed per theme, the rest unplaced.
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "place", "theme": "security", "kind": "automaton", "id": "a-sec"})
	if got := relIDs(relItems(relItems(out, "themes")[0], "items")); !reflect.DeepEqual(got, []string{"a-sec"}) {
		t.Fatalf("placed = %v", got)
	}
	if got := relIDs(relItems(out, "unplaced")); !reflect.DeepEqual(got, []string{"a-plain"}) {
		t.Fatalf("unplaced = %v", got)
	}
	// A move: an item is in one theme only.
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "place", "theme": "Misc", "kind": "automaton", "id": "a-sec"})
	if len(relItems(relItems(out, "themes")[0], "items")) != 0 || len(relItems(relItems(out, "themes")[1], "items")) != 1 {
		t.Fatalf("a place did not move the item: %v", out["themes"])
	}
	// unplace
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "unplace", "kind": "automaton", "id": "a-sec"})
	if got := relIDs(relItems(out, "unplaced")); !reflect.DeepEqual(got, []string{"a-plain", "a-sec"}) {
		t.Fatalf("unplaced = %v", got)
	}
	// rename, reorder
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "rename", "theme": secID, "name": "Security and encryption"})
	if got := themeNames(out); !reflect.DeepEqual(got, []string{"Security and encryption", "Misc"}) {
		t.Fatalf("after rename = %v", got)
	}
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "reorder", "themes": []string{"Misc", secID}})
	if got := themeNames(out); !reflect.DeepEqual(got, []string{"Misc", "Security and encryption"}) {
		t.Fatalf("after reorder = %v", got)
	}
	// reorder_items
	for _, a := range []string{"a-plain", "a-sec"} {
		rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "place", "theme": "Misc", "kind": "automaton", "id": a})
	}
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{
		"action": "reorder_items", "theme": "Misc", "kind": "automaton", "items": []string{"a-sec", "a-plain"}})
	var order []string
	for _, it := range relItems(relItems(out, "themes")[0], "items") {
		order = append(order, it["id"].(string))
	}
	if !reflect.DeepEqual(order, []string{"a-sec", "a-plain"}) {
		t.Fatalf("item order = %v", order)
	}
	// delete: the items stay in the plan, unplaced.
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "delete", "theme": "Misc"})
	if got := relIDs(relItems(out, "unplaced")); !reflect.DeepEqual(got, []string{"a-plain", "a-sec"}) {
		t.Fatalf("after delete, unplaced = %v", got)
	}
	if p, _ := rr.store.Get(id); len(p.Automata) != 2 {
		t.Fatalf("deleting a theme detached items: %v", p.Automata)
	}

	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"no action", map[string]any{}, 400},
		{"unknown action", map[string]any{"action": "explode"}, 400},
		{"create without name", map[string]any{"action": "create"}, 400},
		{"duplicate name (any case)", map[string]any{"action": "create", "name": "Security And Encryption"}, 400},
		{"rename unknown theme", map[string]any{"action": "rename", "theme": "nope", "name": "X"}, 404},
		{"delete unknown theme", map[string]any{"action": "delete", "theme": "nope"}, 404},
		{"place without theme", map[string]any{"action": "place", "kind": "automaton", "id": "a-sec"}, 400},
		{"place an item the plan lacks", map[string]any{"action": "place", "theme": "security and encryption", "kind": "automaton", "id": "a-held"}, 400},
		{"place bad kind", map[string]any{"action": "place", "theme": "security and encryption", "kind": "finding", "id": "x"}, 400},
		{"unplace an unplaced item", map[string]any{"action": "unplace", "kind": "automaton", "id": "a-sec"}, 404},
		{"reorder without list", map[string]any{"action": "reorder"}, 400},
		{"reorder missing a theme", map[string]any{"action": "reorder", "themes": []string{}}, 400},
		{"unknown field", map[string]any{"action": "create", "name": "Q", "bogus": 1}, 400},
	} {
		if code, o := post(c.body); code != c.want {
			t.Errorf("%s: want %d, got %d %v", c.name, c.want, code, o)
		}
	}
	// Themes belong to release plans.
	ord := rr.must(t, 201, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Ordinary"})["plan"].(map[string]any)["id"].(string)
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/"+ord+"/themes", map[string]any{"action": "create", "name": "X"}); code != 400 {
		t.Fatalf("a theme on an ordinary plan: %d", code)
	}
	if code, _ := rr.send(t, http.MethodGet, "/api/plans/nope/themes", nil); code != 404 {
		t.Fatalf("themes of an unknown plan: %d", code)
	}
	// An ordinary plan reads as no themes with every item unplaced.
	if out := rr.must(t, 200, http.MethodGet, "/api/plans/"+ord+"/themes", nil); len(relItems(out, "themes")) != 0 {
		t.Fatalf("ordinary plan themes = %v", out)
	}
}

func TestThemesPutSetsTheWholeLayout(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0", "a-sec", "a-plain", "a-held")
	put := func(themes any) (int, map[string]any) {
		return rr.send(t, http.MethodPut, "/api/plans/"+id+"/themes", map[string]any{"themes": themes})
	}
	// Layout one: two new themes with items in a given order.
	code, out := put([]map[string]any{
		{"name": "Security", "automata": []string{"a-plain", "a-sec"}},
		{"name": "Mobile", "automata": []string{"a-held"}},
	})
	if code != 200 {
		t.Fatalf("put: %d %v", code, out)
	}
	th := relItems(out, "themes")
	if len(th) != 2 || th[0]["name"] != "Security" || th[1]["name"] != "Mobile" {
		t.Fatalf("themes = %v", th)
	}
	var sec []string
	for _, it := range relItems(th[0], "items") {
		sec = append(sec, it["id"].(string))
	}
	if !reflect.DeepEqual(sec, []string{"a-plain", "a-sec"}) || len(relItems(out, "unplaced")) != 0 {
		t.Fatalf("security = %v, unplaced = %v", sec, out["unplaced"])
	}
	secID := th[0]["id"].(string)

	// Layout two: swap the order, rename one by id, drop the other's item and
	// leave a-sec unlisted (so it is unplaced); Mobile is removed.
	code, out = put([]map[string]any{
		{"id": secID, "name": "Security and encryption", "automata": []string{"a-plain"}},
		{"name": "Brand new"},
	})
	if code != 200 {
		t.Fatalf("put 2: %d %v", code, out)
	}
	if got := themeNames(out); !reflect.DeepEqual(got, []string{"Security and encryption", "Brand new"}) {
		t.Fatalf("themes 2 = %v", got)
	}
	if relItems(out, "themes")[0]["id"] != secID {
		t.Fatalf("a rename by id changed the theme id: %v", out["themes"])
	}
	if got := relIDs(relItems(out, "unplaced")); !reflect.DeepEqual(got, []string{"a-held", "a-sec"}) {
		t.Fatalf("unplaced 2 = %v", got)
	}
	// Same layout again changes nothing, an empty list removes every theme and
	// no item leaves the plan.
	if code, _ = put([]map[string]any{{"id": secID, "automata": []string{"a-plain"}}, {"name": "brand new"}}); code != 200 {
		t.Fatalf("idempotent put: %d", code)
	}
	if code, out = put([]map[string]any{}); code != 200 || len(relItems(out, "themes")) != 0 {
		t.Fatalf("empty layout: %d %v", code, out)
	}
	if p, _ := rr.store.Get(id); len(p.Automata) != 3 {
		t.Fatalf("the layout detached items: %v", p.Automata)
	}

	// Refusals write nothing.
	before, _ := rr.store.Get(id)
	for _, c := range []struct {
		name   string
		themes any
		want   int
	}{
		{"missing list", nil, 400},
		{"unknown theme id", []map[string]any{{"id": "nope", "name": "X"}}, 404},
		{"unnamed theme", []map[string]any{{"automata": []string{"a-sec"}}}, 400},
		{"duplicate names", []map[string]any{{"name": "A"}, {"name": "a"}}, 400},
		{"item the plan lacks", []map[string]any{{"name": "A", "automata": []string{"a-other"}}}, 400},
		{"item in two themes", []map[string]any{{"name": "A", "automata": []string{"a-sec"}}, {"name": "B", "automata": []string{"a-sec"}}}, 400},
	} {
		var body any = map[string]any{"themes": c.themes}
		if c.themes == nil {
			body = map[string]any{}
		}
		if code, o := rr.send(t, http.MethodPut, "/api/plans/"+id+"/themes", body); code != c.want {
			t.Errorf("%s: want %d, got %d %v", c.name, c.want, code, o)
		}
	}
	if after, _ := rr.store.Get(id); !reflect.DeepEqual(before.Themes, after.Themes) || before.UpdatedAt != after.UpdatedAt {
		t.Fatal("a refused PUT wrote to the plan")
	}
	ord := rr.must(t, 201, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Ordinary"})["plan"].(map[string]any)["id"].(string)
	if code, _ := rr.send(t, http.MethodPut, "/api/plans/"+ord+"/themes", map[string]any{"themes": []any{}}); code != 400 {
		t.Fatalf("PUT themes on an ordinary plan: %d", code)
	}
}

// --- bulk attach -------------------------------------------------------------

func TestBulkAttachBySelectionPlacesEachInATheme(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0")
	rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "create", "name": "Security"})

	out := rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{
		"ids": []string{"a-sec", "a-plain", "a-other", "ghost", "a-sec"}, "theme": "security"})
	res := map[string]map[string]any{}
	for _, r := range relItems(out, "results") {
		res[r["id"].(string)] = r
	}
	if len(res) != 4 || len(relItems(out, "results")) != 4 { // the duplicate id is dropped
		t.Fatalf("results = %v", out["results"])
	}
	if res["a-sec"]["status"] != "attached" || res["a-plain"]["status"] != "attached" {
		t.Fatalf("selected Automata not attached: %v", res)
	}
	if res["a-other"]["status"] != "refused" || res["a-other"]["holder"] == "" || res["a-other"]["error"] == "" {
		t.Fatalf("an Automaton attached elsewhere must be refused and named: %v", res["a-other"])
	}
	if res["ghost"]["status"] != "not_found" {
		t.Fatalf("unknown id: %v", res["ghost"])
	}
	if relInt(out, "counts", "attached") != 2 || relInt(out, "counts", "refused") != 1 || relInt(out, "counts", "not_found") != 1 {
		t.Fatalf("counts = %v", out["counts"])
	}
	// The refused one is untouched and the others are placed.
	if ref, _ := rr.store.PlanOf("a-other"); ref.Title != "Old plan" {
		t.Fatalf("a-other moved to %+v", ref)
	}
	th := rr.must(t, 200, http.MethodGet, "/api/plans/"+id+"/themes", nil)
	if got := relIDs(relItems(relItems(th, "themes")[0], "items")); !reflect.DeepEqual(got, []string{"a-plain", "a-sec"}) || len(relItems(th, "unplaced")) != 0 {
		t.Fatalf("placement = %v", th)
	}
	// Doing it again reports already_attached and keeps the placement.
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"ids": []string{"a-sec"}, "theme": "Security"})
	if relItems(out, "results")[0]["status"] != "already_attached" || relItems(out, "results")[0]["theme"] == "" {
		t.Fatalf("repeat = %v", out["results"])
	}
	// Without a theme the items are attached and left unplaced.
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"ids": []string{"a-ship"}})
	if relItems(out, "results")[0]["status"] != "attached" {
		t.Fatalf("no-theme attach = %v", out)
	}
	if th = rr.must(t, 200, http.MethodGet, "/api/plans/"+id+"/themes", nil); !reflect.DeepEqual(relIDs(relItems(th, "unplaced")), []string{"a-ship"}) {
		t.Fatalf("unplaced = %v", th["unplaced"])
	}
	// Automations attach in bulk too.
	out = rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "automation", "ids": []string{"au-1"}})
	if relItems(out, "results")[0]["status"] != "attached" {
		t.Fatalf("automation bulk = %v", out)
	}

	// Refusals before anything is written.
	idOnly := rr.releasePlan(t, "13.0")
	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"unknown theme", map[string]any{"ids": []string{"a-held"}, "theme": "nope"}, 404},
		{"id and ids", map[string]any{"id": "a-held", "ids": []string{"a-held"}}, 400},
		{"bad kind", map[string]any{"kind": "finding", "ids": []string{"x"}}, 400},
		{"empty id in list", map[string]any{"ids": []string{" "}}, 400},
	} {
		if code, o := rr.send(t, http.MethodPost, "/api/plans/"+idOnly+"/attach", c.body); code != c.want {
			t.Errorf("%s: want %d, got %d %v", c.name, c.want, code, o)
		}
	}
	if _, ok := rr.store.PlanOf("a-held"); ok {
		t.Fatal("a refused bulk attach attached something")
	}
	ord := rr.must(t, 201, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Ordinary"})["plan"].(map[string]any)["id"].(string)
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/"+ord+"/attach", map[string]any{"ids": []string{"a-held"}, "theme": "X"}); code != 400 {
		t.Fatalf("theme on an ordinary plan: %d", code)
	}
	tooMany := make([]string, maxBulkAttachIDs+1)
	for i := range tooMany {
		tooMany[i] = "a-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/"+idOnly+"/attach", map[string]any{"ids": tooMany}); code != 400 {
		t.Fatalf("too many ids: %d", code)
	}
	// No Automata manager: the list cannot be checked.
	rr.s.planItemSource = nil
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/"+idOnly+"/attach", map[string]any{"ids": []string{"a-held"}}); code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: %d", code)
	}
}

func TestSingleAttachWithThemePlacesTheItem(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0")
	rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "create", "name": "Security"})
	if code, o := rr.send(t, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "automaton", "id": "a-sec", "theme": "nope"}); code != 404 {
		t.Fatalf("unknown theme: %d %v", code, o)
	}
	if _, ok := rr.store.PlanOf("a-sec"); ok {
		t.Fatal("attached despite an unknown theme")
	}
	rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "automaton", "id": "a-sec", "theme": "Security"})
	th := rr.must(t, 200, http.MethodGet, "/api/plans/"+id+"/themes", nil)
	if got := relIDs(relItems(relItems(th, "themes")[0], "items")); !reflect.DeepEqual(got, []string{"a-sec"}) {
		t.Fatalf("placement = %v", th)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "finding", "id": "x", "theme": "Security"}); code != 400 {
		t.Fatalf("a theme on a finding: %d", code)
	}
}

// --- row-plans ---------------------------------------------------------------

func TestRowPlansListAndConversion(t *testing.T) {
	rr := newRelRig(t)
	rr.create(t, "registry-rows", map[string]any{"title": "Backlog item", "legacy_number": 7})
	if _, err := rr.s.runPlanMigration(true, "test"); err != nil {
		t.Fatal(err)
	}
	list := rr.must(t, 200, http.MethodGet, "/api/plans/migrate/row-plans", nil)
	rows := relItems(list, "row_plans")
	if len(rows) != 1 || relInt(list, "total") != 1 || rows[0]["source"] != "registry-row-backlog" || rows[0]["row_found"] != true {
		t.Fatalf("row-plans = %v", list)
	}
	rowPlanID := rows[0]["plan_id"].(string)
	if got := relItems(rr.must(t, 200, http.MethodGet, "/api/plans/migrate/row-plans?project=datawatch", nil), "row_plans"); len(got) != 1 {
		t.Fatalf("filtered = %v", got)
	}
	if code, _ := rr.send(t, http.MethodGet, "/api/plans/migrate/row-plans?project=nope", nil); code != 400 {
		t.Fatalf("unknown project: %d", code)
	}

	// No current release plan yet: the row-plan waits.
	out := rr.must(t, 200, http.MethodPost, "/api/plans/migrate/row-plans", nil)
	if len(relItems(out, "waiting")) != 1 || len(relItems(out, "backlog_items")) != 0 {
		t.Fatalf("waiting preview = %v", out)
	}
	cur := rr.releasePlan(t, "10.0")

	before := rr.store.Len()
	out = rr.must(t, 200, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"project": "datawatch"})
	if out["applied"] != false || len(relItems(out, "backlog_items")) != 1 || out["preview_id"] == "" {
		t.Fatalf("preview = %v", out)
	}
	if p, _ := rr.store.Get(rowPlanID); p.Archived || rr.store.Len() != before {
		t.Fatal("a preview changed records")
	}
	pid := out["preview_id"].(string)
	if code, o := rr.send(t, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"apply": true}); code != 400 {
		t.Fatalf("no preview id: %d %v", code, o)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"apply": true, "preview_id": "0123456789abcdef"}); code != 409 {
		t.Fatalf("stale preview id: %d", code)
	}
	if p, _ := rr.store.Get(rowPlanID); p.Archived {
		t.Fatal("a refused written run archived the row-plan")
	}
	out = rr.must(t, 200, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"apply": true, "preview_id": pid})
	if out["applied"] != true || len(relItems(out, "backlog_items")) != 1 {
		t.Fatalf("apply = %v", out)
	}
	p, _ := rr.store.Get(rowPlanID)
	if !p.Archived || !strings.Contains(p.ArchiveReason, "converted") {
		t.Fatalf("the row-plan is archived with a reason naming the conversion: %+v", p)
	}
	if rows, err := rr.store.RowsOf(cur); err != nil || len(rows.Backlog) != 1 {
		t.Fatalf("backlog items of the release plan: %+v %v", rows, err)
	}
	if rr.store.Len() != before {
		t.Fatal("the conversion deleted or added records")
	}
	again := rr.must(t, 200, http.MethodPost, "/api/plans/migrate/row-plans", nil)
	if len(relItems(again, "backlog_items")) != 0 || len(relItems(again, "unchanged")) != 1 {
		t.Fatalf("second preview = %v", again)
	}
	code, o := rr.send(t, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"apply": true, "preview_id": again["preview_id"]})
	if code != 200 || !strings.Contains(o["note"].(string), "Nothing to convert") {
		t.Fatalf("second apply: %d %v", code, o)
	}
	// The archived row-plan is still listed (archived), never deleted.
	if got := relItems(rr.must(t, 200, http.MethodGet, "/api/plans/migrate/row-plans", nil), "row_plans"); len(got) != 1 || got[0]["archived"] != true {
		t.Fatalf("row-plans after = %v", got)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"project": "nope"}); code != 400 {
		t.Fatalf("unknown project: %d", code)
	}
	if code, _ := rr.send(t, http.MethodPost, "/api/plans/migrate/row-plans", map[string]any{"bogus": 1}); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}
}

// --- gating ------------------------------------------------------------------

func TestReleaseRoutesGating(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0")
	type call struct {
		method, path string
		body         any
		write        bool
	}
	calls := []call{
		{http.MethodGet, "/api/plans/" + id + "/themes", nil, false},
		{http.MethodPut, "/api/plans/" + id + "/themes", map[string]any{"themes": []any{}}, true},
		{http.MethodPost, "/api/plans/" + id + "/themes", map[string]any{"action": "create", "name": "X"}, true},
		{http.MethodPost, "/api/plans/" + id + "/archive", nil, true},
		{http.MethodPost, "/api/plans/" + id + "/restore", nil, true},
		{http.MethodGet, "/api/plans/unattributed", nil, false},
		{http.MethodGet, "/api/plans/migrate/row-plans", nil, false},
		{http.MethodPost, "/api/plans/migrate/row-plans", nil, true},
		{http.MethodPost, plansMigratePath, map[string]any{"mode": "release", "project": "datawatch"}, true},
		{http.MethodPost, "/api/plans/" + id + "/attach", map[string]any{"ids": []string{"a-sec"}}, true},
	}
	for _, c := range calls {
		// A peer with only the read capability reads and cannot write.
		code, _ := rr.send(t, c.method, c.path, c.body, federation.CapAutonomousRead)
		if c.write && code != http.StatusForbidden {
			t.Errorf("%s %s with read only: want 403, got %d", c.method, c.path, code)
		}
		if !c.write && code != http.StatusOK {
			t.Errorf("%s %s with read: want 200, got %d", c.method, c.path, code)
		}
		// No capability at all is refused.
		if code, _ := rr.send(t, c.method, c.path, c.body, "unrelated:cap"); code != http.StatusForbidden {
			t.Errorf("%s %s with no autonomous capability: want 403, got %d", c.method, c.path, code)
		}
		// A peer with write can do what the call asks.
		if code, _ := rr.send(t, c.method, c.path, c.body, federation.CapAutonomousRead, federation.CapAutonomousWrite); code != http.StatusOK {
			t.Errorf("%s %s with read and write: want 200, got %d", c.method, c.path, code)
		}
	}
	// A wrong method answers 405 on the handler routes.
	for _, c := range []struct{ method, path string }{
		{http.MethodDelete, "/api/plans/migrate/row-plans"},
	} {
		if code, _ := rr.send(t, c.method, c.path, nil); code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: want 405, got %d", c.method, c.path, code)
		}
	}
	// Plan records off: every route is 404, nothing is written.
	n := rr.store.Len()
	off := false
	rr.s.cfg.Autonomous.PlanRecords.Enabled = &off
	for _, c := range calls {
		if code, _ := rr.send(t, c.method, c.path, c.body); code != http.StatusNotFound {
			t.Errorf("%s %s with plan records off: want 404, got %d", c.method, c.path, code)
		}
	}
	if rr.store.Len() != n {
		t.Fatal("a route wrote while plan records were off")
	}
	// No store wired: 503.
	on := true
	rr.s.cfg.Autonomous.PlanRecords.Enabled = &on
	rr.s.planRecords = nil
	for _, c := range calls {
		if code, _ := rr.send(t, c.method, c.path, c.body); code != http.StatusServiceUnavailable {
			t.Errorf("%s %s with no store: want 503, got %d", c.method, c.path, code)
		}
	}
}

func TestReleaseHeldFuncIsNilWithoutPlanRecords(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig()}
	if s.releaseHeldFunc() != nil {
		t.Fatal("no store must read as no release plans")
	}
	if n, ok := s.unattributedAutomata("datawatch"); ok || n != 0 {
		t.Fatalf("no Automata manager: %d %v", n, ok)
	}
}
