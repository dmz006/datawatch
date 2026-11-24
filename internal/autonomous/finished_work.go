// Finished work must never sit unmerged. This file holds the daemon rules
// that keep finished Automaton work from being lost or left without an owner:
//
//   - the archive and cancel guard: an Automaton whose branch holds commits
//     that are not on the default branch cannot be archived or cancelled until
//     someone records a keep or drop decision (ArchiveDecided, CancelDecided);
//   - the bounded sweep (RunFinishedWorkSweep): finished work older than a day
//     gets an owner and one open question, an Automaton whose code tasks are
//     all done is offered for landing while its docs and gate tasks stay
//     separately retryable, and a branch that was left alone is asked about
//     again on its re-ask date;
//   - the daily digest of everything waiting (FinishedWorkDigest);
//   - the weekly audit (RunFinishedWorkAudit): a full read over every
//     Automaton, cancelled and branchless ones included, written as a
//     decision record.
//
// All of it is the daemon's own rule. It reads git read-only, never merges,
// never touches a session and never changes a task or an Automaton status; the
// only writes are a landing decision the operator asked for, a decision-log
// line, a question, an alert and the findings store. autonomous.finished_work
// is on (default), shadow (record only, ask and change nothing) or off.
package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// Settings of autonomous.finished_work.
const (
	FinishedWorkOn     = "on"
	FinishedWorkShadow = "shadow"
	FinishedWorkOff    = "off"
)

// Causes a finished-work finding carries.
const (
	// FinishedWorkNotMerged: the Automaton finished and its branch holds
	// commits that are not on the default branch.
	FinishedWorkNotMerged = "finished_not_merged"
	// FinishedWorkCodeDone: every code task is complete and only docs or gate
	// tasks remain; the code can land without waiting for them.
	FinishedWorkCodeDone = "code_done_rest_pending"
	// FinishedWorkReask: a branch was left alone and its re-ask date came.
	FinishedWorkReask = "reask_due"
	// FinishedWorkBranchGone: the work branch no longer exists and nothing
	// records that its work landed (audit only).
	FinishedWorkBranchGone = "branch_gone_unlanded"
	// FinishedWorkStuck: an unfinished Automaton nothing has touched for a
	// week (audit only).
	FinishedWorkStuck = "stuck_no_activity"
)

// Option ids of the finished-work question.
const (
	FinishedWorkOptionLand  = "land_by_hand"
	FinishedWorkOptionLeave = "leave_alone"
	FinishedWorkOptionDrop  = "drop"
)

// Timings of the rule.
const (
	// FinishedWorkAfter is how long finished work may wait before the daemon
	// gives it an owner and a question.
	FinishedWorkAfter = 24 * time.Hour
	// FinishedWorkReaskDays is the re-ask date set by "leave it alone".
	FinishedWorkReaskDays = 14
	// FinishedWorkLandReaskDays is the re-ask date set by "I will land it".
	FinishedWorkLandReaskDays = 3
	// FinishedWorkSweepLimit bounds the git comparisons of one sweep pass.
	FinishedWorkSweepLimit = 100
	// FinishedWorkSweepEvery is the least time between sweep passes the idle
	// tick starts.
	FinishedWorkSweepEvery = 10 * time.Minute
	// FinishedWorkDigestEvery is the least time between two daily digests.
	FinishedWorkDigestEvery = 24 * time.Hour
	// FinishedWorkAuditEvery is the least time between two weekly audits.
	FinishedWorkAuditEvery = 7 * 24 * time.Hour
	// FinishedWorkStuckAfter is how long an unfinished Automaton may sit
	// untouched before the audit calls it stuck.
	FinishedWorkStuckAfter = 7 * 24 * time.Hour
	// finishedWorkDigestRows caps the rows named in the digest alert.
	finishedWorkDigestRows = 15
	// finishedWorkGitTimeout bounds one branch comparison.
	finishedWorkGitTimeout = archiveBranchGitTimeout
)

const (
	finishedWorkAsker = "finished-work"
	finishedWorkActor = "finished-work"
	// DecisionFinishedWorkQuestion is the decision-log kind written when the
	// daemon raises the question for finished work.
	DecisionFinishedWorkQuestion = "finished_work_question"
	// DecisionLeaveAlone is the decision-log kind of a "leave it alone"
	// answer; its note names the re-ask date.
	DecisionLeaveAlone = "leave_alone"
	// DecisionLandByHand is the decision-log kind of an "I will land it"
	// answer.
	DecisionLandByHand = "land_by_hand"
)

// EffectiveFinishedWork returns the rule's mode; empty or unknown reads as on.
func (c Config) EffectiveFinishedWork() string {
	switch strings.ToLower(strings.TrimSpace(c.FinishedWork)) {
	case FinishedWorkOff:
		return FinishedWorkOff
	case FinishedWorkShadow:
		return FinishedWorkShadow
	}
	return FinishedWorkOn
}

// FinishedWorkPass is the outcome of one sweep pass.
type FinishedWorkPass struct {
	Mode string `json:"mode"`
	// Examined is how many Automata a git comparison ran for; GitChecks
	// counts the comparisons. Capped is set when the pass stopped at the
	// limit with Automata left for the next pass.
	Examined  int  `json:"examined"`
	GitChecks int  `json:"git_checks"`
	Capped    bool `json:"capped"`
	// Found counts the Automata with unmerged finished work that waited long
	// enough to need an owner; Younger the ones still inside the first day.
	Found   int `json:"found"`
	Younger int `json:"younger"`
	// Asked counts the questions raised, Reasked the ones raised because a
	// re-ask date came.
	Asked   int `json:"asked"`
	Reasked int `json:"reasked"`
	// Digest is set when this pass sent the daily digest alert.
	Digest   bool                  `json:"digest_sent"`
	Findings []FinishedWorkFinding `json:"findings"`
}

// FinishedWorkAuditResult is the outcome of one audit.
type FinishedWorkAuditResult struct {
	Mode             string                `json:"mode"`
	At               string                `json:"at"`
	Scanned          int                   `json:"scanned"`
	ByCause          map[string]int        `json:"by_cause"`
	Findings         []FinishedWorkFinding `json:"findings"`
	DecisionRecordID string                `json:"decision_record_id,omitempty"`
}

// FinishedWorkDigest is the read view of the work waiting to land: what the
// daily digest says, without running git.
type FinishedWorkDigest struct {
	Mode        string                `json:"mode"`
	GeneratedAt string                `json:"generated_at"`
	Waiting     []FinishedWorkFinding `json:"waiting"`
	LastSweep   string                `json:"last_sweep,omitempty"`
	LastDigest  string                `json:"last_digest,omitempty"`
	LastAudit   string                `json:"last_audit,omitempty"`
	AuditRecord string                `json:"audit_record_id,omitempty"`
}

// ---- the archive and cancel guard ----

// landingDecided says the landing record already holds an explicit outcome:
// landed, kept or dropped. Callers hold the store lock or a read lock.
func landingDecided(p *PRD) bool {
	if p == nil || p.Landing == nil {
		return false
	}
	switch p.Landing.State {
	case LandingLanded, LandingKept, LandingDropped:
		return true
	}
	return false
}

// finishedWorkRefusal is the archive and cancel guard. It returns the reason
// the step must be refused ("" when it may go on): the Automaton has a branch
// holding commits that are not on the default branch, or the branch cannot be
// compared, and nobody has recorded a landing decision for it. A record whose
// shipped record names its work is delivered and passes. Without a base
// repository nothing can be checked and the step passes, as it did before.
// In shadow mode the refusal is recorded in the ledger and the step passes;
// off skips the check.
func (m *Manager) finishedWorkRefusal(prd *PRD, step string) string {
	mode := m.Config().EffectiveFinishedWork()
	if mode == FinishedWorkOff || prd == nil {
		return ""
	}
	branch := strings.TrimSpace(prd.Git.Branch)
	if branch == "" {
		return ""
	}
	var skip bool
	m.readLocked(func() { skip = landingDecided(prd) || hasShippedRecord(prd) })
	if skip {
		return ""
	}
	repo := strings.TrimSpace(m.archivedBaseRepo())
	if repo == "" {
		return ""
	}
	verb := step
	switch step {
	case "archive":
		verb = "archived"
	case "cancel":
		verb = "cancelled"
	}
	ctx, cancel := context.WithTimeout(context.Background(), finishedWorkGitTimeout)
	state, ahead, detail := checkBranchAhead(ctx, m.archivedUnmergedGitRunner(), repo, branch, prd.Git.BaseBranch)
	cancel()
	var reason string
	switch state {
	case branchCheckAhead:
		reason = fmt.Sprintf("automaton %q cannot be %s: its branch %s has %d commit(s) that are not on the default branch; land it, or record a keep or drop decision for its landing first", prd.ID, verb, branch, ahead)
	case branchCheckUnknown:
		reason = fmt.Sprintf("automaton %q cannot be %s: the state of its branch %s could not be checked (%s); record a keep or drop decision for its landing, or try again", prd.ID, verb, branch, detail)
	default:
		m.clearRefusal(prd.ID, step)
		return ""
	}
	first := m.noteRefusal(prd.ID, step, reason)
	if mode == FinishedWorkShadow {
		if first {
			log.Printf("[autonomous] finished work (shadow): would refuse to %s %s: %s", step, prd.ID, reason)
		}
		return ""
	}
	return reason
}

// noteRefusal records a refused step in the refusal ledger (when wired) and
// reports whether it is new, so a caller logs it once.
func (m *Manager) noteRefusal(automaton, step, reason string) bool {
	l := m.RefusalLedger()
	if l == nil {
		return true
	}
	return l.RecordFirst(automaton, step, reason, m.holdNow())
}

// recordLandingDecision records the keep or drop decision a caller passes
// with an archive or cancel. An empty decision records nothing. A record with
// no branch, or whose work already landed, has nothing to decide.
func (m *Manager) recordLandingDecision(id, actor, decision, reason string) error {
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision == "" {
		return nil
	}
	if decision != "keep" && decision != "drop" {
		return fmt.Errorf("decision must be \"keep\" or \"drop\", got %q", decision)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("a %s decision needs a reason", decision)
	}
	p, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	var branch string
	var landed bool
	m.readLocked(func() {
		branch = strings.TrimSpace(p.Git.Branch)
		landed = p.Landing != nil && p.Landing.State == LandingLanded
	})
	if branch == "" || landed {
		return nil
	}
	if decision == "keep" {
		_, err := m.KeepLanding(id, actor, reason)
		return err
	}
	_, err := m.DropLanding(id, actor, reason)
	return err
}

// ArchiveGuarded is the archive every operator surface uses (REST, MCP, CLI,
// chat and the web app all reach it through API.Archive): it refuses a
// terminal Automaton whose branch holds commits not on the default branch,
// or cannot be compared, unless a landing decision is recorded. Manager.Archive
// itself stays the raw state change the supervisor loop's close step uses; that
// step has its own branch check (and the autonomous.archive_keep_unmerged
// setting) before it calls it.
func (m *Manager) ArchiveGuarded(id, actor string) (*PRD, error) {
	if p, ok := m.store.GetPRD(id); ok {
		var live bool
		m.readLocked(func() {
			switch p.Status {
			case PRDCompleted, PRDRejected, PRDCancelled:
				live = true
			}
		})
		if live {
			if reason := m.finishedWorkRefusal(p, "archive"); reason != "" {
				return nil, errors.New(reason)
			}
		}
	}
	return m.Archive(id, actor)
}

// ArchiveDecided archives an Automaton, recording the keep or drop decision
// for its landing first when one is passed. Without a decision it is Archive,
// including the guard.
func (m *Manager) ArchiveDecided(id, actor, decision, reason string) (*PRD, error) {
	if strings.TrimSpace(decision) == "" {
		return m.ArchiveGuarded(id, actor)
	}
	if p, ok := m.store.GetPRD(id); ok {
		var terminal bool
		m.readLocked(func() {
			switch p.Status {
			case PRDCompleted, PRDRejected, PRDCancelled, PRDArchived:
				terminal = true
			}
		})
		if !terminal {
			return m.Archive(id, actor) // the not-terminal refusal, nothing recorded
		}
	}
	if err := m.recordLandingDecision(id, actor, decision, reason); err != nil {
		return nil, fmt.Errorf("archive prd %q: %w", id, err)
	}
	return m.ArchiveGuarded(id, actor)
}

// CancelDecided cancels an Automaton, recording the keep or drop decision for
// its landing first when one is passed. Without a decision it is Cancel,
// including the guard.
func (a *API) CancelDecided(id, decision, reason string) error {
	if err := a.M.recordLandingDecision(id, "operator", decision, reason); err != nil {
		return fmt.Errorf("cancel prd %q: %w", id, err)
	}
	return a.Cancel(id)
}

// ---- classifying finished work ----

// finishedWorkOwner is who owns getting an Automaton's work landed: the last
// person or surface that acted on the record, or the operator when only the
// daemon did.
func finishedWorkOwner(p *PRD) string {
	for i := len(p.Decisions) - 1; i >= 0; i-- {
		switch a := strings.TrimSpace(p.Decisions[i].Actor); a {
		case "", "autonomous", "verifier", supervisorLoopActor, unarchiveRepairActor, finishedWorkActor, "landing_ancestor":
		default:
			return a
		}
	}
	return operatorActor
}

// docsLikeTitle says a task title names documentation work.
func docsLikeTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, pre := range []string{"docs", "doc:", "doc ", "document", "changelog", "release note", "update the doc", "write the doc", "update doc"} {
		if strings.HasPrefix(t, pre) {
			return true
		}
	}
	return strings.Contains(t, "documentation")
}

// codeTask says a task is code work: not a smoke or final gate task and not a
// documentation task.
func codeTask(t *Task) bool {
	if t.Kind == "smoke" || t.Kind == "final_gate" {
		return false
	}
	return !docsLikeTitle(t.Title)
}

// codeProgress counts the code tasks and the code tasks complete, and the
// other (docs and gate) tasks still open.
func codeProgress(p *PRD) (code, codeDone, restOpen int) {
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			t := &p.Story[i].Tasks[j]
			if codeTask(t) {
				code++
				if t.Status == TaskCompleted {
					codeDone++
				}
				continue
			}
			if t.Status != TaskCompleted && t.Status != TaskCancelled {
				restOpen++
			}
		}
	}
	return code, codeDone, restOpen
}

// finishedWorkCause classifies a record as finished work waiting to land:
// the cause, and how many docs or gate tasks are still open. ok is false for
// a record that is not finished work (still being worked, or no code done).
func finishedWorkCause(p *PRD) (cause string, restOpen int, ok bool) {
	switch NormalizePRDStatus(p.Status) {
	case PRDCompleted, PRDRejected, PRDCancelled, PRDArchived:
		return FinishedWorkNotMerged, 0, true
	case PRDNeedsReview, PRDRunning, PRDActive, PRDFailed, PRDBlocked:
		code, done, rest := codeProgress(p)
		if code == 0 || done != code {
			return "", 0, false
		}
		if rest > 0 {
			return FinishedWorkCodeDone, rest, true
		}
		// Every task is complete yet the record never completed.
		switch NormalizePRDStatus(p.Status) {
		case PRDNeedsReview, PRDFailed, PRDBlocked:
			return FinishedWorkNotMerged, 0, true
		}
	}
	return "", 0, false
}

// reaskState reads the landing record's re-ask date: whether the branch was
// left alone with a date (hasDate), and whether the date has come.
func reaskState(p *PRD, now time.Time) (hasDate, due bool, at time.Time) {
	if p.Landing == nil || p.Landing.ReaskAt == nil {
		return false, false, time.Time{}
	}
	at = *p.Landing.ReaskAt
	return true, !now.Before(at), at
}

// finishedWorkQuestionKey is the dedupe key of the question for one finding.
func finishedWorkQuestionKey(id, cause, reaskAt string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{finishedWorkAsker, id, cause, reaskAt}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// ---- the sweep ----

type fwCandidate struct {
	id       string
	cause    string
	restOpen int
	reask    bool
	since    time.Time
}

// RunFinishedWorkSweep is one bounded pass. It looks at the Automata that
// finished (or whose code finished) with a branch and no landing decision,
// skips the ones younger than a day, and compares at most
// FinishedWorkSweepLimit branches with the default branch per pass, starting
// after the Automaton the last pass ended on so every one is reached across
// passes. For each branch still holding unmerged commits it records one
// finding with an owner and, in mode on, raises one open question and writes
// one decision-log line; a finding whose work has landed is dropped and its
// question withdrawn. In mode on it also sends the daily digest. Shadow
// records the findings only. The pass reads git read-only.
func (m *Manager) RunFinishedWorkSweep(ctx context.Context, now time.Time) FinishedWorkPass {
	mode := m.Config().EffectiveFinishedWork()
	pass := FinishedWorkPass{Mode: mode, Findings: []FinishedWorkFinding{}}
	sw := m.FinishedWorkSweep()
	if mode == FinishedWorkOff || sw == nil {
		return pass
	}
	repo := strings.TrimSpace(m.archivedBaseRepo())
	if repo == "" {
		return pass
	}
	sw.UpdateState(func(s *FinishedWorkState) { s.LastSweep = now.UTC().Format(time.RFC3339) })

	// Pre-filter on the shared records under the read lock: cheap field reads
	// only, no copies and no git.
	prds := m.store.ListPRDs()
	var cands []fwCandidate
	m.readLocked(func() {
		for _, p := range prds {
			if strings.TrimSpace(p.Git.Branch) == "" || hasShippedRecord(p) {
				continue
			}
			if p.Landing != nil && (p.Landing.State == LandingLanded || p.Landing.State == LandingDropped) {
				continue
			}
			cause, rest, ok := finishedWorkCause(p)
			if !ok {
				continue
			}
			c := fwCandidate{id: p.ID, cause: cause, restOpen: rest, since: p.UpdatedAt}
			has, due, _ := reaskState(p, now)
			switch {
			case p.Landing != nil && p.Landing.State == LandingKept && !has:
				continue // a standing keep decision with no date
			case has && !due:
				continue // left alone until its re-ask date
			case has && due:
				c.cause, c.reask = FinishedWorkReask, true
			}
			if f, ok := sw.Get(p.ID); ok && f.Since != "" {
				if t, err := time.Parse(time.RFC3339, f.Since); err == nil {
					c.since = t
				}
			}
			if !c.reask && now.Sub(c.since) < FinishedWorkAfter {
				pass.Younger++
				continue
			}
			cands = append(cands, c)
		}
	})
	sort.Slice(cands, func(i, j int) bool { return cands[i].id < cands[j].id })

	// Rotate so a capped pass resumes after the Automaton the last one ended on.
	start := 0
	sw.mu.Lock()
	cursor := sw.cursor
	sw.mu.Unlock()
	if cursor != "" {
		start = sort.Search(len(cands), func(i int) bool { return cands[i].id > cursor })
		if start >= len(cands) {
			start = 0
		}
	}
	git := m.archivedUnmergedGitRunner()
	last := ""
	for k := 0; k < len(cands); k++ {
		if ctx.Err() != nil {
			break
		}
		if pass.GitChecks >= FinishedWorkSweepLimit {
			pass.Capped = true
			break
		}
		c := cands[(start+k)%len(cands)]
		last = c.id
		p, ok := m.store.SnapshotPRD(c.id)
		if !ok {
			continue
		}
		pass.GitChecks++
		pass.Examined++
		cctx, cancel := context.WithTimeout(ctx, finishedWorkGitTimeout)
		state, ahead, detail := checkBranchAhead(cctx, git, repo, p.Git.Branch, p.Git.BaseBranch)
		cancel()
		switch state {
		case branchCheckAhead:
			f := m.finishedWorkFinding(p, c, ahead, now)
			pass.Found++
			m.actOnFinishedWork(sw, mode, p, &f, c, now, &pass)
			pass.Findings = append(pass.Findings, f)
		case branchCheckUnknown:
			if m.noteRefusal(p.ID, "sweep", "the state of branch "+p.Git.Branch+" could not be checked: "+detail) {
				log.Printf("[autonomous] finished work sweep: %s branch %s unknown, skipping until it can be checked: %s", p.ID, p.Git.Branch, detail)
			}
		default: // merged or the branch is gone: nothing waits to land
			m.finishedWorkLanded(sw, p.ID)
		}
	}
	if pass.Capped || cursorWrapped(cands, last) {
		sw.mu.Lock()
		sw.cursor = last
		sw.mu.Unlock()
	} else {
		sw.mu.Lock()
		sw.cursor = ""
		sw.mu.Unlock()
	}
	if mode == FinishedWorkOn {
		pass.Digest = m.sendFinishedWorkDigest(sw, now)
	}
	return pass
}

// cursorWrapped is false: a pass that reached the end of the list starts over
// from the first Automaton next time.
func cursorWrapped(cands []fwCandidate, last string) bool { return false }

func (m *Manager) finishedWorkFinding(p *PRD, c fwCandidate, ahead int, now time.Time) FinishedWorkFinding {
	f := FinishedWorkFinding{
		Automaton: p.ID, Title: p.Title, Status: string(p.Status), Cause: c.cause,
		At: now.UTC().Format(time.RFC3339), Owner: finishedWorkOwner(p),
		Branch: p.Git.Branch, Ahead: ahead, Since: c.since.UTC().Format(time.RFC3339),
	}
	if f.Title == "" {
		f.Title = p.ID
	}
	switch c.cause {
	case FinishedWorkCodeDone:
		f.Action = fmt.Sprintf("land the code; the %d other task(s) (docs or gates) can be retried separately with reset-task", c.restOpen)
	case FinishedWorkReask:
		f.Action = "decide again: land it, leave it alone for another period, or drop it"
	default:
		f.Action = "land the branch, or record a keep or drop decision for its landing"
	}
	if p.Landing != nil && p.Landing.ReaskAt != nil {
		f.ReaskAt = p.Landing.ReaskAt.UTC().Format(time.RFC3339)
	}
	return f
}

// actOnFinishedWork stores the finding and, in mode on, asks the one question.
func (m *Manager) actOnFinishedWork(sw *FinishedWorkSweep, mode string, p *PRD, f *FinishedWorkFinding, c fwCandidate, now time.Time, pass *FinishedWorkPass) {
	if mode != FinishedWorkOn {
		sw.Upsert(*f)
		return
	}
	if prev, ok := sw.Get(p.ID); ok && prev.Cause == f.Cause {
		f.QuestionID = prev.QuestionID
	}
	if f.QuestionID == "" || !m.finishedWorkQuestionOpen(f.QuestionID) {
		q, isNew := m.raiseFinishedWorkQuestion(p, *f, c)
		f.QuestionID = q.ID
		if isNew {
			pass.Asked++
			if c.reask {
				pass.Reasked++
			}
			m.appendFinishedWorkDecision(p.ID, *f, q.ID, now)
		}
	}
	sw.Upsert(*f)
}

func (m *Manager) finishedWorkQuestionOpen(id string) bool {
	if id == "" {
		return false
	}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return false
	}
	q, ok := store.Get(id)
	return ok && q.State == QuestionOpen
}

// buildFinishedWorkQuestion builds the one question about an Automaton's
// finished work. The default is to leave the branch alone, which sets a re-ask
// date, so an unanswered question never lands, drops or loses anything.
func buildFinishedWorkQuestion(p *PRD, f FinishedWorkFinding, c fwCandidate) Question {
	name := f.Title
	var text string
	switch f.Cause {
	case FinishedWorkCodeDone:
		text = fmt.Sprintf("Every code task of Automaton %s (%s) is complete, and its branch %s has %d commit(s) that are not on the default branch, but %d other task(s) (docs or gates) are still open. Those can be retried separately; the code does not have to wait for them. Owner: %s. What should happen to the code?",
			p.ID, name, f.Branch, f.Ahead, c.restOpen, f.Owner)
	case FinishedWorkReask:
		text = fmt.Sprintf("Automaton %s (%s) was left alone until %s and its branch %s still has %d commit(s) that are not on the default branch. Owner: %s. What should happen to it now?",
			p.ID, name, shortDate(f.ReaskAt), f.Branch, f.Ahead, f.Owner)
	default:
		text = fmt.Sprintf("Automaton %s (%s) finished (%s) and its branch %s has %d commit(s) that are not on the default branch. Finished work must not sit unmerged. Owner: %s. What should happen to it?",
			p.ID, name, f.Status, f.Branch, f.Ahead, f.Owner)
	}
	return Question{
		Label:     "decision",
		AskedBy:   finishedWorkAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextPrd,
		PRDID:     p.ID,
		Text:      text,
		Options: []QuestionOption{
			{ID: FinishedWorkOptionLand, Label: "I will land it",
				Tradeoffs: fmt.Sprintf("The branch stays unmerged and cannot be archived; you are asked again in %d days if it is still not on the default branch.", FinishedWorkLandReaskDays)},
			{ID: FinishedWorkOptionLeave, Label: "leave it alone",
				Tradeoffs: fmt.Sprintf("Records a dated keep decision; you are asked again in %d days if it is still not on the default branch.", FinishedWorkReaskDays)},
			{ID: FinishedWorkOptionDrop, Label: "drop the work",
				Tradeoffs: "Records a drop: the work will not be merged and the Automaton can be archived. The branch itself is not deleted."},
		},
		Default:          FinishedWorkOptionLeave,
		Priority:         QuestionPriorityNormal,
		SourceLineSHA256: finishedWorkQuestionKey(p.ID, f.Cause, f.ReaskAt),
	}
}

func shortDate(rfc string) string {
	if t, err := time.Parse(time.RFC3339, rfc); err == nil {
		return t.Format("2006-01-02")
	}
	return rfc
}

// raiseFinishedWorkQuestion raises the question through the operator channel.
// Nothing is raised when the channel is off or no store is wired: the finding,
// the digest and the alert still carry the facts. It reports the question and
// whether it is new.
func (m *Manager) raiseFinishedWorkQuestion(p *PRD, f FinishedWorkFinding, c fwCandidate) (Question, bool) {
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return Question{}, false
	}
	q, isNew, err := store.RaiseUnlessOpenSourceLine(buildFinishedWorkQuestion(p, f, c))
	if err != nil {
		log.Printf("[autonomous] prd=%s finished work question: %v", p.ID, err)
		return Question{}, false
	}
	return q, isNew
}

// appendFinishedWorkDecision writes the one decision-log line for a question
// the daemon raised about finished work.
func (m *Manager) appendFinishedWorkDecision(id string, f FinishedWorkFinding, questionID string, now time.Time) {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		p.Decisions = append(p.Decisions, Decision{At: now, Kind: DecisionFinishedWorkQuestion, Actor: finishedWorkActor,
			Note:       fmt.Sprintf("%s: branch %s has %d commit(s) not on the default branch; owner %s; asked what to do", f.Cause, f.Branch, f.Ahead, f.Owner),
			QuestionID: questionID})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s finished work decision: %v", id, err)
	}
}

// finishedWorkLanded drops the finding of an Automaton whose work is on the
// default branch (or whose branch is gone) and withdraws its open question.
func (m *Manager) finishedWorkLanded(sw *FinishedWorkSweep, id string) {
	f, had := sw.Remove(id)
	if !had {
		return
	}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Asker: finishedWorkAsker, PRDID: id, Limit: 100}) {
		if _, err := store.Withdraw(q.ID, finishedWorkActor, "the branch is on the default branch now"); err != nil && !errors.Is(err, ErrQuestionNotOpen) {
			log.Printf("[autonomous] prd=%s withdraw finished work question %s: %v", id, q.ID, err)
		}
	}
	_ = f
}

// sendFinishedWorkDigest sends the daily digest alert: everything waiting to
// land, each with its owner and open question. At most one a day, and only
// when something waits.
func (m *Manager) sendFinishedWorkDigest(sw *FinishedWorkSweep, now time.Time) bool {
	st := sw.State()
	if st.LastDigest != "" {
		if t, err := time.Parse(time.RFC3339, st.LastDigest); err == nil && now.Sub(t) < FinishedWorkDigestEvery {
			return false
		}
	}
	rows := sw.SweepFindings()
	if len(rows) == 0 {
		return false
	}
	sink := m.AlertSink()
	if sink == nil {
		return false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d Automaton(s) have finished work that is not on the default branch:\n", len(rows))
	for i, f := range rows {
		if i >= finishedWorkDigestRows {
			fmt.Fprintf(&b, "... and %d more (datawatch autonomous finished-work lists them all)\n", len(rows)-i)
			break
		}
		fmt.Fprintf(&b, "- %s %s: %s, %d commit(s) on %s, owner %s", f.Automaton, f.Title, f.Cause, f.Ahead, f.Branch, f.Owner)
		if f.QuestionID != "" {
			fmt.Fprintf(&b, ", question %s", f.QuestionID)
		}
		b.WriteString("\n")
	}
	sink("warn", fmt.Sprintf("Finished work waiting to land: %d", len(rows)), b.String())
	sw.UpdateState(func(s *FinishedWorkState) { s.LastDigest = now.UTC().Format(time.RFC3339) })
	return true
}

// FinishedWorkDigest reads the work waiting to land from the stored findings.
// It runs no git and changes nothing.
func (m *Manager) FinishedWorkDigest(now time.Time) FinishedWorkDigest {
	d := FinishedWorkDigest{Mode: m.Config().EffectiveFinishedWork(), GeneratedAt: now.UTC().Format(time.RFC3339), Waiting: []FinishedWorkFinding{}}
	sw := m.FinishedWorkSweep()
	if sw == nil {
		return d
	}
	d.Waiting = sw.SweepFindings()
	st := sw.State()
	d.LastSweep, d.LastDigest, d.LastAudit, d.AuditRecord = st.LastSweep, st.LastDigest, st.LastAudit, st.AuditRecordID
	return d
}

// FinishedWorkTick is what the idle tick calls: a sweep pass when the last one
// is at least FinishedWorkSweepEvery old, and the weekly audit when the last
// one is at least a week old.
func (m *Manager) FinishedWorkTick(ctx context.Context, now time.Time) {
	if m == nil || m.Config().EffectiveFinishedWork() == FinishedWorkOff {
		return
	}
	sw := m.FinishedWorkSweep()
	if sw == nil {
		return
	}
	st := sw.State()
	if t, err := time.Parse(time.RFC3339, st.LastSweep); err != nil || now.Sub(t) >= FinishedWorkSweepEvery {
		m.RunFinishedWorkSweep(ctx, now)
	}
	if t, err := time.Parse(time.RFC3339, st.LastAudit); err != nil || now.Sub(t) >= FinishedWorkAuditEvery {
		if _, err := m.RunFinishedWorkAudit(ctx, now); err != nil {
			log.Printf("[autonomous] finished work audit: %v", err)
		}
	}
}

// ---- answers and leave-alone decisions ----

// recordLeaveAlone records the dated decision for a branch left alone: a
// decision-log line that names the re-ask date and a landing record carrying
// that date (kept when the Automaton is finished, pending while it is not, so
// an unfinished one still cannot be archived). A branch that already landed
// has nothing to leave. The daemon asks again on the date if the branch is
// still not on the default branch.
func (m *Manager) recordLeaveAlone(id, actor, note, questionID, answerID string) error {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %s not found", id)
	}
	now := m.holdNow()
	reask := now.AddDate(0, 0, FinishedWorkReaskDays)
	m.store.WithLock(func() {
		branch := strings.TrimSpace(p.Git.Branch)
		if branch != "" && !(p.Landing != nil && p.Landing.State == LandingLanded) {
			state := LandingPending
			if closedWorkStatus(p.Status) {
				state = LandingKept
			}
			r := reask
			p.Landing = &Landing{State: state, Actor: actor, At: now, Reason: note, ReaskAt: &r}
		}
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: now, Kind: DecisionLeaveAlone, Actor: actor,
			Note:       fmt.Sprintf("%s; the daemon asks again on %s if the work is still not on the default branch", note, reask.Format("2006-01-02")),
			QuestionID: questionID, AnswerID: answerID}))
	})
	return m.store.SavePRD(p)
}

// recordLandByHand records that someone will land the branch themselves: the
// landing stays pending (so the Automaton cannot be archived) with a short
// re-ask date.
func (m *Manager) recordLandByHand(id, actor, questionID, answerID string) error {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %s not found", id)
	}
	now := m.holdNow()
	reask := now.AddDate(0, 0, FinishedWorkLandReaskDays)
	m.store.WithLock(func() {
		if strings.TrimSpace(p.Git.Branch) != "" && !(p.Landing != nil && p.Landing.State == LandingLanded) {
			r := reask
			p.Landing = &Landing{State: LandingPending, Actor: actor, At: now, Reason: "will be landed by hand", ReaskAt: &r}
		}
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: now, Kind: DecisionLandByHand, Actor: actor,
			Note:       fmt.Sprintf("will be landed by hand; the daemon asks again on %s if the work is still not on the default branch", reask.Format("2006-01-02")),
			QuestionID: questionID, AnswerID: answerID}))
	})
	return m.store.SavePRD(p)
}

// onFinishedWorkAnswer routes the answer to a finished-work question: land by
// hand, leave it alone or drop it. It reports whether the change was one of
// these questions, so the caller does not hand it to the other handlers.
func (m *Manager) onFinishedWorkAnswer(c QuestionChange) bool {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || c.Answer == nil ||
		q.AskedBy != finishedWorkAsker || q.AskerKind != QuestionAskerSystem {
		return false
	}
	if q.PRDID == "" {
		return true
	}
	actor := strings.TrimSpace(c.Answer.Actor)
	if actor == "" {
		actor = operatorActor
	}
	var err error
	switch c.Answer.ChosenOptionID {
	case FinishedWorkOptionLand:
		err = m.recordLandByHand(q.PRDID, actor, q.ID, c.Answer.ID)
	case FinishedWorkOptionLeave:
		err = m.recordLeaveAlone(q.PRDID, actor, "answered the finished-work question: leave it alone", q.ID, c.Answer.ID)
	case FinishedWorkOptionDrop:
		_, err = m.DropLanding(q.PRDID, actor, "answered the finished-work question: drop the work")
	default:
		err = fmt.Errorf("unknown finished-work option %q", c.Answer.ChosenOptionID)
	}
	if err != nil {
		log.Printf("[autonomous] prd=%s finished work answer to question %s: %v", q.PRDID, q.ID, err)
	}
	return true
}

// ---- the weekly audit ----

// RunFinishedWorkAudit is the weekly audit: a full read over every Automaton,
// cancelled, archived and branchless ones included, with no per-pass bound.
// It finds finished work not on the default branch, work offered for landing,
// left-alone branches whose date came, work branches that are gone with
// nothing recording a landing, and unfinished Automata nothing touched for a
// week. It never asks, never changes a record and reads git read-only. The
// findings replace the stored set; in mode on the result is also written as
// one decision record per ISO week, so a second run in the same week returns
// the same record.
func (m *Manager) RunFinishedWorkAudit(ctx context.Context, now time.Time) (FinishedWorkAuditResult, error) {
	mode := m.Config().EffectiveFinishedWork()
	res := FinishedWorkAuditResult{Mode: mode, At: now.UTC().Format(time.RFC3339), ByCause: map[string]int{}, Findings: []FinishedWorkFinding{}}
	sw := m.FinishedWorkSweep()
	if mode == FinishedWorkOff || sw == nil {
		return res, nil
	}
	repo := strings.TrimSpace(m.archivedBaseRepo())
	git := m.archivedUnmergedGitRunner()
	for _, p := range m.store.ListPRDs() {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		p, ok := m.store.SnapshotPRD(p.ID)
		if !ok {
			continue
		}
		res.Scanned++
		f, found := m.auditRecord(ctx, git, repo, p, now)
		if !found {
			continue
		}
		res.ByCause[f.Cause]++
		res.Findings = append(res.Findings, f)
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		if res.Findings[i].Cause != res.Findings[j].Cause {
			return res.Findings[i].Cause < res.Findings[j].Cause
		}
		return res.Findings[i].Automaton < res.Findings[j].Automaton
	})
	sw.Replace(res.Findings)
	week := isoWeekKey(now)
	summary := fmt.Sprintf("%d finding(s) over %d Automata", len(res.Findings), res.Scanned)
	st := sw.State()
	if mode == FinishedWorkOn {
		id, err := m.writeAuditDecisionRecord(res, st, week)
		if err != nil {
			return res, err
		}
		res.DecisionRecordID = id
	}
	sw.UpdateState(func(s *FinishedWorkState) {
		s.LastAudit, s.LastAuditSummary = now.UTC().Format(time.RFC3339), summary
		if res.DecisionRecordID != "" {
			s.AuditWeek, s.AuditRecordID = week, res.DecisionRecordID
		}
	})
	return res, nil
}

// auditRecord classifies one record for the audit.
func (m *Manager) auditRecord(ctx context.Context, git MergeGitRunner, repo string, p *PRD, now time.Time) (FinishedWorkFinding, bool) {
	if hasShippedRecord(p) {
		return FinishedWorkFinding{}, false // delivered
	}
	if p.Landing != nil && (p.Landing.State == LandingLanded || p.Landing.State == LandingDropped) {
		return FinishedWorkFinding{}, false
	}
	branch := strings.TrimSpace(p.Git.Branch)
	base := FinishedWorkFinding{
		Automaton: p.ID, Title: p.Title, Status: string(p.Status), At: now.UTC().Format(time.RFC3339),
		Owner: finishedWorkOwner(p), Branch: branch, Since: p.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if base.Title == "" {
		base.Title = p.ID
	}
	has, due, reaskAt := reaskState(p, now)
	if has {
		base.ReaskAt = reaskAt.UTC().Format(time.RFC3339)
	}
	standingKeep := p.Landing != nil && p.Landing.State == LandingKept && !has
	if branch != "" && repo != "" && !standingKeep && !(has && !due) {
		cctx, cancel := context.WithTimeout(ctx, finishedWorkGitTimeout)
		state, ahead, _ := checkBranchAhead(cctx, git, repo, branch, p.Git.BaseBranch)
		cancel()
		switch state {
		case branchCheckAhead:
			base.Ahead = ahead
			if cause, rest, ok := finishedWorkCause(p); ok {
				base.Cause = cause
				if has && due {
					base.Cause = FinishedWorkReask
				}
				if cause == FinishedWorkCodeDone {
					base.Action = fmt.Sprintf("land the code; the %d other task(s) (docs or gates) can be retried separately with reset-task", rest)
				} else {
					base.Action = "land the branch, or record a keep or drop decision for its landing"
				}
				return base, true
			}
		case branchCheckDropped:
			if p.MergeCommit == "" && completedTasks(p) > 0 && closedWorkStatus(p.Status) {
				base.Cause = FinishedWorkBranchGone
				base.Action = "find where the work went: nothing records that it landed; restore the branch or record a drop"
				return base, true
			}
		}
	}
	if stuckStatus(p.Status) && now.Sub(p.UpdatedAt) >= FinishedWorkStuckAfter {
		base.Cause = FinishedWorkStuck
		base.Action = fmt.Sprintf("no change for %d day(s): resume it, cancel it with a decision, or hold it", int(now.Sub(p.UpdatedAt).Hours()/24))
		return base, true
	}
	return FinishedWorkFinding{}, false
}

func completedTasks(p *PRD) int {
	n := 0
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			if p.Story[i].Tasks[j].Status == TaskCompleted {
				n++
			}
		}
	}
	return n
}

func stuckStatus(s PRDStatus) bool {
	switch NormalizePRDStatus(s) {
	case PRDRunning, PRDActive, PRDNeedsReview, PRDBlocked, PRDFailed:
		return true
	}
	return false
}

func isoWeekKey(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// writeAuditDecisionRecord writes the audit result as one decision record per
// ISO week. A second call in the same week returns the first record's id.
func (m *Manager) writeAuditDecisionRecord(res FinishedWorkAuditResult, st FinishedWorkState, week string) (string, error) {
	if st.AuditWeek == week && st.AuditRecordID != "" {
		return st.AuditRecordID, nil
	}
	m.mu.Lock()
	drs := m.decisionRecords
	m.mu.Unlock()
	if drs == nil {
		log.Printf("[autonomous] finished work audit: no decision record service wired; the findings are stored but no record was written")
		return "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The weekly audit read %d Automata and found %d that need a look.", res.Scanned, len(res.Findings))
	causes := make([]string, 0, len(res.ByCause))
	for c := range res.ByCause {
		causes = append(causes, c)
	}
	sort.Strings(causes)
	for _, c := range causes {
		fmt.Fprintf(&b, " %s: %d.", c, res.ByCause[c])
	}
	b.WriteString(" The first ones:")
	for i, f := range res.Findings {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&b, " %s (%s, owner %s);", f.Automaton, f.Cause, f.Owner)
	}
	rec := DecisionRecord{
		Title:     fmt.Sprintf("Weekly finished-work audit %s: %d to look at", week, len(res.Findings)),
		Situation: b.String(),
		Deciding:  "Whether to act on the list now. The full list is in the finished-work digest; each Automaton on it needs a land, keep or drop decision.",
		Options: []DecisionOption{
			{ID: "act", Label: "act on the list", Tradeoff: "You work through the findings and record a decision for each."},
			{ID: "later", Label: "look at it later", Tradeoff: "The findings stay in the digest and are checked again at the next audit."},
		},
		Default: "later",
	}
	rec.Links = DecisionLinks{}
	v, _, err := drs.Create(rec, "", "")
	if err != nil {
		if errors.Is(err, ErrDecisionRecordDuplicate) {
			return st.AuditRecordID, nil
		}
		if v.ID != "" {
			return v.ID, nil // stored; the question syncs on the next poll
		}
		return "", fmt.Errorf("write the audit decision record: %w", err)
	}
	return v.ID, nil
}

// ---- API wrappers (the operator surfaces reach the rule through these) ----

// ArchiveDecided is the operator archive with an optional keep or drop
// decision for the landing. Without a decision it is Archive, guard included.
func (a *API) ArchiveDecided(id, decision, reason string) (any, error) {
	out, err := a.M.ArchiveDecided(id, operatorActor, decision, reason)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// FinishedWorkDigest reads the work waiting to land.
func (a *API) FinishedWorkDigest() FinishedWorkDigest {
	return a.M.FinishedWorkDigest(time.Now())
}

// RunFinishedWorkSweep runs one bounded sweep pass now.
func (a *API) RunFinishedWorkSweep(ctx context.Context) FinishedWorkPass {
	return a.M.RunFinishedWorkSweep(ctx, time.Now())
}

// RunFinishedWorkAudit runs the audit now.
func (a *API) RunFinishedWorkAudit(ctx context.Context) (FinishedWorkAuditResult, error) {
	return a.M.RunFinishedWorkAudit(ctx, time.Now())
}
