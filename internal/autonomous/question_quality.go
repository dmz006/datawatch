package autonomous

// A pure checker for how readable a question is to the operator. It never
// touches the store, the clock or the network; the caller decides what the
// refuse mode means (typically rejecting the raise).

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Quality modes. Off returns no findings; warn and refuse return the same
// findings and the caller decides what refuse means.
const (
	QualityModeOff    = "off"
	QualityModeWarn   = "warn"
	QualityModeRefuse = "refuse"
)

// Shipped defaults, applied by NormalizeQualityPolicy to empty fields.
const (
	DefaultQualityMode         = QualityModeWarn
	DefaultQualityMinTextChars = 60
	// DefaultQualityTrackerIDPattern matches a short run of capital letters
	// followed by digits, the shape of a planning-registry identifier.
	DefaultQualityTrackerIDPattern = `\b[A-Z]{1,3}[0-9]{2,4}\b`
)

// Stable finding codes.
const (
	QualityCodeNoTradeoffs  = "no_tradeoffs"
	QualityCodeTextTooShort = "text_too_short"
	QualityCodeTrackerID    = "tracker_id"
)

// QualityPolicy configures CheckQuestionQuality. Empty fields mean "use the
// default" once passed through NormalizeQualityPolicy.
type QualityPolicy struct {
	Mode             string
	MinTextChars     int
	TrackerIDPattern string
}

// QualityFinding is one problem found in a question; Message is a plain
// sentence an operator can act on.
type QualityFinding struct {
	Code    string
	Message string
}

// NormalizeQualityPolicy fills empty fields with the shipped defaults.
func NormalizeQualityPolicy(p QualityPolicy) QualityPolicy {
	if strings.TrimSpace(p.Mode) == "" {
		p.Mode = DefaultQualityMode
	}
	if p.MinTextChars == 0 {
		p.MinTextChars = DefaultQualityMinTextChars
	}
	if p.TrackerIDPattern == "" {
		p.TrackerIDPattern = DefaultQualityTrackerIDPattern
	}
	return p
}

// CheckQuestionQuality returns the quality findings for q under p. It returns
// an error only for an invalid mode or a TrackerIDPattern that does not
// compile. The policy is used as given; call NormalizeQualityPolicy first to
// apply the defaults.
func CheckQuestionQuality(q Question, p QualityPolicy) ([]QualityFinding, error) {
	switch p.Mode {
	case QualityModeOff, QualityModeWarn, QualityModeRefuse:
	default:
		return nil, fmt.Errorf("invalid question quality mode %q: use %s, %s or %s",
			p.Mode, QualityModeOff, QualityModeWarn, QualityModeRefuse)
	}
	var re *regexp.Regexp
	if p.TrackerIDPattern != "" {
		var err error
		re, err = regexp.Compile(p.TrackerIDPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid question quality tracker id pattern: %w", err)
		}
	}
	if p.Mode == QualityModeOff {
		return nil, nil
	}

	var out []QualityFinding

	var missing []string
	for _, opt := range q.Options {
		if strings.TrimSpace(opt.Tradeoffs) == "" {
			missing = append(missing, opt.ID)
		}
	}
	if len(missing) > 0 {
		out = append(out, QualityFinding{
			Code: QualityCodeNoTradeoffs,
			Message: fmt.Sprintf("These options have no trade-offs written: %s. Say what the operator gains and gives up with each choice.",
				strings.Join(missing, ", ")),
		})
	}

	if n := utf8.RuneCountInString(strings.TrimSpace(q.Text)); n < p.MinTextChars {
		out = append(out, QualityFinding{
			Code: QualityCodeTextTooShort,
			Message: fmt.Sprintf("The question text is %d characters; at least %d are needed to explain what is being asked and why. Add the context the operator needs to decide.",
				n, p.MinTextChars),
		})
	}

	if re != nil {
		if m := firstQualityMatch(re, q); m != "" {
			out = append(out, QualityFinding{
				Code: QualityCodeTrackerID,
				Message: fmt.Sprintf("The question contains %q, which looks like an internal tracker identifier. The operator reads these questions and may not know it; describe the item in plain words instead.",
					m),
			})
		}
	}
	return out, nil
}

// firstQualityMatch returns the first match of re in the question text, then
// each option label, then each option trade-off, or "" when none matches.
func firstQualityMatch(re *regexp.Regexp, q Question) string {
	if m := re.FindString(q.Text); m != "" {
		return m
	}
	for _, opt := range q.Options {
		if m := re.FindString(opt.Label); m != "" {
			return m
		}
	}
	for _, opt := range q.Options {
		if m := re.FindString(opt.Tradeoffs); m != "" {
			return m
		}
	}
	return ""
}
