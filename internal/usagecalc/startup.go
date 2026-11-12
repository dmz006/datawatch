// startup.go — the start-up advice line: which worker profile carries the largest average
// start-up token size, and the profile change that would reduce it. Advice only: nothing
// here reads or writes a setting, and the line is emitted only when there is start-up data.
//
// The rows come from the cost report's per-profile start-up table (costreport
// Report.StartupByProfile), handed to the Service through a StartupSource so this package
// never reads the attempt records itself.

package usagecalc

import (
	"fmt"
	"strings"
)

// StartupProfile is one worker profile's start-up figures, as the cost report's
// per-profile table carries them: how many attempts ran with the profile, and the average
// start-up token size over the attempts that have one. HasAverage is false when no attempt
// for the profile carries a size (records from before start-up sizes were captured).
type StartupProfile struct {
	Profile    string
	Attempts   int
	AvgTokens  float64
	HasAverage bool
}

// StartupSource returns the current start-up figures per profile. It is called once per
// Advice call; a nil source, or one returning no rows, leaves the advice without the line.
type StartupSource func() []StartupProfile

// StartupAdvice is the start-up line of one advice: the profile with the largest average
// start-up size, that average, the attempts behind it, and the plain-English line.
type StartupAdvice struct {
	Profile   string  `json:"profile"`
	Attempts  int     `json:"attempts"`
	AvgTokens float64 `json:"avg_startup_tokens"`
	Line      string  `json:"line"`
}

// startupProfileChange is the profile change named in the line. It is the generic
// direction the operator can take: a lighter profile for the role.
const startupProfileChange = "a lighter profile (for example digest instead of full for this role)"

// StartupAdviceFor picks the profile with the largest average start-up size from rows and
// words the line. It returns nil when no row carries an average: no start-up data, nothing
// to advise. Rows with no average, no attempts or no profile name are skipped. A tie goes
// to the row that comes first, so the choice is deterministic for sorted rows.
func StartupAdviceFor(rows []StartupProfile) *StartupAdvice {
	var best *StartupProfile
	for i := range rows {
		r := &rows[i]
		if !r.HasAverage || r.Attempts <= 0 || strings.TrimSpace(r.Profile) == "" {
			continue
		}
		if best == nil || r.AvgTokens > best.AvgTokens {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	avg := int64(best.AvgTokens + 0.5)
	return &StartupAdvice{
		Profile:   best.Profile,
		Attempts:  best.Attempts,
		AvgTokens: best.AvgTokens,
		Line: fmt.Sprintf(
			"start-up: profile %s has the largest average start-up, %d tokens over %d attempts; switching it to %s would reduce that. Advice only, no setting changed.",
			best.Profile, avg, best.Attempts, startupProfileChange),
	}
}

// WithStartupSource wires the start-up figures the advice line is computed from. Without it
// the advice carries no start-up line.
func WithStartupSource(src StartupSource) ServiceOption {
	return func(s *Service) { s.startup = src }
}

// startupAdvice reads the wired source, if any, and returns the line's data or nil.
func (s *Service) startupAdvice() *StartupAdvice {
	if s.startup == nil {
		return nil
	}
	return StartupAdviceFor(s.startup())
}
