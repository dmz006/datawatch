package planrecords

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind is the kind of a plan record.
type Kind string

// The record kinds. A plan is a registry record that links down to
// Automations, Automata and findings; a registry row is a numbered work item
// from the old registry; a finding is a problem attached to a plan or an
// Automaton.
const (
	KindPlan        Kind = "plan"
	KindRegistryRow Kind = "registry_row"
	KindFinding     Kind = "finding"
)

// What a finding can be attached to.
const (
	AttachPlan      = "plan"
	AttachAutomaton = "automaton"
)

// State is where a record is in its life. The valid set depends on the kind.
type State string

// Registry-row states.
const (
	StateOpen       State = "open"
	StatePlanned    State = "planned"
	StateInProgress State = "in_progress"
	StateBlocked    State = "blocked"
	StateShipped    State = "shipped"
	StateDone       State = "done"
	StateDeferred   State = "deferred"
	StateCancelled  State = "cancelled"
)

// Finding states (StateOpen is shared with registry rows).
const (
	StateTriaged   State = "triaged"
	StateFixed     State = "fixed"
	StateWontfix   State = "wontfix"
	StateDuplicate State = "duplicate"
)

// Size caps. A record that exceeds one is rejected, never truncated.
const (
	MaxTitleRunes     = 200
	MaxBodyBytes      = 100 * 1024
	MaxAliases        = 50
	MaxLinks          = 100
	MaxLinkKindRunes  = 32
	MaxLinkRefRunes   = 512
	MaxSourceRunes    = 512
	MaxVersionRunes   = 128
	MaxAutomatonRunes = 128
	MaxReasonRunes    = 1000
	MaxProjectRunes   = 64
	MaxIDRunes        = 64
	MaxGoalRunes      = 2000
	MaxLinkedItems    = 500
	MaxOrder          = 1_000_000
	// MaxReleaseRunes bounds a release plan's release label.
	MaxReleaseRunes = 64
	// MaxThemes bounds the themes of one plan; MaxThemeNameRunes bounds a theme name.
	MaxThemes         = 50
	MaxThemeNameRunes = 100
	// MaxArchiveReasonRunes bounds the reason given when a plan is archived.
	MaxArchiveReasonRunes = 1000
)

// PlanKind marks what sort of plan a plan record is. The empty value is an
// ordinary plan, so a record written before the marker existed loads as one.
type PlanKind string

// The plan kinds.
const (
	PlanKindOrdinary PlanKind = ""
	// PlanKindRelease is a release plan: it stands for one release and may group
	// the items it holds into themes.
	PlanKindRelease PlanKind = "release"
)

// ValidPlanKind reports whether k is a known plan kind marker.
func ValidPlanKind(k PlanKind) bool { return k == PlanKindOrdinary || k == PlanKindRelease }

// Theme groups items a release plan already holds. An item (an Automaton or an
// Automation of the plan) is in at most one theme of its plan.
type Theme struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Order is the theme's position, 1 first.
	Order int `json:"order"`
	// Automata and Automations are the ids placed in the theme, in display order.
	Automata    []string `json:"automata,omitempty"`
	Automations []string `json:"automations,omitempty"`
}

func (t Theme) clone() Theme {
	if t.Automata != nil {
		t.Automata = append([]string(nil), t.Automata...)
	}
	if t.Automations != nil {
		t.Automations = append([]string(nil), t.Automations...)
	}
	return t
}

// Link points a record at something else (an Automaton, a doc, an issue).
type Link struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// Record is one registry row or finding.
type Record struct {
	ID           string `json:"id"`
	UID          string `json:"uid"`
	Kind         Kind   `json:"kind"`
	Project      string `json:"project"`
	State        State  `json:"state"`
	Title        string `json:"title"`
	Body         string `json:"body,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	ClosedAt     string `json:"closed_at,omitempty"`
	LegacyNumber int    `json:"legacy_number,omitempty"`
	LegacySource string `json:"legacy_source,omitempty"`
	ShippedBy    string `json:"shipped_by,omitempty"`
	RaisedBy     string `json:"raised_by,omitempty"`
	// ShippedVersion is free text for what the archive said.
	ShippedVersion string   `json:"shipped_version,omitempty"`
	Links          []Link   `json:"links,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
	NeedsReview    bool     `json:"needs_review,omitempty"`
	ReviewReason   string   `json:"review_reason,omitempty"`

	// Plan fields (kind plan only). Order is the priority order, lowest first;
	// Automations, Automata and Findings are the links down, in display order.
	// They change only through the store's Attach, Detach and Reorder methods.
	Goal        string   `json:"goal,omitempty"`
	Order       int      `json:"order,omitempty"`
	Automations []string `json:"automations,omitempty"`
	Automata    []string `json:"automata,omitempty"`
	Findings    []string `json:"findings,omitempty"`
	// LinkInfo keeps the title and state last reported for a linked Automaton
	// or Automation, keyed "automaton:<id>" or "automation:<id>", so lists and
	// search never read the linked item itself.
	LinkInfo map[string]LinkInfo `json:"link_info,omitempty"`
	// PlanKind marks a release plan (PlanKindRelease); empty is an ordinary plan.
	// Release is the release label a release plan stands for. Themes group the
	// items the plan already holds; they change only through the store's theme
	// methods.
	PlanKind PlanKind `json:"plan_kind,omitempty"`
	Release  string   `json:"release,omitempty"`
	Themes   []Theme  `json:"themes,omitempty"`
	// Archived marks a plan that was taken out of the default lists, search
	// results and counts. ArchivedAt is when, and ArchiveReason the optional
	// reason. They change only through the store's Archive and Restore
	// methods; the plan's file and every link it holds stay as they were.
	Archived      bool   `json:"archived,omitempty"`
	ArchivedAt    string `json:"archived_at,omitempty"`
	ArchiveReason string `json:"archive_reason,omitempty"`

	// Finding fields (kind finding only): what the finding is attached to.
	AttachKind string `json:"attach_kind,omitempty"`
	AttachRef  string `json:"attach_ref,omitempty"`

	// Actor names who made the change for the audit event. It is never stored.
	Actor string `json:"-"`
}

// Patch is a partial update; a nil field is left alone. Project and kind
// cannot change after creation.
type Patch struct {
	Title          *string
	Body           *string
	State          *State
	LegacySource   *string
	ShippedBy      *string
	RaisedBy       *string
	ShippedVersion *string
	Links          *[]Link
	AddAliases     []string
	NeedsReview    *bool
	ReviewReason   *string
	// Goal and Order apply to plans only.
	Goal  *string
	Order *int
	// PlanKind and Release apply to plans only.
	PlanKind *PlanKind
	Release  *string
	// Actor names who made the change for the audit event.
	Actor string
}

// LinkInfo is the title and state last reported for a linked Automaton or
// Automation.
type LinkInfo struct {
	Title string `json:"title,omitempty"`
	State string `json:"state,omitempty"`
	// CompletedAt is when the linked Automaton finished (RFC 3339), when the
	// caller knows it. The plan's run order uses it to put finished work first.
	CompletedAt string `json:"completed_at,omitempty"`
}

var (
	projectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	rowRefRe  = regexp.MustCompile(`^(?i)row\s+#?(\d+)$`)
)

// ValidKind reports whether k is one of the record kinds.
func ValidKind(k Kind) bool { return k == KindPlan || k == KindRegistryRow || k == KindFinding }

// States returns the valid states for a kind, in display order.
func States(k Kind) []State {
	switch k {
	case KindRegistryRow, KindPlan:
		return []State{StateOpen, StatePlanned, StateInProgress, StateBlocked, StateShipped, StateDone, StateDeferred, StateCancelled}
	case KindFinding:
		return []State{StateOpen, StateTriaged, StateFixed, StateWontfix, StateDuplicate}
	}
	return nil
}

// ValidState reports whether s is a valid state for kind k.
func ValidState(k Kind, s State) bool {
	for _, v := range States(k) {
		if v == s {
			return true
		}
	}
	return false
}

// IsClosed reports whether the state ends the record's life (closed_at is set).
func IsClosed(k Kind, s State) bool {
	switch k {
	case KindRegistryRow, KindPlan:
		return s == StateShipped || s == StateDone || s == StateCancelled
	case KindFinding:
		return s == StateFixed || s == StateWontfix || s == StateDuplicate
	}
	return false
}

// NormTitle is the form a title is compared in: trimmed, inner whitespace
// collapsed, lower-cased.
func NormTitle(t string) string {
	return strings.ToLower(strings.Join(strings.Fields(t), " "))
}

// ParseRowRef parses "row 12" (also "Row #12") into the legacy number.
func ParseRowRef(ref string) (int, bool) {
	m := rowRefRe.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ErrInvalid is returned (wrapped) for a record or patch that fails validation.
type ErrInvalid struct{ Reason string }

func (e *ErrInvalid) Error() string { return "planrecords: invalid record: " + e.Reason }

func invalid(format string, a ...any) error {
	return &ErrInvalid{Reason: fmt.Sprintf(format, a...)}
}

func runesOver(s string, max int) bool { return utf8.RuneCountInString(s) > max }

func validateLinks(ls []Link) error {
	if len(ls) > MaxLinks {
		return invalid("too many links (max %d)", MaxLinks)
	}
	for _, l := range ls {
		if strings.TrimSpace(l.Kind) == "" || strings.TrimSpace(l.Ref) == "" {
			return invalid("a link needs a kind and a ref")
		}
		if runesOver(l.Kind, MaxLinkKindRunes) || runesOver(l.Ref, MaxLinkRefRunes) {
			return invalid("link kind or ref too long")
		}
	}
	return nil
}

// validate checks a whole record (after defaults were applied).
func (r *Record) validate() error {
	if !ValidKind(r.Kind) {
		return invalid("kind must be plan, registry_row or finding, got %q", r.Kind)
	}
	if r.Project == "" || runesOver(r.Project, MaxProjectRunes) || !projectRe.MatchString(r.Project) {
		return invalid("project must be a registered project alias (letters, digits, . _ -; max %d)", MaxProjectRunes)
	}
	if !ValidState(r.Kind, r.State) {
		return invalid("state %q is not valid for %s", r.State, r.Kind)
	}
	if NormTitle(r.Title) == "" {
		return invalid("title is required")
	}
	if runesOver(r.Title, MaxTitleRunes) {
		return invalid("title longer than %d characters", MaxTitleRunes)
	}
	if len(r.Body) > MaxBodyBytes {
		return invalid("body larger than %d bytes", MaxBodyBytes)
	}
	if r.ID != "" && (runesOver(r.ID, MaxIDRunes) || !idRe.MatchString(r.ID)) {
		return invalid("id must be lower-case letters, digits and hyphens (max %d)", MaxIDRunes)
	}
	if r.LegacyNumber < 0 {
		return invalid("legacy_number must be positive")
	}
	if r.LegacyNumber > 0 && r.Kind != KindRegistryRow {
		return invalid("legacy_number applies to registry rows only")
	}
	if runesOver(r.LegacySource, MaxSourceRunes) {
		return invalid("legacy_source longer than %d characters", MaxSourceRunes)
	}
	if runesOver(r.ShippedVersion, MaxVersionRunes) {
		return invalid("shipped_version longer than %d characters", MaxVersionRunes)
	}
	if runesOver(r.ShippedBy, MaxAutomatonRunes) || runesOver(r.RaisedBy, MaxAutomatonRunes) {
		return invalid("shipped_by / raised_by longer than %d characters", MaxAutomatonRunes)
	}
	if runesOver(r.ReviewReason, MaxReasonRunes) {
		return invalid("review_reason longer than %d characters", MaxReasonRunes)
	}
	if len(r.Aliases) > MaxAliases {
		return invalid("too many aliases (max %d)", MaxAliases)
	}
	for _, a := range r.Aliases {
		if NormTitle(a) == "" || runesOver(a, MaxTitleRunes) {
			return invalid("an alias must be a non-empty title of at most %d characters", MaxTitleRunes)
		}
	}
	if err := r.validatePlanFields(); err != nil {
		return err
	}
	if err := validateLinks(r.Links); err != nil {
		return err
	}
	return r.validateRowLinks()
}

func validateRefList(name string, ids []string) error {
	if len(ids) > MaxLinkedItems {
		return invalid("too many %s (max %d)", name, MaxLinkedItems)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || runesOver(id, MaxLinkRefRunes) {
			return invalid("an entry in %s must be a non-empty reference of at most %d characters", name, MaxLinkRefRunes)
		}
		if seen[id] {
			return invalid("%s lists %q twice", name, id)
		}
		seen[id] = true
	}
	return nil
}

// validatePlanFields checks the plan and finding fields added to the model.
// Every one is optional, so an older record without them stays valid.
func (r *Record) validatePlanFields() error {
	if r.Order < 0 || r.Order > MaxOrder {
		return invalid("order must be between 0 and %d", MaxOrder)
	}
	if runesOver(r.Goal, MaxGoalRunes) {
		return invalid("goal longer than %d characters", MaxGoalRunes)
	}
	planOnly := r.Goal != "" || r.Order != 0 || len(r.Automations) > 0 || len(r.Automata) > 0 ||
		len(r.Findings) > 0 || len(r.LinkInfo) > 0 || r.PlanKind != PlanKindOrdinary || r.Release != "" || len(r.Themes) > 0
	if planOnly && r.Kind != KindPlan {
		return invalid("goal, order, links down, plan kind, release and themes apply to plans only")
	}
	if err := r.validateRelease(); err != nil {
		return err
	}
	if err := r.validateArchive(); err != nil {
		return err
	}
	for name, ids := range map[string][]string{"automations": r.Automations, "automata": r.Automata, "findings": r.Findings} {
		if err := validateRefList(name, ids); err != nil {
			return err
		}
	}
	if r.AttachKind != "" || r.AttachRef != "" {
		if r.Kind != KindFinding {
			return invalid("attach_kind and attach_ref apply to findings only")
		}
		if (r.AttachKind != AttachPlan && r.AttachKind != AttachAutomaton) || strings.TrimSpace(r.AttachRef) == "" ||
			runesOver(r.AttachRef, MaxLinkRefRunes) {
			return invalid("a finding attaches to a plan or an Automaton, with a reference")
		}
	}
	for k, v := range r.LinkInfo {
		if runesOver(k, MaxLinkRefRunes+16) || runesOver(v.Title, MaxTitleRunes) || runesOver(v.State, MaxLinkKindRunes) {
			return invalid("link info too long")
		}
	}
	return nil
}

// validateArchive checks the archive mark. It applies to plans only; the
// time and the reason exist only while the plan is archived.
func (r *Record) validateArchive() error {
	if !r.Archived && (r.ArchivedAt != "" || r.ArchiveReason != "") {
		return invalid("archived_at and archive_reason apply to an archived plan only")
	}
	if r.Archived && r.Kind != KindPlan {
		return invalid("only a plan can be archived")
	}
	if runesOver(r.ArchiveReason, MaxArchiveReasonRunes) {
		return invalid("archive reason longer than %d characters", MaxArchiveReasonRunes)
	}
	return nil
}

// validateRelease checks the plan kind marker, the release label and the
// themes. A theme only groups items the plan already holds, and an item is in
// at most one theme.
func (r *Record) validateRelease() error {
	if !ValidPlanKind(r.PlanKind) {
		return invalid("plan_kind must be empty or %q, got %q", PlanKindRelease, r.PlanKind)
	}
	if r.Release != "" {
		if r.PlanKind != PlanKindRelease {
			return invalid("a release label applies to a release plan only")
		}
		if strings.TrimSpace(r.Release) == "" {
			return invalid("release must not be blank")
		}
		if runesOver(r.Release, MaxReleaseRunes) {
			return invalid("release longer than %d characters", MaxReleaseRunes)
		}
	}
	if len(r.Themes) == 0 {
		return nil
	}
	if r.PlanKind != PlanKindRelease {
		return invalid("themes apply to a release plan only; remove them before changing the plan kind")
	}
	if len(r.Themes) > MaxThemes {
		return invalid("too many themes (max %d)", MaxThemes)
	}
	ids, names := map[string]bool{}, map[string]bool{}
	placed := map[string]string{} // "automaton:<id>" -> theme id
	for _, t := range r.Themes {
		if t.ID == "" || runesOver(t.ID, MaxIDRunes) || !idRe.MatchString(t.ID) {
			return invalid("a theme id must be lower-case letters, digits and hyphens (max %d)", MaxIDRunes)
		}
		if ids[t.ID] {
			return invalid("theme id %q is used twice", t.ID)
		}
		ids[t.ID] = true
		if NormTitle(t.Name) == "" {
			return invalid("a theme needs a name")
		}
		if runesOver(t.Name, MaxThemeNameRunes) {
			return invalid("theme name longer than %d characters", MaxThemeNameRunes)
		}
		if names[NormTitle(t.Name)] {
			return invalid("theme name %q is used twice", t.Name)
		}
		names[NormTitle(t.Name)] = true
		if t.Order < 0 || t.Order > MaxOrder {
			return invalid("theme order must be between 0 and %d", MaxOrder)
		}
		for _, list := range []struct {
			kind     string
			in, held []string
		}{
			{linkAutomaton, t.Automata, r.Automata},
			{linkAutomation, t.Automations, r.Automations},
		} {
			if err := validateRefList("theme "+list.kind+" items", list.in); err != nil {
				return err
			}
			for _, id := range list.in {
				if indexOf(list.held, id) < 0 {
					return invalid("theme %q holds %s %q, which the plan does not hold", t.ID, list.kind, id)
				}
				k := linkKey(list.kind, id)
				if other, dup := placed[k]; dup {
					return invalid("%s %q is in themes %q and %q; an item is in one theme only", list.kind, id, other, t.ID)
				}
				placed[k] = t.ID
			}
		}
	}
	return nil
}

// clone returns a deep copy so callers cannot alter the index.
func (r Record) clone() Record {
	if r.Links != nil {
		r.Links = append([]Link(nil), r.Links...)
	}
	if r.Aliases != nil {
		r.Aliases = append([]string(nil), r.Aliases...)
	}
	if r.Automations != nil {
		r.Automations = append([]string(nil), r.Automations...)
	}
	if r.Automata != nil {
		r.Automata = append([]string(nil), r.Automata...)
	}
	if r.Findings != nil {
		r.Findings = append([]string(nil), r.Findings...)
	}
	if r.Themes != nil {
		ts := make([]Theme, len(r.Themes))
		for i, t := range r.Themes {
			ts[i] = t.clone()
		}
		r.Themes = ts
	}
	if r.LinkInfo != nil {
		m := make(map[string]LinkInfo, len(r.LinkInfo))
		for k, v := range r.LinkInfo {
			m[k] = v
		}
		r.LinkInfo = m
	}
	return r
}

// slugify makes the readable part of an id from a title.
func slugify(title string, max int) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(title) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= max {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
