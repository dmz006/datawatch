package planrecords

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Release plans and their themes.
//
// A release plan is a plan with PlanKind release and a Release label. Its
// themes group the Automata and Automations the plan already holds; a theme
// never adds an item to the plan. An item is in at most one theme. Detaching
// an item from the plan (DetachAutomaton, DetachAutomation) takes it out of its
// theme, and removing a theme leaves its items in the plan, unplaced.
//
// Every theme write follows the Attach and Reorder conventions: the store
// mutex is held, the next version of the plan is validated, written through
// the atomic file write, swapped into the index, and one audit event is sent.

// AuditThemeChanged is the audit action of a theme write. Its details carry
// theme_op (create, rename, remove, place, unplace, reorder, reorder_items) and
// the theme id where one applies.
const AuditThemeChanged = "plan_record_theme_changed"

// Item kinds a theme can hold.
const (
	ItemAutomaton  = linkAutomaton
	ItemAutomation = linkAutomation
)

// ErrReleaseConflict is returned when a project already has a release plan for
// the release label.
type ErrReleaseConflict struct {
	Release  string
	Existing Record
}

func (e *ErrReleaseConflict) Error() string {
	return fmt.Sprintf("planrecords: release %q already has a release plan in project %s: %q (%s)",
		e.Release, e.Existing.Project, e.Existing.Title, e.Existing.ID)
}

// checkReleaseFreeLocked refuses a release plan whose label another release
// plan of the project already holds, so one release plan per release can be
// found. exceptUID is the plan being changed. The caller holds the lock.
func (s *Store) checkReleaseFreeLocked(rec Record, exceptUID string) error {
	if rec.Kind != KindPlan || rec.PlanKind != PlanKindRelease || rec.Release == "" {
		return nil
	}
	if h := s.releaseHolderLocked(rec.Project, rec.Release, exceptUID); h != nil {
		return &ErrReleaseConflict{Release: rec.Release, Existing: s.byUID[h.UID].clone()}
	}
	return nil
}

func (s *Store) releaseHolderLocked(project, release, exceptUID string) *Summary {
	want := NormTitle(release)
	for _, sm := range s.li.sums {
		if sm.Kind == KindPlan && sm.PlanKind == PlanKindRelease && sm.Project == project &&
			sm.UID != exceptUID && NormTitle(sm.Release) == want {
			sm := sm
			return &sm
		}
	}
	return nil
}

// ReleasePlan finds the release plan of a release in a project, from the
// summary index. The release label is compared trimmed and case-insensitively.
func (s *Store) ReleasePlan(project, release string) (Record, bool) {
	if NormTitle(release) == "" {
		return Record{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.releaseHolderLocked(project, release, "")
	if h == nil {
		return Record{}, false
	}
	return s.byUID[h.UID].clone(), true
}

// ReleasePlans lists the release plans, in priority order. A non-empty project
// keeps only that project's.
func (s *Store) ReleasePlans(project string) []Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Summary
	for _, sm := range s.planSummariesLocked() {
		if sm.PlanKind == PlanKindRelease && (project == "" || sm.Project == project) {
			out = append(out, sm)
		}
	}
	return out
}

// releasePlanLocked resolves a reference that must name a release plan.
func (s *Store) releasePlanLocked(ref string) (Record, error) {
	plan, err := s.planLocked(ref)
	if err != nil {
		return Record{}, err
	}
	if plan.PlanKind != PlanKindRelease {
		return Record{}, invalid("plan %q is not a release plan; themes belong to release plans", plan.ID)
	}
	return plan, nil
}

// commitTheme validates and writes next, the changed release plan, and sends
// the theme audit event.
func (s *Store) commitTheme(next Record, op string, extra map[string]any) (Record, error) {
	renumberThemes(next.Themes)
	if err := next.validate(); err != nil {
		return Record{}, err
	}
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	d := map[string]any{"theme_op": op}
	for k, v := range extra {
		d[k] = v
	}
	s.audit(AuditThemeChanged, next, d, "")
	return next.clone(), nil
}

// renumberThemes sets each theme's Order to its position, 1 first.
func renumberThemes(ts []Theme) {
	for i := range ts {
		ts[i].Order = i + 1
	}
}

func themeIndex(ts []Theme, ref string) int {
	ref = strings.TrimSpace(ref)
	for i, t := range ts {
		if t.ID == ref {
			return i
		}
	}
	norm := NormTitle(ref)
	if norm == "" {
		return -1
	}
	for i, t := range ts {
		if NormTitle(t.Name) == norm {
			return i
		}
	}
	return -1
}

func themeNotFound(ref string) error {
	return fmt.Errorf("%w: theme %q", ErrNotFound, ref)
}

func cleanThemeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if NormTitle(name) == "" {
		return "", invalid("a theme needs a name")
	}
	if runesOver(name, MaxThemeNameRunes) {
		return "", invalid("theme name longer than %d characters", MaxThemeNameRunes)
	}
	return name, nil
}

func nameTaken(ts []Theme, name string, exceptIdx int) bool {
	norm := NormTitle(name)
	for i, t := range ts {
		if i != exceptIdx && NormTitle(t.Name) == norm {
			return true
		}
	}
	return false
}

func newThemeID(ts []Theme, name string) string {
	base := slugify(name, 24)
	if base == "" {
		base = "theme"
	}
	for {
		id := base + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
		if themeIndex(ts, id) < 0 {
			return id
		}
	}
}

func cleanItemKind(kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind != ItemAutomaton && kind != ItemAutomation {
		return "", invalid("a theme item is an %s or an %s", ItemAutomaton, ItemAutomation)
	}
	return kind, nil
}

func themeList(t *Theme, kind string) *[]string {
	if kind == ItemAutomaton {
		return &t.Automata
	}
	return &t.Automations
}

func heldList(plan Record, kind string) []string {
	if kind == ItemAutomaton {
		return plan.Automata
	}
	return plan.Automations
}

// themeOfItem returns the index of the theme holding the item, or -1.
func themeOfItem(ts []Theme, kind, id string) int {
	for i := range ts {
		if indexOf(*themeList(&ts[i], kind), id) >= 0 {
			return i
		}
	}
	return -1
}

// removeFromThemes takes an item out of whichever theme holds it. It is what
// detaching an item from its plan calls.
func removeFromThemes(ts []Theme, kind, id string) []Theme {
	for i := range ts {
		l := themeList(&ts[i], kind)
		*l = removeID(*l, id)
	}
	return ts
}

// CreateTheme adds a theme, last in the plan's theme order, to a release plan
// and returns the plan and the new theme. A name already used by another theme
// of the plan is refused.
func (s *Store) CreateTheme(planRef, name string) (Record, Theme, error) {
	name, err := cleanThemeName(name)
	if err != nil {
		return Record{}, Theme{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, Theme{}, err
	}
	if len(plan.Themes) >= MaxThemes {
		return Record{}, Theme{}, invalid("too many themes (max %d)", MaxThemes)
	}
	if nameTaken(plan.Themes, name, -1) {
		return Record{}, Theme{}, invalid("theme name %q is already used in this plan", name)
	}
	next := plan.clone()
	th := Theme{ID: newThemeID(plan.Themes, name), Name: name}
	next.Themes = append(next.Themes, th)
	out, err := s.commitTheme(next, "create", map[string]any{"theme_id": th.ID, "theme_name": name})
	if err != nil {
		return Record{}, Theme{}, err
	}
	return out, out.Themes[len(out.Themes)-1].clone(), nil
}

// RenameTheme renames a theme, found by id or by name. A name already used by
// another theme of the plan is refused.
func (s *Store) RenameTheme(planRef, themeRef, name string) (Record, error) {
	name, err := cleanThemeName(name)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	i := themeIndex(plan.Themes, themeRef)
	if i < 0 {
		return Record{}, themeNotFound(themeRef)
	}
	if nameTaken(plan.Themes, name, i) {
		return Record{}, invalid("theme name %q is already used in this plan", name)
	}
	if plan.Themes[i].Name == name {
		return plan.clone(), nil
	}
	next := plan.clone()
	next.Themes[i].Name = name
	return s.commitTheme(next, "rename", map[string]any{"theme_id": next.Themes[i].ID, "theme_name": name})
}

// RemoveTheme removes a theme. Its items stay in the plan and become unplaced;
// nothing is detached from the plan.
func (s *Store) RemoveTheme(planRef, themeRef string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	i := themeIndex(plan.Themes, themeRef)
	if i < 0 {
		return Record{}, themeNotFound(themeRef)
	}
	next := plan.clone()
	id := next.Themes[i].ID
	next.Themes = append(next.Themes[:i], next.Themes[i+1:]...)
	if len(next.Themes) == 0 {
		next.Themes = nil
	}
	return s.commitTheme(next, "remove", map[string]any{"theme_id": id})
}

// PlaceInTheme puts an Automaton or Automation the plan holds last in a theme.
// An item already in another theme moves; it is never in two. The kind is
// ItemAutomaton or ItemAutomation. An item the plan does not hold is refused: a
// theme only groups what the plan has.
func (s *Store) PlaceInTheme(planRef, themeRef, kind, itemID string) (Record, error) {
	kind, err := cleanItemKind(kind)
	if err != nil {
		return Record{}, err
	}
	id, err := cleanLinkID(itemID)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	i := themeIndex(plan.Themes, themeRef)
	if i < 0 {
		return Record{}, themeNotFound(themeRef)
	}
	if indexOf(heldList(plan, kind), id) < 0 {
		return Record{}, invalid("%s %q is not linked to plan %q; attach it to the plan first", kind, id, plan.ID)
	}
	cur := themeOfItem(plan.Themes, kind, id)
	if cur == i {
		return plan.clone(), nil
	}
	next := plan.clone()
	removeFromThemes(next.Themes, kind, id)
	l := themeList(&next.Themes[i], kind)
	*l = append(*l, id)
	extra := map[string]any{"theme_id": next.Themes[i].ID, "item_kind": kind, "item_id": id}
	if cur >= 0 {
		extra["from_theme_id"] = plan.Themes[cur].ID
	}
	return s.commitTheme(next, "place", extra)
}

// UnplaceFromTheme takes an Automaton or Automation out of its theme. The item
// stays in the plan. An item that is in no theme is ErrNotFound.
func (s *Store) UnplaceFromTheme(planRef, kind, itemID string) (Record, error) {
	kind, err := cleanItemKind(kind)
	if err != nil {
		return Record{}, err
	}
	id, err := cleanLinkID(itemID)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	cur := themeOfItem(plan.Themes, kind, id)
	if cur < 0 {
		return Record{}, fmt.Errorf("%w: %s %q is in no theme of plan %q", ErrNotFound, kind, id, plan.ID)
	}
	next := plan.clone()
	removeFromThemes(next.Themes, kind, id)
	return s.commitTheme(next, "unplace", map[string]any{"theme_id": plan.Themes[cur].ID, "item_kind": kind, "item_id": id})
}

// ReorderThemes sets the order of a plan's themes. orderedRefs (ids or names)
// must name every theme of the plan exactly once.
func (s *Store) ReorderThemes(planRef string, orderedRefs []string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	if len(orderedRefs) != len(plan.Themes) {
		return Record{}, invalid("list every theme of the plan exactly once (%d themes, %d given)", len(plan.Themes), len(orderedRefs))
	}
	seen := map[string]bool{}
	ordered := make([]Theme, 0, len(plan.Themes))
	for _, ref := range orderedRefs {
		i := themeIndex(plan.Themes, ref)
		if i < 0 {
			return Record{}, themeNotFound(ref)
		}
		t := plan.Themes[i]
		if seen[t.ID] {
			return Record{}, invalid("theme %q is listed twice", t.ID)
		}
		seen[t.ID] = true
		ordered = append(ordered, t.clone())
	}
	same := true
	for i := range ordered {
		if ordered[i].ID != plan.Themes[i].ID {
			same = false
		}
	}
	if same {
		return plan.clone(), nil
	}
	next := plan.clone()
	next.Themes = ordered
	ids := make([]string, len(ordered))
	for i, t := range ordered {
		ids[i] = t.ID
	}
	return s.commitTheme(next, "reorder", map[string]any{"themes": ids})
}

// ReorderThemeItems sets the order of one kind of item inside a theme.
// orderedIDs must name exactly the items of that kind the theme holds, each
// once.
func (s *Store) ReorderThemeItems(planRef, themeRef, kind string, orderedIDs []string) (Record, error) {
	kind, err := cleanItemKind(kind)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.releasePlanLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	i := themeIndex(plan.Themes, themeRef)
	if i < 0 {
		return Record{}, themeNotFound(themeRef)
	}
	have := *themeList(&plan.Themes[i], kind)
	if len(orderedIDs) != len(have) {
		return Record{}, invalid("list every %s of the theme exactly once (%d placed, %d given)", kind, len(have), len(orderedIDs))
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(orderedIDs))
	for _, raw := range orderedIDs {
		id := strings.TrimSpace(raw)
		if indexOf(have, id) < 0 {
			return Record{}, invalid("%s %q is not in theme %q", kind, id, plan.Themes[i].ID)
		}
		if seen[id] {
			return Record{}, invalid("%s %q is listed twice", kind, id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	same := true
	for j := range ids {
		if ids[j] != have[j] {
			same = false
		}
	}
	if same {
		return plan.clone(), nil
	}
	next := plan.clone()
	*themeList(&next.Themes[i], kind) = ids
	return s.commitTheme(next, "reorder_items", map[string]any{"theme_id": next.Themes[i].ID, "item_kind": kind, "items": ids})
}

// ThemeItem is one Automaton or Automation of a plan as the themes view lists
// it, with the title and state last reported for it.
type ThemeItem struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	State string `json:"state,omitempty"`
}

// ThemeView is one theme with its items: its Automata first, then its
// Automations, each in the theme's order.
type ThemeView struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Order int         `json:"order"`
	Items []ThemeItem `json:"items"`
}

// ThemesView is a plan's themes in order plus the plan's items that are in no
// theme.
type ThemesView struct {
	Plan     PlanRef     `json:"plan"`
	PlanKind PlanKind    `json:"plan_kind,omitempty"`
	Release  string      `json:"release,omitempty"`
	Themes   []ThemeView `json:"themes"`
	Unplaced []ThemeItem `json:"unplaced"`
}

func themeItem(plan Record, kind, id string) ThemeItem {
	info := plan.LinkInfo[linkKey(kind, id)]
	return ThemeItem{Kind: kind, ID: id, Title: info.Title, State: info.State}
}

// ThemesOf returns a plan's themes with each item's title and state from the
// plan's link info, and the plan's Automata and Automations that are in no
// theme, in the plan's order. An ordinary plan has no themes: every item is
// unplaced.
func (s *Store) ThemesOf(planRef string) (ThemesView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return ThemesView{}, err
	}
	pr, _ := s.planRefLocked(plan.UID)
	v := ThemesView{Plan: pr, PlanKind: plan.PlanKind, Release: plan.Release, Themes: []ThemeView{}, Unplaced: []ThemeItem{}}
	ts := append([]Theme(nil), plan.Themes...)
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Order < ts[j].Order })
	for _, t := range ts {
		tv := ThemeView{ID: t.ID, Name: t.Name, Order: t.Order, Items: []ThemeItem{}}
		for _, id := range t.Automata {
			tv.Items = append(tv.Items, themeItem(plan, ItemAutomaton, id))
		}
		for _, id := range t.Automations {
			tv.Items = append(tv.Items, themeItem(plan, ItemAutomation, id))
		}
		v.Themes = append(v.Themes, tv)
	}
	for _, kind := range []string{ItemAutomaton, ItemAutomation} {
		for _, id := range heldList(plan, kind) {
			if themeOfItem(plan.Themes, kind, id) < 0 {
				v.Unplaced = append(v.Unplaced, themeItem(plan, kind, id))
			}
		}
	}
	return v, nil
}
