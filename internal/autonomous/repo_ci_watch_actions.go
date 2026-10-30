// Package autonomous — repo CI watch action phase.
//
// The action phase (repo-ci-watch-rules-and-lessons.md rule 1 and the
// superseded-run rules) turns one saved record's classification into the
// GitHub writes the watch is allowed to make — and into would_do
// stand-ins in shadow mode, where the phase never writes:
//
//   - a hung run (in_progress past the hung threshold) is cancelled at
//     most once per run ID (a later poll finding it already cancelled or
//     completed cancels nothing);
//
//   - an infra-failing run (cancelled, timed out, a pre-start failure or
//     stale) is rerun at most once per run ID, while the infra retry
//     budget (RetryPolicy["infra"], unset readings 0: no automatic
//     reruns) still has reruns left for that repository/ref/workflow,
//     and only if a later poll has not already named its superseded
//     (there is nothing left to rerun);
//
//   - a failed run that a later green run of the same workflow on a
//     descendant commit (git merge-base --is-ancestor) has superseded is
//     named in that green record's superseded_run_ids the poll it is
//     discovered in (step 1) and, only when the record already names it,
//     deleted by a LATER poll (step 2) — a same-pass discovery never
//     deletes — when delete_superseded is delete and the mode is on;
//     every other combination (report mode, shadow mode, a failed or
//     unresolvable ancestor check) leaves the run and its artifacts and
//     only adds a would_do stand-in when the row is worth one;
//
//   - a security or a policy run is recorded, never cancelled, rerun or
//     deleted, and gets no would_do row for any of those either:
//     "record-only" is literal;
//
//   - an unresolved record (failing, hung or pending) that has been
//     unresolved since first_seen_at past alert_minutes (0 being off)
//     alerts once per episode (record.alerted_at is the stamp) — never
//     in shadow.
//
// The production write implementations live here as nil-seam defaults
// (repo ci watch seams, manager.go): defaultRepoCIAncestor (git),
// defaultRepoCICancel / defaultCIRerun / defaultRepoCIDelete (gh);
// tests inject fakes through the same seams, so the pass writes
// nothing to GitHub.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// repoCIWatchAction* are the three actions the phase can take (and the
// action vocabulary of the record's rerun_log and would_do stand-ins).
const (
	repoCIWatchActionCancel = "cancel"
	repoCIWatchActionRerun  = "rerun"
	repoCIWatchActionDelete = "delete"
)

// The record's status and classification values this phase branches on
// (the record vocabulary, repo_ci_watch_record.go).
const (
	repoCIWatchStatusGreen   = "green"
	repoCIWatchStatusFailing = "failing"
	repoCIWatchStatusHung    = "hung"
	repoCIWatchStatusPending = "pending"
	repoCIWatchClassInfra    = "infra"
	repoCIWatchClassSecurity = "security"
	repoCIWatchClassPolicy   = "policy"
)

// repoCIWatchNoteSource is the note source of the action phase's
// written-down write failures.
const repoCIWatchNoteSource = "repo_ci_watch_actions"

// defaultRepoCIAncestor reports whether older is an ancestor of newer
// via git merge-base --is-ancestor in repoDir. A failed or missing git
// read counts as "not an ancestor" — the superseded check is best-
// effort and must never fail the pass (an unresolvable ancestry leaves
// the run in place).
func defaultRepoCIAncestor(ctx context.Context, repoDir, older, newer string) bool {
	if repoDir == "" || older == "" || newer == "" {
		return false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "merge-base", "--is-ancestor", older, newer)
	return cmd.Run() == nil
}

// defaultRepoCICancel cancels one workflow run (gh run cancel).
func defaultRepoCICancel(ctx context.Context, ownerRepo string, runID int64) error {
	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ghBin, "run", "cancel", fmt.Sprint(runID), "--repo", ownerRepo) // #nosec G204 -- fixed gh subcommand; runID is numeric, ownerRepo comes from git's own output
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh run cancel %d: %w: %s", runID, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultRepoCIDelete removes one workflow run and its artifacts
// (gh run delete).
func defaultRepoCIDelete(ctx context.Context, ownerRepo string, runID int64) error {
	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ghBin, "run", "delete", fmt.Sprint(runID), "--repo", ownerRepo) // #nosec G204 -- fixed gh subcommand; runID is numeric, ownerRepo comes from git's own output
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh run delete %d: %w: %s", runID, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// repoCIWatchHasLog reports whether rec's rerun_log already settled on
// action for runID (the once-per-run gate: a settled action is never
// repeated by a later poll).
func repoCIWatchHasLog(rec *RepoCIWatchRecord, action, runID string) bool {
	if rec == nil {
		return false
	}
	for _, e := range rec.RerunLog {
		if e.Action == action && e.RunID == runID {
			return true
		}
	}
	return false
}

// repoCIWatchHasWouldDo reports whether rec already stands in for
// action on runID (a would_to do row is one decision, not one per
// poll).
func repoCIWatchHasWouldDo(rec *RepoCIWatchRecord, action, runID string) bool {
	if rec == nil {
		return false
	}
	for _, e := range rec.WouldDo {
		if e.Action == action && e.RunID == runID {
			return true
		}
	}
	return false
}

// repoCIWatchNote is one rerun_log entry ("action run <id>").
func repoCIWatchNote(at time.Time, action, runID string) RepoCIWatchRerun {
	return RepoCIWatchRerun{At: at, Action: action, RunID: runID}
}

// repoCIWatchSupKey keys one "this record already named this run ID"
// identity (the superseded-pair key of the pass context).
func repoCIWatchSupKey(recordKey, runID string) string {
	return recordKey + "\x00" + runID
}

// saveRepoCIWatchRecord saves rec and returns the saved (copy) record;
// on a store error it returns rec unchanged and logs.
func (p *repoCIWatchPollContext) saveRepoCIWatchRecord(rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	saved, err := p.m.SaveRepoCIWatchRecord(rec)
	if err != nil {
		log.Printf("repo ci watch: saving record %s: %v", rec.Key(), err)
		return rec
	}
	return saved
}

// failNote writes a write-failure note on rec's record (store errors
// are tolerated: the pass continues).
func (p *repoCIWatchPollContext) failNote(rec *RepoCIWatchRecord, action string, err error) {
	p.m.AppendRepoCIWatchNote(rec.Key(), repoCIWatchNoteSource, action+": "+err.Error())
}

// repoCIWatchRunIDto64 parses a record's run ID (a decimal string from
// the gh listing) to the integer the write seams take.
func repoCIWatchRunIDto64(runID string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(runID), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// canWrite is the phase's write gate: true in mode on only. Shadow
// mode has the phase's decisions and zero writes; off never reaches
// the phase (no pass at all, see the poll header).
func (p *repoCIWatchPollContext) canWrite() bool {
	return p.cfg.EffectiveMode() == "on"
}

// applyRepoCIWatchActions is the phase for one saved record, in the
// order the spec fixes: hung-cancel, infra-rerun, superseded-delete,
// alert. Each sub-step is a no-op the instant its record is not that
// kind, and it is idempotent under re-poll (settled actions and rows
// are skipped). The returned record is the saved copy to keep.
func (p *repoCIWatchPollContext) applyRepoCIWatchActions(ownerRepo string, rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	// Security and policy runs are recorded, never acted on: no cancel,
	// rerun, delete or any would_do stand-in for them ("record-only" is
	// literal).
	if rec.Classification == repoCIWatchClassSecurity || rec.Classification == repoCIWatchClassPolicy {
		return p.alertIfDue(rec)
	}
	rec = p.cancelHungIf(rec)
	rec = p.rerunInfraIf(ownerRepo, rec)
	rec = p.deleteSupersededIf(ownerRepo, rec)
	return p.alertIfDue(rec)
}

// cancelHungIf is the hung branch: a hung (in_progress, past the
// threshold — the classifier already established it) run is cancelled,
// once, if it still is. A later poll finding the run cancelled or
// completed cancels nothing (status no longer hung ⇒ no call).
func (p *repoCIWatchPollContext) cancelHungIf(rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	if rec == nil || rec.Status != repoCIWatchStatusHung {
		return rec
	}
	if repoCIWatchHasLog(rec, repoCIWatchActionCancel, rec.RunID) ||
		repoCIWatchHasWouldDo(rec, repoCIWatchActionCancel, rec.RunID) {
		return rec
	}
	reason := "hung: still " + rec.Status + " past the hung_after_minutes threshold"
	if !p.canWrite() {
		rec.WouldDo = append(rec.WouldDo, RepoCIWatchWouldDo{Action: repoCIWatchActionCancel, RunID: rec.RunID, Reason: reason})
		return p.saveRepoCIWatchRecord(rec)
	}
	id, ok := repoCIWatchRunIDto64(rec.RunID)
	if !ok {
		return rec
	}
	if err := p.m.repoCIWatchCancel(p.ctx, rec.Repo, id); err != nil {
		p.failNote(rec, "cancel run "+rec.RunID, err)
		return rec
	}
	rec.RerunLog = append(rec.RerunLog, repoCIWatchNote(p.now, repoCIWatchActionCancel, rec.RunID))
	return p.saveRepoCIWatchRecord(rec)
}

// rerunInfraIf is the infra branch: an infra-failing run is rerun once
// per run ID, while the infra retry budget (the Manager's
// RetryPolicy["infra"] for this repository, unset reading 0: no
// automatic reruns) still has reruns left, and a later poll has not
// named it superseded (there is nothing left to rerun then). A rerun
// failure logs a note and leaves the action unsettled — the next poll
// may try again.
func (p *repoCIWatchPollContext) rerunInfraIf(ownerRepo string, rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	if rec == nil || rec.Status != repoCIWatchStatusFailing || rec.Classification != repoCIWatchClassInfra {
		return rec
	}
	if repoCIWatchHasLog(rec, repoCIWatchActionRerun, rec.RunID) ||
		repoCIWatchHasWouldDo(rec, repoCIWatchActionRerun, rec.RunID) {
		return rec
	}
	if p.repoCIWatchIsSuperseded(rec.RunID) {
		return rec // a later green run already superseded it
	}
	limit := p.m.RepoCIWatchInfraRetryLimit()
	if limit <= 0 || p.repoCIWatchRerunsUsed(rec.Repo, rec.RefName, rec.Workflow) >= limit {
		return rec // budget exhausted (or unset): record-only
	}
	reason := "infra: the run ended by cancellation, timeout, a pre-start failure or staleness — retryable, not code"
	if !p.canWrite() {
		rec.WouldDo = append(rec.WouldDo, RepoCIWatchWouldDo{Action: repoCIWatchActionRerun, RunID: rec.RunID, Reason: reason})
		return p.saveRepoCIWatchRecord(rec)
	}
	id, ok := repoCIWatchRunIDto64(rec.RunID)
	if !ok {
		return rec
	}
	if err := p.m.repoCIWatchRerun(p.ctx, ownerRepo, id); err != nil {
		p.failNote(rec, "rerun run "+rec.RunID, err)
		return rec
	}
	rec.RerunLog = append(rec.RerunLog, repoCIWatchNote(p.now, repoCIWatchActionRerun, rec.RunID))
	return p.saveRepoCIWatchRecord(rec)
}

// deleteSupersededIf is the superseded branch of a GREEN record: step
// 2, deleting the runs its superseded_run_ids already name — only when
// delete_superseded is delete, the mode is on, and the run was NOT
// named by this very poll (a same-pass discovery deletes on a later
// poll, never on the passing one). Every other combination settles a
// would_do stand-in (report mode, shadow mode) or nothing (a row
// already settled, an ID already deleted).
func (p *repoCIWatchPollContext) deleteSupersededIf(ownerRepo string, rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	if rec == nil || len(rec.SupersededRunIDs) == 0 {
		return rec
	}
	changed := false
	reason := "superseded: a later green run of the same workflow on a descendant commit"
	verdict := "delete"
	if !p.canWrite() || p.cfg.EffectiveDeleteSuperseded() != "delete" {
		verdict = "would_delete"
	}
	for _, sr := range rec.SupersededRunIDs {
		if sr.RunID == rec.RunID {
			continue // the green run does not act on itself
		}
		if repoCIWatchHasLog(rec, repoCIWatchActionDelete, sr.RunID) ||
			repoCIWatchHasWouldDo(rec, repoCIWatchActionDelete, sr.RunID) {
			continue // already settled (deleted once, or stood in for once)
		}
		if _, named := p.supersededThisPass[repoCIWatchSupKey(rec.Key(), sr.RunID)]; named && verdict == "delete" {
			continue // step 2: this poll named it, a later one deletes it
		}
		if verdict == "delete" {
			id, ok := repoCIWatchRunIDto64(sr.RunID)
			if !ok {
				continue
			}
			if err := p.m.repoCIWatchDelete(p.ctx, ownerRepo, id); err != nil {
				p.failNote(rec, "delete run "+sr.RunID, err)
				continue
			}
			rec.RerunLog = append(rec.RerunLog, repoCIWatchNote(p.now, repoCIWatchActionDelete, sr.RunID))
		} else {
			rec.WouldDo = append(rec.WouldDo, RepoCIWatchWouldDo{Action: repoCIWatchActionDelete, RunID: sr.RunID, Reason: reason})
		}
		changed = true
	}
	if !changed {
		return rec
	}
	return p.saveRepoCIWatchRecord(rec)
}

// repoCIWatchIsSuperseded reports whether any stored record already
// names runID in its superseded_run_ids (a named run is left as is:
// no rerun, no alert-then-rerun churn).
func (p *repoCIWatchPollContext) repoCIWatchIsSuperseded(runID string) bool {
	list := p.m.ListRepoCIWatchRecords()
	for _, s := range list {
		if s == nil {
			continue
		}
		for _, sr := range s.SupersededRunIDs {
			if sr.RunID == runID {
				return true
			}
		}
	}
	return false
}

// repoCIWatchRerunsUsed counts settled automatic reruns (rerun_log
// "rerun" entries) for the record's repository/ref/workflow — the
// unit of the infra retry budget (a budget for that
// repository/ref/workflow, not a per-run allowance).
func (p *repoCIWatchPollContext) repoCIWatchRerunsUsed(repo, refName, workflow string) int {
	used := 0
	for _, s := range p.m.ListRepoCIWatchRecords() {
		if s == nil || s.Repo != repo || s.RefName != refName || s.Workflow != workflow {
			continue
		}
		for _, e := range s.RerunLog {
			if e.Action == repoCIWatchActionRerun {
				used++
			}
		}
	}
	return used
}

// markRepoCIWatchSuperseded is step 1 for one green record (of the
// watched repository at repoDir): every stored record of the same
// repository and workflow on a DIFFERENT, ANCESTOR commit (git merge-
// base --is-ancestor in repoDir) that is failing and not security- or
// policy-classified gets appended to the green record's
// superseded_run_ids this instant — the poll it is discovered in — and
// the pair is marked for the pass (step 2 must not delete on this
// passing poll). A failed or unresolvable ancestor check — and a
// same-SHA pair — leaves the pair un-named.
func (p *repoCIWatchPollContext) markRepoCIWatchSuperseded(repoDir string, green *RepoCIWatchRecord) *RepoCIWatchRecord {
	if green == nil || green.Status != repoCIWatchStatusGreen {
		return green
	}
	changed := false
	for _, s := range p.m.ListRepoCIWatchRecords() {
		if s == nil {
			continue
		}
		if s.Repo != green.Repo || s.Workflow != green.Workflow {
			continue
		}
		if s.RunID == green.RunID || s.SHA == green.SHA || s.SHA == "" || green.SHA == "" {
			continue
		}
		if s.Status != repoCIWatchStatusFailing {
			continue
		}
		if s.Classification == repoCIWatchClassSecurity || s.Classification == repoCIWatchClassPolicy {
			continue // security (and policy) runs are never named for deletion
		}
		if !p.m.repoCIWatchAncestor(p.ctx, repoDir, s.SHA, green.SHA) {
			continue // best-effort: unresolvable ancestry leaves it
		}
		named := false
		for _, sr := range green.SupersededRunIDs {
			if sr.RunID == s.RunID {
				named = true
				break
			}
		}
		if named {
			continue
		}
		green.SupersededRunIDs = append(green.SupersededRunIDs, RepoCIWatchSupersededRun{RunID: s.RunID, RunURL: s.RunURL})
		p.supersededThisPass[repoCIWatchSupKey(green.Key(), s.RunID)] = struct{}{}
		changed = true
	}
	if !changed {
		return green
	}
	return p.saveRepoCIWatchRecord(green)
}

// alertIfDue is the alert of the action phase: an unresolved record
// (failing, hung or pending) unresolved since first_seen_at past
// alert_minutes (0 = off) alerts once per episode (the alerted_at
// stamp); in shadow the phase never reaches the alert.
func (p *repoCIWatchPollContext) alertIfDue(rec *RepoCIWatchRecord) *RepoCIWatchRecord {
	if rec == nil || rec.AlertedAt != nil {
		return rec // once per episode, or no record
	}
	if !p.canWrite() {
		return rec // shadow: never alerts
	}
	threshold := p.cfg.EffectiveAlertMinutes()
	if threshold < 0 {
		return rec
	}
	if threshold == 0 {
		return rec // explicitly off
	}
	if rec.Status != repoCIWatchStatusFailing && rec.Status != repoCIWatchStatusHung && rec.Status != repoCIWatchStatusPending {
		return rec // resolved (green/judging is being resolved)
	}
	if rec.FirstSeenAt.IsZero() || p.now.Sub(rec.FirstSeenAt) < time.Duration(threshold)*time.Minute {
		return rec // not yet at the threshold
	}
	title, body := repoCIWatchAlertTitle(rec), repoCIWatchAlertBody(rec)
	if p.co != nil {
		p.co.alert("warn", title, body)
	}
	stamp := p.now
	rec.AlertedAt = &stamp
	return p.saveRepoCIWatchRecord(rec)
}
