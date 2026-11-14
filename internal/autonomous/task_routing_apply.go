// Applying the task-type routing table to tasks.
//
// ApplyTaskRouting is the single apply function: the decomposer calls it for
// every task it materializes and the add-task paths call it for a new task.
// It writes the active profile's row into the task's own backend, model,
// effort and verification fields, and records the outcome on the task
// (Task.TaskRouting) and as one decision-log entry.
//
// Precedence (most specific wins): a task LLM set by an operator, then a
// story LLM, then an Automaton LLM, then the routing table, then the global
// default. The table therefore applies only where none of the others is set.
// A task that was edited by hand (edit_task, set_task_llm or
// set_task_verification_llm by anyone other than the router) is never
// rewritten, and neither is a task that has started: applying never touches a
// running or finished task or one with a live session, and never kills,
// restarts or signals a session.
//
// Switching the active profile changes only tasks created afterwards. A
// planner re-run re-applies only to tasks the router itself routed and nobody
// edited.
package autonomous

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// taskRoutingActor is the actor name the router records. A decision-log entry
// by any other actor on a task counts as a hand edit.
const taskRoutingActor = "task-router"

// taskRoutingDecisionKind is the Decision.Kind of a routing entry.
const taskRoutingDecisionKind = "task_routing"

// Reasons recorded when the table did not decide a task's LLM.
const (
	taskRoutingReasonDisabled   = "routing disabled"
	taskRoutingReasonTaskLLM    = "task LLM set by operator"
	taskRoutingReasonStoryLLM   = "story LLM set"
	taskRoutingReasonAutomaton  = "Automaton LLM set"
	taskRoutingReasonHandEdited = "hand edited"
	taskRoutingReasonStarted    = "task already started"
	taskRoutingReasonNoRow      = "no row for the task type in the profile"
	taskRoutingReasonEmptyRow   = "the row names no LLM, so the default applies"
)

// handEditKinds are the decision kinds that mark a task as hand edited.
var handEditKinds = map[string]bool{
	"edit_task":                 true,
	"set_task_llm":              true,
	"set_task_verification_llm": true,
}

// SetTaskRoutingBackendCheck sets the lookup that tells the router whether an
// LLM backend is known (for example the LLM registry's Get). Nil skips the
// check.
func (m *Manager) SetTaskRoutingBackendCheck(known func(backend string) bool) {
	m.mu.Lock()
	m.taskRoutingBackendKnown = known
	m.mu.Unlock()
}

// taskRoutingSnapshot reads the routing config and the validation lookups
// under the manager lock and returns copies, so no slow work holds the lock.
func (m *Manager) taskRoutingSnapshot() (config.TaskRoutingConfig, config.TaskRoutingChecks) {
	m.mu.Lock()
	cfg := m.cfg.TaskRouting
	known := m.taskRoutingBackendKnown
	m.mu.Unlock()
	cfg = config.TaskRoutingWithDefaults(cfg).Clone()
	return cfg, TaskRoutingChecksFor(known)
}

// taskHandEdited reports whether the decision log holds an edit of this task
// by an actor other than the router.
func taskHandEdited(prd *PRD, taskID string) bool {
	needle := "task=" + taskID
	for _, d := range prd.Decisions {
		if !handEditKinds[d.Kind] || d.Actor == taskRoutingActor {
			continue
		}
		i := strings.Index(d.Note, needle)
		if i < 0 {
			continue
		}
		rest := d.Note[i+len(needle):]
		if rest == "" || rest[0] == ' ' {
			return true
		}
	}
	return false
}

// routerOwnsWorkerLLM reports whether the task's worker fields hold exactly
// what the router wrote earlier.
func routerOwnsWorkerLLM(t *Task) bool {
	r := t.TaskRouting
	if r == nil || !r.Applied || r.Result == nil {
		return false
	}
	return t.Backend == r.Result.WorkerBackend && t.Model == r.Result.WorkerModel &&
		string(t.Effort) == r.Result.Effort
}

// taskRoutingBlock returns the reason the table must not decide this task, or
// "" when it may. enabled false means routing is off.
func taskRoutingBlock(prd *PRD, t *Task, st *Story, auto *Automation, enabled bool) string {
	if !enabled {
		return taskRoutingReasonDisabled
	}
	if !unstartedTask(t.Status) || t.SessionID != "" {
		return taskRoutingReasonStarted
	}
	if (t.Backend != "" || t.Model != "" || t.Effort != "") && !routerOwnsWorkerLLM(t) {
		return taskRoutingReasonTaskLLM
	}
	if taskHandEdited(prd, t.ID) {
		return taskRoutingReasonHandEdited
	}
	if st != nil && (st.Backend != "" || st.Model != "") {
		return taskRoutingReasonStoryLLM
	}
	if prd.Backend != "" || prd.Model != "" || (auto != nil && (auto.Backend != "" || auto.Model != "")) {
		return taskRoutingReasonAutomaton
	}
	return ""
}

// chooseRoutingRow validates the row and returns the choice to apply. The
// row's first choice is tried first, then its second choice. An error means
// both were refused (unknown LLM, a model that does not pair with its
// backend, a premium model without the row's allow flag).
func chooseRoutingRow(profile, taskType string, row config.TaskRoutingRow, chk config.TaskRoutingChecks) (res *TaskRoutingResult, secondUsed bool, err error) {
	build := func(backend, model string) config.TaskRoutingRow {
		r := row
		r.WorkerBackend, r.WorkerModel = backend, model
		r.SecondChoice = nil
		return r
	}
	validate := func(r config.TaskRoutingRow) error {
		cfg := config.TaskRoutingConfig{
			ActiveProfile: profile,
			Profiles:      map[string]config.TaskRoutingProfile{profile: {taskType: r}},
		}
		return joinTaskRoutingErrors(config.ValidateTaskRouting(cfg, chk))
	}
	first := build(row.WorkerBackend, row.WorkerModel)
	firstErr := validate(first)
	if firstErr == nil {
		return &TaskRoutingResult{
			WorkerBackend: row.WorkerBackend, WorkerModel: row.WorkerModel, Effort: row.Effort,
			VerificationBackend: row.VerificationBackend, VerificationModel: row.VerificationModel,
		}, false, nil
	}
	if sc := row.SecondChoice; sc != nil {
		second := build(sc.Backend, sc.Model)
		if sc.Effort != "" {
			second.Effort = sc.Effort
		}
		if validate(second) == nil {
			return &TaskRoutingResult{
				WorkerBackend: sc.Backend, WorkerModel: sc.Model, Effort: second.Effort,
				VerificationBackend: row.VerificationBackend, VerificationModel: row.VerificationModel,
			}, true, nil
		}
	}
	return nil, false, firstErr
}

// planTaskRouting decides what the table does for one task without writing
// anything. profileName empty means the active profile. ignoreEnabled makes
// the decision as if routing were on (dry run).
func planTaskRouting(prd *PRD, t *Task, st *Story, auto *Automation, cfg config.TaskRoutingConfig, chk config.TaskRoutingChecks, profileName string, ignoreEnabled bool) TaskRoutingDecision {
	taskType, by := ClassifyTask(t, st)
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	dec := TaskRoutingDecision{TaskType: taskType, Profile: profileName, ClassifiedBy: by}
	if reason := taskRoutingBlock(prd, t, st, auto, cfg.Enabled || ignoreEnabled); reason != "" {
		dec.Reason = reason
		return dec
	}
	row, ok := cfg.Profiles[profileName][taskType]
	if !ok {
		dec.Reason = taskRoutingReasonNoRow
		return dec
	}
	dec.Row = taskType
	if row.WorkerBackend == "" && row.WorkerModel == "" && row.Effort == "" &&
		row.VerificationBackend == "" && row.VerificationModel == "" {
		dec.Reason = taskRoutingReasonEmptyRow
		return dec
	}
	res, second, err := chooseRoutingRow(profileName, taskType, row, chk)
	if err != nil {
		dec.Reason = "the row's LLM was refused: " + err.Error()
		return dec
	}
	dec.Applied = true
	dec.Result = res
	dec.SecondChoiceUsed = second
	return dec
}

// sameRoutingDecision compares two decisions.
func sameRoutingDecision(a, b *TaskRoutingDecision) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(*a, *b)
}

// ApplyTaskRouting applies the active profile to one task and returns the
// recorded decision. It writes only the task's own fields and appends one
// decision-log entry when the table decided; the caller saves the Automaton.
// It never touches a task that has started and never signals a session. The
// store lock is held only for the in-memory write.
func (m *Manager) ApplyTaskRouting(prd *PRD, task *Task) TaskRoutingDecision {
	if prd == nil || task == nil {
		return TaskRoutingDecision{}
	}
	cfg, chk := m.taskRoutingSnapshot()
	auto := m.AutomationForPRD(prd)
	var out TaskRoutingDecision
	m.store.WithLock(func() {
		out = applyTaskRoutingLocked(prd, task, auto, cfg, chk, time.Now())
	})
	return out
}

// applyTaskRoutingLocked is ApplyTaskRouting's body; the caller owns prd (the
// store lock is held, or prd is a private copy).
func applyTaskRoutingLocked(prd *PRD, task *Task, auto *Automation, cfg config.TaskRoutingConfig, chk config.TaskRoutingChecks, now time.Time) TaskRoutingDecision {
	t, st := findTaskInPRD(prd, task.ID)
	if t == nil {
		t = task
	}
	dec := planTaskRouting(prd, t, st, auto, cfg, chk, "", false)
	prev := t.TaskRouting
	if !dec.Applied {
		// A task the router already routed keeps its result and record when a
		// later look finds nothing to apply: the fields on it are the
		// router's, and rewriting the record would make them look hand set.
		if prev != nil && prev.Applied && routerOwnsWorkerLLM(t) {
			return dec
		}
		if !sameRoutingDecision(prev, &dec) {
			rec := dec
			t.TaskRouting = &rec
			t.UpdatedAt = now
		}
		return dec
	}
	if sameRoutingDecision(prev, &dec) && routerOwnsWorkerLLM(t) {
		return dec
	}
	r := dec.Result
	t.Backend, t.Model, t.Effort = r.WorkerBackend, r.WorkerModel, Effort(r.Effort)
	if verifierFieldsOwnedByRouter(t) {
		t.VerificationBackend, t.VerificationModel = r.VerificationBackend, r.VerificationModel
	}
	rec := dec
	t.TaskRouting = &rec
	t.UpdatedAt = now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: taskRoutingDecisionKind, Actor: taskRoutingActor,
		Backend: r.WorkerBackend, Model: r.WorkerModel,
		Note: fmt.Sprintf("task=%s type=%s profile=%s row=%s worker=%s/%s effort=%s verification=%s/%s second_choice=%t",
			t.ID, dec.TaskType, dec.Profile, dec.Row, r.WorkerBackend, r.WorkerModel, r.Effort,
			r.VerificationBackend, r.VerificationModel, dec.SecondChoiceUsed),
	})
	return dec
}

// verifierFieldsOwnedByRouter reports whether the task's verification fields
// are empty or hold the router's earlier result, so the router may write them.
func verifierFieldsOwnedByRouter(t *Task) bool {
	if t.VerificationBackend == "" && t.VerificationModel == "" {
		return true
	}
	r := t.TaskRouting
	return r != nil && r.Applied && r.Result != nil &&
		t.VerificationBackend == r.Result.VerificationBackend && t.VerificationModel == r.Result.VerificationModel
}

// ApplyTaskRoutingToPRD applies the table to every task of prd and returns
// the number of tasks the table decided. The caller saves the Automaton.
func (m *Manager) ApplyTaskRoutingToPRD(prd *PRD) int {
	if prd == nil {
		return 0
	}
	var ids []string
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				ids = append(ids, prd.Story[si].Tasks[ti].ID)
			}
		}
	})
	n := 0
	for _, id := range ids {
		var t *Task
		m.store.WithLock(func() { t, _ = findTaskInPRD(prd, id) })
		if t == nil {
			continue
		}
		if m.ApplyTaskRouting(prd, t).Applied {
			n++
		}
	}
	return n
}

// TaskRoutingEffectiveLLM is the worker LLM a task will run with.
type TaskRoutingEffectiveLLM struct {
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	// Source is the level that supplied the backend (task, story, prd, ...).
	Source string `json:"source,omitempty"`
}

// TaskRoutingView is one task's routing state for the read surfaces.
type TaskRoutingView struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title,omitempty"`
	TaskType     string `json:"task_type"`
	ClassifiedBy string `json:"classified_by"`
	Profile      string `json:"profile,omitempty"`
	// Row is the row that decided the LLM; empty when none did.
	Row string `json:"row,omitempty"`
	// Reason is why no row decided the LLM; empty when one did.
	Reason    string                  `json:"reason,omitempty"`
	Applied   bool                    `json:"applied"`
	Effective TaskRoutingEffectiveLLM `json:"effective"`
}

// TaskRoutingForAutomaton returns, per task, its type, how it was classified,
// the row that decided its LLM (or the reason none did) and its effective LLM.
// It writes nothing.
func (m *Manager) TaskRoutingForAutomaton(prdID string) ([]TaskRoutingView, error) {
	prd, ok := m.store.SnapshotPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	cfg, chk := m.taskRoutingSnapshot()
	auto := m.AutomationForPRD(prd)
	var out []TaskRoutingView
	for si := range prd.Story {
		st := &prd.Story[si]
		for ti := range st.Tasks {
			t := &st.Tasks[ti]
			var dec TaskRoutingDecision
			if t.TaskRouting != nil && (!t.TaskRouting.Applied || routerOwnsWorkerLLM(t)) {
				dec = *t.TaskRouting
			} else {
				dec = planTaskRouting(prd, t, st, auto, cfg, chk, "", false)
				if dec.Applied {
					dec.Applied, dec.Result = false, nil
					dec.Reason = "not routed yet"
				}
			}
			b, mod, src := resolveTaskExecutionLLM(t, st, prd, auto, executionFallbackBackend(m.cfg, prd))
			eff := string(t.Effort)
			if eff == "" {
				eff = string(st.Effort)
			}
			if eff == "" {
				eff = string(prd.Effort)
			}
			v := TaskRoutingView{
				TaskID: t.ID, Title: t.Title, TaskType: dec.TaskType, ClassifiedBy: dec.ClassifiedBy,
				Profile: dec.Profile, Applied: dec.Applied,
				Effective: TaskRoutingEffectiveLLM{Backend: b, Model: mod, Effort: eff, Source: src},
			}
			if dec.Applied {
				v.Row = dec.Row
			} else {
				v.Reason = dec.Reason
			}
			out = append(out, v)
		}
	}
	return out, nil
}

// TaskRoutingDryRunEntry is what a profile would assign to one task.
type TaskRoutingDryRunEntry struct {
	TaskID       string             `json:"task_id"`
	Title        string             `json:"title,omitempty"`
	TaskType     string             `json:"task_type"`
	ClassifiedBy string             `json:"classified_by"`
	Profile      string             `json:"profile"`
	Row          string             `json:"row,omitempty"`
	WouldApply   bool               `json:"would_apply"`
	Result       *TaskRoutingResult `json:"result,omitempty"`
	SecondChoice bool               `json:"second_choice_used,omitempty"`
	Reason       string             `json:"reason,omitempty"`
}

// TaskRoutingDryRun returns what the active (or the named) profile would
// assign to each task of prd. It writes nothing and appends no decision-log
// entry, and it answers as if routing were enabled.
func (m *Manager) TaskRoutingDryRun(prdID, profile string) ([]TaskRoutingDryRunEntry, error) {
	prd, ok := m.store.SnapshotPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	cfg, chk := m.taskRoutingSnapshot()
	if profile != "" {
		if _, ok := cfg.Profiles[profile]; !ok {
			return nil, fmt.Errorf("profile %q does not exist", profile)
		}
	}
	auto := m.AutomationForPRD(prd)
	var out []TaskRoutingDryRunEntry
	for si := range prd.Story {
		st := &prd.Story[si]
		for ti := range st.Tasks {
			t := &st.Tasks[ti]
			dec := planTaskRouting(prd, t, st, auto, cfg, chk, profile, true)
			out = append(out, TaskRoutingDryRunEntry{
				TaskID: t.ID, Title: t.Title, TaskType: dec.TaskType, ClassifiedBy: dec.ClassifiedBy,
				Profile: dec.Profile, Row: dec.Row, WouldApply: dec.Applied, Result: dec.Result,
				SecondChoice: dec.SecondChoiceUsed, Reason: dec.Reason,
			})
		}
	}
	return out, nil
}

// TaskRoutingForAutomaton is the API wrapper for the read surfaces.
func (a *API) TaskRoutingForAutomaton(prdID string) (any, error) {
	return a.M.TaskRoutingForAutomaton(prdID)
}

// TaskRoutingDryRun is the API wrapper for the dry-run surfaces.
func (a *API) TaskRoutingDryRun(prdID, profile string) (any, error) {
	return a.M.TaskRoutingDryRun(prdID, profile)
}
