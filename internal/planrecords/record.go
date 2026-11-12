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

// The two record kinds.
const (
	KindRegistryRow Kind = "registry_row"
	KindFinding     Kind = "finding"
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
)

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
	// Actor names who made the change for the audit event.
	Actor string
}

var (
	projectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	rowRefRe  = regexp.MustCompile(`^(?i)row\s+#?(\d+)$`)
)

// ValidKind reports whether k is one of the two record kinds.
func ValidKind(k Kind) bool { return k == KindRegistryRow || k == KindFinding }

// States returns the valid states for a kind, in display order.
func States(k Kind) []State {
	switch k {
	case KindRegistryRow:
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
	case KindRegistryRow:
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
		return invalid("kind must be registry_row or finding, got %q", r.Kind)
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
	return validateLinks(r.Links)
}

// clone returns a deep copy so callers cannot alter the index.
func (r Record) clone() Record {
	if r.Links != nil {
		r.Links = append([]Link(nil), r.Links...)
	}
	if r.Aliases != nil {
		r.Aliases = append([]string(nil), r.Aliases...)
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
