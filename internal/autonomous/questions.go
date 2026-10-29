// Question and answer types with their validation and state rules, no
// persistence yet. Design: docs/plans/2026-10-25-operator-decision-channel-
// proposal.md (sections 3 and 5, step 1) and the decided operator answers in
// docs/plans/2026-10-14-automation-orchestration-design.md ("Operator
// decision channel", items 1 to 32, where the decided answers win over the
// proposal). In particular: there is ONE generic question record and the type
// is a label, not a different structure; and questions never expire, so this
// record has no deadline or expiry field.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// QuestionState is the lifecycle state of a question.
type QuestionState string

// Question states. Open and answered are the only states that accept
// answers; answered, withdrawn and expired are terminal.
const (
	QuestionOpen      QuestionState = "open"
	QuestionAnswered  QuestionState = "answered"
	QuestionWithdrawn QuestionState = "withdrawn"
	QuestionExpired   QuestionState = "expired"
)

// AskerKind names who raised a question.
type AskerKind string

const (
	QuestionAskerSession    AskerKind = "session"
	QuestionAskerSupervisor AskerKind = "supervisor"
	QuestionAskerOperator   AskerKind = "operator"
	QuestionAskerSystem     AskerKind = "system"
)

// Question context kinds. The context is one of these and the matching link
// must be present on the record.
const (
	QuestionContextNone        = "none"
	QuestionContextPrd         = "prd"
	QuestionContextTask        = "task"
	QuestionContextProposal    = "proposal"
	QuestionContextRegistryRow = "registry_row"
)

// Question risk levels. Empty means normal; low is the only other value.
const (
	QuestionRiskLow    = "low"
	QuestionRiskNormal = "normal"
)

// Question priority levels. Empty means normal; high is the only other
// value. Priority is a separate field from Risk: the risk says how
// dangerous a wrong answer is, the priority says how urgently an operator
// should look at the question.
const (
	QuestionPriorityNormal = "normal"
	QuestionPriorityHigh   = "high"
)

// DefaultQuestionLabels is the shipped set of question labels. The label is
// a label, not a different structure, and users may add their own, so the
// list is a default, not an allow-list.
var DefaultQuestionLabels = []string{
	"decision",
	"design review",
	"approval",
	"information",
}

// Size caps for question and answer fields.
const (
	questionTextMaxBytes        = 8192
	questionLabelMaxChars       = 64
	questionOptionsMax          = 20
	questionOptionLabelMaxBytes = 200
	questionTradeoffsMaxBytes   = 1000
	questionRationaleMaxBytes   = 1000
	answerQuoteMaxBytes         = 16384
)

// QuestionOption is one choice on a question.
type QuestionOption struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Tradeoffs string `json:"tradeoffs,omitempty"`
}

// QuestionRecommendation is the asker's suggested option and why.
type QuestionRecommendation struct {
	OptionID  string `json:"option_id"`
	Rationale string `json:"rationale,omitempty"`
}

// Question is the single generic question record; the type of question is
// the Label, not a different structure.
type Question struct {
	ID             string                  `json:"id"`
	Label          string                  `json:"label,omitempty"`
	AskedBy        string                  `json:"asked_by,omitempty"`
	AskerKind      AskerKind               `json:"asker_kind,omitempty"`
	SessionID      string                  `json:"session_id,omitempty"`
	Context        string                  `json:"context,omitempty"`
	PRDID          string                  `json:"prd_id,omitempty"`
	TaskID         string                  `json:"task_id,omitempty"`
	ProposalPath   string                  `json:"proposal_path,omitempty"`
	RegistryRowKey string                  `json:"registry_row_key,omitempty"`
	Text           string                  `json:"text"`
	Options        []QuestionOption        `json:"options"`
	Recommendation *QuestionRecommendation `json:"recommendation,omitempty"`
	Default        string                  `json:"default"`
	Risk           string                  `json:"risk,omitempty"`
	// Priority is empty, normal or high (validated); it is separate from
	// Risk. ReturnPath opts this one question in to the per-question
	// session line: the default return path (event plus polled GET) is
	// always there, and ReturnPath true may add the JSON line into the
	// asking session, so it requires the SessionID that line goes to.
	CreatedAt  *time.Time    `json:"created_at,omitempty"`
	State      QuestionState `json:"state,omitempty"`
	Priority   string        `json:"priority,omitempty"`
	ReturnPath bool          `json:"return_path"`
}

// Answer is an immutable reply to a question, stored with the operator's
// own words verbatim. A changed mind is a new answer that supersedes the old
// one, never an edit of this record.
type Answer struct {
	ID              string     `json:"id"`
	QuestionID      string     `json:"question_id"`
	Actor           string     `json:"actor,omitempty"`
	Surface         string     `json:"surface,omitempty"`
	ChosenOptionID  string     `json:"chosen_option_id,omitempty"`
	Quote           string     `json:"quote,omitempty"`
	QuoteSHA256     string     `json:"quote_sha256,omitempty"`
	Supersedes      string     `json:"supersedes,omitempty"`
	SourceMessageID string     `json:"source_message_id,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

// Typed errors for question validation and the answer state rules, usable
// with errors.Is.
var (
	// ErrQuestionNotOpen is returned when an answer arrives for a
	// question that is withdrawn, expired or in any state that no longer
	// accepts answers.
	ErrQuestionNotOpen = errors.New("question is not open")

	// ErrAnswerNeedsSupersedes is returned when an answer to an answered
	// question does not name the answer it supersedes.
	ErrAnswerNeedsSupersedes = errors.New("answer must name the answer it supersedes")

	// ErrSupersedesWrongTarget is returned when an answer names the wrong
	// answer to supersede.
	ErrSupersedesWrongTarget = errors.New("answer supersedes the wrong target")

	// ErrAnswerEmpty is returned when an answer carries neither an option
	// nor any non-blank text.
	ErrAnswerEmpty = errors.New("answer must name an option or carry non-blank text")

	// ErrUnknownOption is returned when an answer names an option id the
	// question does not have.
	ErrUnknownOption = errors.New("answer names an unknown option")

	// ErrSecretInAnswer is returned when an answer's text matches a secret
	// pattern and must not be stored.
	ErrSecretInAnswer = errors.New("answer looks like a secret")

	// ErrInvalidQuestion wraps the joined validation messages from
	// ValidateNewQuestion.
	ErrInvalidQuestion = errors.New("invalid question")
)

// ValidateNewQuestion checks a question that is being raised. It normalises
// the label and risk defaults into q, and enforces the record rules: the
// text is required; there are at least two options with unique non-empty ids
// and labels; the default is required and names either an option of this
// question or the word hold, so no choice is ever silent; a recommendation,
// when given, names an option that exists; the context is one of the known
// kinds and carries the matching link (and none carries none of them); a
// proposal path, when present, is repository-relative; the priority is empty
// or one of its two values (it is a separate field from the risk); a
// question that opts into the session-line return path names the session it
// goes to; and every field fits its cap. The state is never taken from the
// input; the store sets it, so q.State is ignored here.
func ValidateNewQuestion(q *Question) error {
	if q == nil {
		return fmt.Errorf("%w: question is nil", ErrInvalidQuestion)
	}
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// Label: empty defaults to the first shipped label; any other label
	// is accepted when it is at most 64 characters with no control
	// characters, because users may add labels.
	label := q.Label
	if label == "" {
		label = DefaultQuestionLabels[0]
	} else if n := utf8len(label); n > questionLabelMaxChars {
		add("label is %d characters; the cap is %d", n, questionLabelMaxChars)
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			add("label contains control characters")
			break
		}
	}

	// Risk: empty means normal.
	risk := q.Risk
	if risk == "" {
		risk = QuestionRiskNormal
	}
	if risk != QuestionRiskLow && risk != QuestionRiskNormal {
		add("risk is %q; it must be %q or %q", risk, QuestionRiskLow, QuestionRiskNormal)
	}

	// Priority: empty (normal), normal or high; anything else is refused.
	// It is a separate field from the risk and is never derived from it.
	if q.Priority != "" && q.Priority != QuestionPriorityNormal && q.Priority != QuestionPriorityHigh {
		add("priority is %q; it must be empty, %q or %q", q.Priority, QuestionPriorityNormal, QuestionPriorityHigh)
	}

	// Return path: the event plus polled GET is the default return path and
	// is always available; the session line is the per-question opt-in that
	// ReturnPath true switches on, and that line has nowhere to go without
	// the asking session.
	if q.ReturnPath && q.SessionID == "" {
		add("return_path requires session_id: a question that opts into the session line must name the asking session")
	}

	// Text is required and capped.
	if strings.TrimSpace(q.Text) == "" {
		add("text is required")
	}
	if len(q.Text) > questionTextMaxBytes {
		add("text is %d bytes; the cap is %d", len(q.Text), questionTextMaxBytes)
	}

	// Options: at least two, at most the cap, with unique non-empty ids
	// and labels.
	if len(q.Options) < 2 {
		add("at least two options are required; got %d", len(q.Options))
	}
	if len(q.Options) > questionOptionsMax {
		add("%d options exceed the cap of %d", len(q.Options), questionOptionsMax)
	}
	seenIDs := make(map[string]bool, len(q.Options))
	seenLabels := make(map[string]bool, len(q.Options))
	for i, opt := range q.Options {
		if opt.ID == "" {
			add("option %d has an empty id", i+1)
		} else if seenIDs[opt.ID] {
			add("option id %q is repeated", opt.ID)
		} else {
			seenIDs[opt.ID] = true
		}
		if opt.Label == "" {
			add("option %d has an empty label", i+1)
		} else if seenLabels[opt.Label] {
			add("option label %q is repeated", opt.Label)
		} else {
			seenLabels[opt.Label] = true
		}
		if len(opt.Label) > questionOptionLabelMaxBytes {
			add("option label is %d bytes; the cap is %d", len(opt.Label), questionOptionLabelMaxBytes)
		}
		if len(opt.Tradeoffs) > questionTradeoffsMaxBytes {
			add("option tradeoffs are %d bytes; the cap is %d", len(opt.Tradeoffs), questionTradeoffsMaxBytes)
		}
	}

	// Default is required: either an option id of this question or the
	// word hold. It may never be empty, so no choice is ever silent.
	if q.Default == "" {
		add("default is required; name an option or the word hold")
	} else if q.Default != "hold" && !seenIDs[q.Default] {
		add("default %q is neither an option of this question nor hold", q.Default)
	}

	// A recommendation, when given, names an option that exists.
	if q.Recommendation != nil {
		if !seenIDs[q.Recommendation.OptionID] {
			add("recommendation names option %q, which is not among the options", q.Recommendation.OptionID)
		}
		if len(q.Recommendation.Rationale) > questionRationaleMaxBytes {
			add("recommendation rationale is %d bytes; the cap is %d", len(q.Recommendation.Rationale), questionRationaleMaxBytes)
		}
	}

	// Context: one of the kinds, with the matching link.
	switch q.Context {
	case QuestionContextPrd:
		if q.PRDID == "" {
			add("context prd needs prd_id")
		}
	case QuestionContextTask:
		if q.TaskID == "" {
			add("context task needs task_id")
		}
	case QuestionContextProposal:
		if q.ProposalPath == "" {
			add("context proposal needs proposal_path")
		}
	case QuestionContextRegistryRow:
		if q.RegistryRowKey == "" {
			add("context registry_row needs registry_row_key")
		}
	case QuestionContextNone:
		if q.PRDID != "" || q.TaskID != "" || q.ProposalPath != "" || q.RegistryRowKey != "" {
			add("context none must not carry links (prd_id, task_id, proposal_path or registry_row_key)")
		}
	default:
		add("context is %q; it must be one of %s", q.Context,
			strings.Join([]string{
				QuestionContextPrd,
				QuestionContextTask,
				QuestionContextProposal,
				QuestionContextRegistryRow,
				QuestionContextNone,
			}, ", "))
	}

	// A proposal path is repository-relative wherever it appears: no
	// leading '/' and no '..' segment.
	if err := checkProposalPath(q.ProposalPath); err != nil {
		add("%v", err)
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidQuestion, strings.Join(problems, "; "))
	}
	q.Label = label
	q.Risk = risk
	return nil
}

// checkProposalPath rejects proposal paths that are not repository-relative:
// a leading slash, or any path segment that is '..'.
func checkProposalPath(p string) error {
	if p == "" {
		return nil
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("proposal path %q must be repository-relative (no leading /)", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("proposal path %q must not contain a '..' segment", p)
		}
	}
	return nil
}

// CheckAnswerAllowed applies the answer state rules. An answer must name an
// option that exists on the question, or carry non-blank text, or both (a
// quote that is only whitespace counts as empty, though the stored quote is
// never trimmed or changed); a question that is withdrawn or expired accepts
// no answer; an open question accepts an answer with no Supersedes and
// rejects one that carries it; and an answered question accepts only an
// answer whose Supersedes names its current answer, because a changed mind
// is a new answer that supersedes the old one, never an edit. The quote is
// capped at 16384 bytes.
func CheckAnswerAllowed(state QuestionState, currentAnswerID string, a Answer, options []QuestionOption) error {
	switch state {
	case QuestionWithdrawn, QuestionExpired:
		return fmt.Errorf("%w: the question is %s and accepts no answer", ErrQuestionNotOpen, state)
	}

	if a.ChosenOptionID != "" {
		known := false
		for _, opt := range options {
			if opt.ID == a.ChosenOptionID {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("%w: option %q is not among the question's options", ErrUnknownOption, a.ChosenOptionID)
		}
	}

	hasText := strings.TrimSpace(a.Quote) != ""
	if a.ChosenOptionID == "" && !hasText {
		return fmt.Errorf("%w: the answer carries neither an option nor non-blank text", ErrAnswerEmpty)
	}

	if len(a.Quote) > answerQuoteMaxBytes {
		return fmt.Errorf("answer quote is %d bytes; the cap is %d", len(a.Quote), answerQuoteMaxBytes)
	}

	switch state {
	case QuestionOpen:
		if a.Supersedes != "" {
			return fmt.Errorf("%w: the open question has no current answer, yet the answer names %q as supersedes",
				ErrSupersedesWrongTarget, a.Supersedes)
		}
	case QuestionAnswered:
		if a.Supersedes == "" {
			return fmt.Errorf("%w: the question is answered by %q, so the answer must name that id as supersedes",
				ErrAnswerNeedsSupersedes, currentAnswerID)
		}
		if a.Supersedes != currentAnswerID {
			return fmt.Errorf("%w: supersedes names %q, but the current answer is %q",
				ErrSupersedesWrongTarget, a.Supersedes, currentAnswerID)
		}
	default:
		return fmt.Errorf("%w: the question state is %q", ErrQuestionNotOpen, state)
	}
	return nil
}

// QuoteSHA256 is the lowercase hex SHA-256 of the exact bytes of the quote,
// so later copies of a stored quote can be checked against the record.
func QuoteSHA256(quote string) string {
	sum := sha256.Sum256([]byte(quote))
	return hex.EncodeToString(sum[:])
}

// answerSecretRefRe matches well-formed ${secret:name} references (the same
// shape as internal/secrets' ref tokens). These are references to stored
// secrets, not secrets themselves, so they are removed before the scan.
var answerSecretRefRe = regexp.MustCompile(`\$\{secret:[a-zA-Z0-9_.\-]+\}`) // #nosec G101 -- regex for ref syntax, not a credential

// answerSecretRules are the secret patterns checked in an answer, in order.
// The scan returns the rule name, never any matched text, so the refusal can
// be audited without the secret.
var answerSecretRules = []struct {
	name string
	re   *regexp.Regexp
}{ // #nosec G101 -- secret detection patterns, not a credential
	{"private key", regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY`)},
	{"aws key id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}`)},
	{"aws secret key", regexp.MustCompile(`(?i)aws_secret_access_key\s*[=:]\s*["']?[A-Za-z0-9+/]{40}`)},
	{"github token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`)},
	{"api key", regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`)},
	{"google api key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{"bearer token", regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9_\-./=]{20,}`)},
	{"secret assignment", regexp.MustCompile(`(?i)\b(?:password|passwd|secret|token|api_key|apikey|api_secret|secret_key)\s*[=:]\s*["']?[A-Za-z0-9+/=_.\-]{8,}`)},
}

// ScanAnswerForSecret reports whether text looks like it carries a secret,
// and if so returns the name of the first rule that matched. Well-formed
// ${secret:name} references are removed before the scan because they are
// references, not secrets. It returns the rule name only, never any of the
// matched text, so the refusal can be audited without the secret.
func ScanAnswerForSecret(text string) (string, bool) {
	cleaned := answerSecretRefRe.ReplaceAllString(text, "")
	for _, rule := range answerSecretRules {
		if rule.re.MatchString(cleaned) {
			return rule.name, true
		}
	}
	return "", false
}

// SecretRefusalMessage is the operator-facing text for an answer that was not
// stored because it looks like a secret. It names the safe routes: store the
// secret in the datawatch secrets store and refer to it by name, or save it
// on the filesystem and point the session to the file. It never contains
// the matched text.
func SecretRefusalMessage(pattern string) string {
	return "Your answer was not stored because it looks like a secret (matched: " +
		pattern + "). Resend it without the secret. Two safe routes: store the " +
		"secret in the datawatch secrets store and refer to it by name as " +
		"${secret:name}, or save it on the filesystem and point the session to " +
		"the file."
}

// utf8len counts the characters (Unicode code points) in s.
func utf8len(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
