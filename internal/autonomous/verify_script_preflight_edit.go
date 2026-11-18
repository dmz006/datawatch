package autonomous

import (
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// applyEditVerifyScriptPreflight runs the verification-script pre-flight
// for one script just set by a task edit and merges its findings into the
// PRD's DecomposeLint record: any earlier finding for the same script and
// rule is replaced, and the record is created (mode off) when none exists.
// Findings are reported, never enforced. The caller holds the store lock
// and saves the PRD.
func applyEditVerifyScriptPreflight(prd *PRD, script string) {
	var fresh []LintFinding
	for _, f := range RunVerifyScriptPreflight(prd.ProjectDir, prd.Story) {
		if f.Script == script {
			fresh = append(fresh, f)
		}
	}
	rec := prd.DecomposeLint
	if rec == nil {
		if len(fresh) == 0 {
			return
		}
		rec = &DecomposeLintRecord{Mode: config.DecomposeLintModeOff, Attempts: 1}
		prd.DecomposeLint = rec
	}
	replaced := map[string]bool{}
	for _, f := range fresh {
		replaced[f.RuleID] = true
	}
	kept := make([]LintFinding, 0, len(rec.Findings)+len(fresh))
	for _, f := range rec.Findings {
		if f.Script == script && replaced[f.RuleID] {
			continue
		}
		kept = append(kept, f)
	}
	rec.Findings = append(kept, fresh...)
	rec.LintedAt = time.Now()
}
