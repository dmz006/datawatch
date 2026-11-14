package planrecords

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Release migration.
//
// ReleaseMigrate files the Automata and Automations of a project under two
// release plans: the plan of the current release and the plan of the next one.
//
//   - Both release plans are found by their release label, never by title, so
//     a second run finds them. Either one is created when it is absent.
//   - Everything shipped since the last major release, or planned and not held,
//     goes to the current release plan. An item held for after the current
//     release, planned for the next release, or marked by the operator for it
//     goes to the next release plan.
//   - Each item is placed in a theme chosen by ReleaseThemeRules from its
//     Automation, tags and title. An item no rule matches is listed in
//     Unplaced and attached with no theme. The operator's mark wins over the
//     rules.
//   - An item already linked to some other plan is a conflict: the holding plan
//     is named and the item is never moved. An item already on the right plan
//     is unchanged.
//
// The default is a preview. It writes nothing and returns the report with a
// preview id, a stable hash of the report. A written run needs Apply and the
// preview id of the exact preview the operator approved; a missing or stale id
// is refused before anything is written. A second written run with the same
// inputs changes nothing. Nothing is deleted.

// The cut-over release labels used when the caller names none.
const (
	DefaultCurrentRelease = "10.0"
	DefaultNextRelease    = "11.0"
)

// AuditReleaseMigrated is the audit action of every change a written release
// migration makes. Its details carry actor, op (create_plan, create_theme,
// attach, place) and what the change touched.
const AuditReleaseMigrated = "plan_record_release_migrated"

// The fact kinds, and the report item kinds beyond them.
const (
	ReleaseKindAutomaton  = linkAutomaton
	ReleaseKindAutomation = linkAutomation
	releaseKindPlan       = "plan"
	releaseKindTheme      = "theme"
	releaseKindMark       = "mark"
)

// ThemeSource says where an item's theme came from.
const (
	ThemeFromMark = "mark"
	ThemeFromRule = "rule"
)

// The themes the rule table places items in, in display order.
const (
	ThemeCoreAutomation = "Core automation and projects"
	ThemeSecurity       = "Security and encryption"
	ThemeMobile         = "Mobile and the app"
	ThemeReleaseTooling = "Release tooling and tests"
)

// ReleaseThemeNames lists the themes of the rule table, in display order.
func ReleaseThemeNames() []string {
	return []string{ThemeCoreAutomation, ThemeSecurity, ThemeMobile, ThemeReleaseTooling}
}

// Fields a theme rule can look at.
const (
	RuleFieldTag        = "tag"
	RuleFieldAutomation = "automation"
	RuleFieldTitle      = "title"
)

// ThemeRule is one row of the theme table. A tag rule matches a tag exactly
// (ignoring case); an automation or title rule matches when the words of any
// phrase appear together, in order, in the Automation's title or the item's
// title.
type ThemeRule struct {
	Field string   `json:"field"`
	Any   []string `json:"any"`
	Theme string   `json:"theme"`
}

// ReleaseThemeRules is the whole theme table. The first row that matches an
// item decides its theme, so a more specific row goes before a broader one and
// tags go before the Automation title, which goes before the item's own title.
// It is data: change a row here and every surface and doc that lists the table
// follows.
var ReleaseThemeRules = []ThemeRule{
	{RuleFieldTag, []string{"security", "encryption", "secrets", "crypto", "tls"}, ThemeSecurity},
	{RuleFieldTag, []string{"mobile", "android", "ios", "iphone", "app"}, ThemeMobile},
	{RuleFieldTag, []string{"release", "tests", "testing", "e2e", "ci"}, ThemeReleaseTooling},
	{RuleFieldTag, []string{"automata", "automation", "autonomous", "projects", "plans", "core"}, ThemeCoreAutomation},

	{RuleFieldAutomation, []string{"security", "encryption", "secrets", "vault", "tls", "certificate"}, ThemeSecurity},
	{RuleFieldAutomation, []string{"mobile", "android", "ios", "iphone", "companion app"}, ThemeMobile},
	{RuleFieldAutomation, []string{"release", "tests", "testing", "e2e", "smoke", "changelog"}, ThemeReleaseTooling},
	{RuleFieldAutomation, []string{"automata", "automaton", "automation", "projects", "project", "plans", "plan"}, ThemeCoreAutomation},

	{RuleFieldTitle, []string{"security", "encryption", "encrypt", "secret", "secrets", "vault", "tls", "cve", "certificate"}, ThemeSecurity},
	{RuleFieldTitle, []string{"mobile", "android", "ios", "iphone", "companion app"}, ThemeMobile},
	{RuleFieldTitle, []string{"release", "tests", "test", "e2e", "smoke", "changelog", "goreleaser"}, ThemeReleaseTooling},
	{RuleFieldTitle, []string{"automata", "automaton", "automation", "projects", "project", "plans", "plan", "decompose", "executor"}, ThemeCoreAutomation},
}

// ReleaseFact is one Automaton or Automation as the caller's resolver reports
// it. The engine reads nothing else about an item.
type ReleaseFact struct {
	ID string `json:"id"`
	// Kind is ReleaseKindAutomaton or ReleaseKindAutomation.
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Tags  []string `json:"tags,omitempty"`
	// State is the item's own state, such as "completed" or "draft".
	State string `json:"state"`
	// Held is true when the item is held for after the current release.
	Held bool `json:"held,omitempty"`
	// Release is the release label the item shipped in or is planned for, such
	// as a version number. Empty when none is recorded.
	Release string `json:"release,omitempty"`
	// Automation is the id of the Automation an Automaton belongs to, and
	// AutomationTitle is that Automation's title (used by the theme rules).
	Automation      string `json:"automation,omitempty"`
	AutomationTitle string `json:"automation_title,omitempty"`
}

// ReleaseResolver supplies the facts. The caller provides it; the engine never
// reads Automata or Automations itself.
type ReleaseResolver func() ([]ReleaseFact, error)

// ReleaseMark is the operator's choice for one item: a target release and an
// optional theme. An empty field leaves that choice to the engine.
type ReleaseMark struct {
	Release string `json:"release,omitempty"`
	Theme   string `json:"theme,omitempty"`
}

// ReleaseMigrateOptions are the inputs of ReleaseMigrate.
type ReleaseMigrateOptions struct {
	Store   *Store
	Project string
	Resolve ReleaseResolver
	// CurrentRelease and NextRelease are the release labels of the two plans;
	// they default to DefaultCurrentRelease and DefaultNextRelease.
	CurrentRelease string
	NextRelease    string
	// Marks maps an Automaton or Automation id to the operator's choice.
	Marks map[string]ReleaseMark
	// Apply writes. False (the default) is a preview.
	Apply bool
	// ApprovalToken is the preview id of the preview the operator approved. A
	// written run refuses without a matching one.
	ApprovalToken string
	// Audit gets one entry per written change.
	Audit AuditFunc
	// Actor names who ran the migration for the audit entries.
	Actor string
}

// ReleaseMigrateItem is one line of the report.
type ReleaseMigrateItem struct {
	// Kind is "automaton", "automation", "plan", "theme" or "mark".
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	// Release is the release label the item goes to (or the plan stands for).
	Release string `json:"release,omitempty"`
	// Theme and ThemeSource say where an attached item is placed and why.
	Theme       string `json:"theme,omitempty"`
	ThemeSource string `json:"theme_source,omitempty"`
	// PlanID is the release plan, when it exists.
	PlanID string `json:"plan_id,omitempty"`
	// HoldingPlan and HoldingPlanID name the plan a conflicting item is in.
	HoldingPlan   string `json:"holding_plan,omitempty"`
	HoldingPlanID string `json:"holding_plan_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// ReleaseMigrateReport is the result of ReleaseMigrate. After a written run
// the lists hold what was written.
type ReleaseMigrateReport struct {
	Applied bool `json:"applied"`
	// PreviewID is a stable hash of the report; a written run must quote it.
	PreviewID      string `json:"preview_id"`
	CurrentRelease string `json:"current_release"`
	NextRelease    string `json:"next_release"`
	// Creates are the release plans and themes that are (or would be) made.
	Creates []ReleaseMigrateItem `json:"creates"`
	// Attaches are the items attached, with their theme (empty when unplaced).
	Attaches []ReleaseMigrateItem `json:"attaches"`
	// Unplaced are attached items no rule matched and no mark placed.
	Unplaced []ReleaseMigrateItem `json:"unplaced"`
	// Conflicts are items refused: held by another plan, or a bad mark.
	Conflicts []ReleaseMigrateItem `json:"conflicts"`
	// Unchanged are release plans and items already as they should be.
	Unchanged []ReleaseMigrateItem `json:"unchanged"`
	// Skipped are items outside the two releases, with the reason.
	Skipped []ReleaseMigrateItem `json:"skipped"`
}

// Counts is the size of each list.
func (r ReleaseMigrateReport) Counts() map[string]int {
	return map[string]int{
		"creates": len(r.Creates), "attaches": len(r.Attaches), "unplaced": len(r.Unplaced),
		"conflicts": len(r.Conflicts), "unchanged": len(r.Unchanged), "skipped": len(r.Skipped),
	}
}

// Changes is the number of writes the report stands for.
func (r ReleaseMigrateReport) Changes() int {
	n := len(r.Creates)
	for _, a := range r.Attaches {
		n++
		if a.Theme != "" {
			n++
		}
	}
	return n
}

// computePreviewID hashes the report without its own preview id and applied
// flag, so a preview and the written run of the same state agree.
func (r ReleaseMigrateReport) computePreviewID() string {
	r.PreviewID, r.Applied = "", false
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// Errors a written run answers with before it writes anything.
var (
	ErrPreviewMissing = errors.New("planrecords: a written release migration needs the preview id of the approved preview; run the preview first")
	ErrPreviewStale   = errors.New("planrecords: the preview id does not match what a preview shows now; the data changed or the id is wrong, run the preview again")
)

var releaseMigrateMu sync.Mutex

// ReleaseMigrate runs the release migration. See the file comment.
func ReleaseMigrate(opts ReleaseMigrateOptions) (ReleaseMigrateReport, error) {
	var rep ReleaseMigrateReport
	if opts.Store == nil {
		return rep, errors.New("planrecords: release migrate needs a store")
	}
	if opts.Resolve == nil {
		return rep, errors.New("planrecords: release migrate needs a resolver for the Automata and Automations")
	}
	cur := strings.TrimSpace(opts.CurrentRelease)
	if cur == "" {
		cur = DefaultCurrentRelease
	}
	next := strings.TrimSpace(opts.NextRelease)
	if next == "" {
		next = DefaultNextRelease
	}
	opts.CurrentRelease, opts.NextRelease = cur, next
	if !projectRe.MatchString(opts.Project) || runesOver(opts.Project, MaxProjectRunes) {
		return rep, errors.New("planrecords: release migrate needs a valid project name")
	}
	curMajor, ok1 := releaseMajor(cur)
	nextMajor, ok2 := releaseMajor(next)
	if !ok1 || !ok2 {
		return rep, errors.New("planrecords: the release labels must start with a major number")
	}
	if runesOver(cur, MaxReleaseRunes) || runesOver(next, MaxReleaseRunes) {
		return rep, fmt.Errorf("planrecords: a release label is longer than %d characters", MaxReleaseRunes)
	}
	if NormTitle(cur) == NormTitle(next) || nextMajor <= curMajor {
		return rep, errors.New("planrecords: the next release must come after the current release")
	}

	releaseMigrateMu.Lock()
	defer releaseMigrateMu.Unlock()
	facts, err := opts.Resolve()
	if err != nil {
		return rep, fmt.Errorf("planrecords: cannot read the Automata and Automations: %w", err)
	}
	m := &releaseMigrator{opts: opts, s: opts.Store, curMajor: curMajor, nextMajor: nextMajor}
	m.compute(facts)
	rep = m.report()
	rep.PreviewID = rep.computePreviewID()
	if !opts.Apply {
		return rep, nil
	}
	token := strings.TrimSpace(opts.ApprovalToken)
	if token == "" {
		return rep, ErrPreviewMissing
	}
	if token != rep.PreviewID {
		return rep, ErrPreviewStale
	}
	rep.Applied = true
	if err := m.execute(&rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// releaseMajor reads the major number of a release label (a leading v is allowed).
func releaseMajor(label string) (int, bool) {
	label = strings.TrimLeft(strings.TrimSpace(label), "vV")
	end := 0
	for end < len(label) && label[end] >= '0' && label[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(label[:end])
	return n, err == nil
}

// The state classes of a fact.
const (
	stateShipped = "shipped"
	statePlanned = "planned"
	stateOther   = "other"
)

func releaseStateClass(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "shipped", "completed", "done", "merged":
		return stateShipped
	case "draft", "planning", "planned", "needs_review", "revisions_asked", "approved", "active",
		"running", "blocked", "paused", "queued", "open", "in_progress":
		return statePlanned
	}
	return stateOther
}

// relPlan is one of the two release plans, existing or to be created.
type relPlan struct {
	label    string
	existing *Record
	// create is true when the plan does not exist and can be created.
	create bool
	// blocked says why the plan can be neither found nor created.
	blocked string
	// themesNeeded are the theme names attaches will place items in.
	themesNeeded map[string]bool
}

func (p *relPlan) title() string { return "Release " + p.label }

// relAttach is one item to attach, with its target.
type relAttach struct {
	fact   ReleaseFact
	plan   *relPlan
	theme  string
	source string
}

type releaseMigrator struct {
	opts                ReleaseMigrateOptions
	s                   *Store
	curMajor, nextMajor int
	cur, next           *relPlan

	creates, unchanged, conflicts, skipped []ReleaseMigrateItem
	attaches                               []relAttach
	unplaced                               []relAttach
}

func (m *releaseMigrator) titleHolder(title string) (Record, bool) {
	m.s.mu.RLock()
	defer m.s.mu.RUnlock()
	uid, ok := m.s.byTitle[titleKey(m.opts.Project, title)]
	if !ok {
		return Record{}, false
	}
	return m.s.byUID[uid].clone(), true
}

func (m *releaseMigrator) newPlan(label string) *relPlan {
	p := &relPlan{label: label, themesNeeded: map[string]bool{}}
	if rec, ok := m.s.ReleasePlan(m.opts.Project, label); ok {
		p.existing = &rec
		return p
	}
	if ex, ok := m.titleHolder(p.title()); ok {
		p.blocked = fmt.Sprintf("a record titled %q already exists in the project (%s) but is not the release plan; left as it is", p.title(), ex.ID)
		return p
	}
	p.create = true
	return p
}

// planFor returns the plan a release label names, or nil.
func (m *releaseMigrator) planFor(label string) *relPlan {
	switch NormTitle(label) {
	case NormTitle(m.cur.label):
		return m.cur
	case NormTitle(m.next.label):
		return m.next
	}
	return nil
}

func (m *releaseMigrator) compute(facts []ReleaseFact) {
	m.cur, m.next = m.newPlan(m.opts.CurrentRelease), m.newPlan(m.opts.NextRelease)
	facts = append([]ReleaseFact(nil), facts...)
	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].Kind != facts[j].Kind {
			return facts[i].Kind < facts[j].Kind
		}
		return facts[i].ID < facts[j].ID
	})
	seen := map[string]bool{}
	marksUsed := map[string]bool{}
	for _, f := range facts {
		f.ID, f.Title = strings.TrimSpace(f.ID), strings.TrimSpace(f.Title)
		it := ReleaseMigrateItem{Kind: f.Kind, ID: f.ID, Title: f.Title}
		if f.ID == "" || (f.Kind != ReleaseKindAutomaton && f.Kind != ReleaseKindAutomation) {
			it.Reason = "the fact has no id or its kind is not automaton or automation"
			m.skipped = append(m.skipped, it)
			continue
		}
		if _, err := cleanLinkID(f.ID); err != nil {
			it.Reason = err.Error()
			m.skipped = append(m.skipped, it)
			continue
		}
		if k := linkKey(f.Kind, f.ID); seen[k] {
			it.Reason = "listed twice; the first one is used"
			m.skipped = append(m.skipped, it)
			continue
		} else {
			seen[k] = true
		}
		mark, marked := m.opts.Marks[f.ID]
		if marked {
			marksUsed[f.ID] = true
		}
		m.place(f, it, mark, marked)
	}
	var stray []string
	for id := range m.opts.Marks {
		if !marksUsed[id] {
			stray = append(stray, id)
		}
	}
	sort.Strings(stray)
	for _, id := range stray {
		m.skipped = append(m.skipped, ReleaseMigrateItem{
			Kind: releaseKindMark, ID: id, Reason: "the mark names an id that is not among the Automata and Automations",
		})
	}
	m.planRows()
}

// target decides which release plan a fact goes to. A false second result
// means the fact is outside both releases; the string says why.
func (m *releaseMigrator) target(f ReleaseFact, mark ReleaseMark) (*relPlan, string, bool) {
	if rel := strings.TrimSpace(mark.Release); rel != "" {
		p := m.planFor(rel)
		if p == nil {
			return nil, fmt.Sprintf("the mark names release %q; this run files work under %q and %q only", rel, m.cur.label, m.next.label), false
		}
		return p, "", true
	}
	if f.Held {
		return m.next, "", true
	}
	major, haveVersion := releaseMajor(f.Release)
	if haveVersion && major >= m.nextMajor {
		return m.next, "", true
	}
	switch releaseStateClass(f.State) {
	case stateShipped:
		if !haveVersion {
			return nil, "shipped with no release recorded; mark it to file it", false
		}
		if major < m.curMajor-1 {
			return nil, "shipped before the last major release", false
		}
		return m.cur, "", true
	case statePlanned:
		return m.cur, "", true
	}
	return nil, fmt.Sprintf("its state %q is neither shipped nor planned; mark it to file it", f.State), false
}

func (m *releaseMigrator) place(f ReleaseFact, it ReleaseMigrateItem, mark ReleaseMark, marked bool) {
	theme := strings.TrimSpace(mark.Theme)
	if marked && theme != "" && (NormTitle(theme) == "" || runesOver(theme, MaxThemeNameRunes)) {
		it.Reason = fmt.Sprintf("the mark's theme is empty or longer than %d characters", MaxThemeNameRunes)
		m.conflicts = append(m.conflicts, it)
		return
	}
	p, why, ok := m.target(f, mark)
	if !ok {
		it.Reason = why
		if marked && strings.TrimSpace(mark.Release) != "" {
			m.conflicts = append(m.conflicts, it)
		} else {
			m.skipped = append(m.skipped, it)
		}
		return
	}
	it.Release = p.label
	if p.existing != nil {
		it.PlanID = p.existing.ID
	}
	if p.blocked != "" {
		it.Reason = "its release plan cannot be created: " + p.blocked
		m.conflicts = append(m.conflicts, it)
		return
	}
	if h, held := m.holder(f.Kind, f.ID); held {
		if p.existing != nil && h.UID == p.existing.UID {
			it.Reason = "already on the release plan"
			m.unchanged = append(m.unchanged, it)
			return
		}
		it.HoldingPlan, it.HoldingPlanID = h.Title, h.ID
		it.Reason = (&ErrLinkConflict{Kind: f.Kind, ID: f.ID, Holder: h.ID}).Error()
		m.conflicts = append(m.conflicts, it)
		return
	}
	a := relAttach{fact: f, plan: p}
	if theme != "" {
		a.theme, a.source = theme, ThemeFromMark
	} else if t, ok := matchThemeRule(f); ok {
		a.theme, a.source = t, ThemeFromRule
	}
	if a.theme != "" {
		p.themesNeeded[a.theme] = true
	} else {
		m.unplaced = append(m.unplaced, a)
	}
	m.attaches = append(m.attaches, a)
}

func (m *releaseMigrator) holder(kind, id string) (PlanRef, bool) {
	if kind == ReleaseKindAutomation {
		return m.s.PlanOfAutomation(id)
	}
	return m.s.PlanOf(id)
}

// planRows lists the release plans and themes the run creates or finds.
func (m *releaseMigrator) planRows() {
	var creates, unchanged []ReleaseMigrateItem
	for _, p := range []*relPlan{m.cur, m.next} {
		switch {
		case p.existing != nil:
			unchanged = append(unchanged, ReleaseMigrateItem{
				Kind: releaseKindPlan, Title: p.existing.Title, Release: p.label, PlanID: p.existing.ID,
				Reason: "the release plan already exists",
			})
		case p.create:
			creates = append(creates, ReleaseMigrateItem{Kind: releaseKindPlan, Title: p.title(), Release: p.label})
		default:
			m.conflicts = append(m.conflicts, ReleaseMigrateItem{
				Kind: releaseKindPlan, Title: p.title(), Release: p.label, Reason: p.blocked,
			})
			continue
		}
		for _, name := range m.themeOrder(p) {
			if p.existing != nil && themeIndex(p.existing.Themes, name) >= 0 {
				continue
			}
			creates = append(creates, ReleaseMigrateItem{Kind: releaseKindTheme, Theme: name, Release: p.label})
		}
	}
	m.creates = append(creates, m.creates...)
	m.unchanged = append(unchanged, m.unchanged...)
}

// themeOrder lists the themes a plan needs: the rule table's themes in their
// order, then the operator's own, alphabetically.
func (m *releaseMigrator) themeOrder(p *relPlan) []string {
	var out []string
	known := map[string]bool{}
	for _, n := range ReleaseThemeNames() {
		known[NormTitle(n)] = true
		for need := range p.themesNeeded {
			if NormTitle(need) == NormTitle(n) {
				out = append(out, n)
				break
			}
		}
	}
	var own []string
	for need := range p.themesNeeded {
		if !known[NormTitle(need)] {
			own = append(own, need)
		}
	}
	sort.Strings(own)
	return append(out, own...)
}

func themeWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

// hasPhrase reports whether the words of phrase appear together, in order.
func hasPhrase(words []string, phrase string) bool {
	want := themeWords(phrase)
	if len(want) == 0 {
		return false
	}
	for i := 0; i+len(want) <= len(words); i++ {
		match := true
		for j := range want {
			if words[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// MatchThemeRule returns the theme the rule table gives a fact, if any.
func MatchThemeRule(f ReleaseFact) (string, bool) { return matchThemeRule(f) }

func matchThemeRule(f ReleaseFact) (string, bool) {
	autoTitle := f.AutomationTitle
	if autoTitle == "" && f.Kind == ReleaseKindAutomation {
		autoTitle = f.Title
	}
	tags := map[string]bool{}
	for _, t := range f.Tags {
		tags[strings.ToLower(strings.TrimSpace(t))] = true
	}
	autoWords, titleWords := themeWords(autoTitle), themeWords(f.Title)
	for _, r := range ReleaseThemeRules {
		for _, a := range r.Any {
			switch r.Field {
			case RuleFieldTag:
				if tags[strings.ToLower(a)] {
					return r.Theme, true
				}
			case RuleFieldAutomation:
				if hasPhrase(autoWords, a) {
					return r.Theme, true
				}
			case RuleFieldTitle:
				if hasPhrase(titleWords, a) {
					return r.Theme, true
				}
			}
		}
	}
	return "", false
}

func attachItem(a relAttach) ReleaseMigrateItem {
	it := ReleaseMigrateItem{
		Kind: a.fact.Kind, ID: a.fact.ID, Title: a.fact.Title, Release: a.plan.label,
		Theme: a.theme, ThemeSource: a.source,
	}
	if a.plan.existing != nil {
		it.PlanID = a.plan.existing.ID
	}
	if a.theme == "" {
		it.Reason = "no theme rule matched; place it by hand"
	}
	return it
}

func (m *releaseMigrator) report() ReleaseMigrateReport {
	rep := ReleaseMigrateReport{
		CurrentRelease: m.opts.CurrentRelease, NextRelease: m.opts.NextRelease,
		Creates: nonNil(m.creates), Unchanged: nonNil(m.unchanged),
		Conflicts: nonNil(m.conflicts), Skipped: nonNil(m.skipped),
		Attaches: []ReleaseMigrateItem{}, Unplaced: []ReleaseMigrateItem{},
	}
	for _, a := range m.attaches {
		rep.Attaches = append(rep.Attaches, attachItem(a))
	}
	for _, a := range m.unplaced {
		rep.Unplaced = append(rep.Unplaced, attachItem(a))
	}
	return rep
}

func nonNil(l []ReleaseMigrateItem) []ReleaseMigrateItem {
	if l == nil {
		return []ReleaseMigrateItem{}
	}
	return l
}

func (m *releaseMigrator) audit(op string, subject string, d map[string]any) {
	if m.opts.Audit == nil {
		return
	}
	actor := m.opts.Actor
	if actor == "" {
		actor = "system"
	}
	d["actor"], d["op"], d["project"] = actor, op, m.opts.Project
	m.opts.Audit(AuditReleaseMigrated, subject, d)
}

// execute writes what the report lists. The approval was checked and the
// mutex is held, so the state is the one the operator saw.
func (m *releaseMigrator) execute(rep *ReleaseMigrateReport) error {
	actor := m.opts.Actor
	var made []*relPlan
	for _, p := range []*relPlan{m.cur, m.next} {
		if !p.create {
			continue
		}
		state := StatePlanned
		goal := fmt.Sprintf("Work held for after release %s, and work marked for release %s.", m.cur.label, p.label)
		if p == m.cur {
			state = StateInProgress
			goal = fmt.Sprintf("Work shipped or planned for release %s.", p.label)
		}
		rec, err := m.s.Create(Record{
			Kind: KindPlan, Project: m.opts.Project, State: state, Title: p.title(), Goal: goal,
			PlanKind: PlanKindRelease, Release: p.label, Actor: actor,
		})
		if err != nil {
			return fmt.Errorf("planrecords: cannot create the release plan %q: %w", p.title(), err)
		}
		p.existing, p.create = &rec, false
		made = append(made, p)
		m.audit("create_plan", rec.Title, map[string]any{"release": p.label, "id": rec.ID})
	}
	for _, p := range []*relPlan{m.cur, m.next} {
		for _, name := range m.themeOrder(p) {
			if themeIndex(p.existing.Themes, name) >= 0 {
				continue
			}
			rec, th, err := m.s.CreateTheme(p.existing.UID, name)
			if err != nil {
				return fmt.Errorf("planrecords: cannot create the theme %q in %q: %w", name, p.title(), err)
			}
			p.existing = &rec
			m.audit("create_theme", name, map[string]any{"release": p.label, "plan_id": rec.ID, "theme_id": th.ID})
		}
	}
	for i := range rep.Creates {
		it := &rep.Creates[i]
		if it.Kind != releaseKindPlan {
			continue
		}
		for _, p := range made {
			if p.label == it.Release {
				it.PlanID = p.existing.ID
			}
		}
	}
	var refused []ReleaseMigrateItem
	attached := rep.Attaches[:0:0]
	for _, a := range m.attaches {
		info := LinkInfo{Title: a.fact.Title, State: a.fact.State}
		var err error
		if a.fact.Kind == ReleaseKindAutomation {
			_, err = m.s.AttachAutomation(a.plan.existing.UID, a.fact.ID, info)
		} else {
			_, err = m.s.AttachAutomaton(a.plan.existing.UID, a.fact.ID, info)
		}
		if err != nil {
			var lc *ErrLinkConflict
			if errors.As(err, &lc) {
				it := attachItem(a)
				it.Theme, it.ThemeSource, it.HoldingPlanID, it.Reason = "", "", lc.Holder, err.Error()
				refused = append(refused, it)
				continue
			}
			return fmt.Errorf("planrecords: cannot attach %s %q: %w", a.fact.Kind, a.fact.ID, err)
		}
		m.audit("attach", a.fact.Title, map[string]any{
			"release": a.plan.label, "plan_id": a.plan.existing.ID, "item_kind": a.fact.Kind, "item_id": a.fact.ID,
		})
		if a.theme != "" {
			if _, err := m.s.PlaceInTheme(a.plan.existing.UID, a.theme, a.fact.Kind, a.fact.ID); err != nil {
				return fmt.Errorf("planrecords: cannot place %s %q in the theme %q: %w", a.fact.Kind, a.fact.ID, a.theme, err)
			}
			m.audit("place", a.fact.Title, map[string]any{
				"release": a.plan.label, "plan_id": a.plan.existing.ID, "item_kind": a.fact.Kind, "item_id": a.fact.ID, "theme": a.theme,
			})
		}
		attached = append(attached, attachItem(a))
	}
	if len(refused) > 0 {
		// Another writer linked an item between the preview and the write.
		rep.Conflicts = append(rep.Conflicts, refused...)
		rep.Attaches = attached
		rep.Unplaced = rep.Unplaced[:0:0]
		for _, it := range attached {
			if it.Theme == "" {
				rep.Unplaced = append(rep.Unplaced, it)
			}
		}
	}
	for i := range rep.Attaches {
		if p := m.planFor(rep.Attaches[i].Release); p != nil && p.existing != nil {
			rep.Attaches[i].PlanID = p.existing.ID
		}
	}
	for i := range rep.Unplaced {
		if p := m.planFor(rep.Unplaced[i].Release); p != nil && p.existing != nil {
			rep.Unplaced[i].PlanID = p.existing.ID
		}
	}
	return nil
}
