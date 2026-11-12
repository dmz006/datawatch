// The decision record: a structured, stored description of one decision the
// operator is asked to make. The daemon builds the queue question from the
// record, never from caller prose, so a decision always reads the same way:
// a plain-language title, the situation, what exactly is being decided, the
// options with their cost and risk, a recommendation and a forced default.
// This file holds the record type, its validation and its content hash; the
// append-only store is in decision_record_store.go.
package autonomous

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

// DecisionRecordState is where a decision record is in its life.
type DecisionRecordState string

// The states of a decision record.
const (
	DecisionRecordOpen      DecisionRecordState = "open"
	DecisionRecordAnswered  DecisionRecordState = "answered"
	DecisionRecordWithdrawn DecisionRecordState = "withdrawn"
)

// The sources a decision record can come from.
const (
	DecisionSourceREST = "rest"
	DecisionSourceMCP  = "mcp"
	DecisionSourceCLI  = "cli"
	DecisionSourceComm = "comm"
	DecisionSourceFile = "file"
)

// The outcomes recorded for a document change after an answer.
const (
	DecisionDocReported  = "reported"
	DecisionDocApplied   = "applied"
	DecisionDocUnchanged = "unchanged"
	DecisionDocNoMatch   = "no_match"
	DecisionDocAmbiguous = "ambiguous"
	DecisionDocError     = "error"
)

// DecisionDefaultLabel is the label a record gets when none is given, and
// DecisionRecordHold is the word that means "no option, hold".
const (
	DecisionDefaultLabel = "decision"
	DecisionRecordHold   = "hold"
)

// Size caps. Where the question rules have a cap that fits, it is reused so a
// record that validates always produces a question that validates.
const (
	decisionIDHexLen          = 20
	decisionIDPrefix          = "dr-"
	decisionTitleMaxBytes     = questionOptionLabelMaxBytes
	decisionOptionIDMaxBytes  = 64
	decisionOptionsMin        = 2
	decisionLinkMaxBytes      = 512
	decisionSourceRefMaxBytes = 512
	decisionDocFieldMaxBytes  = answerQuoteMaxBytes
	decisionShortFieldMax     = 512
)

// Sentinel errors of the decision record.
var (
	// ErrInvalidDecisionRecord wraps the joined validation messages from
	// ValidateNewDecisionRecord; the message names every problem found.
	ErrInvalidDecisionRecord = errors.New("invalid decision record")
	// ErrDecisionRecordNotFound is returned when an operation names a record
	// id the store does not hold.
	ErrDecisionRecordNotFound = errors.New("decision record not found")
	// ErrDecisionRecordClosed is returned when an operation needs an open
	// record and the record is answered or withdrawn.
	ErrDecisionRecordClosed = errors.New("decision record is closed")
	// ErrDecisionRecordDuplicate is returned when a record with the same
	// non-empty source and source_ref already exists.
	ErrDecisionRecordDuplicate = errors.New("a decision record with this source and source_ref already exists")
	// ErrDecisionQuestionIDSet is returned when a record already has a
	// different question id, or the question id belongs to another record.
	ErrDecisionQuestionIDSet = errors.New("decision record question id is already set")
)

// DecisionOption is one choice: a short label and one line of cost and risk.
type DecisionOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Tradeoff string `json:"tradeoff,omitempty"`
}

// DecisionRecommendation names the option the author recommends and why.
type DecisionRecommendation struct {
	OptionID string `json:"option_id"`
	Reason   string `json:"reason"`
}

// DecisionLinks ties a record to the work it is about. All parts are
// optional: doc is a repository-relative path, automaton an Automaton id and
// registry_row a registry row key.
type DecisionLinks struct {
	Doc         string `json:"doc,omitempty"`
	Automaton   string `json:"automaton,omitempty"`
	RegistryRow string `json:"registry_row,omitempty"`
}

// DecisionAnswer is the answer filled in when the record is answered.
type DecisionAnswer struct {
	AnswerID       string     `json:"answer_id"`
	ChosenOptionID string     `json:"chosen_option_id,omitempty"`
	Quote          string     `json:"quote,omitempty"`
	Actor          string     `json:"actor,omitempty"`
	Surface        string     `json:"surface,omitempty"`
	AnsweredAt     *time.Time `json:"answered_at,omitempty"`
}

// DecisionDocChange reports what was done (or would be done) to the
// document a record links to after the answer. Status is one of the
// DecisionDoc constants.
type DecisionDocChange struct {
	Mode   string     `json:"mode,omitempty"`
	Path   string     `json:"path,omitempty"`
	Line   int        `json:"line,omitempty"`
	Before string     `json:"before,omitempty"`
	After  string     `json:"after,omitempty"`
	Diff   string     `json:"diff,omitempty"`
	Status string     `json:"status"`
	At     *time.Time `json:"at,omitempty"`
}

// DecisionFollowUp reports a configured follow-up action after the answer.
// Requested is true once the operator asked for one and Name is the entry of
// autonomous.decisions.follow_ups they named; Kind and Target are filled from
// that entry when it runs, together with Status (done, skipped or error) and
// a one-line Result.
type DecisionFollowUp struct {
	Requested bool       `json:"requested"`
	Name      string     `json:"name,omitempty"`
	Kind      string     `json:"kind,omitempty"`
	Target    string     `json:"target,omitempty"`
	Status    string     `json:"status,omitempty"`
	Result    string     `json:"result,omitempty"`
	At        *time.Time `json:"at,omitempty"`
}

// DecisionRecord is the structured decision. The content fields (label,
// title, situation, deciding, options, recommendation, default and links) are
// what the content hash covers; the rest is bookkeeping the store owns.
type DecisionRecord struct {
	ID             string                  `json:"id"`
	Label          string                  `json:"label,omitempty"`
	Title          string                  `json:"title"`
	Situation      string                  `json:"situation"`
	Deciding       string                  `json:"deciding"`
	Options        []DecisionOption        `json:"options"`
	Recommendation *DecisionRecommendation `json:"recommendation,omitempty"`
	Default        string                  `json:"default"`
	Links          DecisionLinks           `json:"links"`
	State          DecisionRecordState     `json:"state"`
	QuestionID     string                  `json:"question_id,omitempty"`
	// PRDID is the Automaton this decision is about ('pr-' id; the same id the
	// record's question carries when it asks about an Automaton). Empty = the
	// decision targets no Automaton (a registry row or document), and object-
	// tag derivation then names no axes and derives no tags.
	PRDID  string `json:"prd_id,omitempty"`
	Source string `json:"source,omitempty"`
	// SystemTags is the placement-derived tag set this decision record
	// carries: the fact of the Automaton it names (its project, its
	// Automation and itself), derived by ObjectTagsForDecisionRecord
	// under the daemon's tags.placement block. Empty = no Automaton named,
	// the Automaton gone, not yet derived, or the tags disabled.
	SystemTags    []string           `json:"system_tags,omitempty"`
	SourceRef     string             `json:"source_ref,omitempty"`
	ContentSHA256 string             `json:"content_hash"`
	Answer        *DecisionAnswer    `json:"answer,omitempty"`
	DocChange     *DecisionDocChange `json:"doc_change,omitempty"`
	FollowUp      *DecisionFollowUp  `json:"follow_up,omitempty"`
	CreatedAt     *time.Time         `json:"created_at,omitempty"`
	UpdatedAt     *time.Time         `json:"updated_at,omitempty"`
}

// decisionIDReader is where record ids draw their randomness; a test swaps it
// to make ids predictable or to make the draw fail.
var decisionIDReader io.Reader = rand.Reader

// NewDecisionRecordID returns a fresh record id: 'dr-' followed by 20
// lowercase hex characters from crypto/rand.
func NewDecisionRecordID() string {
	id, err := newDecisionRecordIDFrom(decisionIDReader)
	if err != nil {
		// The system random source failing is not recoverable by the caller;
		// fall back to a hash of the clock so an id is still unique enough
		// for the store's collision check to work with.
		sum := sha256.Sum256([]byte(time.Now().Format(time.RFC3339Nano)))
		return decisionIDPrefix + hex.EncodeToString(sum[:])[:decisionIDHexLen]
	}
	return id
}

// newDecisionRecordIDFrom builds an id from the bytes of r.
func newDecisionRecordIDFrom(r io.Reader) (string, error) {
	b := make([]byte, decisionIDHexLen/2)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("draw decision record id: %w", err)
	}
	return decisionIDPrefix + hex.EncodeToString(b), nil
}

// decisionHashInput is the exact shape that is hashed. Struct field order is
// fixed, so the same content always gives the same bytes.
type decisionHashInput struct {
	Label          string                  `json:"label"`
	Title          string                  `json:"title"`
	Situation      string                  `json:"situation"`
	Deciding       string                  `json:"deciding"`
	Options        []DecisionOption        `json:"options"`
	Recommendation *DecisionRecommendation `json:"recommendation"`
	Default        string                  `json:"default"`
	Links          DecisionLinks           `json:"links"`
}

// ContentHash returns the sha256 (lowercase hex) of the content fields in a
// fixed order: label, title, situation, deciding, options, recommendation,
// default and links. An empty label hashes as the default label, and an empty
// options list hashes the same as a missing one.
func (r DecisionRecord) ContentHash() string {
	in := decisionHashInput{
		Label:          r.Label,
		Title:          r.Title,
		Situation:      r.Situation,
		Deciding:       r.Deciding,
		Options:        r.Options,
		Recommendation: r.Recommendation,
		Default:        r.Default,
		Links:          r.Links,
	}
	if in.Label == "" {
		in.Label = DecisionDefaultLabel
	}
	if in.Options == nil {
		in.Options = []DecisionOption{}
	}
	// Marshalling these plain structs cannot fail.
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// clone returns a deep copy, so a caller never shares slices or pointers
// with the store's view.
func (r DecisionRecord) clone() DecisionRecord {
	out := r
	if r.Options != nil {
		out.Options = append([]DecisionOption(nil), r.Options...)
	}
	if r.Recommendation != nil {
		rec := *r.Recommendation
		out.Recommendation = &rec
	}
	if r.Answer != nil {
		a := *r.Answer
		a.AnsweredAt = cloneTime(r.Answer.AnsweredAt)
		out.Answer = &a
	}
	if r.DocChange != nil {
		d := *r.DocChange
		d.At = cloneTime(r.DocChange.At)
		out.DocChange = &d
	}
	if r.FollowUp != nil {
		f := *r.FollowUp
		f.At = cloneTime(r.FollowUp.At)
		out.FollowUp = &f
	}
	out.CreatedAt = cloneTime(r.CreatedAt)
	out.UpdatedAt = cloneTime(r.UpdatedAt)
	return out
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// ValidateNewDecisionRecord checks the content of a record that is being
// created or updated. It reports every problem found in one error that wraps
// ErrInvalidDecisionRecord, and on success it fills in the default label. The
// rules: title, situation and deciding are required; there are 2 to 20
// options with unique non-empty ids and non-empty labels; a recommendation
// names an option and gives a reason; the default is an option id or the word
// hold; every field fits its cap, and the text that becomes the queue
// question fits the question text cap; a document link is a relative path
// with no '..' segment and no leading slash.
func ValidateNewDecisionRecord(r *DecisionRecord) error {
	if r == nil {
		return fmt.Errorf("%w: record is nil", ErrInvalidDecisionRecord)
	}
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	label := r.Label
	if label == "" {
		label = DecisionDefaultLabel
	} else if n := utf8len(label); n > questionLabelMaxChars {
		add("label is %d characters; the cap is %d", n, questionLabelMaxChars)
	}
	if hasControl(label) {
		add("label contains control characters")
	}

	if strings.TrimSpace(r.Title) == "" {
		add("title is required")
	} else if len(r.Title) > decisionTitleMaxBytes {
		add("title is %d bytes; the cap is %d", len(r.Title), decisionTitleMaxBytes)
	}
	if strings.TrimSpace(r.Situation) == "" {
		add("situation is required")
	}
	if strings.TrimSpace(r.Deciding) == "" {
		add("deciding is required")
	}
	if n := len(DecisionQuestionText(*r)); n > questionTextMaxBytes {
		add("title, situation and deciding are %d bytes together; the cap is %d", n, questionTextMaxBytes)
	}

	if len(r.Options) < decisionOptionsMin {
		add("at least %d options are required; got %d", decisionOptionsMin, len(r.Options))
	}
	if len(r.Options) > questionOptionsMax {
		add("%d options exceed the cap of %d", len(r.Options), questionOptionsMax)
	}
	seen := make(map[string]bool, len(r.Options))
	for i, opt := range r.Options {
		switch {
		case strings.TrimSpace(opt.ID) == "":
			add("option %d has an empty id", i+1)
		case seen[opt.ID]:
			add("option id %q is repeated", opt.ID)
		default:
			seen[opt.ID] = true
		}
		if len(opt.ID) > decisionOptionIDMaxBytes {
			add("option %d id is %d bytes; the cap is %d", i+1, len(opt.ID), decisionOptionIDMaxBytes)
		}
		if strings.TrimSpace(opt.Label) == "" {
			add("option %d has an empty label", i+1)
		} else if len(opt.Label) > questionOptionLabelMaxBytes {
			add("option %d label is %d bytes; the cap is %d", i+1, len(opt.Label), questionOptionLabelMaxBytes)
		}
		if len(opt.Tradeoff) > questionTradeoffsMaxBytes {
			add("option %d tradeoff is %d bytes; the cap is %d", i+1, len(opt.Tradeoff), questionTradeoffsMaxBytes)
		}
	}

	if r.Recommendation != nil {
		if !seen[r.Recommendation.OptionID] {
			add("recommendation names option %q, which is not among the options", r.Recommendation.OptionID)
		}
		if strings.TrimSpace(r.Recommendation.Reason) == "" {
			add("recommendation needs a reason")
		} else if len(r.Recommendation.Reason) > questionRationaleMaxBytes {
			add("recommendation reason is %d bytes; the cap is %d", len(r.Recommendation.Reason), questionRationaleMaxBytes)
		}
	}

	switch {
	case r.Default == "":
		add("default is required; name an option or the word %s", DecisionRecordHold)
	case r.Default != DecisionRecordHold && !seen[r.Default]:
		add("default %q is neither an option nor %s", r.Default, DecisionRecordHold)
	}

	if r.Links.Doc != "" {
		if err := checkDecisionDocPath(r.Links.Doc); err != nil {
			add("link doc: %v", err)
		}
	}
	if len(r.Links.Doc) > decisionLinkMaxBytes {
		add("link doc is %d bytes; the cap is %d", len(r.Links.Doc), decisionLinkMaxBytes)
	}
	if len(r.Links.Automaton) > decisionLinkMaxBytes {
		add("link automaton is %d bytes; the cap is %d", len(r.Links.Automaton), decisionLinkMaxBytes)
	}
	if len(r.Links.RegistryRow) > decisionLinkMaxBytes {
		add("link registry_row is %d bytes; the cap is %d", len(r.Links.RegistryRow), decisionLinkMaxBytes)
	}

	switch r.Source {
	case "", DecisionSourceREST, DecisionSourceMCP, DecisionSourceCLI, DecisionSourceComm, DecisionSourceFile:
	default:
		add("source is %q; it must be empty, %s, %s, %s, %s or %s", r.Source,
			DecisionSourceREST, DecisionSourceMCP, DecisionSourceCLI, DecisionSourceComm, DecisionSourceFile)
	}
	if r.Source == DecisionSourceFile && r.SourceRef == "" {
		add("source %s requires source_ref (the file name)", DecisionSourceFile)
	}
	if r.SourceRef != "" && r.Source == "" {
		add("source_ref requires source")
	}
	if len(r.SourceRef) > decisionSourceRefMaxBytes {
		add("source_ref is %d bytes; the cap is %d", len(r.SourceRef), decisionSourceRefMaxBytes)
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidDecisionRecord, strings.Join(problems, "; "))
	}
	r.Label = label
	return nil
}

// checkDecisionDocPath requires a relative path: not absolute, no '..'
// segment, no control characters. Both slash styles are checked so a
// backslash path cannot slip a '..' past the check.
func checkDecisionDocPath(p string) error {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return fmt.Errorf("path %q must be relative (no leading slash)", p)
	}
	if hasControl(p) {
		return fmt.Errorf("path %q contains control characters", p)
	}
	for _, seg := range strings.FieldsFunc(p, func(c rune) bool { return c == '/' || c == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("path %q must not contain a '..' segment", p)
		}
	}
	return nil
}

func hasControl(s string) bool {
	for _, c := range s {
		if unicode.IsControl(c) {
			return true
		}
	}
	return false
}

// DecisionQuestionText is the queue question text built from the record:
// the title, a blank line, the situation, a blank line, then 'Deciding: '
// and what is being decided.
func DecisionQuestionText(r DecisionRecord) string {
	return r.Title + "\n\n" + r.Situation + "\n\nDeciding: " + r.Deciding
}
