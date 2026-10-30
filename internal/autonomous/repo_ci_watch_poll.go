// repo_ci_watch_poll.go — the coordinator's repo CI watch poll pass, the
// part of autonomous.automation.repo_ci_watch that observes an
// UNOWNED (non-PRD) repository's CI without judging its code: one pass
// per idle-loop tick (automation_idle.go calls c.repoCIWatchPass()
// beside checkIdle/checkBudget/triagePass), gated on the mode being on
// or shadow, at least one configured repository, and poll_seconds
// (repo_ci_watch_poll_seconds, effectively >= 60) having elapsed since
// the pass's last run. Every default comes from the Effective*
// accessors in repo_ci_watch_config.go; no default is hard-coded here.
//
// # What one pass does, per configured repository
//
//  1. `git fetch --tags origin` (bounded by the ciWatchTimeout
//     pattern) so the local view of origin/main and the release tags is
//     current. A failed fetch is logged and tolerated — the pass then
//     uses the local (possibly stale) view — the repository's head is
//     refs/remotes/origin/main, and a repository missing that ref is
//     skipped (it cannot be watched).
//
//  2. The watched commit set is the head (origin/main's SHA, ref kind
//     "main", ref name "main") plus the NEWEST tag_count release tags
//     (repo_ci_watch_tag_count, in `git tag --sort=-v:refname` order,
//     filtered to v<major>.<minor>.<patch>-shaped names — a "v2" marker
//     is not a release), each peeled to its commit. A tag unreadable
//     after the fetch is skipped — not an error — and the rest of the
//     pass continues.
//
//  3. Each SHA is listed once (`gh run list --commit <sha>`, one list
//     per SHA) and every run in that list ends in exactly one
//     classification:
//
//     - green       — conclusion success/skipped/neutral (status green,
//     no classification; the run needs no attention).
//     - infra       — conclusion cancelled/timed_out/startup_failure/
//     stale: the pipeline itself was interrupted, not
//     the code it ran. Status failing, classification
//     infra. Never judged.
//     - security    — a completed run whose workflow name is in
//     repo_ci_watch_security_workflows: it never goes to
//     the judge (judge-once-per-run still applies to the
//     cached verdict, but a fresh judge call is never made
//     for a security run). Status failing,
//     classification security.
//     - policy      — a failed run (conclusion failure, action_required,
//     or an empty conclusion that is not in progress) in
//     a non-security workflow whose judge verdict
//     (verdict.Category, see ciWatchJudge's mapping) is
//     "policy". Status failing, classification policy.
//     - code_or_test— a failed run in a non-security workflow the judge
//     cleared of policy (any other verdict category).
//     Status failing, classification code_or_test.
//     - judging     — a failed run in a non-security workflow the pass
//     could not get a verdict for this poll (the one-judge
//     budget is spent, a prior poll left it mid-judge, or
//     this pass's judge call failed). Status failing,
//     classification judging. A later pass reuses the
//     cached verdict if one exists, else re-judges (the
//     verdict cache is per run ID, so a run is never
//     judged twice once a verdict exists).
//     - hung        — an in-flight run (queued/in_progress) older than
//     hung_after_minutes. Status hung, classification
//     hung. Never judged.
//     - none        — an in-flight run not yet past hung_after_minutes.
//     Status pending, classification none ("none" is the
//     unclassified record value the builder defaults).
//
// # Judge discipline
//
// The extracted judge (m.repoCIJudgeCall, the repo path + label variant
// of the PRD's ciWatchJudge — ci_watch.go) is called for a failed run
// at most once per run ID (a cached verdict in the record is never
// re-judged) and at most once per poll pass in total (one judge call
// for the whole pass, across all repositories, refs and runs). The
// first failed, non-security run the pass reaches receives the call;
// the rest record "judging" and are picked up by a later pass.
//
// # Records
//
// Every run in every listed set is recorded through the RepoCIWatchStore
// (the Manager's m.repoCIWatch, via m.SaveRepoCIWatchRecord /
// m.ListRepoCIWatchRecords), keyed by repo, ref kind, ref name, sha,
// workflow and run ID; a new record's FirstSeenAt is minted at the
// builder and preserved across refreshes. No alert is raised here — the
// alert (when AlertMinutes, effectively >= 0, elapses for a
// failing/hung record) is a later step's job, and superseded-run
// cleanup is likewise deferred (delete_superseded is a later step's
// input, not this pass's).
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// repoCIWatchJudgeFunc is the judge seam's signature: (repository path,
// label describing the failed run) → guardrail verdict. The production
// implementation is Manager.repoCIJudgeCall (ci_watch.go); tests inject
// a fake through Manager.repoCIJudgeFn.
type repoCIWatchJudgeFunc func(ctx context.Context, repoPath, label string) (GuardrailVerdict, error)

// repoCIWatchJudge returns the judge in effect for the repo CI watch:
// the test seam (m.repoCIJudgeFn) when set, otherwise the production
// invocation. Both are nil-safe — a Manager with no guardrail at all
// returns an error from the call, never a panic.
func (m *Manager) repoCIWatchJudge() repoCIWatchJudgeFunc {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	fn := m.repoCIJudgeFn
	m.mu.Unlock()
	if fn != nil {
		return fn
	}
	return m.repoCIJudgeCall
}

// repoCIWatchJudgeBudget — at most this many judge calls per poll pass,
// across every repository, ref and run.
const repoCIWatchJudgeBudget = 1

// repoCIWatchPass is the repo CI watch poll pass (see the file header
// for the pass-wide behavior). It runs on the idle loop's tick, beside
// checkIdle/checkBudget/triagePass; it is silent by design — a pass is
// the normal path, and only anomalies (a failed fetch, an unresolvable
// head, a failed judge call) are logged.
func (c *Coordinator) repoCIWatchPass() {
	if c == nil || c.m == nil {
		return
	}
	cfg := c.m.Config().Automation.RepoCIWatch
	// off (and an empty, unset mode reads off) or nothing configured:
	// there is no pass at all, not even a gated one.
	if cfg.EffectiveMode() == RepoCIWatchModeOff {
		return
	}
	if len(cfg.EffectiveRepos()) == 0 {
		return
	}
	interval := time.Duration(cfg.EffectivePollSeconds()) * time.Second
	if interval <= 0 {
		return // defensive; the write paths refuse < 60
	}
	now := c.now()
	c.mu.Lock()
	if !c.repoCIWatchLastPoll.IsZero() && now.Sub(c.repoCIWatchLastPoll) < interval {
		c.mu.Unlock()
		return
	}
	c.repoCIWatchLastPoll = now
	c.mu.Unlock()
	c.runRepoCIWatchPoll(now, cfg)
}

// RepoCIWatchLastPoll returns the instant the coordinator last ran a
// repo CI watch poll pass: the zero time when it never has (the
// Coordinator's fields are guarded by c.mu; a nil Coordinator reports
// the zero time). Read-only.
func (c *Coordinator) RepoCIWatchLastPoll() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.repoCIWatchLastPoll
}

// repoCIWatchPollContext carries one poll pass's state: the shared
// (timeout-bounded) context, the pass instant (records mint at now, not
// at each store round trip), the effective config, the judge
// discipline (one call per pass, never one already budgeted), and —
// for the action phase (repo_ci_watch_actions.go) — the coordinator
// (alerts raise through it) and the superseded pairs this pass itself
// named (step 1), which step 2 must leave to a later poll.
type repoCIWatchPollContext struct {
	m                  *Manager
	co                 *Coordinator // alerts raise through Coordinator.alert
	ctx                context.Context
	now                time.Time
	cfg                RepoCIWatchConfig
	judge              repoCIWatchJudgeFunc
	budget             int                 // remaining judge calls this pass (repoCIWatchJudgeBudget)
	judgedIDS          map[string]struct{} // run IDs already budgeted this pass
	supersededThisPass map[string]struct{} // "recordKey\x00runID" pairs named this pass
	log                func(format string, args ...any)
}

// runRepoCIWatchPoll runs the ungated pass: for each configured
// repository, resolve the head and tag set and record every run.
func (c *Coordinator) runRepoCIWatchPoll(now time.Time, cfg RepoCIWatchConfig) {
	m := c.m
	ctx, cancel := context.WithTimeout(context.Background(), m.ciWatchTimeout())
	defer cancel()
	p := &repoCIWatchPollContext{
		m:                  m,
		co:                 c,
		ctx:                ctx,
		now:                now,
		cfg:                cfg,
		judge:              m.repoCIWatchJudge(),
		budget:             repoCIWatchJudgeBudget,
		judgedIDS:          map[string]struct{}{},
		supersededThisPass: map[string]struct{}{},
	}
	repos := cfg.EffectiveRepos()
	for _, repoDir := range repos {
		repoDir = strings.TrimSpace(repoDir)
		if repoDir == "" {
			continue // defensive; validated on every write path
		}
		p.pollRepo(m.repoCIWatchDeps(), repoDir)
	}
}

// repoCIWatchRef is one watched ref of a repository: its kind
// ("main" or "tag"), its name and the commit it resolved to.
type repoCIWatchRef struct {
	kind string
	name string
	sha  string
}

// pollRepo runs one repository through the pass (fetch, resolve, list,
// record). Every step's failure is logged and tolerated where the pass
// can continue (a failed fetch → stale view; an unreadable tag → skip
// the tag; a failed run list → skip the SHA); a repository whose head
// cannot be resolved is skipped outright.
func (p *repoCIWatchPollContext) pollRepo(d repoCIWatchDeps, repoDir string) {
	// 1. Refresh the local view of origin and its tags.
	if err := d.fetch(p.ctx, repoDir); err != nil {
		log.Printf("repo ci watch: %s: git fetch --tags origin failed (continuing with the local view): %v", repoDir, err)
	}
	// owner/repo comes from the local origin URL (a repository with no
	// recognisable github origin is not listable by gh and is skipped).
	originURL, err := d.origin(p.ctx, repoDir)
	if err != nil {
		log.Printf("repo ci watch: %s: origin is not resolvable (%v); skipping this repository", repoDir, err)
		return
	}
	ownerRepo := githubOwnerRepo(originURL)
	if ownerRepo == "" {
		log.Printf("repo ci watch: %s: origin %q is not a github origin; skipping this repository", repoDir, originURL)
		return
	}
	// 2. Resolve the head (origin/main) and the newest tag_count tags.
	headSHA, err := d.head(p.ctx, repoDir)
	if err != nil {
		log.Printf("repo ci watch: %s: %v; skipping this repository", repoDir, err)
		return
	}
	refs := []repoCIWatchRef{{kind: "main", name: "main", sha: headSHA}}
	if tagCount := p.cfg.EffectiveTagCount(); tagCount > 0 {
		tags, terr := d.tags(p.ctx, repoDir)
		if terr != nil {
			// The tags are not required for the head to be covered.
			log.Printf("repo ci watch: %s: listing release tags failed (main is still covered): %v", repoDir, terr)
		} else {
			for i, tag := range tags {
				if i >= tagCount {
					break
				}
				sha, perr := d.peel(p.ctx, repoDir, tag)
				if perr != nil {
					// One unreadable tag skips that tag — not the pass.
					log.Printf("repo ci watch: %s: tag %s is unreadable (skipping it): %v", repoDir, tag, perr)
					continue
				}
				refs = append(refs, repoCIWatchRef{kind: "tag", name: tag, sha: sha})
			}
		}
	}
	// 3. One run list per SHA, then record every run.
	for _, ref := range refs {
		p.pollRef(ownerRepo, repoDir, ref)
	}
}

// pollRef lists the runs at one ref's SHA and records each; then it runs
// the action phase (repo_ci_watch_actions.go) for the ref's records in
// the spec's two-step order: first the superseded naming (step 1) for
// the ref's green runs, so every same-ref record is stored and visible,
// and then per record the cancel/rerun/delete decision (step 2) and the
// alert.
func (p *repoCIWatchPollContext) pollRef(ownerRepo, repoDir string, ref repoCIWatchRef) {
	runs, err := p.m.repoCIWatchDeps().runs(p.ctx, ownerRepo, ref.sha)
	if err != nil {
		log.Printf("repo ci watch: %s %s@%s: listing runs failed: %v", ownerRepo, ref.name, ref.sha[:8], err)
		return
	}
	var recorded []*RepoCIWatchRecord
	for _, run := range runs {
		if rec := p.recordRun(ownerRepo, repoDir, ref, run); rec != nil {
			recorded = append(recorded, rec)
		}
	}
	// Step 1: a green run can name earlier failed runs of the same
	// workflow it superseded; run it over all of this ref's green runs
	// (a failing run named in step 1 may itself have been recorded after
	// this green one in the same list).
	for i, rec := range recorded {
		if rec.Status == repoCIWatchStatusGreen {
			recorded[i] = p.markRepoCIWatchSuperseded(repoDir, rec)
		}
	}
	// Step 2 (and the alert): the writes of this pass, each either a real
	// GitHub write (mode on) or a would_do stand-in (shadow).
	for i, rec := range recorded {
		recorded[i] = p.applyRepoCIWatchActions(ownerRepo, rec)
	}
}

// recordRun classifies one run, looks up (or mints) its record, and
// saves it through the Manager's store accessor; it returns the saved
// record (nil when the run cannot be recorded).
func (p *repoCIWatchPollContext) recordRun(ownerRepo, repoDir string, ref repoCIWatchRef, run RepoCIWatchRun) *RepoCIWatchRecord {
	runID := repoCIWatchRunID(run)
	if runID == "" {
		log.Printf("repo ci watch: %s: a listed run in %s@%s has no run ID; it cannot be recorded", ownerRepo, ref.name, ref.sha[:8])
		return nil
	}
	key := RepoCIWatchKey(ownerRepo, ref.kind, ref.name, ref.sha, run.WorkflowName, runID)

	// The stored record (if any) carries the run's lifetime: FirstSeenAt
	// is preserved across refreshes, a cached judge verdict is the
	// once-per-run source of truth, and the action phase's state — the
	// automatic-action log, the would_do stand-ins, the superseded list,
	// the alert stamp and any notes — must survive a reclassification,
	// so every refresh carries them over.
	var existing *RepoCIWatchRecord
	if s := p.m.repoCIWatch; s != nil {
		if rec, ok := s.Get(key); ok {
			existing = rec
		}
	}

	status, classification, verdict := p.classify(ownerRepo, repoDir, runID, existing, run)
	rec, err := BuildRepoCIWatchRecord(ownerRepo, ref.kind, ref.name, ref.sha, run.WorkflowName, runID, run.URL, status, classification, p.now)
	if err != nil {
		log.Printf("repo ci watch: recording %s %s for run %s: %v", ownerRepo, ref.name, runID, err)
		return nil
	}
	if existing != nil {
		rec.FirstSeenAt = existing.FirstSeenAt
		rec.RerunLog = existing.RerunLog
		rec.WouldDo = existing.WouldDo
		rec.SupersededRunIDs = existing.SupersededRunIDs
		rec.AlertedAt = existing.AlertedAt
		rec.Notes = existing.Notes
	}
	rec.JudgeVerdict = verdict
	if saved, err := p.m.SaveRepoCIWatchRecord(rec); err != nil {
		log.Printf("repo ci watch: saving record %s: %v", key, err)
		return nil
	} else {
		return saved
	}
}

// classifyRun is the per-run classification (see the file header for the
// full vocabulary and precedence). The order is fixed: green and infra
// never judge; hung and pending are in-flight only; security never
// judges; policy and code_or_test come from the judge; judging is the
// "no verdict available this poll" state.
func (p *repoCIWatchPollContext) classify(ownerRepo, repoDir, runID string, existing *RepoCIWatchRecord, run RepoCIWatchRun) (string, string, *RepoCIWatchJudgeVerdict) {
	switch run.Conclusion {
	case "success", "skipped", "neutral":
		return "green", "none", nil
	case "cancelled", "timed_out", "startup_failure", "stale":
		return "failing", "infra", nil
	}

	// In-flight runs: hung past the threshold, else pending. An in-flight
	// run is never judged (there is nothing to judge yet) and never
	// security-classified — the security set applies to completed runs.
	if run.Status == "queued" || run.Status == "in_progress" {
		age := p.runAge(run)
		hungAfter := time.Duration(p.cfg.EffectiveHungAfterMinutes()) * time.Minute
		if age >= hungAfter {
			return "hung", "hung", nil
		}
		return "pending", "none", nil
	}

	// A completed run in a security workflow: never judged, no matter
	// its conclusion (a green one was already green above).
	name := strings.TrimSpace(run.WorkflowName)
	if p.securitySet()[name] {
		return "failing", "security", nil
	}

	// A failed, non-security, completed run: judge-once-per-run,
	// at-most-one-judge-per-pass (see the file header's discipline).
	if cached := p.cachedVerdict(existing, runID); cached != nil {
		return p.verdictClass(cached)
	}
	if p.budget <= 0 {
		return "failing", "judging", nil
	}
	if _, taken := p.judgedIDS[runID]; taken {
		return "failing", "judging", nil
	}
	p.judgedIDS[runID] = struct{}{}
	p.budget-- // one call spent this pass (at-most-one-judge-per-poll)
	label := repoCIWatchJudgeLabel(ownerRepo, refName(run.WorkflowName), run)
	v, err := p.judge(p.ctx, repoDir, label)
	if err != nil {
		log.Printf("repo ci watch: judging %s run %s: %v (record stays \"judging\")", ownerRepo, runID, err)
		return "failing", "judging", nil
	}
	verdict := &RepoCIWatchJudgeVerdict{RunID: runID, Verdict: v.Category}
	if v.Category == "policy" {
		return "failing", "policy", verdict
	}
	return "failing", "code_or_test", verdict
}

// cachedVerdict finds the run's cached verdict on its stored record.
func (p *repoCIWatchPollContext) cachedVerdict(existing *RepoCIWatchRecord, runID string) *RepoCIWatchJudgeVerdict {
	if existing == nil || existing.JudgeVerdict == nil || existing.JudgeVerdict.RunID != runID {
		return nil
	}
	return existing.JudgeVerdict
}

// verdictClass maps a cached verdict to the run's final class: a
// "policy" verdict is a policy violation, everything else the judge
// resolved is a code/test defect.
func (p *repoCIWatchPollContext) verdictClass(v *RepoCIWatchJudgeVerdict) (string, string, *RepoCIWatchJudgeVerdict) {
	if v.Verdict == "policy" {
		return "failing", "policy", v
	}
	return "failing", "code_or_test", v
}

// runAge is how long the run has been out: from its start (its creation
// when it never started) to the pass instant.
func (p *repoCIWatchPollContext) runAge(run RepoCIWatchRun) time.Duration {
	t := run.StartedAt
	if t.IsZero() {
		t = run.CreatedAt
	}
	if t.IsZero() {
		return 0
	}
	if d := p.now.Sub(t); d > 0 {
		return d
	}
	return 0
}

// securitySet returns this pass's effective security workflow set
// (repo_ci_watch_security_workflows).
func (p *repoCIWatchPollContext) securitySet() map[string]bool {
	s := make(map[string]bool, len(p.cfg.EffectiveSecurityWorkflows()))
	for _, w := range p.cfg.EffectiveSecurityWorkflows() {
		s[strings.TrimSpace(w)] = true
	}
	return s
}

// repoCIWatchRunID renders a run's ID for the record key (an absent ID
// is "" and such a run cannot be recorded).
func repoCIWatchRunID(run RepoCIWatchRun) string {
	if run.ID <= 0 {
		return ""
	}
	return strconv.FormatInt(run.ID, 10)
}

// refName is the label's ref name — kept tiny because it is already on
// the invocation (the run's workflow name, not the ref: the judge is
// told what the watch saw, and the workflow is the run-level fact).
func refName(workflow string) string {
	return strings.TrimSpace(workflow)
}

// repoCIWatchJudgeLabel builds the judge's label for one failed run:
// enough to name the repository, ref, commit and run without the judge
// having to ask.
func repoCIWatchJudgeLabel(ownerRepo, workflow string, run RepoCIWatchRun) string {
	return fmt.Sprintf("repository %s, workflow %q, run %q (status %s, conclusion %s, %s)",
		ownerRepo, workflow, run.Name, run.Status, run.Conclusion, run.URL)
}
