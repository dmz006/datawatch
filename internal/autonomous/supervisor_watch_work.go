// Supervisor watch work — the daemon-side checks over running work the
// supervisor watch raises on each coordinator poll, with no model call:
//
//   - a task of an Automaton failed (task_failed: the triage record the
//     Automaton already carries names the task, and the task sits in its
//     failed state);
//
//   - a task of an Automaton is now waiting for review
//     (task_review_needed: a running or blocked Automaton has a task in
//     its review state — read from the tasks, because the triage record
//     never names a task held for review);
//
//   - an Automaton finished all of its work but is not merged
//     (automaton_not_merged: its triage record sits at merge_pending —
//     the state merge_pending_pass.go writes — and every one of its
//     tasks is complete; the pass's own comparison is not re-run here);
//
//   - a lane is idle while eligible work is waiting
//     (idle_lane_eligible_work: the existing lane fill status says a
//     lane is idle and its queued-eligible count is non-zero; the
//     eligibility rules themselves live in lane_fill_eligibility.go
//     and are not re-implemented here);
//
//   - a CI run is red, or is stuck (ci_red / ci_stuck: the existing
//     repo CI watch records say so — the record's status is "failing"
//     for red and "hung" for stuck; no call to GitHub and no new CI
//     state from here).
//
//   - an Automaton is waiting above the priority floor past the stall
//     threshold (priority_stall: the Automaton sits in the waiting set
//     — draft, planning, needs_review, approved or revisions_asked —
//     has a resolved priority at or above the priority floor, and has
//     now been waiting strictly longer than the stall threshold);
//
//   - the consolidated backlog (backlog: the single finding listing
//     every waiting Automaton plus every failed or blocked Automaton
//     that still has work left, sorted by highest resolved priority
//     first, within the same priority by oldest created first, capped
//     at sixteen entries with a "+k more" tail).
//
// Like the host checks (supervisor_watch_host.go), the checks are pure
// over their inputs: they read state that was already observed and
// mint a finding from it, and they never act. The checks take their
// views through the SupervisorWatchWork struct (PRD lister, lane fill
// status, repo CI watch records), so tests run against fakes and a
// production coordinator wires the live ones.
package autonomous

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// SupervisorWatchPRDLister is the live view of the Automata the work
// checks read. The coordinator's PRD store satisfies this shape; tests
// use a fake that returns fixed PRDs. A nil lister reads as no
// Automata: the task and Automaton checks then find nothing.
type SupervisorWatchPRDLister interface {
	PRDs() []*PRD
}

// SupervisorWatchLaneFillStatuser is the lane fill status view the
// idle-lane check reads. The coordinator's lane fill satisfies this
// shape (or wraps its laneFillStatus); tests use a fake that returns a
// fixed status. A nil statuser, or one reporting no status, reads as
// "lane fill is not configured": the check finds nothing and never
// guesses a lane.
type SupervisorWatchLaneFillStatuser interface {
	LaneFillStat() (LaneFillStatus, bool)
}

// SupervisorWatchCILister is the view of the existing repo CI watch
// records the CI checks read — the same records the repo CI watch
// already polls. *RepoCIWatchStore satisfies this shape via its List
// method (wrapped by the coordinator); tests use a fake that returns
// fixed records. A nil lister reads as "no repo CI watch configured":
// the CI checks find nothing, without ever calling GitHub.
type SupervisorWatchCILister interface {
	CIWatchRecords() []*RepoCIWatchRecord
}

// SupervisorWatchWork runs the daemon-side supervisor watch checks over
// running work. It holds the injected views the checks read and the
// set of finding keys already raised, so the same finding is reported
// only the first time it is observed on a consecutive poll: once a
// finding has been raised (and recorded by the shared finding store,
// see supervisor_watch_record.go), a later check of the same
// kind+subject is not raised again by this watch. In shadow mode (the
// release default, SupervisorWatchConfig.EffectiveMode) the watch
// records, never acts — and the same is true in on mode, for this
// step: the checks only compute what to report.
type SupervisorWatchWork struct {
	PRDs     SupervisorWatchPRDLister
	LaneFill SupervisorWatchLaneFillStatuser
	CI       SupervisorWatchCILister
	// Stall, when set, names the blocker and action of each priority stall
	// finding. Nil leaves the finding without them.
	Stall LaneFillDecisionSource

	mu     sync.Mutex
	raised map[string]bool
}

// NewSupervisorWatchWork wires the work checks over the given views.
// Any nil view degrades its checks to "finds nothing" rather than
// panicking — a missing dependency must never mint a finding.
func NewSupervisorWatchWork(
	prds SupervisorWatchPRDLister,
	laneFill SupervisorWatchLaneFillStatuser,
	ci SupervisorWatchCILister,
) *SupervisorWatchWork {
	return &SupervisorWatchWork{
		PRDs:     prds,
		LaneFill: laneFill,
		CI:       ci,
		raised:   map[string]bool{},
	}
}

// Raise marks the finding as already raised, so an identical
// kind+subject is not raised again on a later poll. Called by the
// coordinator after it has recorded the finding in the store.
func (w *SupervisorWatchWork) Raise(f SupervisorWatchFinding) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if f.Kind != "" {
		w.raised[SupervisorWatchKey(f.Kind, f.Subject)] = true
	}
}

func (w *SupervisorWatchWork) isRaised(kind SupervisorWatchKind, subject string) bool {
	key := SupervisorWatchKey(kind, subject)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.raised[key]
}

func (w *SupervisorWatchWork) markRaised(findings []SupervisorWatchFinding) {
	if len(findings) == 0 {
		return
	}
	w.mu.Lock()
	for _, f := range findings {
		w.raised[SupervisorWatchKey(f.Kind, f.Subject)] = true
	}
	w.mu.Unlock()
}

// CheckTaskFailed returns one task_failed finding per Automaton whose
// triage record names a task and that task — looked up among the
// Automaton's own stories' tasks — is in its failed state. The finding
// names the Automaton (the finding's subject is the Automaton id, the
// same subject the finding text inlines). Automata without a triage
// record, whose record names no task, whose record names a task that
// is not among the tasks, or whose named task is in any state other
// than failed are skipped. A finding already raised (see Raise) is not
// raised again; a newly minted finding is marked raised when it is
// returned.
func (w *SupervisorWatchWork) CheckTaskFailed() []SupervisorWatchFinding {
	var out []SupervisorWatchFinding
	for _, p := range w.sortedPRDs() {
		if !supervisorWatchTriageNamesFailedTask(p, TaskFailed) {
			continue
		}
		if w.isRaised(SupervisorWatchTaskFailed, p.ID) {
			continue
		}
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchTaskFailed, p.ID))
	}
	w.markRaised(out)
	return out
}

// CheckTaskReviewNeeded returns one task_review_needed finding per
// running or blocked Automaton that has a task held in its review
// state. It reads the tasks themselves, not the triage record: the
// record is built from failed or blocked tasks and never names a task
// that was held for review, so an Automaton that escalated a task to
// review (and went blocked) carries no task id the record could be
// matched on. A task already reported as failed is not also reported
// here, and an Automaton with several held tasks is one finding (the
// subject is the Automaton id). A finding already raised is not raised
// again; a newly minted finding is marked raised when it is returned.
func (w *SupervisorWatchWork) CheckTaskReviewNeeded() []SupervisorWatchFinding {
	var out []SupervisorWatchFinding
	for _, p := range w.sortedPRDs() {
		if !supervisorWatchHasReviewHeldTask(p) {
			continue
		}
		if w.isRaised(SupervisorWatchTaskReviewNeeded, p.ID) {
			continue
		}
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchTaskReviewNeeded, p.ID))
	}
	w.markRaised(out)
	return out
}

// CheckAutomatonNotMerged returns one automaton_not_merged finding per
// Automaton whose triage record sits at merge_pending — the state
// merge_pending_pass.go writes when the work is ready but not merged —
// and whose work is complete: every one of its tasks is in its
// terminal completed state, and it has at least one task (an Automaton
// with no tasks has nothing that could have been merged). The record's
// own state is read, not recomputed: the pass's merge readiness
// comparison is never re-run here. A finding already raised is not
// raised again; a newly minted finding is marked raised when it is
// returned.
func (w *SupervisorWatchWork) CheckAutomatonNotMerged() []SupervisorWatchFinding {
	var out []SupervisorWatchFinding
	for _, p := range w.sortedPRDs() {
		if p.Triage == nil || p.Triage.Status != TriageStatusMergePending {
			continue
		}
		if !supervisorWatchAllTasksCompleted(p) {
			continue
		}
		if w.isRaised(SupervisorWatchAutomatonNotMerged, p.ID) {
			continue
		}
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchAutomatonNotMerged, p.ID))
	}
	w.markRaised(out)
	return out
}

// CheckIdleLane returns one idle_lane_eligible_work finding per lane
// that is idle and has eligible, ready-to-run work waiting on its
// backend: the lane fill status says the lane's state is idle and the
// queued-eligible count for the lane's backend type is non-zero. The
// counts are the lane fill's own (see lane_fill_eligibility.go); no
// eligibility rules are re-implemented here and an unconfigured or
// unavailable lane fill finds nothing. Lane findings are sorted by
// lane id so the output is deterministic. A finding already raised is
// not raised again; a newly minted finding is marked raised when it is
// returned.
func (w *SupervisorWatchWork) CheckIdleLane() []SupervisorWatchFinding {
	if w.LaneFill == nil {
		return nil
	}
	st, ok := w.LaneFill.LaneFillStat()
	if !ok {
		return nil
	}
	var out []SupervisorWatchFinding
	for _, lane := range st.Lanes {
		if lane.State != "idle" {
			continue
		}
		if supervisorWatchQueuedEligible(lane.Type, st.QueuedEligible) == 0 {
			continue
		}
		if w.isRaised(SupervisorWatchIdleLaneEligibleWork, lane.ID) {
			continue
		}
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchIdleLaneEligibleWork, lane.ID))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	w.markRaised(out)
	return out
}

// CheckCIRed returns one ci_red finding per repo CI watch record whose
// status is failing — the run red. The records are the existing repo
// CI watch's (see repo_ci_watch_record.go); no call to GitHub is made
// here and no new CI state is minted. An unconfigured or unavailable
// repo CI watch finds nothing. Findings are sorted by run id so the
// output is deterministic. A finding already raised is not raised
// again; a newly minted finding is marked raised when it is returned.
func (w *SupervisorWatchWork) CheckCIRed() []SupervisorWatchFinding {
	var out []SupervisorWatchFinding
	for _, r := range w.sortedCIRecords() {
		if r == nil || r.Status != "failing" {
			continue
		}
		if w.isRaised(SupervisorWatchCIRed, r.RunID) {
			continue
		}
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchCIRed, r.RunID))
	}
	w.markRaised(out)
	return out
}

// CheckCISTuck returns one ci_stuck finding per repo CI watch record
// whose status is hung — the run stuck (the repo CI watch has already
// decided the run stopped progressing; this check only re-raises the
// state the watch recorded). As with CheckCIRed, no call to GitHub is
// made here and an unconfigured or unavailable repo CI watch finds
// nothing. Findings are sorted by run id; a finding already raised is
// not raised again; a newly minted finding is marked raised when it is
// returned.
func (w *SupervisorWatchWork) CheckCISTuck() []SupervisorWatchFinding {
	var out []SupervisorWatchFinding
	for _, r := range w.sortedCIRecords() {
		if r == nil || r.Status != "hung" {
			continue
		}
		if w.isRaised(SupervisorWatchCISTuck, r.RunID) {
			continue
		}
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchCISTuck, r.RunID))
	}
	w.markRaised(out)
	return out
}

// supervisorWatchInWaitingStatus reports whether s is one of the
// waiting statuses of the supervisor watch: draft, planning,
// needs_review, approved or revisions_asked. Held, running, and every
// closed status — as well as failed and blocked, which are their own
// queue problems — are not waiting; held is by the operator's decision
// a pause, not a stall, and sortedPRDs drops it from the read view
// anyway.
func supervisorWatchInWaitingStatus(s PRDStatus) bool {
	switch s {
	case PRDDraft, PRDPlanning, PRDNeedsReview, PRDApproved, PRDRevisionsAsked:
		return true
	default:
		return false
	}
}

// supervisorWatchWaitingAge returns the whole-minute age of a waiting
// Automaton at now: now minus the Automaton's creation instant, clamped
// at zero for the case the clock moved backwards, or (0, false) when
// the Automaton is not in the waiting set or has no creation instant —
// a zero creation instant reads as not started yet, and never as the
// oldest thing in the queue.
func supervisorWatchWaitingAge(p *PRD, now time.Time) (int, bool) {
	if p == nil || !supervisorWatchInWaitingStatus(p.Status) || p.CreatedAt.IsZero() {
		return 0, false
	}
	mins := int(now.Sub(p.CreatedAt) / time.Minute)
	if mins < 0 {
		mins = 0
	}
	return mins, true
}

// CheckPriorityStall raises one finding per waiting Automaton that
// meets both queue conditions at once: its priority is at or above the
// configured priority floor (an explicit floor of zero admits whatever
// priorities are in the queue), and it has been waiting strictly longer
// than the configured stall threshold measured from its creation
// instant. The check reads only the PRDs and the config it is handed —
// it does nothing, does not touch triage, and does not re-report an
// Automaton already raised for the same kind+subject pair.
func (w *SupervisorWatchWork) CheckPriorityStall(now time.Time, cfg SupervisorWatchConfig) []SupervisorWatchFinding {
	var out []SupervisorWatchFinding
	for _, p := range w.sortedPRDs() {
		age, waiting := supervisorWatchWaitingAge(p, now)
		if !waiting {
			continue
		}
		if p.Priority < cfg.EffectivePriorityFloor() || age <= cfg.EffectiveStallThresholdMinutes() {
			continue
		}
		if w.isRaised(SupervisorWatchPriorityStall, p.ID) {
			continue
		}
		var btext, action string
		if w.Stall != nil {
			_, btext, action = NewStallDetector(w.Stall, nil).blockerFor(p)
		}
		out = append(out, SupervisorWatchFinding{
			Kind:    SupervisorWatchPriorityStall,
			Subject: p.ID,
			Text: SupervisorWatchPriorityStallText(p.ID, p.Priority, age,
				cfg.EffectiveStallThresholdMinutes(), cfg.EffectivePriorityFloor(), btext, action),
			BlockerText: btext,
			Action:      action,
		})
	}
	w.markRaised(out)
	return out
}

// supervisorWatchBacklogEntry is one Automaton of the consolidated
// backlog finding, with exactly the fields the sort and the entry text
// use.
type supervisorWatchBacklogEntry struct {
	id       string
	status   PRDStatus
	priority int
	created  time.Time
	left     int // tasks not completed and not cancelled; 0 renders without the "n left" tail
}

// supervisorWatchBacklogCap is the backlog finding's entry cap: the
// queue is listed whole in order, but the one-line text names at most
// this many entries before a "+k more" tail, so a long queue never
// buries the reason in a wall of ids.
const supervisorWatchBacklogCap = 16

// supervisorWatchTasksLeft counts the Automaton's tasks that are not
// completed and not cancelled: work that is not finished.
func supervisorWatchTasksLeft(p *PRD) int {
	n := 0
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			s := p.Story[i].Tasks[j].Status
			if s != TaskCompleted && s != TaskCancelled {
				n++
			}
		}
	}
	return n
}

// supervisorWatchBacklogEntryText renders one backlog entry: id(status)
// — the Automaton's id and status — or id(status, n left) when the
// Automaton still carries tasks that are not completed or cancelled.
func supervisorWatchBacklogEntryText(e supervisorWatchBacklogEntry) string {
	if e.left > 0 {
		return fmt.Sprintf("%s(%s, %d left)", e.id, e.status, e.left)
	}
	return fmt.Sprintf("%s(%s)", e.id, e.status)
}

// CheckBacklog raises the single consolidated backlog finding: every
// waiting Automaton (the same waiting set priority_stall uses) plus
// every failed or blocked Automaton that still has work left (at least
// one task that is not completed or cancelled — a failed or blocked
// Automaton with no work left is closed work and gets no entry). The
// finding's subject is the fixed string "backlog", its text starts
// with "BACKLOG <n> Automata (highest priority first, then oldest)",
// and its entries are sorted by highest priority first, within the same
// priority by oldest created first (a missing creation instant reads as
// oldest, because the queue cannot age it any sooner; a
// created-instant tie falls back to Automaton id, so the order is total
// and stable). The entry list is capped at sixteen entries with a
// "+k more" when the queue is longer, and an empty queue (no waiting,
// no unfinished failure) raises nothing. The check reads only the PRDs
// — it does not touch triage, does not act, and keeps the existing
// kind+subject dedupe so the episode is not re-reported on the same
// pass.
func (w *SupervisorWatchWork) CheckBacklog(now time.Time) []SupervisorWatchFinding {
	if w.isRaised(SupervisorWatchBacklog, SupervisorWatchBacklogSubject) {
		return nil
	}
	var entries []supervisorWatchBacklogEntry
	for _, p := range w.sortedPRDs() {
		left := supervisorWatchTasksLeft(p)
		var include bool
		switch {
		case supervisorWatchInWaitingStatus(p.Status):
			include = true
		case (p.Status == PRDFailed || p.Status == PRDBlocked) && left > 0:
			include = true
		}
		if !include {
			continue
		}
		entries = append(entries, supervisorWatchBacklogEntry{
			id:       p.ID,
			status:   p.Status,
			priority: p.Priority,
			created:  p.CreatedAt,
			left:     left,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	// Highest priority first; within the same priority, oldest created
	// first; a created-instant tie falls back to Automaton id so the
	// order is deterministic either way.
	sort.Slice(entries, func(i, j int) bool {
		a, b := &entries[i], &entries[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.created.IsZero() != b.created.IsZero() {
			return a.created.IsZero()
		}
		if !a.created.Equal(b.created) {
			return a.created.Before(b.created)
		}
		return a.id < b.id
	})
	list := make([]string, 0, supervisorWatchBacklogCap+1)
	for _, e := range entries {
		if len(list) == supervisorWatchBacklogCap {
			list = append(list, fmt.Sprintf("+%d more", len(entries)-supervisorWatchBacklogCap))
			break
		}
		list = append(list, supervisorWatchBacklogEntryText(e))
	}
	f := SupervisorWatchFinding{
		Kind:    SupervisorWatchBacklog,
		Subject: SupervisorWatchBacklogSubject,
		Text:    SupervisorWatchBacklogText(len(entries), list),
	}
	w.markRaised([]SupervisorWatchFinding{f})
	return []SupervisorWatchFinding{f}
}

// supervisorWatchWorkFinding mints one finding of the given kind for
// the given subject, with its operator-facing text built in the single
// wording home (supervisor_watch_text.go). FirstSeenAt and LastSeenAt
// are left zero: the shared finding store sets and preserves them on
// record (see SupervisorWatchStore.Upsert).
func supervisorWatchWorkFinding(kind SupervisorWatchKind, subject string) SupervisorWatchFinding {
	return SupervisorWatchFinding{
		Kind:    kind,
		Subject: subject,
		Text:    supervisorWatchFindingText(kind, subject),
	}
}

// supervisorWatchTriageNamesFailedTask reports whether p's triage
// record names one of its own tasks and that task is in the task
// status want. It is the shared condition behind the task_failed and
// task_review_needed checks: the record must name a task (an empty
// task id — the Automaton blocked without a failed task — is no task
// finding), the task must exist among the Automaton's stories' tasks,
// and it must sit in exactly the state the check is about.
func supervisorWatchTriageNamesFailedTask(p *PRD, want TaskStatus) bool {
	if p == nil || p.Triage == nil || p.Triage.TaskID == "" {
		return false
	}
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			t := &p.Story[i].Tasks[j]
			if t.ID != p.Triage.TaskID {
				continue
			}
			return t.Status == want
		}
	}
	return false
}

// supervisorWatchHasReviewHeldTask reports whether p is running or
// blocked (the two states a held task keeps an Automaton in) and has at
// least one task in the review state.
func supervisorWatchHasReviewHeldTask(p *PRD) bool {
	if p == nil || p.Status == PRDHeld || (p.Status != PRDRunning && p.Status != PRDBlocked) {
		return false
	}
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			if p.Story[i].Tasks[j].Status == TaskReviewNeeded {
				return true
			}
		}
	}
	return false
}

// supervisorWatchAllTasksCompleted reports whether every one of p's
// tasks is in its terminal completed state and p has at least one —
// the "finished all of its work" half of the automaton_not_merged
// check. A task in any other state, or an Automaton with no tasks at
// all, is not finished.
func supervisorWatchAllTasksCompleted(p *PRD) bool {
	n := 0
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			n++
			if p.Story[i].Tasks[j].Status != TaskCompleted {
				return false
			}
		}
	}
	return n > 0
}

// supervisorWatchQueuedEligible returns the queued-eligible count for
// a lane's backend type from the lane fill status's counts. An unknown
// type has no count and reads as zero.
func supervisorWatchQueuedEligible(t LaneType, q LaneFillQueued) int {
	switch t {
	case LaneTypeGPU:
		return q.GPU
	case LaneTypeClaude:
		return q.Claude
	default:
		return 0
	}
}

// sortedPRDs returns the lister's PRDs in stable Automaton-id order so
// the check output is deterministic, skipping empties and duplicates.
// A held Automaton is waiting by the operator's decision, so it is
// skipped too: it never produces a task-failed, task-review or
// not-merged finding. The Held group of the read view lists it instead.
func (w *SupervisorWatchWork) sortedPRDs() []*PRD {
	prds := w.prdView()
	if len(prds) == 0 {
		return nil
	}
	out := make([]*PRD, 0, len(prds))
	seen := map[string]bool{}
	for _, p := range prds {
		if p == nil || p.ID == "" || seen[p.ID] || p.Status == PRDHeld {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// prdView returns the PRDs from the lister, or nil when no lister is
// wired.
func (w *SupervisorWatchWork) prdView() []*PRD {
	if w.PRDs == nil {
		return nil
	}
	return w.PRDs.PRDs()
}

// sortedCIRecords returns the CI records in stable run-id order,
// skipping empties and duplicates, so the check output is
// deterministic.
func (w *SupervisorWatchWork) sortedCIRecords() []*RepoCIWatchRecord {
	recs := w.ciRecordView()
	if len(recs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]*RepoCIWatchRecord, 0, len(recs))
	for _, r := range recs {
		if r == nil || r.RunID == "" || seen[r.RunID] {
			continue
		}
		seen[r.RunID] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out
}

// ciRecordView returns the CI watch records from the lister, or nil
// when no lister is wired.
func (w *SupervisorWatchWork) ciRecordView() []*RepoCIWatchRecord {
	if w.CI == nil {
		return nil
	}
	return w.CI.CIWatchRecords()
}

// Bounds on the archived-unmerged check: at most this many branch checks
// run git per pass, and a check result is reused for the cache lifetime.
const (
	supervisorWatchArchivedMaxChecks = 8
	supervisorWatchArchivedCacheTTL  = 2 * time.Minute
)

// supervisorWatchArchivedCheck is one cached branch check.
type supervisorWatchArchivedCheck struct {
	state  string
	ahead  int
	at     time.Time
	branch string
	base   string
}

// supervisorWatchArchivedUnmerged returns one archived_unmerged finding per
// archived record whose branch holds commits not on the base (the same
// comparison the ArchivedUnmerged finder makes). Git use is read-only and
// bounded: at most supervisorWatchArchivedMaxChecks uncached branch checks per
// pass, cached results reused for a short time. A record that was not checked
// this pass (cap reached, or the comparison was unknown) keeps its open
// finding rather than resolving it; a merged or deleted branch yields no
// finding, so the pass resolves the stored one.
func (m *Manager) supervisorWatchArchivedUnmerged(now time.Time) []SupervisorWatchFinding {
	if m == nil || m.store == nil {
		return nil
	}
	repo := m.archivedBaseRepo()
	if repo == "" {
		return nil
	}
	git := m.archivedUnmergedGitRunner()
	open := map[string]*SupervisorWatchFinding{}
	for _, rec := range m.supervisorWatchList() {
		if rec != nil && !rec.Resolved && rec.Kind == SupervisorWatchArchivedUnmerged {
			open[rec.Subject] = rec
		}
	}
	var out []SupervisorWatchFinding
	checks := 0
	for _, p := range m.store.ListPRDs() {
		if p == nil || p.Status != PRDArchived || p.Git.Branch == "" {
			continue
		}
		m.mu.Lock()
		c, cached := m.supervisorWatchArchCache[p.ID]
		m.mu.Unlock()
		fresh := cached && c.branch == p.Git.Branch && c.base == p.Git.BaseBranch &&
			now.Sub(c.at) >= 0 && now.Sub(c.at) < supervisorWatchArchivedCacheTTL
		if !fresh {
			if checks >= supervisorWatchArchivedMaxChecks {
				if rec := open[p.ID]; rec != nil {
					out = append(out, SupervisorWatchFinding{Kind: rec.Kind, Subject: rec.Subject, Text: rec.Text})
				}
				continue
			}
			checks++
			ctx, cancel := context.WithTimeout(context.Background(), archiveBranchGitTimeout)
			state, ahead, _ := checkBranchAheadDelivered(ctx, git, repo, p.Git.Branch, p.Git.BaseBranch, p)
			cancel()
			c = supervisorWatchArchivedCheck{state: state, ahead: ahead, at: now, branch: p.Git.Branch, base: p.Git.BaseBranch}
			m.mu.Lock()
			if m.supervisorWatchArchCache == nil {
				m.supervisorWatchArchCache = map[string]supervisorWatchArchivedCheck{}
			}
			m.supervisorWatchArchCache[p.ID] = c
			m.mu.Unlock()
		}
		switch c.state {
		case branchCheckAhead:
			out = append(out, SupervisorWatchFinding{
				Kind: SupervisorWatchArchivedUnmerged, Subject: p.ID,
				Text: SupervisorWatchArchivedUnmergedText(p.ID, p.Title, p.Git.Branch, c.ahead),
			})
		case branchCheckUnknown:
			if rec := open[p.ID]; rec != nil { // keep what we knew
				out = append(out, SupervisorWatchFinding{Kind: rec.Kind, Subject: rec.Subject, Text: rec.Text})
			}
		}
	}
	return out
}
