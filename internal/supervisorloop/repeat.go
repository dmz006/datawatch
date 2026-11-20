package supervisorloop

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FindingRepeatCorrection is the finding kind raised when the same
// correction keeps being recorded.
const FindingRepeatCorrection = "correction.repeated"

// RepeatThreshold is the count the same correction may reach without a
// finding. A correction recorded more than this many times raises one.
const RepeatThreshold = 2

// RepeatFinding says one correction was recorded more often than allowed,
// so the rule it states is not reaching the supervisor.
type RepeatFinding struct {
	Kind    string    `json:"kind"`
	Text    string    `json:"text"`    // normalised correction text
	Count   int       `json:"count"`   // how many times it was recorded
	IDs     []string  `json:"ids"`     // the correction ids, sorted
	Summary string    `json:"summary"` // plain-text description
	At      time.Time `json:"at"`      // from the injected clock
}

// NormalizeCorrectionText lowercases the text, drops punctuation and
// collapses whitespace so rewordings of spacing and case compare equal.
func NormalizeCorrectionText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '_' || r == '-' || r == '\'':
			// joiners vanish only for quotes; keep word breaks for the others
			if r == '\'' {
				continue
			}
			b.WriteRune(' ')
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r > 127 && !isSpaceOrPunct(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isSpaceOrPunct(r rune) bool {
	return strings.ContainsRune(" ‘’“”–—…", r)
}

// RepeatDetector finds corrections recorded more than twice.
type RepeatDetector struct {
	corrs *Corrections
	clock Clock
}

// NewRepeatDetector builds the detector over the corrections accessor.
func NewRepeatDetector(corrs *Corrections, clock Clock) *RepeatDetector {
	return &RepeatDetector{corrs: corrs, clock: clock}
}

// Detect reads the recorded corrections for the project and role and returns
// one finding per normalised text that appears more than RepeatThreshold
// times, ordered by text.
func (d *RepeatDetector) Detect(project, role string) ([]RepeatFinding, error) {
	if d == nil || d.corrs == nil || d.clock == nil {
		return nil, fmt.Errorf("repeat detector: corrections and clock are required")
	}
	list, err := d.corrs.List(project, role)
	if err != nil {
		return nil, err
	}
	return DetectRepeats(list, d.clock.Now()), nil
}

// DetectRepeats groups corrections by normalised text and returns a finding
// for each group larger than RepeatThreshold. Blank texts are ignored.
func DetectRepeats(list []Correction, now time.Time) []RepeatFinding {
	groups := map[string][]string{}
	for _, c := range list {
		n := NormalizeCorrectionText(c.Text)
		if n == "" {
			continue
		}
		groups[n] = append(groups[n], c.ID)
	}
	var out []RepeatFinding
	for text, ids := range groups {
		if len(ids) <= RepeatThreshold {
			continue
		}
		sort.Strings(ids)
		out = append(out, RepeatFinding{
			Kind: FindingRepeatCorrection, Text: text, Count: len(ids), IDs: ids, At: now,
			Summary: fmt.Sprintf("the same correction was recorded %d times; the rule is not reaching the supervisor: %s", len(ids), text),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	return out
}
