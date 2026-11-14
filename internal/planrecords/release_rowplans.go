package planrecords

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Row-plan conversion.
//
// The plans migration made one plan for every registry row. Those row-plans
// were a stop-gap: a release plan now holds the work, so a row-plan is
// converted into an item of a release plan and taken out of the plan list.
//
//   - ListRowPlans finds every row-plan, so the operator can see all of them.
//     A row-plan is recognised by its source marker (a "migrated-from" link
//     that names a row) together with the "Plan: <row text>" title form, never
//     by title alone, so an ordinary plan that happens to start with "Plan: "
//     is never listed.
//   - ConvertRowPlans is a preview by default. A row-plan that holds Automata
//     becomes a registry-row link of the release plan that holds those Automata
//     (the first by plan order when they sit on several, and the others are
//     reported). A row-plan that holds no Automaton becomes a backlog item of
//     the current release plan. The row-plan is then archived with a reason that
//     names the conversion.
//   - A row-plan whose Automata are not all held by a release plan yet is
//     waiting and is left alone. The Automata themselves never move here: only
//     the release migration attaches them to a release plan. The same holds for
//     the Automations the row-plan holds or its row names, and for a row-plan
//     that also holds findings or other rows nothing else holds: it waits.
//   - Nothing is deleted. The row, the findings, the Automata and the
//     row-plan's file all stay; the only change to a row-plan is its archived
//     mark, and the only record that gains something is a release plan.
//
// A written run needs Apply and the preview id of the exact preview the
// operator approved; a missing or stale id is refused before anything is
// written. A second written run finds the row-plans archived and changes
// nothing. Every written change is audit-logged with the actor.

// AuditRowPlansConverted is the audit action of every change a written
// row-plan conversion makes. Its details carry actor, op (link_row,
// add_backlog_item, archive), the row-plan, the row and the release plan.
const AuditRowPlansConverted = "plan_record_row_plans_converted"

// The conversion operations a report line names.
const (
	// RowPlanOpLinkRow is a registry-row link added to the release plan that
	// holds the row-plan's Automata.
	RowPlanOpLinkRow = "link-row"
	// RowPlanOpBacklogItem is a backlog item added to the current release plan.
	RowPlanOpBacklogItem = "backlog-item"
)

const rowPlanMarkerPrefix = "row:"

// RowPlan is one plan the plans migration made from a registry row.
type RowPlan struct {
	PlanID   string `json:"plan_id"`
	Title    string `json:"title"`
	Project  string `json:"project"`
	State    State  `json:"state"`
	Archived bool   `json:"archived,omitempty"`
	// Source is the migration source value: "registry-row" for a row-plan that
	// holds Automata or Automations, "registry-row-backlog" for one that holds
	// none.
	Source string `json:"source"`
	// RowID, RowUID and RowTitle name the row the plan stands for. RowFound is
	// false when no row with that uid is in the store any more.
	RowID    string `json:"row_id,omitempty"`
	RowUID   string `json:"row_uid,omitempty"`
	RowTitle string `json:"row_title,omitempty"`
	RowFound bool   `json:"row_found"`
	// Automata and Automations are the ids the row-plan holds, then the ones its
	// row names that it no longer holds (the release migration moved them).
	Automata    []string `json:"automata"`
	Automations []string `json:"automations"`
	// ReleasePlanID and ReleasePlanTitle name the release plan that holds the
	// Automata: the first by plan order when they sit on several. Both are empty
	// when the row-plan holds no Automaton or none is on a release plan.
	ReleasePlanID    string `json:"release_plan_id,omitempty"`
	ReleasePlanTitle string `json:"release_plan_title,omitempty"`
	// OtherReleasePlans are the ids of the other release plans that hold some of
	// the Automata.
	OtherReleasePlans []string `json:"other_release_plans,omitempty"`
	// Unheld lists the Automata and Automations ("automaton:<id>") that no
	// release plan holds yet.
	Unheld []string `json:"unheld,omitempty"`
	// Extras lists what the row-plan holds besides its marker links, Automata
	// and Automations: findings and rows nothing else holds.
	Extras []string `json:"extras,omitempty"`

	uid        string
	releaseUID string
}

// ListRowPlans returns every plan the plans migration made from a registry
// row, archived ones included, in plan order, each with its row, the Automata
// and Automations it holds and the release plan that holds those Automata. Only
// the row's own links name an Automaton or Automation here; ConvertRowPlans
// accepts a resolver for ids found in row text.
func ListRowPlans(s *Store) ([]RowPlan, error) {
	if s == nil {
		return nil, errors.New("planrecords: list row plans needs a store")
	}
	return listRowPlans(s, "", nil), nil
}

// rowPlanMarker returns the row uid a plan's source marker names, and whether
// the plan is a row-plan: an ordinary plan with a "migrated-from" link naming a
// row and the migration's "Plan: " title form.
func rowPlanMarker(r Record) (string, bool) {
	if r.Kind != KindPlan || r.PlanKind != PlanKindOrdinary || !strings.HasPrefix(r.Title, migratedTitlePrefix) {
		return "", false
	}
	for _, l := range r.Links {
		if l.Kind == LinkMigratedFrom && strings.HasPrefix(l.Ref, rowPlanMarkerPrefix) {
			if uid := strings.TrimPrefix(l.Ref, rowPlanMarkerPrefix); uid != "" {
				return uid, true
			}
		}
	}
	return "", false
}

func (s *Store) rowByUID(uid string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byUID[uid]
	if !ok || r.Kind != KindRegistryRow {
		return Record{}, false
	}
	return r.clone(), true
}

// releaseHolderOf returns the release plan that holds an Automaton or
// Automation. A plan that is not a release plan, or is archived, does not count.
func (s *Store) releaseHolderOf(kind, id string) (Summary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.li.linked[linkKey(kind, id)]
	if !ok {
		return Summary{}, false
	}
	sm, ok := s.li.sums[e.planUID]
	if !ok || sm.PlanKind != PlanKindRelease || sm.Archived {
		return Summary{}, false
	}
	return sm, true
}

func (s *Store) allPlanRecords() []Record {
	var out []Record
	for off := 0; ; off += MaxLimit {
		recs, total := s.List(Filter{Kind: KindPlan, IncludeArchived: true, Limit: MaxLimit, Offset: off})
		out = append(out, recs...)
		if off+MaxLimit >= total {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return planLess(
			Summary{Order: out[i].Order, CreatedAt: out[i].CreatedAt, ID: out[i].ID},
			Summary{Order: out[j].Order, CreatedAt: out[j].CreatedAt, ID: out[j].ID})
	})
	return out
}

func listRowPlans(s *Store, project string, resolve func(string) (MigrateRef, bool)) []RowPlan {
	finder := &migrator{opts: MigrateOptions{Resolve: resolve}}
	out := []RowPlan{}
	for _, p := range s.allPlanRecords() {
		rowUID, ok := rowPlanMarker(p)
		if !ok || (project != "" && p.Project != project) {
			continue
		}
		rp := RowPlan{
			PlanID: p.ID, Title: p.Title, Project: p.Project, State: p.State, Archived: p.Archived,
			RowUID: rowUID, Automata: []string{}, Automations: []string{}, uid: p.UID,
		}
		rp.Automata = append(rp.Automata, p.Automata...)
		rp.Automations = append(rp.Automations, p.Automations...)
		row, found := s.rowByUID(rowUID)
		rp.RowFound = found
		if found {
			rp.RowID, rp.RowTitle = row.ID, row.Title
			for _, ref := range finder.rowRefs(row) {
				if ref.kind == linkAutomation {
					rp.Automations = appendUnique(rp.Automations, ref.id)
				} else {
					rp.Automata = appendUnique(rp.Automata, ref.id)
				}
			}
		}
		rp.Source = migrateSourceRowBacklog
		if len(rp.Automata)+len(rp.Automations) > 0 {
			rp.Source = migrateSourceRow
		}
		rp.holders(s)
		rp.extras(p, rowUID)
		out = append(out, rp)
	}
	return out
}

// holders fills the release plan that holds the Automata and the items no
// release plan holds.
func (rp *RowPlan) holders(s *Store) {
	var found []Summary
	seen := map[string]bool{}
	check := func(kind string, ids []string, counts bool) {
		for _, id := range ids {
			sm, ok := s.releaseHolderOf(kind, id)
			if !ok {
				rp.Unheld = append(rp.Unheld, kind+":"+id)
				continue
			}
			if counts && !seen[sm.UID] {
				seen[sm.UID] = true
				found = append(found, sm)
			}
		}
	}
	check(linkAutomaton, rp.Automata, true)
	check(linkAutomation, rp.Automations, false)
	if len(found) == 0 {
		return
	}
	sort.SliceStable(found, func(i, j int) bool { return planLess(found[i], found[j]) })
	rp.ReleasePlanID, rp.ReleasePlanTitle, rp.releaseUID = found[0].ID, found[0].Title, found[0].UID
	for _, sm := range found[1:] {
		rp.OtherReleasePlans = append(rp.OtherReleasePlans, sm.ID)
	}
}

// extras lists what a row-plan holds beyond the marker links, Automata and
// Automations: findings and rows other than the one it stands for.
func (rp *RowPlan) extras(p Record, rowUID string) {
	for _, f := range p.Findings {
		rp.Extras = append(rp.Extras, "finding:"+f)
	}
	for _, l := range p.Links {
		if isRowLinkKind(l.Kind) && strings.TrimSpace(l.Ref) != rowUID {
			rp.Extras = append(rp.Extras, l.Kind+":"+strings.TrimSpace(l.Ref))
		}
	}
}

// ConvertRowPlansOptions are the inputs of ConvertRowPlans.
type ConvertRowPlansOptions struct {
	Store *Store
	// Project limits the run to one project's row-plans; empty means all.
	Project string
	// CurrentRelease is the label of the current release plan, which takes the
	// backlog items. It defaults to DefaultCurrentRelease.
	CurrentRelease string
	// Resolve says whether an id found in a row's text is an Automaton or an
	// Automation, as the plans migration did. Nil means only the row's links
	// count.
	Resolve func(id string) (MigrateRef, bool)
	// Apply writes. False (the default) is a preview.
	Apply bool
	// ApprovalToken is the preview id of the preview the operator approved. A
	// written run refuses without a matching one.
	ApprovalToken string
	// Audit gets one entry per written change.
	Audit AuditFunc
	// Actor names who ran the conversion for the audit entries.
	Actor string
}

// RowPlanChange is one line of the conversion report.
type RowPlanChange struct {
	PlanID   string `json:"plan_id"`
	Title    string `json:"title"`
	Project  string `json:"project"`
	RowID    string `json:"row_id,omitempty"`
	RowTitle string `json:"row_title,omitempty"`
	// Op is RowPlanOpLinkRow or RowPlanOpBacklogItem for a conversion.
	Op string `json:"op,omitempty"`
	// ReleasePlanID and ReleasePlanTitle name the release plan that gets the
	// row; OtherReleasePlans are other release plans holding some of the Automata.
	ReleasePlanID     string   `json:"release_plan_id,omitempty"`
	ReleasePlanTitle  string   `json:"release_plan_title,omitempty"`
	OtherReleasePlans []string `json:"other_release_plans,omitempty"`
	// LinkPresent is true when the release plan already holds the row, so only
	// the archive is written.
	LinkPresent bool   `json:"link_present,omitempty"`
	Reason      string `json:"reason,omitempty"`

	uid, rowUID, releaseUID string
}

// ConvertRowPlansReport is the result of ConvertRowPlans. After a written run
// the lists hold what was written.
type ConvertRowPlansReport struct {
	Applied bool `json:"applied"`
	// PreviewID is a stable hash of the report; a written run must quote it.
	PreviewID      string `json:"preview_id"`
	CurrentRelease string `json:"current_release"`
	// Converted are the row-plans turned into a registry-row link (LinkRows) or a
	// backlog item (BacklogItems) and archived.
	LinkRows     []RowPlanChange `json:"link_rows"`
	BacklogItems []RowPlanChange `json:"backlog_items"`
	// Waiting are row-plans left alone because the release migration has not
	// filed everything they hold, or no current release plan exists.
	Waiting []RowPlanChange `json:"waiting"`
	// Conflicts are row-plans left alone because the row is gone or a link was
	// refused.
	Conflicts []RowPlanChange `json:"conflicts"`
	// Unchanged are row-plans that were archived already.
	Unchanged []RowPlanChange `json:"unchanged"`
}

// Counts is the size of each list.
func (r ConvertRowPlansReport) Counts() map[string]int {
	return map[string]int{
		"link_rows": len(r.LinkRows), "backlog_items": len(r.BacklogItems), "waiting": len(r.Waiting),
		"conflicts": len(r.Conflicts), "unchanged": len(r.Unchanged),
	}
}

// Changes is the number of row-plans a written run converts.
func (r ConvertRowPlansReport) Changes() int { return len(r.LinkRows) + len(r.BacklogItems) }

func (r ConvertRowPlansReport) computePreviewID() string {
	r.PreviewID, r.Applied = "", false
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// Errors a written run answers with before it writes anything.
var (
	ErrRowPlansPreviewMissing = errors.New("planrecords: a written row-plan conversion needs the preview id of the approved preview; run the preview first")
	ErrRowPlansPreviewStale   = errors.New("planrecords: the preview id does not match what a preview shows now; the data changed or the id is wrong, run the preview again")
)

var convertRowPlansMu sync.Mutex

func nonNilChanges(l []RowPlanChange) []RowPlanChange {
	if l == nil {
		return []RowPlanChange{}
	}
	return l
}

// ConvertRowPlans converts the row-plans into release plan items and archives
// them. See the file comment.
func ConvertRowPlans(opts ConvertRowPlansOptions) (ConvertRowPlansReport, error) {
	var rep ConvertRowPlansReport
	if opts.Store == nil {
		return rep, errors.New("planrecords: convert row plans needs a store")
	}
	cur := strings.TrimSpace(opts.CurrentRelease)
	if cur == "" {
		cur = DefaultCurrentRelease
	}
	opts.CurrentRelease = cur
	if opts.Project != "" && (!projectRe.MatchString(opts.Project) || runesOver(opts.Project, MaxProjectRunes)) {
		return rep, errors.New("planrecords: convert row plans needs a valid project name")
	}
	convertRowPlansMu.Lock()
	defer convertRowPlansMu.Unlock()

	rep = classifyRowPlans(opts)
	rep.PreviewID = rep.computePreviewID()
	if !opts.Apply {
		return rep, nil
	}
	token := strings.TrimSpace(opts.ApprovalToken)
	if token == "" {
		return rep, ErrRowPlansPreviewMissing
	}
	if token != rep.PreviewID {
		return rep, ErrRowPlansPreviewStale
	}
	rep.Applied = true
	executeRowPlans(opts, &rep)
	return rep, nil
}

func classifyRowPlans(opts ConvertRowPlansOptions) ConvertRowPlansReport {
	s := opts.Store
	rep := ConvertRowPlansReport{CurrentRelease: opts.CurrentRelease}
	for _, rp := range listRowPlans(s, opts.Project, opts.Resolve) {
		ch := RowPlanChange{
			PlanID: rp.PlanID, Title: rp.Title, Project: rp.Project, RowID: rp.RowID, RowTitle: rp.RowTitle,
			uid: rp.uid, rowUID: rp.RowUID,
		}
		switch {
		case rp.Archived:
			ch.Reason = "already archived"
			rep.Unchanged = append(rep.Unchanged, ch)
			continue
		case !rp.RowFound:
			ch.Reason = "the row this plan stands for is not in the store; left as it is"
			rep.Conflicts = append(rep.Conflicts, ch)
			continue
		case len(rp.Unheld) > 0:
			ch.Reason = "waiting: " + strings.Join(rp.Unheld, ", ") + " not held by a release plan yet; the release migration files it"
			rep.Waiting = append(rep.Waiting, ch)
			continue
		case len(rp.Extras) > 0:
			ch.Reason = "waiting: it also holds " + strings.Join(rp.Extras, ", ") + ", which nothing else holds"
			rep.Waiting = append(rep.Waiting, ch)
			continue
		}
		var target Record
		if len(rp.Automata) > 0 {
			ch.Op = RowPlanOpLinkRow
			ch.ReleasePlanID, ch.ReleasePlanTitle, ch.releaseUID = rp.ReleasePlanID, rp.ReleasePlanTitle, rp.releaseUID
			ch.OtherReleasePlans = rp.OtherReleasePlans
			if len(rp.OtherReleasePlans) > 0 {
				ch.Reason = fmt.Sprintf("its Automata sit on %d release plans; the first by plan order is used", len(rp.OtherReleasePlans)+1)
			}
			rec, err := s.Get(rp.releaseUID)
			if err != nil {
				ch.Reason = "cannot read the release plan: " + err.Error()
				rep.Conflicts = append(rep.Conflicts, ch)
				continue
			}
			target = rec
		} else {
			ch.Op = RowPlanOpBacklogItem
			cp, ok := s.ReleasePlan(rp.Project, opts.CurrentRelease)
			if !ok || cp.Archived {
				ch.Reason = fmt.Sprintf("waiting: the project has no current release plan for release %q", opts.CurrentRelease)
				ch.Op = ""
				rep.Waiting = append(rep.Waiting, ch)
				continue
			}
			ch.ReleasePlanID, ch.ReleasePlanTitle, ch.releaseUID = cp.ID, cp.Title, cp.UID
			target = cp
		}
		switch {
		case hasLink(target.Links, LinkKindRegistryRow, rp.RowUID):
			ch.LinkPresent = true
		case hasLink(target.Links, LinkKindBacklogItem, rp.RowUID):
			if ch.Op == RowPlanOpLinkRow {
				ch.Reason = "the release plan holds the row as a backlog item; remove that link first"
				rep.Conflicts = append(rep.Conflicts, ch)
				continue
			}
			ch.LinkPresent = true
		}
		if ch.Op == RowPlanOpLinkRow {
			rep.LinkRows = append(rep.LinkRows, ch)
		} else {
			rep.BacklogItems = append(rep.BacklogItems, ch)
		}
	}
	rep.LinkRows, rep.BacklogItems = nonNilChanges(rep.LinkRows), nonNilChanges(rep.BacklogItems)
	rep.Waiting, rep.Conflicts, rep.Unchanged = nonNilChanges(rep.Waiting), nonNilChanges(rep.Conflicts), nonNilChanges(rep.Unchanged)
	return rep
}

func rowPlanArchiveReason(ch RowPlanChange) string {
	what := "a registry-row link"
	if ch.Op == RowPlanOpBacklogItem {
		what = "a backlog item"
	}
	return clipRunes(fmt.Sprintf("converted: the row is now %s of release plan %q", what, ch.ReleasePlanTitle), MaxArchiveReasonRunes)
}

func (o ConvertRowPlansOptions) audit(op string, ch RowPlanChange, extra map[string]any) {
	if o.Audit == nil {
		return
	}
	actor := o.Actor
	if actor == "" {
		actor = "system"
	}
	d := map[string]any{
		"actor": actor, "op": op, "project": ch.Project, "plan_id": ch.PlanID, "row_id": ch.RowID,
		"release_plan_id": ch.ReleasePlanID,
	}
	for k, v := range extra {
		d[k] = v
	}
	o.Audit(AuditRowPlansConverted, ch.Title, d)
}

// executeRowPlans writes what the report lists. The approval was checked and
// the mutex is held. A row-plan whose link or archive is refused moves to the
// conflicts, and is left unarchived when the link was not written.
func executeRowPlans(opts ConvertRowPlansOptions, rep *ConvertRowPlansReport) {
	s := opts.Store
	actor := opts.Actor
	convert := func(list []RowPlanChange) []RowPlanChange {
		done := []RowPlanChange{}
		for _, ch := range list {
			if !ch.LinkPresent {
				var err error
				op := "link_row"
				if ch.Op == RowPlanOpBacklogItem {
					op = "add_backlog_item"
					_, err = s.AddBacklogItem(ch.releaseUID, ch.rowUID)
				} else {
					_, err = s.AttachRow(ch.releaseUID, ch.rowUID)
				}
				if err != nil {
					ch.Reason = "cannot link the row: " + err.Error()
					rep.Conflicts = append(rep.Conflicts, ch)
					continue
				}
				opts.audit(op, ch, nil)
			}
			if _, err := s.Archive(ch.uid, rowPlanArchiveReason(ch), actor); err != nil {
				ch.Reason = "the row is linked but the plan was not archived: " + err.Error()
				rep.Conflicts = append(rep.Conflicts, ch)
				continue
			}
			opts.audit("archive", ch, map[string]any{"reason": rowPlanArchiveReason(ch)})
			done = append(done, ch)
		}
		return done
	}
	rep.LinkRows = convert(rep.LinkRows)
	rep.BacklogItems = convert(rep.BacklogItems)
}
