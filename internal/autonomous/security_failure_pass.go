// security_failure_pass.go — the coordinator's security failure pass: it
// applies the decision of the decide step (security_failure_decide.go)
// to the repo CI watch's failing security runs and, in on mode, stops
// the Automaton line the failing commit belongs to.
//
// One pass per idle-loop tick (automation_idle.go calls
// c.securityFailurePass() beside triagePass and repoCIWatchPass). It
// reads the security mode first: off returns before any log read or
// lookup. It then visits the repo CI watch records whose status is
// failing, whose classification is security and whose workflow is in the
// effective security workflow set. Each run is assessed ONCE: a record
// that already carries a report is skipped, so a repeat poll never
// repeats the log read, the lookup or the alert.
//
// For a new record the pass
//
//  1. reads the failed log through the Manager's securityFailureLog seam
//     (gh run view <run-id> --log-failed),
//  2. assesses it (AssessSecurityFailure), reads the accepted-risks
//     registry and runs the advisory lookup, and calls
//     decideSecurityFailure,
//  3. finds the linked Automaton as the PRD whose last completed task
//     post-verify SHA equals the record SHA (prdLastPostVerifySHA),
//  4. in on mode stops it by the executor's own blocked transition
//     (commitRollupStatus to PRDBlocked, reason security_failure_block,
//     the one-line report summary as the reason text), builds its triage
//     record with triageFresh, sets Security and saves it through
//     SaveTriageRecord; in shadow mode it appends a would-do entry with
//     action stop to the repo record and changes no Automaton,
//  5. stores the report on the repo record, and in on mode only raises
//     the report alert through the coordinator alert sink, immediately
//     and not after triage_alert_minutes.
//
// The pass never reruns, cancels or deletes a run, never writes
// the generated ignore file, docs/security-review.md or
// security/accepted-risks.yml, and does not touch the repo CI watch
// action phase (repo_ci_watch_actions.go).
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// securityFailureWouldDoStop is the action name of the shadow-mode
// would-do entry: the stop the pass would have applied to the linked
// Automaton.
const securityFailureWouldDoStop = "stop"

// securityFailureBlockReason is the reason the blocked transition
// records (the executor's ci_watch_block twin).
const securityFailureBlockReason = "security_failure_block"

// defaultSecurityFailureLog returns the failed jobs' log of one run
// (gh run view <runID> --log-failed --repo <owner/repo>). On a gh
// failure it returns whatever gh did print together with the error.
func defaultSecurityFailureLog(ctx context.Context, ownerRepo string, runID int64) (string, error) {
	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, ghBin, "run", "view", fmt.Sprint(runID), "--log-failed", "--repo", ownerRepo) // #nosec G204 -- fixed gh subcommand; runID is numeric, ownerRepo comes from git's own output
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("gh run view %d --log-failed: %w", runID, err)
	}
	return string(out), nil
}

// securityFailurePass is the pass described in the file header.
func (c *Coordinator) securityFailurePass() {
	if c == nil || c.m == nil {
		return
	}
	cfg := c.m.Config().Automation
	mode := cfg.SecurityFailure.EffectiveMode()
	if mode != SecurityFailureModeShadow && mode != SecurityFailureModeOn {
		return // off (or anything that is not shadow/on) does nothing at all
	}
	workflows := map[string]bool{}
	for _, w := range cfg.RepoCIWatch.EffectiveSecurityWorkflows() {
		workflows[w] = true
	}
	if len(workflows) == 0 {
		return
	}
	var todo []*RepoCIWatchRecord
	for _, rec := range c.m.ListRepoCIWatchRecords() {
		if rec == nil || rec.Security != nil {
			continue // assessed once per run
		}
		if rec.Status != repoCIWatchStatusFailing || rec.Classification != repoCIWatchClassSecurity {
			continue
		}
		if !workflows[rec.Workflow] {
			continue
		}
		todo = append(todo, rec)
	}
	if len(todo) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.m.ciWatchTimeout())
	defer cancel()
	for _, rec := range todo {
		c.securityFailureRecord(ctx, mode, cfg.RepoCIWatch, rec)
	}
}

// securityFailureRecord assesses one new record and applies the result.
func (c *Coordinator) securityFailureRecord(ctx context.Context, mode string, watch RepoCIWatchConfig, rec *RepoCIWatchRecord) {
	m := c.m
	now := c.now()

	// 1. The failed log, through the seam. A gh failure still hands
	// over whatever gh printed; the failure itself goes in the note.
	var logText, logNote string
	if runID, ok := repoCIWatchRunIDto64(rec.RunID); ok {
		text, err := m.securityFailureLog(ctx, rec.Repo, runID)
		logText = text
		if err != nil {
			logNote = "failed log could not be read: " + err.Error()
		}
	} else {
		logNote = fmt.Sprintf("failed log could not be read: run id %q is not numeric", rec.RunID)
	}

	// 2. Assess, consult the registry and the advisory lookup, decide.
	assessment := AssessSecurityFailure(SecurityFailureAssessInput{
		Workflow:         rec.Workflow,
		RunURL:           rec.RunURL,
		HeadSHA:          rec.SHA,
		Conclusion:       "failure",
		FailedRunLogText: logText,
	})
	var registry SecurityFailureRegistryResult
	var lookup SecurityFailureAdvisoryResult
	if len(assessment.AdvisoryIDs) > 0 {
		firstID := assessment.AdvisoryIDs[0]
		found, entry, err := LookupSecurityFailureRisk(c.securityFailureRepoDir(ctx, watch, rec.Repo), firstID)
		registry = SecurityFailureRegistryResult{Found: found, Entry: entry}
		if err != nil {
			registry.Error = err.Error()
		}
		lookup = LookupSecurityAdvisory(ctx, firstID)
	}
	linked := c.securityFailureLinkedPRD(rec.SHA)
	run := SecurityFailureRunIdentity{Workflow: rec.Workflow, RunURL: rec.RunURL, SHA: rec.SHA}
	if id, ok := repoCIWatchRunIDto64(rec.RunID); ok {
		run.RunID = id
	}
	report, action := decideSecurityFailure(assessment, run, registry, lookup, mode, linked != nil)
	if logNote != "" {
		report.Note = joinSecurityNote(report.Note, logNote)
	}

	// 3. Apply: stop (on) or would-stop (shadow).
	stopped := false
	switch {
	case action == actionStopped && mode == SecurityFailureModeOn:
		if prd, ok := c.stopSecurityFailureAutomaton(linked.ID, report); ok {
			stopped = true
			c.saveSecurityFailureTriage(prd, report, now)
		} else {
			report.Action = actionReportOnly
			report.Note = joinSecurityNote(report.Note, "the linked Automaton "+linked.ID+" could not be stopped: its status changed or it still has unfinished tasks")
		}
	case action == actionWouldStop && mode == SecurityFailureModeShadow:
		if !repoCIWatchHasWouldDo(rec, securityFailureWouldDoStop, rec.RunID) {
			rec.WouldDo = append(rec.WouldDo, RepoCIWatchWouldDo{
				Action: securityFailureWouldDoStop,
				RunID:  rec.RunID,
				Reason: "security failure: would stop Automaton " + linked.ID + " (" + securityFailureSummary(report) + ")",
			})
		}
	}

	// 4. Persist the report on the repo record: from here on the run is
	// never assessed again.
	rec.Security = &report
	if _, err := m.SaveRepoCIWatchRecord(rec); err != nil {
		log.Printf("[autonomous] security failure: saving report for run %s of %s: %v", rec.RunID, rec.Repo, err)
		return // not stored: the next tick assesses (and alerts) again
	}

	// 5. The alert: on mode only, once per run, immediately.
	if mode == SecurityFailureModeOn {
		name := ""
		if stopped && linked != nil {
			name = linked.Title
			if name == "" {
				name = linked.ID
			}
		}
		c.alert("warn", securityFailureAlertTitle(report, name), securityFailureAlertBody(report, name))
	}
}

// securityFailureRepoDir returns the configured repository directory
// whose origin is ownerRepo ("" when none matches — the registry read
// then fails and is reported as registry_error, never as keep).
func (c *Coordinator) securityFailureRepoDir(ctx context.Context, watch RepoCIWatchConfig, ownerRepo string) string {
	d := c.m.repoCIWatchDeps()
	for _, dir := range watch.EffectiveRepos() {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		origin, err := d.origin(ctx, dir)
		if err != nil {
			continue
		}
		if githubOwnerRepo(origin) == ownerRepo {
			return dir
		}
	}
	return ""
}

// securityFailureLinkedPRD returns the Automaton whose last completed
// task post-verify SHA equals sha (nil when none does).
func (c *Coordinator) securityFailureLinkedPRD(sha string) *PRD {
	if strings.TrimSpace(sha) == "" {
		return nil
	}
	for _, p := range c.m.store.ListPRDs() {
		snap, ok := c.m.store.SnapshotPRD(p.ID)
		if !ok {
			continue
		}
		if prdLastPostVerifySHA(snap) == sha {
			return snap
		}
	}
	return nil
}

// stopSecurityFailureAutomaton stops the Automaton by the executor's
// own blocked transition (commitRollupStatus to PRDBlocked, the
// ci_watch_block case) and returns the stored PRD afterwards.
func (c *Coordinator) stopSecurityFailureAutomaton(prdID string, report SecurityFailureReport) (*PRD, bool) {
	m := c.m
	snap, ok := m.store.SnapshotPRD(prdID)
	if !ok {
		return nil, false
	}
	rev, ok := m.store.PRDRev(prdID)
	if !ok {
		return nil, false
	}
	old, ok := m.commitRollupStatus(snap, snap.Status, rev, PRDBlocked, securityFailureBlockReason, securityFailureSummary(report))
	if !ok {
		return nil, false
	}
	stopped, ok := m.store.SnapshotPRD(prdID)
	if !ok {
		return nil, false
	}
	m.emitPRDStatus(stopped, old, nil, nil)
	return stopped, true
}

// saveSecurityFailureTriage builds the stopped Automaton's triage record
// with triageFresh, sets its Security field and saves it through the
// existing triage save path. The record is stamped alerted: the report
// alert is raised at once, so triage_alert_minutes must not alert again.
func (c *Coordinator) saveSecurityFailureTriage(prd *PRD, report SecurityFailureReport, now time.Time) {
	fresh := c.triageFresh(prd, now)
	if fresh == nil {
		log.Printf("[autonomous] security failure: no triage record for stopped automaton %s", prd.ID)
		return
	}
	r := report
	fresh.Security = &r
	stamp := now
	fresh.AlertedAt = &stamp
	if err := c.m.SaveTriageRecord(prd.ID, fresh); err != nil {
		log.Printf("[autonomous] security failure: triage record for %s: %v", prd.ID, err)
	}
}

func joinSecurityNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// securityFailureSubject names what failed: the dependency when the log
// stated one, the workflow otherwise.
func securityFailureSubject(r SecurityFailureReport) string {
	if dep := strings.TrimSpace(r.Dependency); dep != "" {
		return dep
	}
	return r.Workflow
}

func securityFailureOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

// securityFailureSummary is the one-line summary of the report used as
// the blocked reason text.
func securityFailureSummary(r SecurityFailureReport) string {
	ids := securityFailureOrUnknown(strings.Join(r.AdvisoryIDs, ", "))
	return fmt.Sprintf("security failure in %s (%s): advisory %s, recommendation %s, run %s",
		securityFailureSubject(r), r.Workflow, ids, r.Recommendation, r.RunURL)
}

// securityFailureAlertTitle is the report alert's title.
func securityFailureAlertTitle(r SecurityFailureReport, stoppedName string) string {
	if stoppedName != "" {
		return fmt.Sprintf("Automaton %q stopped: security failure in %s", stoppedName, securityFailureSubject(r))
	}
	return fmt.Sprintf("Automaton security failure: %s (%s)", securityFailureSubject(r), r.Workflow)
}

// securityFailureAlertBody carries the dependency, the advisory
// identifier, the publication date (or unknown), the registry entry and
// its dates, and the recommendation.
func securityFailureAlertBody(r SecurityFailureReport, stoppedName string) string {
	var b strings.Builder
	if stoppedName != "" {
		fmt.Fprintf(&b, "Automaton %q was stopped (blocked) because a security run failed on its commit %s.\n", stoppedName, r.SHA)
	} else {
		fmt.Fprintf(&b, "Automaton stopped nothing: a security run failed on commit %s and no Automaton was stopped.\n", r.SHA)
	}
	fmt.Fprintf(&b, "Workflow: %s\nRun: %s\n", r.Workflow, r.RunURL)
	fmt.Fprintf(&b, "Dependency: %s\n", securityFailureOrUnknown(r.Dependency))
	fmt.Fprintf(&b, "Advisory: %s\n", securityFailureOrUnknown(strings.Join(r.AdvisoryIDs, ", ")))
	fmt.Fprintf(&b, "Published: %s\n", securityFailureOrUnknown(r.PublishedAt))
	switch {
	case r.RegistryFound:
		fmt.Fprintf(&b, "Registry entry: %s (first added %s, added %s, expires %s)\n",
			r.RegistryID, securityFailureOrUnknown(r.RegistryFirstAdded), securityFailureOrUnknown(r.RegistryAdded), securityFailureOrUnknown(r.RegistryExpires))
	case r.RegistryError != "":
		fmt.Fprintf(&b, "Registry entry: unreadable (%s)\n", r.RegistryError)
	default:
		b.WriteString("Registry entry: none\n")
	}
	if r.FixedVersion != "" {
		fmt.Fprintf(&b, "Fixed version: %s\n", r.FixedVersion)
	}
	if r.LookupError != "" {
		fmt.Fprintf(&b, "Advisory lookup failed: %s\n", r.LookupError)
	}
	fmt.Fprintf(&b, "Recommendation: %s", r.Recommendation)
	if r.Note != "" {
		fmt.Fprintf(&b, "\nNote: %s", r.Note)
	}
	return b.String()
}
