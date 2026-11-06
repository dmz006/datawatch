package autonomous

// A pure check of how readable a decision record is to the operator. It builds
// on CheckQuestionQuality for the minimum text length and the tracker
// identifier pattern, and adds the two checks that are specific to a
// decision: a trade-off for every option and a human label. Each of the two
// can be switched off on its own. It never touches the store, the clock or
// the network; the caller decides what the refuse mode means.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/config"
)

// QualityCodeLabelNotHuman is the stable finding code for a record label that
// a person would not recognise as a label.
const QualityCodeLabelNotHuman = "label_not_human"

// decisionLabelMinChars is the shortest label that reads as a word.
const decisionLabelMinChars = 3

// DecisionClarityPolicy configures CheckDecisionClarity. The policy is used
// as given: an empty TrackerIDPattern turns the tracker identifier check off
// and a MinTextChars of 0 turns the length check off.
type DecisionClarityPolicy struct {
	Mode             string
	MinTextChars     int
	RequireTradeoffs bool
	RequireLabel     bool
	TrackerIDPattern string
}

// DecisionClarityPolicyFromConfig builds the policy from autonomous.decisions.
// Absent keys read their defaults through the Effective helpers, so the
// result is complete.
func DecisionClarityPolicyFromConfig(d config.AutonomousDecisionsConfig) DecisionClarityPolicy {
	return DecisionClarityPolicy{
		Mode:             d.EffectiveClarityMode(),
		MinTextChars:     d.EffectiveClarityMinTextChars(),
		RequireTradeoffs: d.EffectiveClarityRequireTradeoffs(),
		RequireLabel:     d.EffectiveClarityRequireLabel(),
		TrackerIDPattern: d.EffectiveClarityTrackerIDPattern(),
	}
}

// CheckDecisionClarity returns the clarity findings for a decision record and
// the question built from it. Mode off returns none; warn and refuse return
// the same findings. It returns an error only for an invalid mode or a tracker
// identifier pattern that does not compile.
func CheckDecisionClarity(rec DecisionRecord, q Question, p DecisionClarityPolicy) ([]QualityFinding, error) {
	findings, err := CheckQuestionQuality(q, QualityPolicy{
		Mode:             p.Mode,
		MinTextChars:     p.MinTextChars,
		TrackerIDPattern: p.TrackerIDPattern,
	})
	if err != nil {
		return nil, fmt.Errorf("check decision clarity: %w", err)
	}
	if p.Mode == QualityModeOff {
		return nil, nil
	}

	var out []QualityFinding
	for _, f := range findings {
		if f.Code == QualityCodeNoTradeoffs && !p.RequireTradeoffs {
			continue
		}
		out = append(out, f)
	}

	if p.RequireLabel {
		var re *regexp.Regexp
		if p.TrackerIDPattern != "" {
			// CheckQuestionQuality already compiled this pattern.
			re = regexp.MustCompile(p.TrackerIDPattern)
		}
		if msg := decisionLabelProblem(rec.Label, re); msg != "" {
			out = append(out, QualityFinding{Code: QualityCodeLabelNotHuman, Message: msg})
		}
	}
	return out, nil
}

// decisionLabelProblem returns a plain sentence naming what is wrong with the
// label, or "" when it reads as a human label.
func decisionLabelProblem(label string, tracker *regexp.Regexp) string {
	l := strings.TrimSpace(label)
	switch {
	case l == "":
		return "The decision has no label. Give it a short word or two that a person would recognise, such as \"decision\"."
	case !decisionHasLetter(l):
		return fmt.Sprintf("The label %q has no letters. Use a short word or two that a person would recognise.", l)
	case utf8.RuneCountInString(l) < decisionLabelMinChars:
		return fmt.Sprintf("The label %q is shorter than %d characters. Use a short word or two that a person would recognise.", l, decisionLabelMinChars)
	case decisionLooksLikeRawID(l):
		return fmt.Sprintf("The label %q looks like an internal record or question id. Use a short word or two that a person would recognise.", l)
	case tracker != nil && tracker.MatchString(l):
		return fmt.Sprintf("The label %q looks like an internal tracker identifier. Describe it in plain words instead.", l)
	}
	return ""
}

func decisionHasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// decisionLooksLikeRawID reports whether s is a record id ("dr-" prefix) or a
// question id ("q-" prefix) followed by what looks like an identifier.
func decisionLooksLikeRawID(s string) bool {
	lower := strings.ToLower(s)
	for _, prefix := range []string{decisionIDPrefix, "q-"} {
		if rest, ok := strings.CutPrefix(lower, prefix); ok && rest != "" && !strings.ContainsAny(rest, " \t") {
			return true
		}
	}
	return false
}
