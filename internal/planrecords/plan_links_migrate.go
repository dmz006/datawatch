package planrecords

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Plan links migration.
//
// Migrate turns the two older ways of saying "this work comes next" into plan
// records:
//
//   - every project arc (title, goal, priority, state and its Automation)
//     becomes one plan linked to that Automation;
//   - every registry row whose text or links name an Automaton or Automation
//     id becomes a plan linked to it, and every open row that names none
//     becomes a backlog plan ordered by its old row number.
//
// It is a dry run unless Apply is set. It never overwrites an existing link
// and never touches a plan created earlier: each plan it creates carries a
// "migrated-from" link naming its source, so a second apply finds the plan
// again and changes nothing. It deletes no arc, row or config key.

// AuditMigrated is the audit action of a migration that wrote records.
const AuditMigrated = "plan_record_migrated"

// Link and reference kinds the migration writes or reads.
const (
	// LinkMigratedFrom marks a plan the migration made. Its Ref names the
	// source: "arc:<project>:<automation id>" or "row:<row uid>".
	LinkMigratedFrom = "migrated-from"
	// linkRegistryRow points a migrated plan back at its source row.
	linkRegistryRow = "registry-row"
)

// The report actions.
const (
	MigrateWouldCreate      = "would-create"
	MigrateWouldLink        = "would-link"
	MigrateSkippedExisting  = "skipped-existing"
	MigrateConflict         = "conflict"
	migrateSourceArc        = "arc"
	migrateSourceRow        = "registry-row"
	migrateSourceRowBacklog = "registry-row-backlog"
)

// MigrateArc is one project arc as the migration reads it. The caller fills
// Title and Goal from the arc's Automation.
type MigrateArc struct {
	Project      string `json:"project"`
	AutomationID string `json:"automation_id"`
	// Priority becomes the plan's order; 0 means last.
	Priority int `json:"priority,omitempty"`
	// State is planned, active or done; empty means planned.
	State string `json:"state,omitempty"`
	Title string `json:"title,omitempty"`
	Goal  string `json:"goal,omitempty"`
}

// MigrateRef says what an id found in a row is.
type MigrateRef struct {
	// Kind is "automaton" or "automation".
	Kind  string
	Title string
	State string
}

// MigrateOptions are the inputs of Migrate.
type MigrateOptions struct {
	// Apply writes. False (the default) is a dry run.
	Apply bool
	Store *Store
	// Arcs are the project arcs to migrate.
	Arcs []MigrateArc
	// Resolve says whether an id found in row text is an Automaton or an
	// Automation. Nil means only ids named by a link of kind "automaton" or
	// "automation" count.
	Resolve func(id string) (MigrateRef, bool)
	// Audit is the plan-record audit hook; it gets one entry for an apply.
	Audit AuditFunc
	// Actor names who ran the migration for the audit entry.
	Actor string
}

// MigrateItem is one line of the report.
type MigrateItem struct {
	// Source is "arc", "registry-row" or "registry-row-backlog".
	Source string `json:"source"`
	// SourceRef names the arc (<project>:<automation id>) or the row id.
	SourceRef string `json:"source_ref"`
	Title     string `json:"title"`
	Project   string `json:"project,omitempty"`
	// PlanID is the plan created or linked to, when it exists.
	PlanID string `json:"plan_id,omitempty"`
	// LinkKind and LinkID say what was or would be linked.
	LinkKind string `json:"link_kind,omitempty"`
	LinkID   string `json:"link_id,omitempty"`
	Order    int    `json:"order,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// MigrateReport is the result of Migrate. After an apply the would-* lists
// hold what was written.
type MigrateReport struct {
	Applied         bool          `json:"applied"`
	WouldCreate     []MigrateItem `json:"would_create"`
	WouldLink       []MigrateItem `json:"would_link"`
	SkippedExisting []MigrateItem `json:"skipped_existing"`
	Conflicts       []MigrateItem `json:"conflicts"`
}

// Counts is the size of each list.
func (r MigrateReport) Counts() map[string]int {
	return map[string]int{
		"would_create": len(r.WouldCreate), "would_link": len(r.WouldLink),
		"skipped_existing": len(r.SkippedExisting), "conflicts": len(r.Conflicts),
	}
}

var migrateMu sync.Mutex

var migrateIDRe = regexp.MustCompile(`\b[0-9a-f]{8}\b`)

// Migrate builds the plan links from the arcs and registry rows. See the
// file comment.
func Migrate(opts MigrateOptions) (MigrateReport, error) {
	rep := MigrateReport{
		Applied:     opts.Apply,
		WouldCreate: []MigrateItem{}, WouldLink: []MigrateItem{},
		SkippedExisting: []MigrateItem{}, Conflicts: []MigrateItem{},
	}
	if opts.Store == nil {
		return rep, errors.New("planrecords: migrate needs a store")
	}
	migrateMu.Lock()
	defer migrateMu.Unlock()
	m := &migrator{opts: opts, s: opts.Store, rep: &rep, claimed: map[string]string{}}
	m.loadPlans()
	m.arcs()
	m.rows()
	if opts.Apply && opts.Audit != nil {
		actor := opts.Actor
		if actor == "" {
			actor = "system"
		}
		d := map[string]any{"actor": actor}
		for k, v := range rep.Counts() {
			d[k] = v
		}
		opts.Audit(AuditMigrated, "plans migrate", d)
	}
	return rep, nil
}

type migrator struct {
	opts MigrateOptions
	s    *Store
	rep  *MigrateReport
	// markers maps a migrated-from ref to the plan made for it.
	markers map[string]Record
	// claimed maps "kind:id" to the plan title that holds it in this run, so a
	// dry run and an apply agree about what would be linked.
	claimed map[string]string
}

func (m *migrator) loadPlans() {
	m.markers = map[string]Record{}
	for off := 0; ; off += MaxLimit {
		recs, total := m.s.List(Filter{Kind: KindPlan, Limit: MaxLimit, Offset: off})
		for _, r := range recs {
			for _, l := range r.Links {
				if l.Kind == LinkMigratedFrom {
					m.markers[l.Ref] = r
				}
			}
		}
		if off+MaxLimit >= total {
			return
		}
	}
}

func clipRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max]))
}

func clampOrder(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxOrder {
		return MaxOrder
	}
	return n
}

// holder reports the plan an Automaton or Automation is linked to, in the
// store or earlier in this run.
func (m *migrator) holder(kind, id string) (string, bool) {
	if t, ok := m.claimed[linkKey(kind, id)]; ok {
		return t, true
	}
	var p PlanRef
	var ok bool
	if kind == linkAutomation {
		p, ok = m.s.PlanOfAutomation(id)
	} else {
		p, ok = m.s.PlanOf(id)
	}
	if ok {
		return p.Title, true
	}
	return "", false
}

// titleTaken returns the record that already holds title in project.
func (m *migrator) titleTaken(project, title string) (Record, bool) {
	m.s.mu.RLock()
	defer m.s.mu.RUnlock()
	uid, ok := m.s.byTitle[titleKey(project, title)]
	if !ok {
		return Record{}, false
	}
	return m.s.byUID[uid].clone(), true
}

func (m *migrator) add(list *[]MigrateItem, it MigrateItem) { *list = append(*list, it) }

func (m *migrator) conflict(it MigrateItem, why string) {
	it.Reason = why
	m.add(&m.rep.Conflicts, it)
}

func (m *migrator) skip(it MigrateItem, why string) {
	it.Reason = why
	m.add(&m.rep.SkippedExisting, it)
}

func arcPlanState(s string) State {
	switch s {
	case "active":
		return StateInProgress
	case "done":
		return StateDone
	}
	return StatePlanned
}

func (m *migrator) arcs() {
	arcs := append([]MigrateArc(nil), m.opts.Arcs...)
	sort.SliceStable(arcs, func(i, j int) bool {
		if arcs[i].Project != arcs[j].Project {
			return arcs[i].Project < arcs[j].Project
		}
		return arcs[i].Priority < arcs[j].Priority
	})
	for _, a := range arcs {
		id := strings.TrimSpace(a.AutomationID)
		title := clipRunes(a.Title, MaxTitleRunes)
		if title == "" {
			title = "Automation " + id
		}
		it := MigrateItem{
			Source: migrateSourceArc, SourceRef: a.Project + ":" + id, Title: title, Project: a.Project,
			LinkKind: linkAutomation, LinkID: id, Order: clampOrder(a.Priority),
		}
		if id == "" || !projectRe.MatchString(a.Project) {
			m.conflict(it, "the arc has no Automation id or its project name is not valid")
			continue
		}
		if t, ok := m.holder(linkAutomation, id); ok {
			m.skip(it, fmt.Sprintf("the Automation is already linked to the plan %q", t))
			continue
		}
		marker := "arc:" + a.Project + ":" + id
		info := LinkInfo{Title: title, State: a.State}
		if plan, ok := m.markers[marker]; ok {
			// An earlier run made the plan but did not finish the link.
			it.PlanID, it.Title = plan.ID, plan.Title
			m.linkTo(it, plan.UID, info)
			continue
		}
		if ex, ok := m.titleTaken(a.Project, title); ok {
			m.conflict(it, fmt.Sprintf("a record titled %q already exists in the project (%s); left as it is", title, ex.ID))
			continue
		}
		m.createAndLink(it, Record{
			Kind: KindPlan, Project: a.Project, State: arcPlanState(a.State), Title: title,
			Goal: clipRunes(a.Goal, MaxGoalRunes), Order: clampOrder(a.Priority),
			Links: []Link{{Kind: LinkMigratedFrom, Ref: marker}},
		}, info)
	}
}

// foundRef is an Automaton or Automation id named by a row.
type foundRef struct {
	kind string
	id   string
	info LinkInfo
}

// rowRefs lists the ids a row names: links of kind automaton or automation
// first, then ids in its title and body that Resolve recognises.
func (m *migrator) rowRefs(r Record) []foundRef {
	var out []foundRef
	seen := map[string]bool{}
	push := func(kind, id string, info LinkInfo) {
		id = strings.TrimSpace(id)
		k := linkKey(kind, id)
		if id == "" || seen[k] {
			return
		}
		if _, err := cleanLinkID(id); err != nil {
			return
		}
		seen[k] = true
		out = append(out, foundRef{kind: kind, id: id, info: info})
	}
	for _, l := range r.Links {
		if l.Kind == linkAutomaton || l.Kind == linkAutomation {
			info := LinkInfo{}
			if m.opts.Resolve != nil {
				if ref, ok := m.opts.Resolve(strings.TrimSpace(l.Ref)); ok {
					info = LinkInfo{Title: ref.Title, State: ref.State}
				}
			}
			push(l.Kind, l.Ref, info)
		}
	}
	if m.opts.Resolve != nil {
		for _, id := range migrateIDRe.FindAllString(r.Title+"\n"+r.Body, -1) {
			if ref, ok := m.opts.Resolve(id); ok && (ref.Kind == linkAutomaton || ref.Kind == linkAutomation) {
				push(ref.Kind, id, LinkInfo{Title: ref.Title, State: ref.State})
			}
		}
	}
	return out
}

func (m *migrator) allRows() []Record {
	var rows []Record
	for off := 0; ; off += MaxLimit {
		recs, total := m.s.List(Filter{Kind: KindRegistryRow, Limit: MaxLimit, Offset: off})
		rows = append(rows, recs...)
		if off+MaxLimit >= total {
			break
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		an, bn := a.LegacyNumber, b.LegacyNumber
		if an == 0 {
			an = MaxOrder + 1
		}
		if bn == 0 {
			bn = MaxOrder + 1
		}
		if an != bn {
			return an < bn
		}
		return a.ID < b.ID
	})
	return rows
}

func (m *migrator) rows() {
	for _, r := range m.allRows() {
		refs := m.rowRefs(r)
		marker := "row:" + r.UID
		plan, hasPlan := m.markers[marker]
		title := clipRunes("Plan: "+r.Title, MaxTitleRunes)
		it := MigrateItem{
			Source: migrateSourceRow, SourceRef: r.ID, Title: title, Project: r.Project,
			Order: clampOrder(r.LegacyNumber),
		}
		if len(refs) == 0 {
			it.Source = migrateSourceRowBacklog
			switch {
			case hasPlan:
				it.PlanID, it.Title = plan.ID, plan.Title
				m.skip(it, "a backlog plan was already made for this row")
			case IsClosed(KindRegistryRow, r.State):
				m.skip(it, "the row is closed and names no Automaton or Automation")
			default:
				m.createPlan(it, r, marker, title)
			}
			continue
		}
		if hasPlan {
			it.PlanID, it.Title = plan.ID, plan.Title
		}
		for _, ref := range refs {
			ri := it
			ri.LinkKind, ri.LinkID = ref.kind, ref.id
			if t, ok := m.holder(ref.kind, ref.id); ok {
				if hasPlan && t == plan.Title && m.heldBy(plan, ref) {
					m.skip(ri, "already linked to this plan")
				} else {
					m.skip(ri, fmt.Sprintf("already linked to the plan %q", t))
				}
				continue
			}
			if hasPlan {
				m.linkTo(ri, plan.UID, ref.info)
				continue
			}
			if ex, ok := m.titleTaken(r.Project, title); ok {
				m.conflict(ri, fmt.Sprintf("a record titled %q already exists in the project (%s); left as it is", title, ex.ID))
				continue
			}
			created, ok := m.createAndLinkRow(ri, r, marker, title, ref)
			if ok {
				plan, hasPlan = created, true
				it.PlanID, it.Title = created.ID, created.Title
			}
		}
	}
}

// heldBy reports whether plan lists the ref.
func (m *migrator) heldBy(plan Record, ref foundRef) bool {
	list := plan.Automata
	if ref.kind == linkAutomation {
		list = plan.Automations
	}
	return indexOf(list, ref.id) >= 0
}

func rowPlanRecord(r Record, marker, title string) Record {
	return Record{
		Kind: KindPlan, Project: r.Project, State: r.State, Title: title,
		Goal: clipRunes(r.Body, MaxGoalRunes), Order: clampOrder(r.LegacyNumber),
		Links: []Link{{Kind: LinkMigratedFrom, Ref: marker}, {Kind: linkRegistryRow, Ref: r.ID}},
	}
}

func (m *migrator) createPlan(it MigrateItem, r Record, marker, title string) {
	if ex, ok := m.titleTaken(r.Project, title); ok {
		m.conflict(it, fmt.Sprintf("a record titled %q already exists in the project (%s); left as it is", title, ex.ID))
		return
	}
	if !m.opts.Apply {
		m.add(&m.rep.WouldCreate, it)
		return
	}
	rec := rowPlanRecord(r, marker, title)
	rec.Actor = m.opts.Actor
	made, err := m.s.Create(rec)
	if err != nil {
		m.conflict(it, "cannot create the plan: "+err.Error())
		return
	}
	m.markers[marker] = made
	it.PlanID, it.Order = made.ID, made.Order
	m.add(&m.rep.WouldCreate, it)
}

func (m *migrator) createAndLinkRow(it MigrateItem, r Record, marker, title string, ref foundRef) (Record, bool) {
	rec := rowPlanRecord(r, marker, title)
	made, ok := m.createAndLinkKind(it, rec, ref.kind, ref.id, ref.info)
	if ok {
		m.markers[marker] = made
	}
	return made, ok
}

func (m *migrator) createAndLink(it MigrateItem, rec Record, info LinkInfo) {
	m.createAndLinkKind(it, rec, it.LinkKind, it.LinkID, info)
}

// createAndLinkKind reports the create and the link, and writes both on an
// apply. The record returned is the stored plan on an apply and the planned
// one otherwise.
func (m *migrator) createAndLinkKind(it MigrateItem, rec Record, kind, id string, info LinkInfo) (Record, bool) {
	if !m.opts.Apply {
		m.claimed[linkKey(kind, id)] = rec.Title
		m.add(&m.rep.WouldCreate, it)
		m.add(&m.rep.WouldLink, it)
		return Record{Title: rec.Title, UID: "dry-run"}, true
	}
	rec.Actor = m.opts.Actor
	made, err := m.s.Create(rec)
	if err != nil {
		m.conflict(it, "cannot create the plan: "+err.Error())
		return Record{}, false
	}
	it.PlanID, it.Order = made.ID, made.Order
	m.add(&m.rep.WouldCreate, it)
	m.markers[firstMarker(made)] = made
	if !m.attach(it, made.UID, kind, id, info) {
		return made, true
	}
	return made, true
}

func firstMarker(r Record) string {
	for _, l := range r.Links {
		if l.Kind == LinkMigratedFrom {
			return l.Ref
		}
	}
	return ""
}

// linkTo links an existing plan (made by an earlier run) to the item's id.
func (m *migrator) linkTo(it MigrateItem, planUID string, info LinkInfo) {
	if !m.opts.Apply {
		if it.PlanID == "" {
			it.PlanID = planUID
		}
		m.claimed[linkKey(it.LinkKind, it.LinkID)] = it.Title
		m.add(&m.rep.WouldLink, it)
		return
	}
	m.attach(it, planUID, it.LinkKind, it.LinkID, info)
}

func (m *migrator) attach(it MigrateItem, planUID, kind, id string, info LinkInfo) bool {
	var err error
	if kind == linkAutomation {
		_, err = m.s.AttachAutomation(planUID, id, info)
	} else {
		_, err = m.s.AttachAutomaton(planUID, id, info)
	}
	if err != nil {
		var lc *ErrLinkConflict
		if errors.As(err, &lc) {
			m.conflict(it, err.Error())
		} else {
			m.conflict(it, "cannot link: "+err.Error())
		}
		return false
	}
	m.claimed[linkKey(kind, id)] = it.Title
	m.add(&m.rep.WouldLink, it)
	return true
}
