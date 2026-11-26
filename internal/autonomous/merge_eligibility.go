// The one merge selection rule. Every place that selects what datawatch may
// merge asks this file: the supervisor loop merge job, the merge run request
// (through the merge pipeline runner), the shadow merge plan, the selection
// read and the finished-work findings that say "land it".
//
// datawatch may merge an Automaton's branch when the Automaton is completed and
// its completion review passed on exactly the commit the branch points at, or
// when the operator or the supervisor accepted that exact commit with a reason
// (review pending or timed out, or the Automaton is needs_review). Failed,
// blocked, cancelled, archived and review-blocked work is never merged,
// accepted or not, and an acceptance of one commit never covers a later one.
// Every exclusion says why.
//
// The acceptance records who it names, the way other decision records do. It
// does not prove who that was.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Bases on which a branch is selected.
const (
	// MergeBasisReviewPassed: completed, and the completion review passed on
	// the commit the branch points at.
	MergeBasisReviewPassed = "review_passed"
	// MergeBasisAccepted: an acceptance names the commit the branch points at.
	MergeBasisAccepted = "accepted"
)

// Decision kinds of the selection rule and the landing it leads to.
const (
	DecisionMergeAccept = "merge_accept"
	DecisionMergeLanded = "merge_landed"
)

// mergePipelineActor is the actor of a landing the merge pipeline confirmed.
const mergePipelineActor = "merge_pipeline"

// landByHandReason is the landing reason of the answer "I will land it".
const landByHandReason = "will be landed by hand"

const mergeSelectGitTimeout = 30 * time.Second

// MergeAcceptance is the acceptance of one exact commit for merging.
type MergeAcceptance struct {
	Commit string    `json:"commit"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// MergeSelectionEntry is the rule's answer for one Automaton.
type MergeSelectionEntry struct {
	AutomatonID    string `json:"automaton_id"`
	Name           string `json:"name"`
	AutomatonState string `json:"automaton_state"`
	Branch         string `json:"branch"`
	// Tip is the commit the branch points at; empty when it cannot be read.
	Tip      string `json:"tip,omitempty"`
	Eligible bool   `json:"eligible"`
	// Basis is why an eligible branch is selected: review_passed or accepted.
	Basis string `json:"basis,omitempty"`
	// Reasons says why a branch is excluded; empty when it is eligible.
	Reasons []string `json:"reasons"`
}

// MergeSelection is the selection read: what the rule selects now and every
// branch it leaves out, with why.
type MergeSelection struct {
	Eligible []MergeSelectionEntry `json:"eligible"`
	Excluded []MergeSelectionEntry `json:"excluded"`
	// Note explains an empty candidate list.
	Note string `json:"note,omitempty"`
}

// Summary is the rule's answer in one line.
func (e MergeSelectionEntry) Summary() string {
	switch {
	case e.Eligible && e.Basis == MergeBasisAccepted:
		return "datawatch may merge it: commit " + e.Tip + " is accepted"
	case e.Eligible:
		return "datawatch may merge it: review passed on commit " + e.Tip
	default:
		return "datawatch will not merge it: " + strings.Join(e.Reasons, "; ")
	}
}

// mergeTaskBranch is what the rule needs to know about one task branch.
type mergeTaskBranch struct{ task, branch, state string }

// mergeFacts is the part of an Automaton the rule reads, copied under the
// store lock so git runs with no lock held.
type mergeFacts struct {
	entry      MergeSelectionEntry
	status     PRDStatus
	projectDir string
	// barred are the reasons no acceptance can lift.
	barred []string
	// byHand is set while someone said they will land the branch and no
	// later acceptance handed it back.
	byHand       bool
	review       ReviewState
	passedCommit string // commit of the newest completion pass that records one
	hasPass      bool   // a completion review pass exists
	acceptance   *MergeAcceptance
	tasks        []mergeTaskBranch
}

// unitReviewBlocked reports whether, for any guardrail among one unit's
// verdicts, the newest real judgment (a pass or a block) is a block. A
// judge-timeout hold is not a judgment: it neither blocks here nor replaces an
// earlier block.
func unitReviewBlocked(vs []GuardrailVerdict) bool {
	newest := map[string]GuardrailVerdict{}
	for _, v := range vs {
		if (v.Outcome != "pass" && v.Outcome != "block") || IsJudgeTimeoutHold(v) {
			continue
		}
		if cur, ok := newest[v.Guardrail]; !ok || !v.VerdictAt.Before(cur.VerdictAt) {
			newest[v.Guardrail] = v
		}
	}
	for _, v := range newest {
		if v.Outcome == "block" {
			return true
		}
	}
	return false
}

// anyReviewBlocked applies unitReviewBlocked to the Automaton, each story and
// each task on its own, so a pass on one unit never hides a block on another.
func anyReviewBlocked(p *PRD) bool {
	if unitReviewBlocked(p.Verdicts) {
		return true
	}
	for i := range p.Story {
		if unitReviewBlocked(p.Story[i].Verdicts) {
			return true
		}
		for j := range p.Story[i].Tasks {
			if unitReviewBlocked(p.Story[i].Tasks[j].Verdicts) {
				return true
			}
		}
	}
	return false
}

// mergeBarred lists the reasons, read from the record alone, that datawatch
// may not merge p whether or not it is accepted.
func mergeBarred(p *PRD) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	if p.IsTemplate {
		add("it is a template")
	}
	status := NormalizePRDStatus(p.Status)
	switch status {
	case PRDCompleted, PRDNeedsReview:
	case PRDFailed, PRDBlocked, PRDCancelled, PRDArchived, PRDRejected:
		add("%s work is never merged", status)
	case PRDHeld:
		add("it is on an operator hold")
	case PRDPaused:
		add("it is paused")
	default:
		add("it is %s: the work is not finished", status)
	}
	if p.Archive != nil && status != PRDArchived {
		add("archived work is never merged")
	}
	if ReviewStateOf(p).State == ReviewStateBlocked || anyReviewBlocked(p) {
		add("its review is blocked: review-blocked work is never merged")
	}
	if p.Hold != nil && status != PRDHeld {
		add("it is on an operator hold")
	}
	if p.UncommittedWork != nil {
		add("it is held for uncommitted work in its worktree")
	}
	if p.IsShipped() {
		add("it already has a shipped record (commit %s)", p.MergeCommit)
	}
	if l := p.Landing; l != nil {
		switch l.State {
		case LandingLanded:
			add("its branch is already recorded landed (commit %s)", l.Commit)
		case LandingKept:
			add("its branch is recorded kept unmerged: %s", l.Reason)
		case LandingDropped:
			add("its branch is recorded dropped: %s", l.Reason)
		}
	}
	switch p.ClosedOutcome {
	case OutcomeSuperseded, OutcomeDropped, OutcomeHeld, OutcomeDelivered:
		add("it is closed as %s", p.ClosedOutcome)
	}
	done := 0
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			t := &p.Story[i].Tasks[j]
			if t.Status == TaskCompleted {
				// Finished work counts whatever its title says; a gate
				// task on its own is not work.
				if t.Kind != TaskKindSmoke && t.Kind != TaskKindFinalGate {
					done++
				}
				continue
			}
			// An open docs or gate task does not hold the code back.
			if !codeTask(t) || t.Status == TaskCancelled {
				continue
			}
			if t.Status == "" {
				add("code task %s is not completed", t.ID)
				continue
			}
			add("code task %s is %s", t.ID, t.Status)
		}
	}
	if done == 0 {
		add("it has no completed code task (no finished task other than gate tasks)")
	}
	return out
}

// mergeFactsOf copies what the rule reads from p. Callers hold the store lock.
func mergeFactsOf(p *PRD) mergeFacts {
	f := mergeFacts{
		entry: MergeSelectionEntry{AutomatonID: p.ID, Name: p.Title, AutomatonState: string(p.Status),
			Branch: strings.TrimSpace(p.Git.Branch), Reasons: []string{}},
		status:     NormalizePRDStatus(p.Status),
		projectDir: p.ProjectDir,
		barred:     mergeBarred(p),
		review:     ReviewStateOf(p).State,
	}
	if f.entry.Name == "" {
		f.entry.Name = p.ID
	}
	if a := p.MergeAcceptance; a != nil {
		c := *a
		f.acceptance = &c
	}
	if l := p.Landing; l != nil && l.State == LandingPending && l.Reason == landByHandReason {
		f.byHand = f.acceptance == nil || !f.acceptance.At.After(l.At)
	}
	var anchored []GuardrailVerdict
	for _, v := range p.Verdicts {
		if v.Outcome != "pass" {
			continue
		}
		f.hasPass = true
		if v.JudgedCommitSHA != "" {
			anchored = append(anchored, v)
		}
	}
	if v := latestVerdict(anchored, "pass"); v != nil {
		f.passedCommit = v.JudgedCommitSHA
	}
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			t := &p.Story[i].Tasks[j]
			if b := strings.TrimSpace(t.WorktreeBranch); b != "" && b != f.entry.Branch && t.Status != TaskCancelled {
				f.tasks = append(f.tasks, mergeTaskBranch{task: t.ID, branch: b, state: t.MergeState})
			}
		}
	}
	return f
}

// mergeGitCommonDir is the shared git directory of the repository at dir.
func mergeGitCommonDir(ctx context.Context, git MergeGitRunner, dir string) (string, bool) {
	out, err := git.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	d := strings.TrimSpace(out)
	if err != nil || !filepath.IsAbs(d) {
		return "", false
	}
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	return d, true
}

func branchCommit(ctx context.Context, git MergeGitRunner, repo, branch string) (string, bool) {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return "", false
	}
	out, err := git.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	sha := strings.TrimSpace(out)
	return sha, err == nil && sha != ""
}

func isAncestor(ctx context.Context, git MergeGitRunner, repo, commit, of string) bool {
	_, err := git.Run(ctx, repo, "merge-base", "--is-ancestor", commit, of)
	return err == nil
}

// evaluateMerge applies the rule to the copied facts, reading git only.
func (m *Manager) evaluateMerge(ctx context.Context, f mergeFacts) MergeSelectionEntry {
	ctx, cancel := context.WithTimeout(ctx, mergeSelectGitTimeout)
	defer cancel()
	e := f.entry
	why := append([]string{}, f.barred...)
	add := func(format string, a ...any) { why = append(why, fmt.Sprintf(format, a...)) }
	if f.byHand {
		add("someone answered that they will land it by hand; accept the commit to hand it back to datawatch")
	}

	repo := strings.TrimSpace(m.MergeBaseRepo())
	if b, paused := m.pauseBlock(f.projectDir); paused {
		add("%s", b.Message())
	} else if b, paused := m.pauseBlock(repo); paused && repo != "" {
		add("%s", b.Message())
	}

	git := m.archivedUnmergedGitRunner()
	switch {
	case e.Branch == "":
		add("it has no work branch")
	case repo == "":
		add("%s", ErrMergeNoBaseRepo.Error())
	default:
		tip, ok := branchCommit(ctx, git, repo, e.Branch)
		if !ok {
			add("branch %s is not in the merge repository (it belongs to another repository, or it is gone)", e.Branch)
			break
		}
		e.Tip = tip
		if st, err := os.Stat(f.projectDir); f.projectDir != "" && err == nil && st.IsDir() {
			own, ok1 := mergeGitCommonDir(ctx, git, f.projectDir)
			base, ok2 := mergeGitCommonDir(ctx, git, repo)
			if ok1 && ok2 && own != base {
				add("its work directory belongs to another repository")
			}
		}
		for _, t := range f.tasks {
			if t.state == MergeStateConflict || t.state == MergeStateHeld {
				add("task branch %s of task %s is not merged (%s)", t.branch, t.task, t.state)
				continue
			}
			sha, exists := branchCommit(ctx, git, repo, t.branch)
			switch {
			case !exists && t.state != MergeStateMerged:
				add("task branch %s of task %s is gone and not recorded merged", t.branch, t.task)
			case exists && !isAncestor(ctx, git, repo, sha, tip):
				add("task branch %s of task %s has commits that are not on the Automaton branch", t.branch, t.task)
			}
		}
	}

	basis, short := "", ""
	switch {
	case f.status == PRDNeedsReview:
		short = "the Automaton is needs_review"
	case f.review != ReviewStatePassed:
		short = fmt.Sprintf("its review is %s", f.review)
	case !f.hasPass:
		short = "no completion review passed (the passing verdicts are task or story checks)"
	case f.passedCommit == "":
		short = "the passing completion review records no commit"
	case f.passedCommit != e.Tip:
		short = fmt.Sprintf("the review passed on commit %s but the branch is at %s", f.passedCommit, orUnknown(e.Tip))
	default:
		basis = MergeBasisReviewPassed
	}
	// Work barred outright gets no review line: no acceptance would help it.
	if basis == "" && len(f.barred) == 0 {
		switch a := f.acceptance; {
		case a != nil && e.Tip != "" && a.Commit == e.Tip:
			basis = MergeBasisAccepted
		case a != nil:
			add("%s; the acceptance is for commit %s and the branch is at %s (an acceptance never covers a later commit)", short, a.Commit, orUnknown(e.Tip))
		default:
			add("%s and no acceptance of commit %s is recorded", short, orUnknown(e.Tip))
		}
	}

	e.Reasons = why
	if len(why) == 0 {
		e.Eligible, e.Basis = true, basis
	}
	return e
}

func orUnknown(s string) string {
	if s == "" {
		return "an unknown commit"
	}
	return s
}

// MergeEligibility applies the selection rule to one Automaton. It only reads.
func (m *Manager) MergeEligibility(ctx context.Context, id string) MergeSelectionEntry {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return MergeSelectionEntry{AutomatonID: id, Reasons: []string{"there is no such Automaton"}}
	}
	var f mergeFacts
	m.readLocked(func() { f = mergeFactsOf(p) })
	return m.evaluateMerge(ctx, f)
}

// MergeSelection is the selection read. With an id it answers for that
// Automaton. Without one it answers for the candidates the supervisor merge
// job looks at: the branches the merge-pending pass lists as pending.
func (m *Manager) MergeSelection(ctx context.Context, id string) MergeSelection {
	out := MergeSelection{Eligible: []MergeSelectionEntry{}, Excluded: []MergeSelectionEntry{}}
	var ids []string
	if id = strings.TrimSpace(id); id != "" {
		ids = []string{id}
	} else {
		st := m.MergeStatus()
		for _, c := range st.Automata {
			if c.State == MergeStatePending && c.Branch != "" {
				ids = append(ids, c.AutomatonID)
			}
		}
		if len(ids) == 0 {
			out.Note = fmt.Sprintf("no branches waiting to merge (merge-pending mode %s)", st.Mode)
		}
	}
	sort.Strings(ids)
	for _, c := range ids {
		if e := m.MergeEligibility(ctx, c); e.Eligible {
			out.Eligible = append(out.Eligible, e)
		} else {
			out.Excluded = append(out.Excluded, e)
		}
	}
	return out
}

// SelectMergeRun applies the rule to a merge run request: exactly one branch,
// owned by exactly one eligible Automaton (the named one, when automatonID is
// set). The error says why a request is refused.
func (m *Manager) SelectMergeRun(ctx context.Context, branches []string, automatonID string) (MergeSelectionEntry, error) {
	var none MergeSelectionEntry
	if len(branches) != 1 {
		return none, fmt.Errorf("one Automaton per run: a run merges exactly one Automaton branch, %d were asked for", len(branches))
	}
	branch := strings.TrimSpace(branches[0])
	var ids []string
	for _, p := range m.store.ListPRDs() {
		var own bool
		m.readLocked(func() { own = branch != "" && strings.TrimSpace(p.Git.Branch) == branch })
		if own {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return none, fmt.Errorf("no Automaton has branch %s: datawatch merges only Automaton work that passed review or was accepted", branch)
	}
	sort.Strings(ids)
	var eligible []MergeSelectionEntry
	var why []string
	for _, id := range ids {
		if automatonID != "" && id != automatonID {
			continue
		}
		e := m.MergeEligibility(ctx, id)
		if e.Eligible && e.Branch == branch {
			eligible = append(eligible, e)
		} else {
			why = append(why, fmt.Sprintf("Automaton %s: %s", id, strings.Join(e.Reasons, "; ")))
		}
	}
	switch {
	case len(eligible) == 1:
		return eligible[0], nil
	case len(eligible) > 1:
		return none, fmt.Errorf("branch %s belongs to %d eligible Automata: name the one this run is for", branch, len(eligible))
	case len(why) == 0:
		return none, fmt.Errorf("automaton %s does not own branch %s", automatonID, branch)
	}
	return none, errors.New(strings.Join(why, " | "))
}

var fullCommitID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// sessionDoesAutomatonWork reports whether sessionID is a worker, verifier,
// planner or judge session of any Automaton.
func (m *Manager) sessionDoesAutomatonWork(sessionID string) bool {
	same := func(s string) bool {
		return s != "" && (s == sessionID || strings.HasSuffix(s, "-"+sessionID) || strings.HasSuffix(sessionID, "-"+s))
	}
	found := false
	for _, p := range m.store.ListPRDs() {
		m.readLocked(func() {
			found = found || same(p.PlannerSessionID)
			for _, v := range reviewVerdicts(p) {
				found = found || same(v.JudgeSessionID)
			}
			for i := range p.Story {
				for j := range p.Story[i].Tasks {
					t := &p.Story[i].Tasks[j]
					found = found || same(t.SessionID) || same(t.VerifierSessionID)
				}
			}
		})
		if found {
			break
		}
	}
	return found
}

// AcceptForMerge records that actor accepts exactly commit of the Automaton's
// branch for merging, and why. commit must be the full id of the commit the
// branch points at. Work the rule never merges cannot be accepted. A session
// doing Automaton work cannot accept: it is held like other work.
func (m *Manager) AcceptForMerge(ctx context.Context, id, commit, actor, reason string) (*PRD, error) {
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	commit = strings.ToLower(strings.TrimSpace(commit))
	switch {
	case actor == "":
		return nil, fmt.Errorf("accept Automaton %q for merge: actor is required", id)
	case reason == "":
		return nil, fmt.Errorf("accept Automaton %q for merge: reason is required", id)
	case !fullCommitID.MatchString(commit):
		return nil, fmt.Errorf("accept Automaton %q for merge: give the full id of the exact commit", id)
	}
	if sid, ok := strings.CutPrefix(actor, "session:"); ok && m.sessionDoesAutomatonWork(sid) {
		return nil, fmt.Errorf("accept Automaton %q for merge: session %s is doing Automaton work and cannot accept work for merge", id, sid)
	}
	p, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("automaton %q not found", id)
	}
	var f mergeFacts
	m.readLocked(func() { f = mergeFactsOf(p) })
	refuse := func(why []string) error {
		return fmt.Errorf("automaton %q cannot be accepted for merge: %s", id, strings.Join(why, "; "))
	}
	if len(f.barred) > 0 {
		return nil, refuse(f.barred)
	}
	e := m.evaluateMerge(ctx, f)
	if e.Tip == "" {
		return nil, refuse(e.Reasons)
	}
	if e.Tip != commit {
		return nil, refuse([]string{fmt.Sprintf("branch %s is at commit %s, not %s", e.Branch, e.Tip, commit)})
	}
	now := m.holdNow()
	var barred []string
	m.store.WithLock(func() {
		if barred = mergeBarred(p); len(barred) > 0 {
			return
		}
		p.MergeAcceptance = &MergeAcceptance{Commit: commit, Actor: actor, Reason: reason, At: now}
		p.UpdatedAt = now
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: now, Kind: DecisionMergeAccept, Actor: actor,
			Note: fmt.Sprintf("accepted commit %s of branch %s for merge: %s", commit, e.Branch, reason)}))
	})
	if len(barred) > 0 {
		return nil, refuse(barred)
	}
	if err := m.store.SavePRD(p); err != nil {
		return nil, fmt.Errorf("merge acceptance: save: %w", err)
	}
	out, _ := m.store.GetPRD(id)
	return out, nil
}

// mergeRemote names the repository's remote: origin when it has one, else the
// first. "" means the repository has no remote.
func mergeRemote(ctx context.Context, git MergeGitRunner, repo string) string {
	out, err := git.Run(ctx, repo, "remote")
	if err != nil {
		return ""
	}
	names := strings.Fields(out)
	for _, n := range names {
		if n == "origin" {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// confirmOnRemote asks the remote itself whether its default branch contains
// commit, which must be a full commit id: any other text is refused before
// git runs. Anything it cannot prove is an error. It may fetch that branch,
// which moves only the remote-tracking ref.
func confirmOnRemote(ctx context.Context, git MergeGitRunner, repo, commit string) error {
	if !fullCommitID.MatchString(commit) {
		return fmt.Errorf("%q is not a full commit id (hex only)", commit)
	}
	remote := mergeRemote(ctx, git, repo)
	if remote == "" {
		return errors.New("the repository has no remote, so a push cannot be confirmed")
	}
	base := resolveMergeBase(ctx, git, repo)
	out, err := git.Run(ctx, repo, "ls-remote", "--heads", remote, "refs/heads/"+base)
	if err != nil {
		return fmt.Errorf("reading %s on remote %s: %v", base, remote, err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return fmt.Errorf("remote %s has no branch %s", remote, base)
	}
	head := fields[0]
	if head == commit || isAncestor(ctx, git, repo, commit, head) {
		return nil
	}
	if _, err := git.Run(ctx, repo, "fetch", "--quiet", remote, base); err == nil && isAncestor(ctx, git, repo, commit, head) {
		return nil
	}
	return fmt.Errorf("%s on remote %s (at %s) does not contain commit %s", base, remote, head, commit)
}

// onTrackedRemote reports whether commit is on the remote-tracking copy of the
// default branch. A repository with no remote has nothing more to ask: its
// default branch is where work lands.
func onTrackedRemote(ctx context.Context, git MergeGitRunner, repo, base, commit string) bool {
	remote := mergeRemote(ctx, git, repo)
	return remote == "" || isAncestor(ctx, git, repo, commit, "refs/remotes/"+remote+"/"+base)
}

// RecordMergeLanding records, in one write, that the Automaton's work is
// landed, completed and shipped. It writes nothing unless the remote's default
// branch contains commit and commit contains the Automaton's branch. A
// completed or needs_review Automaton becomes completed; if its state changed
// during the run to another one, the state is left alone and the landing and
// shipped facts are still recorded.
func (m *Manager) RecordMergeLanding(ctx context.Context, id, release, commit string, at time.Time) (*PRD, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	commit = strings.TrimSpace(commit)
	p, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	var branch string
	m.readLocked(func() { branch = strings.TrimSpace(p.Git.Branch) })
	repo := strings.TrimSpace(m.MergeBaseRepo())
	switch {
	case commit == "":
		return nil, fmt.Errorf("landing of Automaton %q not recorded: no merge commit", id)
	case repo == "":
		return nil, fmt.Errorf("landing of Automaton %q not recorded: %w", id, ErrMergeNoBaseRepo)
	}
	gctx, cancel := context.WithTimeout(ctx, mergeSelectGitTimeout)
	defer cancel()
	git := m.archivedUnmergedGitRunner()
	if err := confirmOnRemote(gctx, git, repo, commit); err != nil {
		return nil, fmt.Errorf("landing of Automaton %q not recorded: %w", id, err)
	}
	tip, ok := branchCommit(gctx, git, repo, branch)
	if !ok || !isAncestor(gctx, git, repo, tip, commit) {
		return nil, fmt.Errorf("landing of Automaton %q not recorded: commit %s does not contain its branch %s", id, commit, branch)
	}
	if at.IsZero() {
		at = m.holdNow()
	}
	outcomeMode := m.Config().EffectiveClosedOutcomeArchive()
	changed := false
	m.store.WithLock(func() {
		finished := NormalizePRDStatus(p.Status)
		completes := finished == PRDNeedsReview
		landed := p.Landing != nil && p.Landing.State == LandingLanded && p.Landing.Commit == commit
		if landed && p.MergeCommit == commit && !completes {
			return
		}
		changed = true
		if completes {
			p.Status = PRDCompleted
		}
		markLandingLandedLocked(p, commit, mergePipelineActor, at)
		setPRDShipped(p, release, at, commit, ShippedSourceMergeStep)
		if outcomeMode != ClosedOutcomeOff && closedWorkStatus(p.Status) && p.ClosedOutcome == "" {
			m.setOutcomeLocked(p, OutcomeInput{}, "autonomous", m.holdNow(), outcomeMode)
		}
		p.UpdatedAt = m.holdNow()
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionMergeLanded, Actor: mergePipelineActor,
			Note: fmt.Sprintf("landed, shipped in %s: the remote's default branch contains commit %s (branch %s; state was %s)", release, commit, branch, finished)}))
	})
	if changed {
		if err := m.store.SavePRD(p); err != nil {
			return nil, fmt.Errorf("merge landing: save: %w", err)
		}
	}
	out, _ := m.store.GetPRD(id)
	return out, nil
}
