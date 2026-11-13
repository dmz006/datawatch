// planning_failure.go — the planning failure view, its one alert and its
// one question. When a planning attempt fails for good, the record on the
// Automaton (PRD.LastDecomposeFailure) names the rule and the exact phrases
// that tripped it; every surface reads PlanningFailureView. A non-retryable
// failure raises one alert, and when the spec lint can reword the spec the
// operator is asked, through the ordinary question store, whether to fix
// the wording and plan again.
//
// Pure bookkeeping plus text work: it never touches, kills or signals a
// session.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PlanningFailureQuestionAsker is the asker name on the planning question.
const PlanningFailureQuestionAsker = "planning"

// Option ids of the planning failure question.
const (
	PlanningFailureOptionFix   = "fix_wording"
	PlanningFailureOptionLeave = "leave_it"
)

// Option labels of the planning failure question.
const (
	PlanningFailureLabelFix   = "Fix the wording and plan again"
	PlanningFailureLabelLeave = "Leave it"
)

// Decision kinds written by this file.
const (
	planningFailureAlertKind    = "planning_failure_alert"
	planningFailureQuestionKind = "planning_failure_question"
	planningFailureAnswerKind   = "planning_failure_answer"
)

// planningFailurePhraseCap bounds the phrases kept on one record.
const planningFailurePhraseCap = 20

// PlanningFailureLocaleKeys names the locale keys the surfaces need to
// render the planning failure view, its alert and its question. The next
// task documents them; no user-facing string is added for them here
// because the alert and question text are English constants below.
var PlanningFailureLocaleKeys = []string{
	"planning_failure_title",
	"planning_failure_reason",
	"planning_failure_category",
	"planning_failure_source",
	"planning_failure_phrases",
	"planning_failure_retryable",
	"planning_failure_retry_counts",
	"planning_failure_retry_budget",
	"planning_failure_question_fix",
	"planning_failure_question_leave",
}

// PlanningFailure is what every surface shows for a failed planning attempt.
type PlanningFailure struct {
	Reason      string         `json:"reason"`
	Category    string         `json:"category"`
	Source      string         `json:"source"`
	Phrases     []string       `json:"phrases,omitempty"`
	Retryable   bool           `json:"retryable"`
	RetryCounts map[string]int `json:"retry_counts,omitempty"`
	RetryBudget int            `json:"retry_budget"`
	OccurredAt  time.Time      `json:"occurred_at"`
}

// PlanningFailureView returns the planning failure of prd, or nil when the
// last planning attempt did not fail. Retry counts are a copy of
// RetryCountsByCategory; the budget is the resolved retry budget of the
// failure's category.
func (m *Manager) PlanningFailureView(prd *PRD) *PlanningFailure {
	if prd == nil {
		return nil
	}
	var fr FailureReason
	var counts map[string]int
	var have bool
	m.store.WithLock(func() {
		if prd.LastDecomposeFailure == nil {
			return
		}
		have = true
		fr = *prd.LastDecomposeFailure
		fr.Phrases = append([]string(nil), fr.Phrases...)
		if len(prd.RetryCountsByCategory) > 0 {
			counts = make(map[string]int, len(prd.RetryCountsByCategory))
			for k, v := range prd.RetryCountsByCategory {
				counts[k] = v
			}
		}
	})
	if !have {
		return nil
	}
	return &PlanningFailure{
		Reason:      fr.Detail,
		Category:    fr.Category,
		Source:      fr.Source,
		Phrases:     fr.Phrases,
		Retryable:   fr.Retryable,
		RetryCounts: counts,
		RetryBudget: m.resolveRetryPolicy(prd, fr.Category),
		OccurredAt:  fr.OccurredAt,
	}
}

// --- phrases ---------------------------------------------------------------

var fixedVersionPhraseRE = regexp.MustCompile(`fixed version number ("(?:[^"\\]|\\.)*")`)

// planningFailurePhrases returns the exact offending texts of a planning
// failure: the phrase a validator error quotes, plus the phrases of the spec
// lint findings that carry the same rule name as the failure's source.
func planningFailurePhrases(prd *PRD, source, detail string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] || len(out) >= planningFailurePhraseCap {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range fixedVersionPhraseRE.FindAllStringSubmatch(detail, -1) {
		if p, err := strconv.Unquote(m[1]); err == nil {
			add(p)
		}
	}
	if prd != nil {
		for _, f := range LintSpecText(prd.Spec) {
			if f.Rule == source {
				add(f.Phrase)
			}
		}
	}
	return out
}

// --- reword ----------------------------------------------------------------

// specReplacement returns the text that replaces a correctable finding's
// phrase, and whether the finding can be reworded by the daemon.
func specReplacement(f SpecFinding) (string, bool) {
	if !f.Correctable {
		return "", false
	}
	switch f.Rule {
	case SpecRuleReleaseVersionFixed:
		low := strings.ToLower(f.Phrase)
		if strings.HasPrefix(low, "version ") || strings.HasPrefix(low, "release ") {
			return releaseNeutralWord, true
		}
		return releaseNeutralBare, true
	case SpecRuleTrackerID:
		return "", true
	case SpecRuleSystemPath:
		return f.Phrase[strings.LastIndex(f.Phrase, "/")+1:], true
	}
	return "", false
}

// specAllCorrectable reports whether findings is non-empty and every finding
// is one the daemon can reword.
func specAllCorrectable(findings []SpecFinding) bool {
	if len(findings) == 0 {
		return false
	}
	for _, f := range findings {
		if _, ok := specReplacement(f); !ok {
			return false
		}
	}
	return true
}

// RewordSpecText rewrites text with the lint suggestions of its correctable
// findings and returns the new text and the number of phrases reworded. A
// finding that is not correctable is left alone.
func RewordSpecText(text string) (string, int) {
	out, n := text, 0
	for _, f := range LintSpecText(text) {
		repl, ok := specReplacement(f)
		if !ok || !strings.Contains(out, f.Phrase) {
			continue
		}
		if repl == "" {
			re := regexp.MustCompile(`[ \t]?` + regexp.QuoteMeta(f.Phrase))
			out = re.ReplaceAllString(out, "")
		} else {
			out = strings.ReplaceAll(out, f.Phrase, repl)
		}
		n++
	}
	return out, n
}

// --- state, alert and question --------------------------------------------

// planningFailureState is the Manager's planning failure bookkeeping.
type planningFailureState struct {
	mu sync.Mutex
	// replan replaces the production re-plan (Decompose in the background);
	// tests set it.
	replan func(prdID string) error
}

func (m *Manager) planningReplan(prdID string) error {
	m.planningFailure.mu.Lock()
	fn := m.planningFailure.replan
	m.planningFailure.mu.Unlock()
	if fn != nil {
		return fn(prdID)
	}
	go func() {
		if _, err := m.Decompose(prdID); err != nil {
			log.Printf("[autonomous] prd=%s planning again after the spec was reworded: %v", prdID, err)
		}
	}()
	return nil
}

func planningFailureHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// planningFailureKey identifies one failure of one Automaton: the same
// recorded failure read twice has the same key.
func planningFailureKey(prdID string, fr *FailureReason) string {
	return planningFailureHash(prdID, fr.Source, strconv.FormatInt(fr.OccurredAt.UnixNano(), 10))
}

// planningFailureSourceLine is the stable source line of the question: it
// depends on the Automaton, the rule and the phrases, not on when it failed.
func planningFailureSourceLine(prdID, source string, phrases []string) string {
	sorted := append([]string(nil), phrases...)
	sort.Strings(sorted)
	return planningFailureHash(PlanningFailureQuestionAsker, prdID, source, strings.Join(sorted, "\x00"))
}

// markPlanningFailure records, once per (kind, key), that the alert or
// question for a failure was raised, on the Automaton's decision log so it
// survives a restart. It reports whether this call is the first.
func (m *Manager) markPlanningFailure(prd *PRD, kind, key, note string) bool {
	m.planningFailure.mu.Lock()
	defer m.planningFailure.mu.Unlock()
	first := true
	m.store.WithLock(func() {
		for i := range prd.Decisions {
			if d := &prd.Decisions[i]; d.Kind == kind && d.Result == key {
				first = false
				return
			}
		}
		prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
			At: time.Now(), Kind: kind, Actor: "autonomous", Note: note, Result: key,
		}))
	})
	if first {
		if err := m.store.SavePRD(prd); err != nil {
			log.Printf("[autonomous] prd=%s planning failure %s record: %v", prd.ID, kind, err)
		}
	}
	return first
}

func planningFailureTitle(prd *PRD) string {
	if prd.Title != "" {
		return prd.Title
	}
	return prd.ID
}

// onPlanningFailureTerminal runs when a planning attempt has given up. A
// retryable failure raises nothing. A non-retryable one raises one alert and,
// when every spec lint finding is correctable, one question.
func (m *Manager) onPlanningFailureTerminal(prd *PRD) {
	var fr FailureReason
	var have bool
	var spec string
	m.store.WithLock(func() {
		if prd.LastDecomposeFailure != nil {
			have, fr, spec = true, *prd.LastDecomposeFailure, prd.Spec
		}
	})
	if !have || fr.Retryable {
		return
	}
	key := planningFailureKey(prd.ID, &fr)
	if m.markPlanningFailure(prd, planningFailureAlertKind, key, "alert raised: "+fr.Detail) {
		m.mu.Lock()
		alert := m.alertSink
		m.mu.Unlock()
		if alert != nil {
			alert("warn", "Automaton could not be planned: "+planningFailureTitle(prd), planningFailureAlertBody(&fr))
		}
	}
	if findings := LintSpecText(spec); specAllCorrectable(findings) {
		m.raisePlanningFailureQuestion(prd, &fr, findings)
	}
}

func planningFailureAlertBody(fr *FailureReason) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Planning failed (%s, rule %s): %s", fr.Category, fr.Source, fr.Detail)
	if len(fr.Phrases) > 0 {
		fmt.Fprintf(&b, "\nPhrases: %s", strings.Join(fr.Phrases, "; "))
	}
	return b.String()
}

// BuildPlanningFailureQuestion builds the question that offers to reword the
// spec of an Automaton and plan again. The default is to leave it.
func BuildPlanningFailureQuestion(prdID, source string, findings []SpecFinding) Question {
	var b strings.Builder
	fmt.Fprintf(&b, "Planning of Automaton %s failed on rule %s. The spec can be reworded:\n", prdID, source)
	phrases := make([]string, 0, len(findings))
	for _, f := range findings {
		phrases = append(phrases, f.Phrase)
		fmt.Fprintf(&b, "- %s\n", f.Suggestion)
	}
	b.WriteString("Reword the spec and plan again?")
	return Question{
		Label:     "decision",
		AskedBy:   PlanningFailureQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextPrd,
		PRDID:     prdID,
		Text:      b.String(),
		Options: []QuestionOption{
			{ID: PlanningFailureOptionFix, Label: PlanningFailureLabelFix,
				Tradeoffs: "The spec is rewritten with the suggested wording and the Automaton is planned again."},
			{ID: PlanningFailureOptionLeave, Label: PlanningFailureLabelLeave,
				Tradeoffs: "Nothing changes; the failure stays on the Automaton."},
		},
		Default:          PlanningFailureOptionLeave,
		SourceLineSHA256: planningFailureSourceLine(prdID, source, phrases),
	}
}

func (m *Manager) raisePlanningFailureQuestion(prd *PRD, fr *FailureReason, findings []SpecFinding) {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	key := planningFailureKey(prd.ID, fr)
	if !m.markPlanningFailure(prd, planningFailureQuestionKind, key, "question raised: reword the spec and plan again") {
		return
	}
	q := BuildPlanningFailureQuestion(prd.ID, fr.Source, findings)
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s planning failure question: %v", prd.ID, err)
	}
}

// --- answer ----------------------------------------------------------------

func (m *Manager) recordPlanningFailureAnswer(prdID, note string, c QuestionChange) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
			At: time.Now(), Kind: planningFailureAnswerKind, Actor: "operator", Note: note,
			QuestionID: c.Question.ID, AnswerID: c.Answer.ID,
		}))
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s planning failure answer record: %v", prdID, err)
	}
}

// ApplyPlanningFailureAnswer applies the chosen option: the first rewords the
// spec with the lint suggestions, applies it through the revision path and
// plans again; the second changes nothing.
func (m *Manager) ApplyPlanningFailureAnswer(prdID, optionID string) error {
	switch optionID {
	case PlanningFailureOptionLeave:
		return nil
	case PlanningFailureOptionFix:
	default:
		return fmt.Errorf("unknown planning failure option %q", optionID)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %s not found", prdID)
	}
	reworded, n := RewordSpecText(prd.Spec)
	if n == 0 || reworded == prd.Spec {
		return fmt.Errorf("automaton %s: the spec has nothing the lint can reword", prdID)
	}
	if _, err := m.store.UpdatePRDFields(prdID, "", reworded); err != nil {
		return fmt.Errorf("apply the reworded spec: %w", err)
	}
	switch prd.Status {
	case PRDNeedsReview, PRDApproved, PRDRevisionsAsked, PRDFailed:
		note := fmt.Sprintf("spec reworded by the planning lint (%d phrase(s)); planning again", n)
		if _, err := m.RequestRevision(prdID, "operator", note); err != nil {
			return fmt.Errorf("request revision: %w", err)
		}
	}
	return m.planningReplan(prdID)
}

// onPlanningFailureAnswer is the question store's route to the Automaton for
// the planning failure question. It reports whether the change was such an
// answer. An answer that names no listed option, or an Automaton that is
// gone, is logged and changes nothing.
func (m *Manager) onPlanningFailureAnswer(c QuestionChange) bool {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || c.Answer == nil ||
		q.AskedBy != PlanningFailureQuestionAsker || q.AskerKind != QuestionAskerSystem {
		return false
	}
	if q.PRDID == "" {
		return true
	}
	opt := c.Answer.ChosenOptionID
	note := "planning failure: operator chose " + opt
	if err := m.ApplyPlanningFailureAnswer(q.PRDID, opt); err != nil {
		log.Printf("[autonomous] prd=%s planning failure answer to question %s: %v", q.PRDID, q.ID, err)
		note += " (not applied: " + err.Error() + ")"
	}
	m.recordPlanningFailureAnswer(q.PRDID, note, c)
	return true
}
