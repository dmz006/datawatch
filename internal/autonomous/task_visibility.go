// Per-task visibility for the autonomous status and the capacity API: for
// every queued task the reason it is waiting, and for every running task
// what it holds. The views are built from the PRD store plus the capacity
// ledger and carry only operator-readable text (see wait_reason.go).
package autonomous

import (
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// Causes for a queued task that is not waiting on capacity. They sit next to
// the capacity WaitCause values in TaskWaitView.Cause.
const (
	// WaitCauseDependencies: one or more tasks it depends on are unfinished.
	WaitCauseDependencies WaitCause = "dependencies"
	// WaitCauseNotStarted: its Automaton is approved but has not been run.
	WaitCauseNotStarted WaitCause = "automaton_not_started"
	// WaitCauseAutomatonPaused: its Automaton is paused, so nothing dispatches.
	WaitCauseAutomatonPaused WaitCause = "automaton_paused"
	// WaitCauseNextInLine: ready to start, waiting for the executor's turn.
	WaitCauseNextInLine WaitCause = "next_in_line"
)

// TaskWaitView is one waiting task in the status and capacity responses:
// why it is waiting, since when, and (when the reason names them) when the
// wait ends and who holds what it needs.
type TaskWaitView struct {
	TaskID    string    `json:"task_id"`
	PRDID     string    `json:"prd_id"`
	PRDTitle  string    `json:"prd_title,omitempty"`
	TaskTitle string    `json:"task_title,omitempty"`
	Cause     WaitCause `json:"cause,omitempty"`
	Reason    string    `json:"reason"`
	// ReadableReason is the same reason with operator-readable holder names
	// ("<pool> full (u/l), held by <PRD title> / <task title> (<short id>) and
	// …"), resolved through the shared holder-title resolver; empty when the
	// reason names no holders, such as a backoff or budget wait.
	ReadableReason string     `json:"readable_reason,omitempty"`
	Since          time.Time  `json:"since"`
	ResetsAt       *time.Time `json:"resets_at,omitempty"`
	Holder         string     `json:"holder,omitempty"`
	// Readable is the operator-readable sentence for the waiting task
	// ("<PRD title> / <task title> (<short id>)"), ShortID the first 8 chars
	// of the task id. Both are added beside, never instead of, the id fields.
	Readable string `json:"readable,omitempty"`
	ShortID  string `json:"short_id,omitempty"`
}

// TaskHoldView is one running task in the status and capacity responses:
// the pools it holds (host, llm:, node: and any tree lock key), the node and
// the session it runs on, and since when.
type TaskHoldView struct {
	TaskID    string   `json:"task_id"`
	PRDID     string   `json:"prd_id"`
	PRDTitle  string   `json:"prd_title,omitempty"`
	TaskTitle string   `json:"task_title,omitempty"`
	Pools     []string `json:"pools"`
	Node      string   `json:"node,omitempty"`
	// NodeUnknown is set when the task's surviving worker was adopted after a
	// restart without a determinable compute node: the task holds its host and
	// llm pools but no node pool.
	NodeUnknown bool      `json:"node_unknown,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Since       time.Time `json:"since"`
	// Note is set when the holdings need a sentence: a task parked on an
	// external condition has released its inference slot.
	Note string `json:"note,omitempty"`
	// Readable is the operator-readable sentence for the running task
	// ("<PRD title> / <task title> (<short id>)"), ShortID the first 8 chars
	// of the task id.
	Readable string `json:"readable,omitempty"`
	ShortID  string `json:"short_id,omitempty"`
}

// visTask is the slice of a task the views need, copied under the store lock
// so the views are built from a stable snapshot.
type visTask struct {
	id, title, sessionID, waitReason string
	status                           TaskStatus
	dependsOn                        []string
	projectDir                       string
	startedAt                        *time.Time
	updatedAt                        time.Time
}

// visPRD is the slice of a PRD the views need.
type visPRD struct {
	id, title, projectDir, branch string
	status                        PRDStatus
	tasks                         []visTask
}

// snapshotVisibility copies the live PRDs' tasks out of the store.
func (m *Manager) snapshotVisibility() []visPRD {
	if m.store == nil {
		return nil
	}
	var out []visPRD
	for _, p := range m.store.ListPRDs() {
		m.readLocked(func() {
			switch p.Status {
			case PRDApproved, PRDActive, PRDRunning, PRDPaused:
			default:
				return
			}
			v := visPRD{id: p.ID, title: p.Title, projectDir: p.ProjectDir, branch: p.Git.Branch, status: p.Status}
			for i := range p.Story {
				for j := range p.Story[i].Tasks {
					t := &p.Story[i].Tasks[j]
					v.tasks = append(v.tasks, visTask{
						id: t.ID, title: t.Title, sessionID: t.SessionID, waitReason: t.WaitReason,
						status: t.Status, dependsOn: append([]string(nil), t.DependsOn...),
						projectDir: t.ProjectDir, startedAt: t.StartedAt, updatedAt: t.UpdatedAt,
					})
				}
			}
			out = append(out, v)
		})
	}
	return out
}

// TaskVisibility returns the waiting and running task views: queue lists
// every queued or capacity-waiting task with its reason, holdings every
// running task with what it holds. Both are non-nil. It must not be called
// with m.mu held; Status calls buildTaskVisibility before taking m.mu.
func (m *Manager) TaskVisibility() (queue []TaskWaitView, holdings []TaskHoldView) {
	return m.buildTaskVisibility(m.budgetsSummary())
}

// buildTaskVisibility builds the views from the PRD store and the capacity
// ledger. budgets is the already-read budget state, used to find when a full
// budget window rolls over.
func (m *Manager) buildTaskVisibility(budgets []BudgetStatus) ([]TaskWaitView, []TaskHoldView) {
	queue := []TaskWaitView{}
	holdings := []TaskHoldView{}
	prds := m.snapshotVisibility()
	if len(prds) == 0 {
		return queue, holdings
	}

	m.mu.Lock()
	led, bo, cooldownFn := m.capacity, m.backoff, m.cooldownUntilFn
	m.mu.Unlock()

	waiters := map[string]capacity.Waiter{}
	if led != nil {
		for _, w := range led.Snapshot().Waiting {
			waiters[w.Holder] = w
		}
	}

	for _, p := range prds {
		byID := make(map[string]*visTask, len(p.tasks))
		for i := range p.tasks {
			byID[p.tasks[i].id] = &p.tasks[i]
		}
		ready := 0
		for i := range p.tasks {
			t := &p.tasks[i]
			switch t.status {
			case TaskWaitingCapacity:
				w := m.waitingTaskView(p, t, waiters, bo, cooldownFn, budgets)
				queue = append(queue, w)
				if w.Cause == WaitCauseAwaitExternal || w.Cause == WaitCauseAwaitDecision {
					holdings = append(holdings, m.awaitHoldView(p, t, w.Cause))
				}
			case TaskQueued, TaskPending:
				w, isReady := queuedTaskView(p, t, byID, ready)
				if isReady {
					ready++
				}
				queue = append(queue, w)
			case TaskInProgress, TaskRunningTests, TaskVerifying:
				holdings = append(holdings, m.runningHoldView(p, t, led))
			}
		}
	}
	// Name every view through the shared holder resolver (capacity_titles.go): one
	// snapshot for the whole batch, so the status and capacity responses stay
	// fast even with many tasks.
	for i := range queue {
		queue[i].Readable, queue[i].ShortID = m.holderNames(queue[i].TaskID)
		if queue[i].Reason != "" {
			queue[i].ReadableReason = m.ReadableWaitSentence(queue[i].Reason)
		}
	}
	for i := range holdings {
		holdings[i].Readable, holdings[i].ShortID = m.holderNames(holdings[i].TaskID)
	}
	return queue, holdings
}

// holderNames returns the readable sentence and short id for one task id
// (the resolved view of the task's own lease), resolved through the shared
// cached resolver.
func (m *Manager) holderNames(taskID string) (string, string) {
	n := m.ResolveHolder(taskID)
	return n.Readable, n.ShortID
}

// ReadableWaitSentence words a wait reason readably for operators: the holder
// ids it names are resolved through the shared holder-title resolver, so a
// wait shows who is actually holding the slot ("<PRD title> /
// <task title> (<short id>)", "Verifying: <task title> (<short id>)"). Reasons
// that name no holders are returned unchanged, exactly as recorded.
func (m *Manager) ReadableWaitSentence(reason string) string {
	if reason == "" {
		return ""
	}
	return ReadableWaitText(reason, WaitReasonHolders(reason), func(holderID string) string {
		return m.ResolveHolder(holderID).Full
	})
}

// waitingTaskView words a task in waiting_capacity from its persisted reason,
// falling back to the ledger waiter's reason when none is persisted yet.
func (m *Manager) waitingTaskView(p visPRD, t *visTask, waiters map[string]capacity.Waiter, bo *Backoff, cooldownFn func() time.Time, budgets []BudgetStatus) TaskWaitView {
	reason := strings.TrimSpace(t.waitReason)
	since := t.updatedAt
	for _, h := range []string{t.id, VerifyHolder(t.id)} {
		if w, ok := waiters[h]; ok {
			if reason == "" {
				reason = w.Reason
			}
			if !w.Since.IsZero() {
				since = w.Since
			}
			break
		}
	}
	if reason == "" {
		reason = "waiting for a capacity slot"
	}
	cause := ClassifyWaitReason(reason)
	return TaskWaitView{
		TaskID: t.id, PRDID: p.id, PRDTitle: p.title, TaskTitle: t.title,
		Cause: cause, Reason: reason, Since: since,
		ResetsAt: waitResetsAt(cause, reason, bo, cooldownFn, budgets),
		Holder:   waitReasonHolder(cause, reason),
	}
}

// queuedTaskView words a queued task that has no capacity wait reason: what
// it is waiting for in plain words. ahead is how many ready tasks of this
// Automaton precede it; the second result reports whether this task is ready.
func queuedTaskView(p visPRD, t *visTask, byID map[string]*visTask, ahead int) (TaskWaitView, bool) {
	w := TaskWaitView{
		TaskID: t.id, PRDID: p.id, PRDTitle: p.title, TaskTitle: t.title, Since: t.updatedAt,
	}
	var unfinished []string
	for _, dep := range t.dependsOn {
		d, ok := byID[dep]
		if !ok {
			unfinished = append(unfinished, dep)
			continue
		}
		if d.status != TaskCompleted {
			title := d.title
			if title == "" {
				title = d.id
			}
			unfinished = append(unfinished, title)
		}
	}
	switch {
	case len(unfinished) > 0:
		w.Cause = WaitCauseDependencies
		w.Reason = "waiting for unfinished dependencies: " + strings.Join(unfinished, ", ")
		return w, false
	case p.status == PRDApproved:
		w.Cause = WaitCauseNotStarted
		w.Reason = "its Automaton is approved but has not been started"
		return w, false
	case p.status == PRDPaused:
		w.Cause = WaitCauseAutomatonPaused
		w.Reason = "its Automaton is paused, so no new tasks start"
		return w, false
	}
	w.Cause = WaitCauseNextInLine
	if ahead == 0 {
		w.Reason = "next in line to start"
	} else {
		w.Reason = "in line to start, " + strconv.Itoa(ahead) + " ready task(s) ahead in this Automaton"
	}
	return w, true
}

// runningHoldView lists what a running task holds: its lease (the verifier's
// lease while verifying) and the tree lock key when the executor holds one.
func (m *Manager) runningHoldView(p visPRD, t *visTask, led *capacity.Ledger) TaskHoldView {
	h := TaskHoldView{
		TaskID: t.id, PRDID: p.id, PRDTitle: p.title, TaskTitle: t.title, SessionID: t.sessionID,
		Pools: []string{}, Since: t.updatedAt,
	}
	if t.startedAt != nil {
		h.Since = *t.startedAt
	}
	if led != nil {
		lease := led.LeaseOf(t.id)
		if lease == nil {
			lease = led.LeaseOf(VerifyHolder(t.id))
		}
		if lease != nil {
			h.Pools = append(h.Pools, lease.Pools...)
			h.Node = lease.Node
			h.NodeUnknown = lease.NodeUnknown
			if !lease.Acquired.IsZero() {
				h.Since = lease.Acquired
			}
			if h.SessionID == "" {
				h.SessionID = lease.SessionID
			}
		}
	}
	if h.Node == "" {
		for _, pool := range h.Pools {
			if strings.HasPrefix(pool, "node:") {
				h.Node = strings.TrimPrefix(pool, "node:")
				break
			}
		}
	}
	if key := m.heldTreeKey(p, t); key != "" {
		h.Pools = append(h.Pools, key)
	}
	return h
}

// awaitHoldView words what a task parked on an external condition or on an
// open worker question holds: the inference slot is released; the tree lock is
// kept unless the park released it.
func (m *Manager) awaitHoldView(p visPRD, t *visTask, cause WaitCause) TaskHoldView {
	what := "an external condition"
	if cause == WaitCauseAwaitDecision {
		what = "an operator decision"
	}
	h := TaskHoldView{
		TaskID: t.id, PRDID: p.id, PRDTitle: p.title, TaskTitle: t.title, SessionID: t.sessionID,
		Pools: []string{}, Since: t.updatedAt,
	}
	if key := m.heldTreeKey(p, t); key != "" {
		h.Pools = append(h.Pools, key)
		h.Note = "waiting on " + what + ": the inference slot is released but the tree lock " + key + " is kept"
	} else {
		h.Note = "waiting on " + what + ": the inference slot and the tree lock are released"
	}
	return h
}

// heldTreeKey returns the tree lock key the executor currently holds for the
// task, or "" when it holds none (or the key cannot be resolved).
func (m *Manager) heldTreeKey(p visPRD, t *visTask) string {
	box := m.treeLockBoxLocked(t.id)
	if box == nil || *box == nil {
		return ""
	}
	prd := &PRD{ID: p.id, ProjectDir: p.projectDir}
	prd.Git.Branch = p.branch
	key, _, err := ResolveTreeKey(prd, &Task{ID: t.id, ProjectDir: t.projectDir})
	if err != nil {
		return ""
	}
	return key
}

// waitResetsAt returns when the wait ends when the reason's own source knows:
// a back-off with a named reset, a budget window's rollover, a global
// cooldown's end. Nil when unknown.
func waitResetsAt(cause WaitCause, reason string, bo *Backoff, cooldownFn func() time.Time, budgets []BudgetStatus) *time.Time {
	switch cause {
	case WaitCauseBackoff:
		if bo == nil {
			return nil
		}
		name := between(reason, backoffPrefix, backoffInfix)
		for _, e := range bo.Live() {
			if e.Backend == name && e.ResetKnown {
				until := e.Until
				return &until
			}
		}
	case WaitCauseBudget:
		llm := between(reason, budgetPrefix+" ", " spent")
		for _, b := range budgets {
			if b.LLM == llm && b.RollsOverAt != nil {
				at := *b.RollsOverAt
				return &at
			}
		}
	case WaitCauseRateCeiling:
		return rateCeilingResetsAt(reason)
	case WaitCauseGlobalCooldown:
		if cooldownFn != nil {
			if until := cooldownFn(); !until.IsZero() {
				return &until
			}
		}
	}
	return nil
}

// waitReasonHolder extracts the holder a reason names: the sessions holding a
// full pool or node, or the task holding a tree lock. "" when it names none.
func waitReasonHolder(cause WaitCause, reason string) string {
	const marker = "held by "
	i := strings.Index(reason, marker)
	if i < 0 {
		return ""
	}
	rest := reason[i+len(marker):]
	var holder string
	switch cause {
	case WaitCauseTreeLock:
		const taskMarker = "task "
		if !strings.HasPrefix(rest, taskMarker) {
			return ""
		}
		holder = rest[len(taskMarker):]
		if j := strings.Index(holder, " "); j >= 0 {
			holder = holder[:j]
		}
	case WaitCausePoolFull:
		holder = rest
		if j := strings.Index(holder, ")"); j >= 0 {
			holder = holder[:j]
		}
	case WaitCauseNodeBusy:
		holder = rest
	default:
		return ""
	}
	holder = strings.TrimSpace(holder)
	if holder == holderList(nil) {
		return ""
	}
	return holder
}

// between returns the text of s between the first after and the next before
// that follows it, or "" when either is missing.
func between(s, after, before string) string {
	i := strings.Index(s, after)
	if i < 0 {
		return ""
	}
	rest := s[i+len(after):]
	j := strings.Index(rest, before)
	if j < 0 {
		return ""
	}
	return rest[:j]
}
