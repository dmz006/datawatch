// PRD-completion CI watch ("ci-watch" PRD-level guardrail).
//
// Incident (2026-10-13): an automaton whose last task committed and
// pushed a release commit rolled up to PRDCompleted while the CI run
// for that very commit was still running — and that run then failed.
// Nothing in the system noticed. Both existing completion checks
// (runPRDCompletionProjectRulesCheck and runPRDCompletionRulesReview)
// judge only the LOCAL diff; neither asks "did the remote build agree
// with what this automaton just pushed?". "Completed" therefore meant
// "the local work finished," not "the shipped commit is green."
//
// Design:
//
//   - Watches the PRD's own commit, not whatever live HEAD happens to be at
//     roll-up time (2026-10-13 incident, root cause iii — a sibling PRD or
//     the operator can move the same tree between the last task verifying
//     and the roll-up, so "HEAD" no longer names the work that was just
//     shipped). Each task records its post-verify HEAD in PostVerifySHA
//     (executor.go captures git rev-parse HEAD right where it sets
//     TaskCompleted; BL465 fires the deterministic commit at
//     DATAWATCH_COMPLETE, before verify, so that IS the task's own commit).
//     ci-watch pins the last completed task's PostVerifySHA and, for
//     reachability, tests that pinned SHA (not live HEAD) against the
//     remote-tracking refs — git merge-base --is-ancestor <sha> <ref>
//     against each ref (equivalently git branch -r --contains <sha>) — so
//     a pinned commit that was pushed but is no longer a branch tip still
//     reads as pushed (not tip equality, ancestry). The pinned SHA is also
//     what gets listed and judged on. Only when no task recorded a pin
//     (legacy stores, capture failure) does ci-watch fall back to live
//     HEAD exactly as before.
//
//   - Push-mechanism-agnostic. ci-watch never pushes and never cares
//     HOW a commit reached the remote (a task's own `git push`, a
//     cluster push, a release step). It only acts when the final commit
//     (the pinned one, or live HEAD when no pin exists) is already
//     reachable from a remote-tracking ref; otherwise it
//     records a skip. Consequence worth stating plainly: worktree/
//     auto-PR automata push inside the PRDCompleted branch
//     (handleWorktreeCompletion) AFTER this check runs, so for them HEAD
//     is usually not pushed yet and ci-watch skips. That is deliberate —
//     handleWorktreeCompletion is not moved; their CI is the PR's own
//     checks, reviewed on the PR.
//
//   - Same opt-in shape as rules-review: fires only when "ci-watch" is
//     in resolveGuardrails(prd, "prd") (PRD.PerPRDGuardrails >
//     GuardrailProfile > Config.PerPRDGuardrails, empty by default).
//     GitHub Actions via the `gh` CLI only; anything else (no origin,
//     non-GitHub host, gh missing) is a recorded skip, never an error.
//
//   - Judgment against the operator's own rules file, never hardcoded
//     rule content (the operator decision recorded at the top of
//     rules_review.go). A failed run is not auto-classified by job name
//     or by a fixed list of finding types: ciWatchSuffix points the
//     configured judge at the configured rules/context file
//     (m.rulesContextFilenames) and at the real failure logs, and the
//     judge decides — including whether a project rule says an operator
//     must decide ("category":"policy") rather than auto-fix.
//
//   - Reuses the existing failure/retry machinery instead of a new
//     loop: runGuardrailWithInfraRetry for the judge session,
//     retryBudgetRemaining/consumeRetryBudget/recordAutoRetryDecision
//     for re-running CI runs the CI machinery itself broke (cancelled/
//     timed_out/startup_failure/stale), the content auto-fix condition
//     shared with rules-review (handing a contentFixVerdict back for the
//     caller to synthesizeCorrectiveWork from), and recordFailureLearning
//     once a failure is genuinely unresolved.
//
//   - Never holds an automaton hostage to slow CI: if runs are still
//     queued/in progress when the ci-watch timeout (ciWatchTimeout)
//     elapses, the automaton completes with CIWatch.Status "pending" and
//     exactly one background follow-up check records the eventual
//     result (it cannot re-open a completed automaton; it records,
//     learns and logs).
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// defaultCIPollInterval is how often ci-watch re-lists a commit's CI
// runs when Manager.ciPollInterval is unset.
const defaultCIPollInterval = 30 * time.Second

// ciFollowUpCallTimeout bounds the single background follow-up's one
// `gh run list` call (it has no caller context to inherit).
const ciFollowUpCallTimeout = 2 * time.Minute

// ciFetchTimeout bounds defaultCIGit's best-effort `git fetch`.
const ciFetchTimeout = 60 * time.Second

// ghAvailable reports whether the gh CLI resolves on $PATH. A var so
// tests can override it without touching the real environment.
var ghAvailable = func() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// ciWatchDeps is a snapshot of the ci-watch seams with defaults applied.
type ciWatchDeps struct {
	git      func(ctx context.Context, projectDir string) (string, string, bool, error)
	gitSHA   func(ctx context.Context, projectDir, sha string) (string, bool, error) // pinned-SHA variant; nil → defaultCIGitSHA
	runs     func(ctx context.Context, ownerRepo, sha string) ([]CIWatchRun, error)
	rerun    func(ctx context.Context, ownerRepo string, runID int64) error
	poll     time.Duration
	followUp time.Duration
	timeout  time.Duration
}

func (m *Manager) ciWatchDeps() ciWatchDeps {
	m.mu.Lock()
	d := ciWatchDeps{git: m.ciGitFn, gitSHA: m.ciGitSHAFn, runs: m.ciRunsFn, rerun: m.ciRerunFn, poll: m.ciPollInterval, followUp: m.ciFollowUpDelay}
	m.mu.Unlock()
	if d.git == nil {
		d.git = defaultCIGit
	}
	if d.gitSHA == nil {
		d.gitSHA = defaultCIGitSHA
	}
	if d.runs == nil {
		d.runs = defaultCIRuns
	}
	if d.rerun == nil {
		d.rerun = defaultCIRerun
	}
	if d.poll <= 0 {
		d.poll = defaultCIPollInterval
	}
	d.timeout = m.ciWatchTimeout() // takes m.mu itself
	if d.followUp <= 0 {
		d.followUp = d.timeout
	}
	return d
}

// defaultCIGit reads HEAD and the origin URL in projectDir and decides
// whether HEAD has reached a remote.
//
// An err means projectDir is not a usable git repository (HEAD
// unreadable). A missing origin remote is reported as originURL == ""
// with a nil error, so the caller can tell the two skip reasons apart.
//
// "Pushed" is decided by a single check: `git branch -r --contains
// <sha>` is non-empty, i.e. HEAD is reachable from some remote-tracking
// ref. Chosen over scanning `git ls-remote origin` because ls-remote
// only lists ref TIPS — if anything lands on the branch after this
// automaton's push, its commit is no longer a tip and would read as
// unpushed even though it is on the remote. Reachability handles that,
// and it needs no network access to be correct for the common case: a
// successful `git push` updates the matching remote-tracking ref
// locally. The preceding `git fetch` (bounded, failure tolerated) only
// refreshes those refs for pushes made from a different clone.
func defaultCIGit(ctx context.Context, projectDir string) (headSHA, originURL string, pushed bool, err error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", "", false, err
	}
	run := func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, gitBin, args...) // #nosec G204 -- fixed git subcommands; projectDir and sha come from the PRD record and git's own output
		cmd.Dir = projectDir
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	headSHA, err = run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", "", false, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	originURL, err = run(ctx, "remote", "get-url", "origin")
	if err != nil || originURL == "" {
		return headSHA, "", false, nil
	}
	fctx, cancel := context.WithTimeout(ctx, ciFetchTimeout)
	if _, ferr := run(fctx, "fetch", "--quiet", "origin"); ferr != nil {
		log.Printf("[autonomous] ci-watch: git fetch in %s failed (continuing with local remote-tracking refs): %v", projectDir, ferr)
	}
	cancel()
	refs, err := run(ctx, "branch", "-r", "--contains", headSHA)
	return headSHA, originURL, err == nil && refs != "", nil
}

// defaultCIGitSHA is the pinned-SHA variant of defaultCIGit: it reports
// the origin URL for projectDir and whether the given sha is reachable
// from ANY remote-tracking ref. Unlike defaultCIGit it never reads live
// HEAD — the caller pins the SHA from the last completed task's
// PostVerifySHA, and for a commit that is no longer a branch tip,
// "pushed" is decided by ancestry, not tip equality.
//
// Reachability check: git merge-base --is-ancestor <sha> <ref> against
// every remote-tracking ref (git branch -r), skipping the origin/HEAD
// decoration line — exit 0 means the pinned sha is an ancestor of (or
// equal to) that ref's tip, i.e. it was pushed.
func defaultCIGitSHA(ctx context.Context, projectDir, sha string) (originURL string, pushed bool, err error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", false, err
	}
	run := func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, gitBin, args...) // #nosec G204 -- fixed git subcommands; projectDir comes from the PRD record and sha was produced by git itself
		cmd.Dir = projectDir
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := run(ctx, "rev-parse", "--git-dir"); err != nil {
		return "", false, fmt.Errorf("git rev-parse --git-dir: %w", err)
	}
	originURL, err = run(ctx, "remote", "get-url", "origin")
	if err != nil || originURL == "" {
		return "", false, nil
	}
	fctx, cancel := context.WithTimeout(ctx, ciFetchTimeout)
	if _, ferr := run(fctx, "fetch", "--quiet", "origin"); ferr != nil {
		log.Printf("[autonomous] ci-watch: git fetch in %s failed (continuing with local remote-tracking refs): %v", projectDir, ferr)
	}
	cancel()
	refsOut, err := run(ctx, "branch", "-r")
	if err != nil {
		return originURL, false, fmt.Errorf("git branch -r: %w", err)
	}
	pushed = false
	for _, line := range strings.Split(refsOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, " -> ") { // skip the origin/HEAD decoration line
			continue
		}
		// exit 0 ⇔ sha is an ancestor of (or equal to) that ref's tip —
		// the pinned commit was pushed.
		cmd := exec.CommandContext(ctx, gitBin, "merge-base", "--is-ancestor", sha, line) // #nosec G204 -- fixed subcommand; sha from git's output, line from `git branch -r`
		cmd.Dir = projectDir
		if cmd.Run() == nil {
			pushed = true
			break
		}
	}
	return originURL, pushed, nil
}

// prdLastPostVerifySHA returns the PostVerifySHA of the last completed
// task in the PRD (stories and tasks in order), skipping completed tasks
// that recorded no pin, or "" when none recorded one.
func prdLastPostVerifySHA(prd *PRD) string {
	sha := ""
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.Status == TaskCompleted && t.PostVerifySHA != "" {
				sha = t.PostVerifySHA
			}
		}
	}
	return sha
}

// defaultCIRuns lists the GitHub Actions runs for sha via the gh CLI.
func defaultCIRuns(ctx context.Context, ownerRepo, sha string) ([]CIWatchRun, error) {
	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, ghBin, "run", "list", "--commit", sha, "--repo", ownerRepo, // #nosec G204 -- fixed gh subcommand; sha/ownerRepo come from git's own output
		"--json", "databaseId,name,status,conclusion,url", "--limit", "50")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh run list: %w", err)
	}
	var raw []struct {
		DatabaseID int64  `json:"databaseId"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh run list: parse: %w", err)
	}
	runs := make([]CIWatchRun, 0, len(raw))
	for _, r := range raw {
		runs = append(runs, CIWatchRun{ID: r.DatabaseID, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, URL: r.URL})
	}
	return runs, nil
}

// defaultCIRerun re-runs the failed jobs of one GitHub Actions run.
func defaultCIRerun(ctx context.Context, ownerRepo string, runID int64) error {
	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ghBin, "run", "rerun", fmt.Sprint(runID), "--failed", "--repo", ownerRepo) // #nosec G204 -- fixed gh subcommand; runID is numeric, ownerRepo comes from git's own output
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh run rerun %d: %w: %s", runID, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// githubOwnerRepo returns "owner/repo" for a github.com remote URL
// (https, ssh:// or scp-style git@github.com:owner/repo), or "" for
// any other host or an unusable path. Reuses repoFromGitURL for the
// path extraction once the host is confirmed.
func githubOwnerRepo(remote string) string {
	var host string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		host = u.Hostname()
	} else if at, colon := strings.Index(remote, "@"), strings.Index(remote, ":"); colon > 0 && at < colon {
		host = remote[at+1 : colon] // scp-style user@host:path
	}
	if !strings.EqualFold(host, "github.com") {
		return ""
	}
	repo := repoFromGitURL(remote)
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || strings.Contains(owner, ":") {
		return ""
	}
	return repo
}

// ciRunsSettled reports whether at least one run exists and every run
// has finished.
func ciRunsSettled(runs []CIWatchRun) bool {
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if r.Status != "completed" {
			return false
		}
	}
	return true
}

// classifyCIRuns splits completed runs into failures the judge must look
// at and runs the CI machinery itself broke (cancelled, timed_out,
// startup_failure, stale — the code never got a verdict). success,
// skipped and neutral are neither. Any other conclusion (failure,
// action_required, or one GitHub adds later) is treated as a failure to
// judge, so an unknown outcome is looked at rather than waved through.
func classifyCIRuns(runs []CIWatchRun) (failed, infra []CIWatchRun) {
	for _, r := range runs {
		switch r.Conclusion {
		case "success", "skipped", "neutral":
		case "cancelled", "timed_out", "startup_failure", "stale":
			infra = append(infra, r)
		default:
			failed = append(failed, r)
		}
	}
	return failed, infra
}

// pollCIRuns lists sha's runs every poll until all have completed or
// deadline passes (one final listing happens at the deadline). Zero runs
// keeps polling — CI can take a while to register a push. A listing
// error is logged and polling continues; only ctx cancellation is
// returned as an error.
func pollCIRuns(ctx context.Context, d ciWatchDeps, ownerRepo, sha string, deadline time.Time) ([]CIWatchRun, bool, error) {
	var last []CIWatchRun
	for {
		runs, err := d.runs(ctx, ownerRepo, sha)
		if err != nil {
			log.Printf("[autonomous] ci-watch: listing runs for %s@%s: %v", ownerRepo, sha, err)
		} else {
			last = runs
			if ciRunsSettled(runs) {
				return runs, true, nil
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return last, false, nil
		}
		if err := sleepCtx(ctx, min(d.poll, remaining)); err != nil {
			return last, false, err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ciRunList renders runs as "name (run id): conclusion — url" lines.
func ciRunList(runs []CIWatchRun) string {
	var b strings.Builder
	for _, r := range runs {
		concl := r.Conclusion
		if concl == "" {
			concl = r.Status
		}
		fmt.Fprintf(&b, "- %s (run %d): %s — %s\n", r.Name, r.ID, concl, r.URL)
	}
	return b.String()
}

// ciFailureIssue renders a FailureReason as one CIWatchResult.Issues
// entry, so the pending/failed record carries the same structured
// classification every other failure in this package uses.
func ciFailureIssue(fr FailureReason) string {
	return fmt.Sprintf("category=%s source=%s retryable=%t: %s", fr.Category, fr.Source, fr.Retryable, fr.Detail)
}

// recordCIWatch stamps res onto prd.CIWatch and appends the matching
// ci_watch Decision. Every ci-watch outcome goes through here.
func recordCIWatch(prd *PRD, res CIWatchResult, outcome, category string) {
	res.CheckedAt = time.Now()
	prd.CIWatch = &res
	note := fmt.Sprintf("status=%s", res.Status)
	if res.SHA != "" {
		note += " sha=" + res.SHA
	}
	var urls []string
	for _, r := range res.Runs {
		if r.URL != "" {
			urls = append(urls, r.URL)
		}
	}
	if len(urls) > 0 {
		note += " runs=" + strings.Join(urls, ",")
	}
	if res.Summary != "" {
		note += " — " + res.Summary
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: res.CheckedAt, Kind: "ci_watch", Actor: "autonomous",
		VerdictOutcome: outcome, FailureCategory: category, Note: note,
	})
}

// ciWatchSuffix is the CI sibling of rulesReviewSuffix: the judgment
// prompt for CI runs that failed after this automaton's own push. It
// names no finding types and encodes no project rule — it points the
// judge at the real logs and at whichever configured rules/context file
// exists (same detection as rulesReviewSuffix) and lets it decide. With
// no rules/context file the judge still runs, just without the pointer.
func (m *Manager) ciWatchSuffix(prd *PRD, sha, ownerRepo string, failed []CIWatchRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This automaton pushed commit %s to %s. After that push, the following CI run(s) for that exact commit did not succeed:\n%s\n", sha, ownerRepo, ciRunList(failed))
	if list := m.presentRulesContextFiles(prd); list != "" {
		b.WriteString("Read " + list + " at the root of your working directory yourself, using your own tools, to see whether this project defines how CI failures like this are to be handled. ")
	} else {
		b.WriteString("This project has no rules/context file, so judge the failure on its own merits. ")
	}
	fmt.Fprintf(&b, "Fetch the real failure logs yourself (for each run: gh run view <id> --log-failed --repo %s) rather than guessing from job or workflow names. ", ownerRepo)
	b.WriteString("Decide whether an existing project rule applies to this failure and what it says to do — in particular, check explicitly whether the rule says a human/operator must decide rather than having it fixed automatically." +
		" Respond with the usual verdict JSON (outcome pass|warn|block, severity, summary, issues)." +
		` Add "category":"policy" ONLY when a project rule requires operator sign-off for this failure;` +
		" leave category empty for a genuine, fixable problem in this automaton's own work." +
		" A failure the project's rules say is acceptable, or that is clearly unrelated to this automaton's change, may be pass or warn — do not invent a block.")
	return b.String()
}

// runPRDCompletionCIWatch fires once, right before a PRD rolls up to
// PRDCompleted (after rules-review in executor.go's rollup). See the
// file header for the design. Returns (blocked, contentFixVerdict, err)
// with exactly runPRDCompletionRulesReview's meaning: a content-category
// block eligible for auto-fix is handed back for the caller to
// synthesizeCorrectiveWork from on the SAME PRD.
//
// Skips (recorded on prd.CIWatch + a Decision; never an error, never a
// block): "ci-watch" not opted in (returns silently), no project dir,
// not a git repo, no origin remote, HEAD not pushed, non-GitHub origin,
// gh not on PATH.
func (m *Manager) runPRDCompletionCIWatch(ctx context.Context, prd *PRD) (blocked bool, contentFixVerdict *GuardrailVerdict, err error) {
	if !slices.Contains(m.resolveGuardrails(prd, "prd"), "ci-watch") {
		return false, nil, nil
	}
	skip := func(res CIWatchResult) (bool, *GuardrailVerdict, error) {
		res.Status = "skipped"
		recordCIWatch(prd, res, "n/a", "")
		return false, nil, nil
	}
	if prd.ProjectDir == "" {
		return skip(CIWatchResult{Summary: "no project directory"})
	}
	d := m.ciWatchDeps()
	var sha, origin string
	var pushed bool
	var gerr error
	if pinned := prdLastPostVerifySHA(prd); pinned != "" {
		// Pin: watch the last completed task's own commit (its
		// PostVerifySHA), not live HEAD — the tree may have moved since
		// that task verified (2026-10-13 incident, root cause iii).
		// "Pushed" means the pinned sha is reachable from a
		// remote-tracking ref (ancestry, not tip equality), and the
		// pinned sha is what gets listed and judged on below.
		sha = pinned
		origin, pushed, gerr = d.gitSHA(ctx, prd.ProjectDir, pinned)
	} else {
		// Fallback: no completed task recorded a PostVerifySHA (legacy
		// store or capture failure) — watch live HEAD exactly as before.
		sha, origin, pushed, gerr = d.git(ctx, prd.ProjectDir)
	}
	if gerr != nil {
		return skip(CIWatchResult{Summary: "not a git repository: " + gerr.Error()})
	}
	if origin == "" {
		return skip(CIWatchResult{SHA: sha, Summary: "no origin remote"})
	}
	if !pushed {
		return skip(CIWatchResult{SHA: sha, Summary: "the commit is not reachable from any remote-tracking ref (not pushed yet — e.g. a worktree/auto-PR automaton, which pushes after completion)"})
	}
	ownerRepo := githubOwnerRepo(origin)
	if ownerRepo == "" {
		return skip(CIWatchResult{SHA: sha, Summary: "origin is not a GitHub remote; only GitHub Actions is watched"})
	}
	if !ghAvailable() {
		return skip(CIWatchResult{SHA: sha, Repo: ownerRepo, Summary: "gh CLI not found on PATH"})
	}

	deadline := time.Now().Add(d.timeout)
	for {
		runs, settled, perr := pollCIRuns(ctx, d, ownerRepo, sha, deadline)
		if perr != nil {
			return false, nil, fmt.Errorf("ci watch for prd %s: %w", prd.ID, perr)
		}
		base := CIWatchResult{SHA: sha, Repo: ownerRepo, Runs: runs}
		if !settled {
			m.ciWatchPending(prd, base, d)
			return false, nil, nil
		}
		failed, infra := classifyCIRuns(runs)
		if len(failed) > 0 {
			return m.ciWatchJudge(ctx, prd, base, failed)
		}
		if len(infra) == 0 {
			base.Status = "pass"
			base.Summary = fmt.Sprintf("all %d CI run(s) passed", len(runs))
			recordCIWatch(prd, base, "pass", "")
			return false, nil, nil
		}
		// Only CI-machinery failures: re-run them while infra budget lasts.
		if !m.retryBudgetRemaining(prd, "infra") {
			base.Status = "failed_infra"
			base.Category = "infra"
			base.Summary = "CI run(s) did not complete (cancelled/timed out/failed to start) and the infra retry budget is exhausted"
			base.Issues = []string{ciFailureIssue(FailureReason{Category: "infra", Source: "ci-watch", Detail: strings.TrimSpace(ciRunList(infra))})}
			recordCIWatch(prd, base, "block", "infra")
			m.recordFailureLearning(prd, prd.ID, "infra", "ci-watch: "+base.Summary+"\n"+ciRunList(infra))
			return true, nil, nil
		}
		_ = m.consumeRetryBudget(prd, "infra")
		m.recordAutoRetryDecision(prd, prd.ID, "infra", prd.RetryCountsByCategory["infra"], false)
		for _, r := range infra {
			if rerr := d.rerun(ctx, ownerRepo, r.ID); rerr != nil {
				log.Printf("[autonomous] ci-watch: prd=%s rerun %d: %v", prd.ID, r.ID, rerr)
			}
		}
		// Give the re-run a poll interval to register before re-listing,
		// so the stale completed state isn't read straight back.
		if remaining := time.Until(deadline); remaining > 0 {
			if serr := sleepCtx(ctx, min(d.poll, remaining)); serr != nil {
				return false, nil, fmt.Errorf("ci watch for prd %s: %w", prd.ID, serr)
			}
		}
	}
}

// ciWatchPending records a timed-out watch (runs still queued/in
// progress, or none appeared) without blocking, and schedules exactly
// one background follow-up check.
func (m *Manager) ciWatchPending(prd *PRD, res CIWatchResult, d ciWatchDeps) {
	detail := fmt.Sprintf("CI for %s had not finished after %s (%d run(s) seen); completing without waiting — one follow-up check in %s", res.SHA, d.timeout, len(res.Runs), d.followUp)
	res.Status = "pending"
	res.Category = "infra"
	res.Summary = detail
	res.Issues = []string{ciFailureIssue(FailureReason{Category: "infra", Source: "ci-watch", Retryable: false, Detail: detail})}
	recordCIWatch(prd, res, "n/a", "infra")

	prdID, sha, ownerRepo := prd.ID, res.SHA, res.Repo
	go func() { // #nosec G118 -- single follow-up CI check after PRD completion; request context already done
		if err := sleepCtx(context.Background(), d.followUp); err != nil {
			return
		}
		m.ciWatchFollowUp(prdID, sha, ownerRepo)
	}()
}

// ciWatchFollowUp is the one post-timeout re-check. It re-fetches the
// PRD (never touching the caller's *PRD), lists the runs once, records
// what it finds and saves. It cannot re-open a completed automaton and
// does not judge or schedule anything further: a failure is recorded
// with the same run classification, a Decision and a learning, and
// logged for the operator.
func (m *Manager) ciWatchFollowUp(prdID, sha, ownerRepo string) {
	ctx, cancel := context.WithTimeout(context.Background(), ciFollowUpCallTimeout)
	defer cancel()
	runs, err := m.ciWatchDeps().runs(ctx, ownerRepo, sha)
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	res := CIWatchResult{SHA: sha, Repo: ownerRepo, Runs: runs}
	outcome, category := "n/a", ""
	switch {
	case err != nil || !ciRunsSettled(runs):
		res.Status = "pending"
		res.Category = "infra"
		res.Summary = "follow-up check: CI still not finished; no further checks scheduled"
		if err != nil {
			res.Summary = "follow-up check: listing runs failed: " + err.Error()
		}
		category = "infra"
	default:
		failed, infra := classifyCIRuns(runs)
		switch {
		case len(failed) > 0:
			res.Status, outcome, category = "failed_content", "block", ""
			res.Summary = "failed after completion: CI run(s) failed for the commit this automaton pushed (not judged — the automaton had already completed; operator review needed)"
			res.Issues = []string{strings.TrimSpace(ciRunList(failed))}
		case len(infra) > 0:
			res.Status, outcome, category = "failed_infra", "block", "infra"
			res.Category = "infra"
			res.Summary = "failed after completion: CI run(s) did not complete (cancelled/timed out/failed to start)"
			res.Issues = []string{ciFailureIssue(FailureReason{Category: "infra", Source: "ci-watch", Detail: strings.TrimSpace(ciRunList(infra))})}
		default:
			res.Status, outcome = "pass", "pass"
			res.Summary = fmt.Sprintf("follow-up check: all %d CI run(s) passed", len(runs))
		}
	}
	recordCIWatch(p, res, outcome, category)
	if strings.HasPrefix(res.Status, "failed_") {
		m.recordFailureLearning(p, prdID, category, "ci-watch: "+res.Summary+"\n"+ciRunList(runs))
		log.Printf("[autonomous] ci-watch: prd=%s %s", prdID, res.Summary)
	}
	if serr := m.store.SavePRD(p); serr != nil {
		log.Printf("[autonomous] ci-watch follow-up save: prd=%s: %v", prdID, serr)
	}
}

// ciWatchJudge dispatches failed runs to the configured judge through
// the same GuardrailFn (and infra auto-retry) rules-review uses, then
// maps the verdict exactly the way runPRDCompletionRulesReview does.
func (m *Manager) ciWatchJudge(ctx context.Context, prd *PRD, res CIWatchResult, failed []CIWatchRun) (bool, *GuardrailVerdict, error) {
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	if fn == nil {
		// Same as rules-review with no judge wired: nothing to judge with.
		res.Status = "error"
		res.Summary = "CI run(s) failed but no guardrail judge is configured; not blocking"
		res.Issues = []string{strings.TrimSpace(ciRunList(failed))}
		recordCIWatch(prd, res, "n/a", "")
		return false, nil, nil
	}
	inv := GuardrailInvocation{
		PRDID:      prd.ID,
		Level:      "prd",
		UnitID:     prd.ID,
		UnitTitle:  prd.Title,
		UnitSpec:   fmt.Sprintf("Original request for this automaton:\n%s\n\n%s", prd.Spec, m.ciWatchSuffix(prd, res.SHA, res.Repo, failed)),
		Guardrail:  "ci-watch",
		ProjectDir: prd.ProjectDir,
		OwnerPeer:  prd.OwnerPeer,
	}
	// v9.10.18 — paired judge-model resolution, no backend-default
	// fallback; see rules-review's identical handling.
	gb, gm, pinned, resErr := m.resolveUnitGuardrailModel(&Task{}, prd)
	if resErr == nil {
		inv.GuardrailBackend, inv.GuardrailModel = gb, gm
		inv.GuardrailPinned = pinned
	}

	var v GuardrailVerdict
	var ferr error
	if resErr != nil {
		// Loud, non-retried infra block — retrying cannot configure a
		// model that is not configured.
		v = NoGuardrailModelVerdict("ci-watch", resErr)
	} else {
		v, ferr = m.runGuardrailWithInfraRetry(ctx, prd, inv, fn)
	}
	if ferr != nil {
		return false, nil, fmt.Errorf("ci watch for prd %s: %w", prd.ID, ferr)
	}
	if v.VerdictAt.IsZero() {
		v.VerdictAt = time.Now()
	}
	v.Guardrail = "ci-watch"
	prd.Verdicts = append(prd.Verdicts, v)
	if err := m.recordGuardrailVerdict(prd, prd.ID, v); err != nil {
		return false, nil, err
	}
	res.Summary = v.Summary
	res.Issues = v.Issues
	res.Category = v.Category

	if v.Outcome != "block" {
		// A CI failure the judge found acceptable is recorded, not blocking.
		res.Status = v.Outcome
		recordCIWatch(prd, res, v.Outcome, v.Category)
		return false, nil, nil
	}
	// Identical auto-fix condition to rules-review: a "policy" (operator
	// sign-off required) or "infra" block never qualifies.
	if v.Category == "" && m.resolveContentAutoFixEnabled(prd) && m.retryBudgetRemaining(prd, "content") {
		res.Status = "failed_content"
		recordCIWatch(prd, res, v.Outcome, v.Category)
		_ = m.consumeRetryBudget(prd, "content")
		vCopy := v
		m.recordAutoRetryDecision(prd, prd.ID, "content", prd.RetryCountsByCategory["content"], false)
		return false, &vCopy, nil
	}
	switch v.Category {
	case "policy":
		res.Status = "failed_policy"
	case "infra":
		res.Status = "failed_infra"
	default:
		res.Status = "failed_content"
	}
	recordCIWatch(prd, res, v.Outcome, v.Category)
	m.recordFailureLearning(prd, prd.ID, v.Category, contentFixHint(v))
	return true, nil, nil
}

// repoCIJudgeCall (repo CI watch pass, repo_ci_watch_poll.go) is the
// extracted judge invocation for a failed run in an UNOWNED (non-PRD)
// repository: it takes a repo path and a label describing the failed
// run instead of a PRD, because there is no PRD here to hold the verdict
// or an infra retry budget — the judge runs exactly once and its verdict
// lands on the run's repo CI watch record (recorded by the poll pass),
// where verdict.Category "policy" means the failure is a policy/rules
// violation and anything else is a code/test defect. The inv shape and
// judge-model resolution mirror ciWatchJudge (the PRD wrapper, whose
// behavior is unchanged): the judge model still resolves from the same
// global tiers, and an unresolvable judge model yields
// NoGuardrailModelVerdict (loud, infra, non-retried) rather than a
// launch. fn is resolved under mu exactly like ciWatchJudge.
func (m *Manager) repoCIJudgeCall(ctx context.Context, repoPath, label string) (GuardrailVerdict, error) {
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	if fn == nil {
		return GuardrailVerdict{}, fmt.Errorf("repo ci watch for %s: no guardrail judge is configured", repoPath)
	}
	inv := GuardrailInvocation{
		PRDID:     "repo-ci-watch/" + repoPath,
		Level:     "repo",
		UnitID:    repoPath,
		UnitTitle: label,
		UnitSpec: label + " — the subject is ONE failed CI run in a repository that has no PRD. " +
			"Judge why it failed: if the failure looks like a policy or rules " +
			"violation, use category \"policy\"; if it is a code or test defect, use " +
			"category \"content\". Respond with the same JSON as any other guardrail: " +
			"outcome (pass|warn|block), severity, summary, issues, category.",
		Guardrail:  "repo-ci-watch",
		ProjectDir: repoPath,
	}
	// Same paired judge-model resolution as ciWatchJudge, at the global
	// tier (no task/PRD override exists for an unowned repo).
	gb, gm, pinned, resErr := m.resolveUnitGuardrailModel(nil, nil)
	if resErr == nil {
		inv.GuardrailBackend, inv.GuardrailModel = gb, gm
		inv.GuardrailPinned = pinned
	}
	if resErr != nil {
		// Loud, non-retried infra block — retrying cannot configure a
		// model that is not configured (same rule as ciWatchJudge).
		v := NoGuardrailModelVerdict("repo-ci-watch", resErr)
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		return v, nil
	}
	v, ferr := fn(ctx, inv)
	if ferr != nil {
		return GuardrailVerdict{}, fmt.Errorf("repo ci watch for %s: %w", repoPath, ferr)
	}
	if v.VerdictAt.IsZero() {
		v.VerdictAt = time.Now()
	}
	v.Guardrail = "repo-ci-watch"
	return v, nil
}
