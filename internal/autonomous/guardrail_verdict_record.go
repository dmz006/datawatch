package autonomous

import (
	"fmt"
	"log"
	"time"
)

// recordGuardrailVerdict appends one guardrail_verdict Decision for v to
// prd and saves the PRD before anything else can change its status. The
// judge output file is removed only after that save succeeds, so the file
// backing a verdict is never deleted before the verdict is durable. A failed
// save is returned and leaves the file in place. A failed removal is logged,
// not returned, because the verdict is already recorded.
func (m *Manager) recordGuardrailVerdict(prd *PRD, unitID string, v GuardrailVerdict) error {
	d := Decision{
		At:              time.Now(),
		Kind:            "guardrail_verdict",
		Actor:           "autonomous",
		VerdictOutcome:  v.Outcome,
		FailureCategory: v.Category,
		JudgeSessionID:  v.JudgeSessionID,
		Note:            guardrailVerdictNote(v, unitID),
	}
	if v.JudgeSessionID == "" {
		d.Backend = v.Backend
	}
	prd.Decisions = append(prd.Decisions, d)
	if err := m.savePRDOwn(prd); err != nil {
		return err
	}
	// Opt-in KindGuardrailWarn: emitted only for the warn outcome, right
	// after the record save succeeded. Informational only — pass and
	// block outcomes emit nothing and the rest of this function is
	// untouched.
	if v.Outcome == "warn" {
		m.emitGuardrailWarn(prd, unitID, &v, &d)
	}
	if v.JudgeOutputPath != "" {
		if err := RemoveJudgeOutputFile(v.JudgeOutputPath); err != nil {
			log.Printf("autonomous: remove judge output %s for PRD %s: %v", v.JudgeOutputPath, prd.ID, err)
		}
	}
	return nil
}

// guardrailVerdictNote renders the one-sentence audit note for a verdict.
// The detail is the verdict summary, or its first issue when the summary is
// empty.
func guardrailVerdictNote(v GuardrailVerdict, unitID string) string {
	detail := v.Summary
	if detail == "" && len(v.Issues) > 0 {
		detail = v.Issues[0]
	}
	note := fmt.Sprintf("guardrail %s on unit %s returned %s", v.Guardrail, unitID, v.Outcome)
	if detail != "" {
		note += " - " + detail
	}
	return note
}

// blockedReasonFor renders the blocked-reason sentence for a block verdict.
// The parenthetical names the judge session when there is one, else the
// backend, else that the judgment was text-only.
func blockedReasonFor(v GuardrailVerdict) string {
	who := v.JudgeSessionID
	if who == "" {
		who = v.Backend
	}
	if who == "" {
		who = "text-only judge"
	}
	return fmt.Sprintf("guardrail %s returned block (%s) - %s", v.Guardrail, who, v.Summary)
}

// blockedReasonFromVerdicts returns blockedReasonFor the most recent block
// verdict in vs, or "" when vs holds no block. Callers that block on a
// guardrail verdict set it as the PRD's BlockedReason in the same save as
// the PRDBlocked status.
func blockedReasonFromVerdicts(vs []GuardrailVerdict) string {
	v := lastBlockVerdict(vs)
	if v == nil {
		return ""
	}
	return blockedReasonFor(*v)
}
