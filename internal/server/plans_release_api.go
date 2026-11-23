// plans_release_api.go — the release plans API: themes, archive and restore,
// the Automata no release plan holds, the bulk attach, and the release and
// row-plan migrations.
//
//	GET  /api/plans/{ref}/themes            a plan's themes and its unplaced items (autonomous:read)
//	PUT  /api/plans/{ref}/themes            set the whole theme layout of a release plan (autonomous:write)
//	POST /api/plans/{ref}/themes            one theme action: create, rename, delete, place, unplace,
//	                                        reorder, reorder_items (autonomous:write)
//	POST /api/plans/{ref}/archive           archive a plan; nothing is deleted (autonomous:write)
//	POST /api/plans/{ref}/restore           clear the archive mark (autonomous:write)
//	GET  /api/plans/unattributed            the Automata no release plan holds, with a suggested
//	                                        release and theme (autonomous:read)
//	GET  /api/plans/migrate/row-plans       list the plans the plans migration made from registry rows
//	POST /api/plans/migrate/row-plans       convert them into release plan items (preview by default)
//
// The release migration itself is mode "release" of POST /api/plans/migrate
// (see plans_migrate.go), and the bulk attach is the "ids" form of
// POST /api/plans/{ref}/attach (see plans_write_api.go).
//
// Every route reuses the plan-record store methods; none rewrites a record
// itself. The gating is the one the other plan routes use: plan records off
// answers 404, a read needs autonomous:read, a write needs autonomous:write,
// and the audit actor comes from auditActor. The theme store methods, like
// attach and detach, write their own audit entries; archive, restore and the
// two migrations take the actor and pass it on.
//
// A written migration needs the preview id of the exact preview the operator
// approved. A missing id answers 400 and a stale one 409, and neither writes
// anything. A request only reads and writes record files; it never starts,
// signals or restarts a process.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// maxBulkAttachIDs bounds one bulk attach.
const maxBulkAttachIDs = 500

// maxPlanThemes and maxPlanThemeItems bound the sizes a themes layout
// request may name, before any of them is used to size an allocation. The
// request body is capped, so a real layout always fits; the bounds exist so
// a crafted body can never steer a make() size near the int limit.
const (
	maxPlanThemes     = 4096
	maxPlanThemeItems = 4096
)

// ---------------------------------------------------------------------------
// Facts: what the release engine and the unattributed list know about work.

// planItemFact is one Automaton or Automation as the release plans API sees
// it: the engine's fact plus the project the item resolves to.
type planItemFact struct {
	planrecords.ReleaseFact
	Project string `json:"project,omitempty"`
}

// errPlanFactsUnavailable is returned when the Automata manager is not wired,
// so no Automaton can be read.
var errPlanFactsUnavailable = errors.New("the Automata manager is not available")

// planItemFacts reads every Automaton and Automation with its project. When
// detail is false the tags and the hold check (which reads each Automaton's
// text) are left out; a count does not need them. A test sets planFactsSource.
// maxFactsPrealloc caps the preallocation hint planItemFacts passes to
// make: two lengths that each fit in an int can still overflow on their
// sum, and no preallocation is worth that.
const maxFactsPrealloc = 1 << 20

// factsPreallocHint returns len(prds)+len(autos) as a preallocation hint,
// clamped to maxFactsPrealloc so the addition can never overflow.
func factsPreallocHint(prds, autos int) int {
	if prds < 0 || autos < 0 {
		return 0
	}
	if prds > maxFactsPrealloc || autos > maxFactsPrealloc-prds {
		return maxFactsPrealloc
	}
	return prds + autos
}

func (s *Server) planItemFacts(detail bool) ([]planItemFact, error) {
	if s.planFactsSource != nil {
		return s.planFactsSource()
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		return nil, errPlanFactsUnavailable
	}
	prds := mgr.Store().ListPRDs()
	autos := mgr.ListAutomations()
	idx := autonomous.NewAutomatonProjectIndex(prds, autos, rollupProjectDirs(s.cfg.Projects))
	shipped := s.shippedSettings().EffectiveEnabled()

	byID := map[string]*autonomous.Automation{}
	memberOf := map[string]*autonomous.Automation{}
	for _, a := range autos {
		if a == nil {
			continue
		}
		byID[a.ID] = a
		for _, m := range a.Members {
			if m.PRDID == "" {
				continue
			}
			if _, ok := memberOf[m.PRDID]; !ok {
				memberOf[m.PRDID] = a
			}
		}
	}
	tagName := map[string]string{}
	if detail {
		for _, t := range mgr.ListTags() {
			if t != nil {
				tagName[t.ID] = t.Name
			}
		}
	}

	out := make([]planItemFact, 0, factsPreallocHint(len(prds), len(autos)))
	for _, p := range prds {
		if p == nil || p.IsTemplate {
			continue
		}
		f := planItemFact{Project: idx.Of(p.ID).Project}
		f.ID, f.Kind, f.Title = p.ID, planrecords.ReleaseKindAutomaton, p.Title
		f.State = autonomous.PRDEffectiveDisplayStatus(p, shipped)
		f.Release = p.MergedInRelease
		auto := byID[p.AutomationID]
		if auto == nil {
			auto = memberOf[p.ID]
		}
		if auto != nil {
			f.Automation, f.AutomationTitle = auto.ID, auto.Name
		}
		if detail {
			for _, id := range p.TagIDs {
				if n := tagName[id]; n != "" {
					f.Tags = append(f.Tags, n)
				}
			}
			sort.Strings(f.Tags)
			code, _ := autonomous.PRDHoldCode(p, nil)
			f.Held = code != ""
		}
		out = append(out, f)
	}
	for _, a := range autos {
		if a == nil {
			continue
		}
		f := planItemFact{Project: a.Project}
		f.ID, f.Kind, f.Title = a.ID, planrecords.ReleaseKindAutomation, a.Name
		f.State = autonomous.AutomationEffectiveDisplayStatus(a, shipped)
		f.Release = a.MergedInRelease
		out = append(out, f)
	}
	return out, nil
}

// releaseResolver gives the release engine the facts of one project.
func (s *Server) releaseResolver(project string) planrecords.ReleaseResolver {
	return func() ([]planrecords.ReleaseFact, error) {
		facts, err := s.planItemFacts(true)
		if err != nil {
			return nil, err
		}
		var out []planrecords.ReleaseFact
		for _, f := range facts {
			if f.Project == project {
				out = append(out, f.ReleaseFact)
			}
		}
		return out, nil
	}
}

// releaseHeldFunc says whether a release plan holds an Automaton. An archived
// release plan, and a plan that is not a release plan, does not count. It is
// nil when plan records are off or not wired, which reads as "none holds it".
func (s *Server) releaseHeldFunc() func(automatonID string) bool {
	if s == nil || s.planRecords == nil || s.cfg == nil || !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		return nil
	}
	live := map[string]bool{}
	for _, sm := range s.planRecords.ReleasePlans("") {
		live[sm.UID] = true
	}
	return func(id string) bool {
		ref, ok := s.planRecords.PlanOf(id)
		return ok && live[ref.UID]
	}
}

// unattributedAutomata counts the project's Automata that no release plan
// holds. The second result is false while the Automata manager cannot be read.
func (s *Server) unattributedAutomata(project string) (int, bool) {
	facts, err := s.planItemFacts(false)
	if err != nil {
		return 0, false
	}
	held := s.releaseHeldFunc()
	n := 0
	for _, f := range facts {
		if f.Kind != planrecords.ReleaseKindAutomaton || f.Project != project {
			continue
		}
		if held != nil && held(f.ID) {
			continue
		}
		n++
	}
	return n, true
}

// ---------------------------------------------------------------------------
// Shared helpers.

// planBlock is the small plan object the plan-scoped answers carry. A release
// plan adds its kind and release label, an archived plan its mark.
func planBlock(sm planrecords.Summary) map[string]any {
	b := map[string]any{"title": sm.Title, "id": sm.ID, "state": string(sm.State)}
	if sm.PlanKind != planrecords.PlanKindOrdinary {
		b["plan_kind"] = string(sm.PlanKind)
	}
	if sm.Release != "" {
		b["release"] = sm.Release
	}
	if sm.Archived {
		b["archived"] = true
	}
	if sm.SetByHand {
		b["set_by_hand"] = true
	}
	return b
}

func planBlockOf(r planrecords.Record) map[string]any {
	return planBlock(planrecords.Summary{
		Title: r.Title, ID: r.ID, State: r.State, SetByHand: r.SetByHand, PlanKind: r.PlanKind, Release: r.Release, Archived: r.Archived,
	})
}

// archiveScopeOf reads the archived list option: archived=exclude (default),
// include or only.
func archiveScopeOf(w http.ResponseWriter, r *http.Request) (planrecords.ArchiveScope, bool) {
	switch v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("archived"))); v {
	case "", "exclude":
		return planrecords.ArchiveScope{}, true
	case "include":
		return planrecords.ArchiveScope{IncludeArchived: true}, true
	case "only":
		return planrecords.ArchiveScope{ArchivedOnly: true}, true
	default:
		planRecordsError(w, http.StatusBadRequest, `archived must be "exclude", "include" or "only"`)
		return planrecords.ArchiveScope{}, false
	}
}

// decodeOptionalPlanJSON reads a body that may be empty: an empty body leaves
// v at its zero value; anything else must be exactly one JSON object.
func decodeOptionalPlanJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPlanRecordBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			planRecordsError(w, http.StatusRequestEntityTooLarge, "request body too large (cap 256 KiB)")
		} else {
			planRecordsError(w, http.StatusBadRequest, "cannot read the body: "+err.Error())
		}
		return false
	}
	if strings.TrimSpace(string(raw)) == "" {
		return true
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		planRecordsError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		planRecordsError(w, http.StatusBadRequest, "invalid JSON body: expected exactly one JSON object")
		return false
	}
	return true
}

// planMethodGate is planReadGate and planWriteGate for a handler that serves both reads and
// writes on one path: it checks plan records on, the capability for the
// method, and the store.
func (s *Server) planMethodGate(w http.ResponseWriter, r *http.Request, methods ...string) (write bool, ok bool) {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return false, false
	}
	if !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		http.Error(w, "plan records are turned off (autonomous.plan_records.enabled)", http.StatusNotFound)
		return false, false
	}
	allowed := false
	for _, m := range methods {
		if r.Method == m {
			allowed = true
		}
	}
	if !allowed {
		w.Header().Set("Allow", strings.Join(methods, ", "))
		planRecordsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false, false
	}
	write = r.Method != http.MethodGet
	need := federation.CapAutonomousRead
	if write {
		need = federation.CapAutonomousWrite
	}
	if !s.fedCap(w, r, need) {
		return write, false
	}
	if s.planRecords == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "plan records are not available")
		return write, false
	}
	return write, true
}

// writeThemeError answers a failure of a theme store call: an unknown theme or
// plan is 404 and names it, anything else follows the plan write errors.
func writeThemeError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, planrecords.ErrNotFound) {
		planRecordsError(w, http.StatusNotFound, strings.TrimPrefix(err.Error(), "planrecords: "))
		return
	}
	writePlanWriteError(w, err, what)
}

// themeIDOf finds a theme of a plan by id or name. Release plans only.
func themeIDOf(plan planrecords.Record, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	for _, t := range plan.Themes {
		if t.ID == ref {
			return t.ID, true
		}
	}
	norm := planrecords.NormTitle(ref)
	if norm == "" {
		return "", false
	}
	for _, t := range plan.Themes {
		if planrecords.NormTitle(t.Name) == norm {
			return t.ID, true
		}
	}
	return "", false
}

// checkThemeRef resolves a theme named in a request, answering the failure
// itself: 400 for a plan that is not a release plan, 404 for an unknown theme.
func checkThemeRef(w http.ResponseWriter, plan planrecords.Record, ref string) (string, bool) {
	if plan.PlanKind != planrecords.PlanKindRelease {
		planRecordsError(w, http.StatusBadRequest,
			fmt.Sprintf("plan %q is not a release plan; themes belong to release plans", plan.ID))
		return "", false
	}
	id, ok := themeIDOf(plan, ref)
	if !ok {
		planRecordsError(w, http.StatusNotFound,
			fmt.Sprintf("no theme matches %q in plan %q; create it first", ref, plan.ID))
		return "", false
	}
	return id, true
}

// planOfRef resolves {ref} to a plan record, answering the failure itself.
func (s *Server) planOfRef(w http.ResponseWriter, ref string) (planrecords.Record, bool) {
	plan, err := s.planRecords.Resolve(ref, "")
	if err == nil && plan.Kind != planrecords.KindPlan {
		err = planrecords.ErrNotFound
	}
	if err != nil {
		writePlanWriteError(w, err, "read the plan")
		return planrecords.Record{}, false
	}
	return plan, true
}

// ---------------------------------------------------------------------------
// Themes.

// themesAnswer is the themes view of a plan with the plan block first.
func (s *Server) themesAnswer(plan planrecords.Record) (map[string]any, error) {
	v, err := s.planRecords.ThemesOf(plan.UID)
	if err != nil {
		return nil, err
	}
	cur, err := s.planRecords.Resolve(plan.UID, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"plan": planBlockOf(cur), "themes": v.Themes, "unplaced": v.Unplaced}, nil
}

// handlePlanThemesGet serves GET /api/plans/{ref}/themes.
func (s *Server) handlePlanThemesGet(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	plan, ok := s.planOfRef(w, ref)
	if !ok {
		return
	}
	out, err := s.themesAnswer(plan)
	if err != nil {
		writeThemeError(w, err, "read the themes")
		return
	}
	writeJSONOK(w, out)
}

// planThemeSetBody is the PUT body: the whole layout of the plan's themes.
type planThemeSetBody struct {
	Themes []planThemeSetEntry `json:"themes"`
}

// planThemeSetEntry is one theme of the layout. ID names an existing theme
// (and may rename it); without an ID the name finds an existing theme or makes
// a new one. Automata and Automations are listed in the order they should have.
type planThemeSetEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Automata    []string `json:"automata"`
	Automations []string `json:"automations"`
}

// handlePlanThemesPut serves PUT /api/plans/{ref}/themes. It sets the plan's
// themes to the list in the body, in that order: a theme not listed is
// removed (its items stay in the plan, unplaced), an item the plan holds that
// no theme lists is unplaced, and a listed item moves into its theme. An item
// the plan does not hold is refused for the whole request before anything is
// written.
func (s *Server) handlePlanThemesPut(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPut) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planThemeSetBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	plan, ok := s.planOfRef(w, ref)
	if !ok {
		return
	}
	if plan.PlanKind != planrecords.PlanKindRelease {
		planRecordsError(w, http.StatusBadRequest,
			fmt.Sprintf("plan %q is not a release plan; themes belong to release plans", plan.ID))
		return
	}
	switch {
	case len(body.Themes) > maxPlanThemes:
		planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("themes holds at most %d entries", maxPlanThemes))
		return
	case body.Themes == nil:
		planRecordsError(w, http.StatusBadRequest, "themes is required: list every theme of the plan in the order it should have (an empty list removes them all)")
		return
	}
	steps, err := planThemeSetSteps(plan, body.Themes)
	if err != nil {
		var inv *planrecords.ErrInvalid
		if errors.As(err, &inv) {
			planRecordsError(w, http.StatusBadRequest, inv.Error())
		} else if errors.Is(err, planrecords.ErrNotFound) {
			planRecordsError(w, http.StatusNotFound, strings.TrimPrefix(err.Error(), "planrecords: "))
		} else {
			planRecordsError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if err := s.applyThemeSet(plan, steps); err != nil {
		writeThemeError(w, err, "set the themes")
		return
	}
	out, err := s.themesAnswer(plan)
	if err != nil {
		writeThemeError(w, err, "read the themes")
		return
	}
	writeJSONOK(w, out)
}

// themeSetSteps is the checked plan of a PUT.
type themeSetSteps struct {
	// keep maps an existing theme id to its new name ("" when unchanged).
	keep map[string]string
	// order is the requested theme order: an existing id or, for a new theme,
	// its name prefixed with "+".
	order []themeSetTheme
	// listed is every item the layout names, by kind and id.
	listed map[string]bool
}

type themeSetTheme struct {
	existingID string // "" for a new theme
	name       string
	automata   []string
	automation []string
}

func cleanIDList(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strings.TrimSpace(id))
	}
	return out
}

// planThemeSetSteps checks a layout against the plan and returns what to do.
// It writes nothing.
func planThemeSetSteps(plan planrecords.Record, entries []planThemeSetEntry) (themeSetSteps, error) {
	st := themeSetSteps{keep: map[string]string{}, listed: map[string]bool{}}
	names := map[string]bool{}
	used := map[string]bool{}
	held := map[string]map[string]bool{planrecords.ItemAutomaton: {}, planrecords.ItemAutomation: {}}
	for _, id := range plan.Automata {
		held[planrecords.ItemAutomaton][id] = true
	}
	for _, id := range plan.Automations {
		held[planrecords.ItemAutomation][id] = true
	}
	for i, e := range entries {
		if len(e.Automata) > maxPlanThemeItems || len(e.Automations) > maxPlanThemeItems {
			return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf(
				"theme %d holds at most %d items per kind (automata, automations)", i+1, maxPlanThemeItems)}
		}
		name := strings.TrimSpace(e.Name)
		norm := planrecords.NormTitle(name)
		t := themeSetTheme{name: name, automata: cleanIDList(e.Automata), automation: cleanIDList(e.Automations)}
		switch {
		case strings.TrimSpace(e.ID) != "":
			id, ok := themeIDOf(plan, e.ID)
			if !ok {
				return st, fmt.Errorf("%w: theme %q", planrecords.ErrNotFound, e.ID)
			}
			t.existingID = id
			if name == "" {
				for _, ex := range plan.Themes {
					if ex.ID == id {
						t.name, norm = ex.Name, planrecords.NormTitle(ex.Name)
					}
				}
			}
		case norm == "":
			return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf("theme %d needs a name or an id", i+1)}
		default:
			if id, ok := themeIDOf(plan, name); ok {
				t.existingID = id
			}
		}
		if norm == "" {
			return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf("theme %d needs a name", i+1)}
		}
		if names[norm] {
			return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf("theme name %q is listed twice", t.name)}
		}
		names[norm] = true
		if t.existingID != "" {
			if used[t.existingID] {
				return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf("theme %q is listed twice", t.existingID)}
			}
			used[t.existingID] = true
			for _, ex := range plan.Themes {
				if ex.ID == t.existingID && ex.Name != t.name {
					st.keep[ex.ID] = t.name
				}
			}
			if _, ok := st.keep[t.existingID]; !ok {
				st.keep[t.existingID] = ""
			}
		}
		for kind, ids := range map[string][]string{planrecords.ItemAutomaton: t.automata, planrecords.ItemAutomation: t.automation} {
			for _, id := range ids {
				if id == "" || !held[kind][id] {
					return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf(
						"%s %q is not linked to plan %q; attach it to the plan first", kind, id, plan.ID)}
				}
				key := kind + ":" + id
				if st.listed[key] {
					return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf("%s %q is listed in more than one theme", kind, id)}
				}
				st.listed[key] = true
			}
		}
		st.order = append(st.order, t)
	}
	// A rename onto the current name of another kept theme needs two steps.
	for id, newName := range st.keep {
		if newName == "" {
			continue
		}
		for _, ex := range plan.Themes {
			if ex.ID != id && planrecords.NormTitle(ex.Name) == planrecords.NormTitle(newName) {
				if _, kept := st.keep[ex.ID]; kept {
					return st, &planrecords.ErrInvalid{Reason: fmt.Sprintf(
						"theme name %q is the current name of another theme; rename in two steps", newName)}
				}
			}
		}
	}
	return st, nil
}

// applyThemeSet writes a checked layout through the theme store methods.
func (s *Server) applyThemeSet(plan planrecords.Record, st themeSetSteps) error {
	pid := plan.UID
	// 1. Remove the themes the layout leaves out.
	for _, ex := range plan.Themes {
		if _, kept := st.keep[ex.ID]; !kept {
			if _, err := s.planRecords.RemoveTheme(pid, ex.ID); err != nil {
				return err
			}
		}
	}
	// 2. Rename kept themes, then create the new ones.
	ids := make([]string, len(st.order))
	for i, t := range st.order {
		if t.existingID != "" {
			ids[i] = t.existingID
			if nn := st.keep[t.existingID]; nn != "" {
				if _, err := s.planRecords.RenameTheme(pid, t.existingID, nn); err != nil {
					return err
				}
			}
		}
	}
	for i, t := range st.order {
		if t.existingID == "" {
			_, th, err := s.planRecords.CreateTheme(pid, t.name)
			if err != nil {
				return err
			}
			ids[i] = th.ID
		}
	}
	// 3. Unplace the items no theme lists.
	cur, err := s.planRecords.Resolve(pid, "")
	if err != nil {
		return err
	}
	for _, th := range cur.Themes {
		for _, id := range th.Automata {
			if !st.listed[planrecords.ItemAutomaton+":"+id] {
				if _, err := s.planRecords.UnplaceFromTheme(pid, planrecords.ItemAutomaton, id); err != nil {
					return err
				}
			}
		}
		for _, id := range th.Automations {
			if !st.listed[planrecords.ItemAutomation+":"+id] {
				if _, err := s.planRecords.UnplaceFromTheme(pid, planrecords.ItemAutomation, id); err != nil {
					return err
				}
			}
		}
	}
	// 4. Place the listed items, then fix the order inside each theme.
	for i, t := range st.order {
		for _, id := range t.automata {
			if _, err := s.planRecords.PlaceInTheme(pid, ids[i], planrecords.ItemAutomaton, id); err != nil {
				return err
			}
		}
		for _, id := range t.automation {
			if _, err := s.planRecords.PlaceInTheme(pid, ids[i], planrecords.ItemAutomation, id); err != nil {
				return err
			}
		}
		if len(t.automata) > 0 {
			if _, err := s.planRecords.ReorderThemeItems(pid, ids[i], planrecords.ItemAutomaton, t.automata); err != nil {
				return err
			}
		}
		if len(t.automation) > 0 {
			if _, err := s.planRecords.ReorderThemeItems(pid, ids[i], planrecords.ItemAutomation, t.automation); err != nil {
				return err
			}
		}
	}
	// 5. Order the themes.
	if len(ids) > 1 {
		if _, err := s.planRecords.ReorderThemes(pid, ids); err != nil {
			return err
		}
	}
	return nil
}

// planThemeActionBody is the POST body. Which fields apply depends on Action:
//
//	create         name
//	rename         theme, name
//	delete         theme
//	place          theme, kind, id
//	unplace        kind, id
//	reorder        themes (every theme of the plan, in the new order)
//	reorder_items  theme, kind, items (every item of that kind the theme holds)
type planThemeActionBody struct {
	Action string   `json:"action"`
	Theme  string   `json:"theme"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	ID     string   `json:"id"`
	Themes []string `json:"themes"`
	Items  []string `json:"items"`
}

// handlePlanThemesAction serves POST /api/plans/{ref}/themes.
func (s *Server) handlePlanThemesAction(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planThemeActionBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	body.Action = strings.ToLower(strings.TrimSpace(body.Action))
	body.Kind, body.ID = strings.ToLower(strings.TrimSpace(body.Kind)), strings.TrimSpace(body.ID)
	need := func(field, v string) bool {
		if strings.TrimSpace(v) == "" {
			planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("%s is required for action %s", field, body.Action))
			return false
		}
		return true
	}
	var rec planrecords.Record
	var err error
	extra := map[string]any{}
	switch body.Action {
	case "create":
		if !need("name", body.Name) {
			return
		}
		var th planrecords.Theme
		rec, th, err = s.planRecords.CreateTheme(ref, body.Name)
		extra["theme"] = th
	case "rename":
		if !need("theme", body.Theme) || !need("name", body.Name) {
			return
		}
		rec, err = s.planRecords.RenameTheme(ref, body.Theme, body.Name)
	case "delete":
		if !need("theme", body.Theme) {
			return
		}
		rec, err = s.planRecords.RemoveTheme(ref, body.Theme)
	case "place":
		if !need("theme", body.Theme) || !need("kind", body.Kind) || !need("id", body.ID) {
			return
		}
		rec, err = s.planRecords.PlaceInTheme(ref, body.Theme, body.Kind, body.ID)
	case "unplace":
		if !need("kind", body.Kind) || !need("id", body.ID) {
			return
		}
		rec, err = s.planRecords.UnplaceFromTheme(ref, body.Kind, body.ID)
	case "reorder":
		if len(body.Themes) == 0 {
			planRecordsError(w, http.StatusBadRequest, "themes is required for action reorder: list every theme of the plan in the new order")
			return
		}
		if len(body.Themes) > maxPlanThemes {
			planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("themes holds at most %d entries", maxPlanThemes))
			return
		}
		rec, err = s.planRecords.ReorderThemes(ref, body.Themes)
	case "reorder_items":
		if !need("theme", body.Theme) || !need("kind", body.Kind) {
			return
		}
		if len(body.Items) > maxPlanThemeItems {
			planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("items holds at most %d entries", maxPlanThemeItems))
			return
		}
		rec, err = s.planRecords.ReorderThemeItems(ref, body.Theme, body.Kind, body.Items)
	default:
		planRecordsError(w, http.StatusBadRequest,
			"action must be create, rename, delete, place, unplace, reorder or reorder_items")
		return
	}
	if err != nil {
		writeThemeError(w, err, "change the themes")
		return
	}
	out, err := s.themesAnswer(rec)
	if err != nil {
		writeThemeError(w, err, "read the themes")
		return
	}
	out["action"] = body.Action
	for k, v := range extra {
		out[k] = v
	}
	writeJSONOK(w, out)
}

// ---------------------------------------------------------------------------
// Archive and restore.

// planArchiveBody is the archive body. ProtectedPlan names one plan (usually
// the current release plan) that must not be archived while it holds Automata.
type planArchiveBody struct {
	Reason        string `json:"reason"`
	ProtectedPlan string `json:"protected_plan"`
}

// handlePlanArchive serves POST /api/plans/{ref}/archive. Archiving marks the
// plan and nothing else: its file, links, Automata, Automations and findings
// stay. Archiving an archived plan changes nothing.
func (s *Server) handlePlanArchive(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planArchiveBody
	if !decodeOptionalPlanJSON(w, r, &body) {
		return
	}
	var opts []planrecords.ArchiveOption
	if p := strings.TrimSpace(body.ProtectedPlan); p != "" {
		opts = append(opts, planrecords.WithProtectedPlan(normalizePlanRef(p)))
	}
	res, err := s.planRecords.Archive(ref, body.Reason, s.auditActor(r.Context()), opts...)
	if err != nil {
		var prot *planrecords.ErrProtectedPlan
		if errors.As(err, &prot) {
			writeJSONError(w, http.StatusConflict, map[string]any{
				"error": prot.Error(), "plan_id": prot.Plan.ID, "automata_held": prot.Automata,
			})
			return
		}
		writePlanWriteError(w, err, "archive the plan")
		return
	}
	writeJSONOK(w, map[string]any{
		"plan": newPlanRecordView(res.Plan), "automata_held": res.AutomataHeld, "changed": res.Changed,
	})
}

// handlePlanRestore serves POST /api/plans/{ref}/restore. The plan comes back
// in the place its order gave it.
func (s *Server) handlePlanRestore(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	before, ok := s.planOfRef(w, ref)
	if !ok {
		return
	}
	rec, err := s.planRecords.Restore(before.UID, s.auditActor(r.Context()))
	if err != nil {
		writePlanWriteError(w, err, "restore the plan")
		return
	}
	writeJSONOK(w, map[string]any{"plan": newPlanRecordView(rec), "changed": before.Archived})
}

// ---------------------------------------------------------------------------
// Unattributed Automata.

// unattributedRow is one Automaton no release plan holds.
type unattributedRow struct {
	Title   string   `json:"title"`
	ID      string   `json:"id"`
	State   string   `json:"state"`
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
	// SuggestedRelease and SuggestedTheme come from the release migration's
	// own rules; they are empty when no rule files the Automaton.
	SuggestedRelease     string `json:"suggested_release,omitempty"`
	SuggestedTheme       string `json:"suggested_theme,omitempty"`
	SuggestedThemeSource string `json:"suggested_theme_source,omitempty"`
	// HoldingPlan names an ordinary plan that already holds the Automaton; it
	// cannot join a release plan until it is detached from that plan.
	HoldingPlan   string `json:"holding_plan,omitempty"`
	HoldingPlanID string `json:"holding_plan_id,omitempty"`
}

// handlePlansUnattributed serves GET /api/plans/unattributed.
func (s *Server) handlePlansUnattributed(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	q := r.URL.Query()
	pg, ok := pageParams(w, r, "")
	if !ok {
		return
	}
	project := strings.TrimSpace(q.Get("project"))
	if project != "" && !s.checkPlanRecordProject(w, project) {
		return
	}
	facts, err := s.planItemFacts(true)
	if err != nil {
		if errors.Is(err, errPlanFactsUnavailable) {
			planRecordsError(w, http.StatusServiceUnavailable, "the Automata manager is not available, so the Automata cannot be listed")
			return
		}
		planRecordsError(w, http.StatusInternalServerError, "cannot read the Automata: "+err.Error())
		return
	}
	held := s.releaseHeldFunc()
	var rows []unattributedRow
	projects := map[string]bool{}
	for _, f := range facts {
		if f.Kind != planrecords.ReleaseKindAutomaton || (project != "" && f.Project != project) {
			continue
		}
		if held != nil && held(f.ID) {
			continue
		}
		row := unattributedRow{Title: f.Title, ID: f.ID, State: f.State, Project: f.Project, Tags: f.Tags}
		if row.Tags == nil {
			row.Tags = []string{}
		}
		if ref, linked := s.planRecords.PlanOf(f.ID); linked {
			row.HoldingPlan, row.HoldingPlanID = ref.Title, ref.ID
		}
		rows = append(rows, row)
		if f.Project != "" {
			projects[f.Project] = true
		}
	}
	cur, next := strings.TrimSpace(q.Get("current_release")), strings.TrimSpace(q.Get("next_release"))
	suggest := map[string]planrecords.ReleaseMigrateItem{}
	curLabel, nextLabel := cur, next
	for p := range projects {
		rep, err := planrecords.ReleaseMigrate(planrecords.ReleaseMigrateOptions{
			Store: s.planRecords, Project: p, Resolve: s.releaseResolver(p),
			CurrentRelease: cur, NextRelease: next,
		})
		if err != nil {
			if rep.PreviewID == "" && !errors.Is(err, errPlanFactsUnavailable) {
				planRecordsError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "planrecords: "))
			} else {
				planRecordsError(w, http.StatusInternalServerError, "cannot suggest releases: "+err.Error())
			}
			return
		}
		curLabel, nextLabel = rep.CurrentRelease, rep.NextRelease
		for _, it := range rep.Attaches {
			if it.Kind == planrecords.ReleaseKindAutomaton {
				suggest[p+"\x00"+it.ID] = it
			}
		}
	}
	for i := range rows {
		if it, ok := suggest[rows[i].Project+"\x00"+rows[i].ID]; ok {
			rows[i].SuggestedRelease, rows[i].SuggestedTheme, rows[i].SuggestedThemeSource = it.Release, it.Theme, it.ThemeSource
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if la, lb := strings.ToLower(a.Title), strings.ToLower(b.Title); la != lb {
			return la < lb
		}
		return a.ID < b.ID
	})
	lo, hi := pageWindow(pg, len(rows))
	page := rows[lo:hi]
	if page == nil {
		page = []unattributedRow{}
	}
	resp := map[string]any{"automata": page, "page": pageMeta(pg, len(rows))}
	if curLabel != "" {
		resp["current_release"], resp["next_release"] = curLabel, nextLabel
	}
	writeJSONOK(w, resp)
}

// ---------------------------------------------------------------------------
// Bulk attach.

// attachResult is the outcome for one id of a bulk attach.
type attachResult struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	// Status is attached, already_attached, refused (linked elsewhere),
	// not_found, placement_failed or failed.
	Status string `json:"status"`
	// Theme is the theme id the item was placed in, when a theme was asked for.
	Theme  string `json:"theme,omitempty"`
	Holder string `json:"holder,omitempty"`
	Error  string `json:"error,omitempty"`
}

// handlePlanAttachBulk attaches a list of Automata (or Automations) to a plan,
// one result per id. An id attached elsewhere, an unknown id or a failure is
// reported on its own line and the others still go through. A theme, when
// given, must already exist in the release plan and every attached item is
// placed in it. The answer is 200 whenever the request was valid.
func (s *Server) handlePlanAttachBulk(w http.ResponseWriter, ref string, body planLinkBody) {
	if body.ID != "" {
		planRecordsError(w, http.StatusBadRequest, "send id for one item or ids for a list, not both")
		return
	}
	kind := body.Kind
	if kind == "" {
		kind = "automaton"
	}
	if kind != "automaton" && kind != "automation" {
		planRecordsError(w, http.StatusBadRequest, "kind must be automaton or automation for a list of ids")
		return
	}
	if len(body.IDs) > maxBulkAttachIDs {
		planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("ids holds at most %d entries", maxBulkAttachIDs))
		return
	}
	var ids []string
	seen := map[string]bool{}
	for _, id := range body.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			planRecordsError(w, http.StatusBadRequest, "ids must not hold an empty id")
			return
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	plan, ok := s.planOfRef(w, ref)
	if !ok {
		return
	}
	themeID := ""
	if t := strings.TrimSpace(body.Theme); t != "" {
		if themeID, ok = checkThemeRef(w, plan, t); !ok {
			return
		}
	}
	resolve := s.planItemResolver()
	if resolve == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "the Automata manager is not available, so the "+kind+" cannot be checked")
		return
	}
	counts := map[string]int{"attached": 0, "already_attached": 0, "refused": 0, "not_found": 0, "placement_failed": 0, "failed": 0}
	results := make([]attachResult, 0, len(ids))
	for _, id := range ids {
		res := s.attachOne(plan, kind, id, themeID, resolve)
		counts[res.Status]++
		results = append(results, res)
	}
	writeJSONOK(w, map[string]any{
		"attached": kind, "plan": planBlockOf(plan), "theme": themeID, "results": results, "counts": counts,
	})
}

func (s *Server) attachOne(plan planrecords.Record, kind, id, themeID string, resolve func(string) (planrecords.MigrateRef, bool)) attachResult {
	res := attachResult{ID: id}
	ref, ok := resolve(id)
	if !ok || ref.Kind != kind {
		res.Status, res.Error = "not_found", fmt.Sprintf("no %s matches %q", kind, id)
		return res
	}
	res.Title = ref.Title
	var holder planrecords.PlanRef
	var linked bool
	if kind == "automaton" {
		holder, linked = s.planRecords.PlanOf(id)
	} else {
		holder, linked = s.planRecords.PlanOfAutomation(id)
	}
	refuse := func(holderID string) attachResult {
		res.Status, res.Holder = "refused", holderID
		res.Error = (&planrecords.ErrLinkConflict{Kind: kind, ID: id, Holder: holderID}).Error()
		return res
	}
	already := linked && holder.UID == plan.UID
	if linked && !already {
		return refuse(holder.ID)
	}
	info := planrecords.LinkInfo{Title: ref.Title, State: ref.State}
	var err error
	if kind == "automaton" {
		_, err = s.planRecords.AttachAutomaton(plan.ID, id, info)
	} else {
		_, err = s.planRecords.AttachAutomation(plan.ID, id, info)
	}
	if err != nil {
		var lc *planrecords.ErrLinkConflict
		if errors.As(err, &lc) {
			return refuse(lc.Holder)
		}
		res.Status, res.Error = "failed", err.Error()
		return res
	}
	res.Status = "attached"
	if already {
		res.Status = "already_attached"
	}
	if themeID != "" {
		if _, err := s.planRecords.PlaceInTheme(plan.ID, themeID, kind, id); err != nil {
			res.Status, res.Error = "placement_failed", "attached, but not placed in the theme: "+err.Error()
			return res
		}
		res.Theme = themeID
	}
	return res
}

// ---------------------------------------------------------------------------
// Release migration answers.

// writeReleaseMigrationError answers a failed release migration. A missing
// preview id is 400, a stale one 409, an unreadable Automata manager 503, a
// bad option (the report has no preview id yet) 400, and anything after the
// preview was computed 500. Nothing is written in the first four cases.
func writeReleaseMigrationError(w http.ResponseWriter, err error, previewID string, missing, stale error) {
	switch {
	case errors.Is(err, missing):
		planRecordsError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "planrecords: "))
	case errors.Is(err, stale):
		planRecordsError(w, http.StatusConflict, strings.TrimPrefix(err.Error(), "planrecords: "))
	case errors.Is(err, errPlanFactsUnavailable):
		planRecordsError(w, http.StatusServiceUnavailable, "the Automata manager is not available, so the Automata cannot be read")
	case previewID == "":
		planRecordsError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "planrecords: "))
	default:
		planRecordsError(w, http.StatusInternalServerError, "cannot migrate: "+err.Error())
	}
}

// serveReleaseMigration serves POST /api/plans/migrate with mode release.
func (s *Server) serveReleaseMigration(w http.ResponseWriter, r *http.Request, body planMigrateBody) {
	if !s.checkPlanRecordProject(w, body.Project) {
		return
	}
	body.Project = strings.TrimSpace(body.Project)
	rep, err := s.runReleaseMigration(body, s.auditActor(r.Context()))
	if err != nil {
		writeReleaseMigrationError(w, err, rep.PreviewID, planrecords.ErrPreviewMissing, planrecords.ErrPreviewStale)
		return
	}
	note := "Preview only: nothing was written. Send {\"mode\": \"release\", \"apply\": true, \"preview_id\": \"" +
		rep.PreviewID + "\"} to write exactly this."
	if rep.Applied {
		note = "Release migration finished. Run the preview again any time; items already on a release plan are left alone."
		if rep.Changes() == 0 {
			note = "Nothing to change: the release plans already hold what the rules file under them."
		}
	}
	writeJSONOK(w, map[string]any{
		"mode": "release", "note": note, "applied": rep.Applied, "preview_id": rep.PreviewID,
		"current_release": rep.CurrentRelease, "next_release": rep.NextRelease,
		"counts":  rep.Counts(),
		"created": rep.Creates, "attached": rep.Attaches, "unplaced": rep.Unplaced,
		"conflicts": rep.Conflicts, "unchanged": rep.Unchanged, "skipped": rep.Skipped,
	})
}

// ---------------------------------------------------------------------------
// Row-plans.

// rowPlansBody is the POST body of the row-plan conversion.
type rowPlansBody struct {
	Project        string `json:"project"`
	CurrentRelease string `json:"current_release"`
	Apply          bool   `json:"apply"`
	PreviewID      string `json:"preview_id"`
}

// handlePlansRowPlans serves GET and POST /api/plans/migrate/row-plans.
func (s *Server) handlePlansRowPlans(w http.ResponseWriter, r *http.Request) {
	write, ok := s.planMethodGate(w, r, http.MethodGet, http.MethodPost)
	if !ok {
		return
	}
	if !write {
		s.listRowPlans(w, r)
		return
	}
	var body rowPlansBody
	if !decodeOptionalPlanJSON(w, r, &body) {
		return
	}
	body.Project = strings.TrimSpace(body.Project)
	if body.Project != "" && !s.checkPlanRecordProject(w, body.Project) {
		return
	}
	rep, err := s.runRowPlanConversion(body, s.auditActor(r.Context()))
	if err != nil {
		writeReleaseMigrationError(w, err, rep.PreviewID, planrecords.ErrRowPlansPreviewMissing, planrecords.ErrRowPlansPreviewStale)
		return
	}
	note := "Preview only: nothing was written. Send {\"apply\": true, \"preview_id\": \"" + rep.PreviewID + "\"} to write exactly this."
	if rep.Applied {
		note = "Conversion finished. Each converted plan is archived, not deleted; run the preview again any time."
		if rep.Changes() == 0 {
			note = "Nothing to convert."
		}
	}
	writeJSONOK(w, map[string]any{
		"note": note, "applied": rep.Applied, "preview_id": rep.PreviewID, "current_release": rep.CurrentRelease,
		"counts":    rep.Counts(),
		"link_rows": rep.LinkRows, "backlog_items": rep.BacklogItems, "waiting": rep.Waiting,
		"conflicts": rep.Conflicts, "unchanged": rep.Unchanged,
	})
}

// listRowPlans answers GET /api/plans/migrate/row-plans.
func (s *Server) listRowPlans(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project != "" && !s.checkPlanRecordProject(w, project) {
		return
	}
	all, err := planrecords.ListRowPlans(s.planRecords)
	if err != nil {
		planRecordsError(w, http.StatusInternalServerError, "cannot list the row-plans: "+err.Error())
		return
	}
	out := make([]planrecords.RowPlan, 0, len(all))
	for _, rp := range all {
		if project == "" || rp.Project == project {
			out = append(out, rp)
		}
	}
	writeJSONOK(w, map[string]any{"row_plans": out, "total": len(out)})
}
