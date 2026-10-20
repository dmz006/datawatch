// Plan-coverage gate — the consumer side of the stored per-PRD
// plan-coverage record introduced alongside CheckPlanCoverage /
// CheckPlanStructure (see plan_coverage.go and plan_coverage_findings.go):
// compute the deterministic (non-LLM) report at decompose time for EVERY
// mode, store it on the PRD, gate Approve in "block" mode (default), and
// let the operator acknowledge an exact failing surface with an actor +
// note instead of flipping the setting.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// PlanCoverageAck.Fingerprint is bound to the failure surface of the
// stored report at the time the ack was recorded (see
// PlanCoverage.Fingerprint), so an ack can never silently cover a *different*
// set of unmatched requirements or structural findings.

// IsFailing reports whether this stored coverage record fails under its
// own pinned settings: missing/weak requirements below the pinned minimum
// coverage ratio, OR any structural finding. A spec with zero extractable
// requirements covers everything it asks for (nothing to cover) and is not
// a failure on the ratio dimension — a structural finding still is.
func (pc *PlanCoverage) IsFailing() bool {
	if pc == nil {
		return false
	}
	if len(pc.Findings) > 0 {
		return true
	}
	total := pc.Report.Covered + pc.Report.Missing + pc.Report.Weakened
	if total == 0 {
		return false
	}
	ratio := float64(pc.Report.Covered) / float64(total)
	return ratio < pc.MinRatio
}

// Fingerprint is a stable hash of this record's reported failing surface:
// the set of unmatched requirements (each with its verdict) plus the
// structural findings — both order-insensitive. Two reports with the
// same failing surface share the same fingerprint no matter when they
// were computed or in what order entries were produced, and ANY
// difference in the set of unmatched requirements OR in the structural
// findings changes it. That is exactly the surface an Ack is scoped
// to (see AcknowledgePlanCoverage and planCoverageApprovalGate).
func (pc *PlanCoverage) Fingerprint() string {
	hasher := sha256.New()
	type reqEntry struct{ requirement, verdict string }
	reqs := make([]reqEntry, 0, len(pc.Report.Unmatched))
	for _, u := range pc.Report.Unmatched {
		reqs = append(reqs, reqEntry{u.Requirement, string(u.Verdict)})
	}
	sort.Slice(reqs, func(i, j int) bool {
		if reqs[i].requirement != reqs[j].requirement {
			return reqs[i].requirement < reqs[j].requirement
		}
		return reqs[i].verdict < reqs[j].verdict
	})
	for _, u := range reqs {
		hasher.Write([]byte(u.requirement))
		hasher.Write([]byte{0})
		hasher.Write([]byte(u.verdict))
		hasher.Write([]byte{0})
	}
	type findingEntry struct{ kind, storyID, taskID, detail string }
	fds := make([]findingEntry, 0, len(pc.Findings))
	for _, f := range pc.Findings {
		fds = append(fds, findingEntry{f.Kind, f.StoryID, f.TaskID, f.Detail})
	}
	sort.Slice(fds, func(i, j int) bool {
		switch {
		case fds[i].kind != fds[j].kind:
			return fds[i].kind < fds[j].kind
		case fds[i].storyID != fds[j].storyID:
			return fds[i].storyID < fds[j].storyID
		case fds[i].taskID != fds[j].taskID:
			return fds[i].taskID < fds[j].taskID
		default:
			return fds[i].detail < fds[j].detail
		}
	})
	for _, f := range fds {
		hasher.Write([]byte(f.kind))
		hasher.Write([]byte{0})
		hasher.Write([]byte(f.storyID))
		hasher.Write([]byte{0})
		hasher.Write([]byte(f.taskID))
		hasher.Write([]byte{0})
		hasher.Write([]byte(f.detail))
		hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// recordPlanCoverage computes the deterministic (zero-LLM) coverage
// report for the just-validated plan — requirements coverage plus
// structural findings — and attaches it to prd, pinned to the effective
// plan-coverage settings AT COMPUTE TIME so a later config flip cannot
// silently re-interpret this stored report. The report is computed and
// stored in ALL modes: "off" stores but gates nothing, "warn" stores +
// emits a warn-level event, "block" (default) stores and gates Approve.
//
// operator acknowledgement from a previous report is carried over (an
// ack's fingerprint no longer matching the new surface is rejected by
// the gate, not silently dropped), so a re-decompose never loses prior
// operator judgment data.
func (m *Manager) recordPlanCoverage(prd *PRD, s []Story, settings config.PlanCoverageSettings) *PlanCoverage {
	var carryAck *PlanCoverageAck
	if prev := prd.PlanCoverage; prev != nil {
		carryAck = prev.Ack
	}
	rep := CheckPlanCoverage(prd.Spec, s)
	structured := CheckPlanStructure(prd, s)
	pc := &PlanCoverage{
		Mode:     settings.Mode,
		MinRatio: settings.MinRatio,
		Report:   *rep,
		Findings: structured.Findings,
		Ack:      carryAck,
	}
	prd.PlanCoverage = pc

	if pc.IsFailing() && settings.Mode == config.PlanCoverageWarn {
		unmatched := len(pc.Report.Unmatched)
		findings := len(pc.Findings)
		log.Printf("[autonomous] prd=%s plan-coverage WARN: %d unmatched requirement(s), %d structural finding(s) — mode=warn so this is reported but not gating",
			prd.ID, unmatched, findings)
		if m.auditFn != nil {
			m.auditFn("plan_coverage_warn", map[string]any{
				"prd":         prd.ID,
				"unmatched":   unmatched,
				"findings":    findings,
				"fingerprint": pc.Fingerprint(),
			})
		}
	}
	return pc
}

// planCoverageApprovalGate is the Approve gate for a stored
// plan-coverage record: in "block" mode a failing stored report
// blocks approval unless the operator has acknowledged THAT exact
// failing surface (an ack fingerprint matching the current record's
// fingerprint). "off" and "warn" records are ignored here — both
// modes store the report (see recordPlanCoverage) but neither gates.
func (m *Manager) planCoverageApprovalGate(prdID string, prd *PRD) error {
	pc := prd.PlanCoverage
	if pc == nil || pc.Mode != config.PlanCoverageBlock {
		return nil
	}
	if !pc.IsFailing() {
		return nil
	}
	if pc.Ack != nil && pc.Ack.Fingerprint != "" && pc.Ack.Fingerprint == pc.Fingerprint() {
		// operator acknowledged exactly this failing surface —
		// their written note is the accepted record.
		return nil
	}
	unmatched := make([]string, 0, len(pc.Report.Unmatched))
	for _, u := range pc.Report.Unmatched {
		unmatched = append(unmatched, strconv.Quote(u.Requirement))
	}
	return fmt.Errorf(
		"plan-coverage gate (plan_coverage_check=block): prd %q is missing %d requirement(s) from its spec — %s — and carries %d structural finding(s); approval is blocked. Re-decompose with a plan that covers the spec, or explicitly acknowledge exactly this failing surface with an actor and a non-empty note (AcknowledgePlanCoverage). A different set of unmatched requirements or findings is a new failing surface and needs a fresh acknowledgement",
		prdID, len(unmatched), strings.Join(unmatched, ", "), len(pc.Findings))
}

// approveAuto is the AutoApproveChildren approval path (see
// internal/autonomous/executor.go child spawn): identical to Approve,
// except that when the plan-coverage gate blocks it, the automation —
// which is itself operator-enabled and therefore carries the operator's
// intent to auto-approve this child — acknowledges the exact failing
// surface with an auditable auto-ack (actor "autonomous", note stating
// the config basis) and approves. Operator approvals (Approve, and
// the API endpoints that call it) never do this: an operator must
// either see the gate error and act on it, or explicitly acknowledge.
func (m *Manager) approveAuto(prdID, actor, note string) (*PRD, error) {
	prd, err := m.Approve(prdID, actor, note)
	if err == nil {
		return prd, nil
	}
	if !m.planCoverageGateBlocks(prdID) {
		return nil, err
	}
	autoNote := "auto-approved per auto_approve_children (operator-enabled automation); blocking surface: " + note
	if _, ackErr := m.AcknowledgePlanCoverage(prdID, "autonomous", autoNote); ackErr != nil {
		return nil, err // keep the original gate error as the caller sees it
	}
	return m.Approve(prdID, actor, note)
}

// planCoverageGateBlocks reports whether the stored plan-coverage record
// is currently failing the Approve gate in block mode without a matching
// acknowledgement — i.e. exactly the surface approveAuto may auto-ack.
func (m *Manager) planCoverageGateBlocks(prdID string) bool {
	prd, ok := m.store.GetPRD(prdID)
	if !ok || prd.PlanCoverage == nil {
		return false
	}
	return prd.PlanCoverage.Mode == config.PlanCoverageBlock &&
		prd.PlanCoverage.isBlockingWithoutAck()
}

// isBlockingWithoutAck mirrors planCoverageApprovalGate's refusal
// condition (failing record, no ack covering this surface).
func (pc *PlanCoverage) isBlockingWithoutAck() bool {
	if !pc.IsFailing() {
		return false
	}
	return !(pc.Ack != nil && pc.Ack.Fingerprint != "" && pc.Ack.Fingerprint == pc.Fingerprint())
}

// AcknowledgePlanCoverage records the operator's explicit
// acknowledgement (actor, timestamp, note) of a PRD's currently stored,
// failing plan-coverage report, bound to the stable fingerprint of
// that report's failing surface at this moment. A later stored report
// whose set of unmatched requirements OR structural findings differs
// has a different fingerprint and does NOT inherit this ack (the gate
// rejects it — re-acknowledgement required). Requires a non-empty note
// and only from the needs_review status.
func (m *Manager) AcknowledgePlanCoverage(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview {
		return nil, fmt.Errorf("prd %q is in status %q; plan-coverage acknowledgement is only allowed from needs_review", prdID, prd.Status)
	}
	if prd.PlanCoverage == nil {
		return nil, fmt.Errorf("prd %q has no stored plan-coverage report to acknowledge (decompose it first)", prdID)
	}
	if !prd.PlanCoverage.IsFailing() {
		return nil, fmt.Errorf("prd %q's stored plan-coverage report is not failing — nothing to acknowledge", prdID)
	}
	if strings.TrimSpace(note) == "" {
		return nil, fmt.Errorf("acknowledging prd %q's failing plan-coverage report requires a non-empty note explaining the risk accepted", prdID)
	}
	now := time.Now()
	fp := prd.PlanCoverage.Fingerprint()
	prd.PlanCoverage.Ack = &PlanCoverageAck{
		By:          actor,
		At:          &now,
		Note:        note,
		Fingerprint: fp,
	}
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At:             now,
		Kind:           "plan_coverage_ack",
		Actor:          actor,
		Note:           "acknowledged failing plan coverage (fingerprint " + fp[:8] + "): " + note,
		VerdictOutcome: "warn",
	})
	if m.auditFn != nil {
		m.auditFn("plan_coverage_ack", map[string]any{
			"prd":         prdID,
			"actor":       actor,
			"note":        note,
			"fingerprint": fp,
		})
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}
