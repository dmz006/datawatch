package autonomous

// Daemon wiring for the gate repair engine: the Manager defaults the engine
// is built from (failed-gate resolution, the gate-run reset, the hold
// question, the safe script edits), and the engine's contribution to the
// housekeeping repairs view.
//
// WireGateRepair builds the engine from the wiring (any field left nil takes
// the Manager-based default), installs it on the Manager (so the REST, CLI
// and chat reads serve live data) and returns it. Its records carry
// Group=gate_repair, so they group with the other housekeeping groups in the
// bounded record.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// SetGateRepairer installs the gate repair engine the reads serve from. A
// nil engine uninstalls it (the engine's group then falls out of the
// housekeeping view).
func (m *Manager) SetGateRepairer(g *GateRepairer) {
	m.mu.Lock()
	m.gateRepairer = g
	m.mu.Unlock()
}

// GateRepairer returns the installed engine (nil when none is wired).
func (m *Manager) GateRepairer() *GateRepairer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gateRepairer
}

// RenderGateRepairs writes the engine status as plain text, newest record
// first, shared by the CLI and the chat verb. A read: it runs no repair,
// raises no alert and changes no mode.
func RenderGateRepairs(b io.StringWriter, s *GateRepairStatus) {
	w := func(s string) { _, _ = b.WriteString(s) }
	w("Gate repairs (mode " + s.Mode + ") — " + strconv.Itoa(len(s.Records)) + " record(s), newest first\n")
	for i := len(s.Records) - 1; i >= 0; i-- {
		rec := s.Records[i]
		line := "  " + rec.Kind + ": " + rec.Subject
		if rec.Found != "" {
			line += " — found: " + rec.Found
		}
		if rec.Done != "" {
			line += "; done: " + rec.Done
		}
		if rec.Undo != "" {
			line += "; undo: " + rec.Undo
		}
		if rec.Before != "" {
			line += "; before: " + rec.Before
		}
		if rec.After != "" {
			line += "; after: " + rec.After
		}
		w(line + "\n")
	}
	if len(s.Budget) > 0 {
		w("  repair budget spent on " + strconv.Itoa(len(s.Budget)) + " (task, class) pair(s)\n")
	}
}

// GateRepairerStatus returns the engine's read state (an empty status in
// report mode when no engine is wired), for the REST, CLI and chat reads.
func (m *Manager) GateRepairerStatus() GateRepairStatus {
	if g := m.GateRepairer(); g != nil {
		return g.Status()
	}
	return GateRepairStatus{Mode: config.GateRepairModeReport, Records: []RepairRecord{}}
}

// RunGateRepair runs the gate repair engine once for one Automaton's one
// failed task gate, for the POST /api/autonomous/gate-repair/run single-target
// surface. It delegates to the wired engine (which honours the engine mode and
// the repair budget); when no engine is wired it returns one refused outcome
// rather than panicking. The outcome is a plain value — an empty Err means the
// gate was resolved (or reported), a non-empty Err means it was refused or the
// repair failed.
func (m *Manager) RunGateRepair(ctx context.Context, automatonID, taskID string) GateRepairOutcome {
	if g := m.GateRepairer(); g != nil {
		return g.Run(ctx, automatonID, taskID)
	}
	return GateRepairOutcome{Mode: config.GateRepairModeReport, Err: "gate repair engine is not wired to this daemon"}
}

// RunGateRepairPass runs the gate repair engine once across the daemon: every
// currently failed task gate on every Automaton is resolved (report mode only
// records what it would do; on mode still stops at the repair budget). It is
// the POST /api/autonomous/gate-repair/run no-target surface. The target list
// is collected under the read lock and released before any repair runs, and the
// scan stops early when the request context expires. When no engine is wired it
// returns an empty list.
func (m *Manager) RunGateRepairPass(ctx context.Context) []GateRepairOutcome {
	g := m.GateRepairer()
	if g == nil || m.store == nil {
		return []GateRepairOutcome{}
	}
	type gateTarget struct{ automatonID, taskID string }
	var targets []gateTarget
	for _, p := range m.store.ListPRDs() {
		if err := ctx.Err(); err != nil {
			break
		}
		m.readLocked(func() {
			for i := range p.Story {
				for j := range p.Story[i].Tasks {
					if p.Story[i].Tasks[j].Status == TaskFailed {
						targets = append(targets, gateTarget{automatonID: p.ID, taskID: p.Story[i].Tasks[j].ID})
					}
				}
			}
		})
	}
	out := make([]GateRepairOutcome, 0, len(targets))
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			break
		}
		out = append(out, g.Run(ctx, t.automatonID, t.taskID))
	}
	return out
}

// HousekeepingGateRepairView is the engine's group in the housekeeping
// repairs read view, newest first within each list.
func (g *GateRepairer) HousekeepingGateRepairView() HousekeepingRepairGroupView {
	gv := HousekeepingRepairGroupView{
		Group: GateRepairGroup, Mode: g.mode(),
		Would: []RepairRecord{}, Did: []RepairRecord{}, Failed: []RepairRecord{}, SkippedUnsafe: []RepairRecord{},
	}
	recs := g.Records()
	for i := len(recs) - 1; i >= 0; i-- {
		rec := recs[i]
		if rec.Group != GateRepairGroup {
			continue
		}
		switch rec.Kind {
		case RepairWould:
			gv.Would = append(gv.Would, rec)
		case RepairDid:
			gv.Did = append(gv.Did, rec)
		case RepairFailed:
			gv.Failed = append(gv.Failed, rec)
		case RepairSkippedUnsafe:
			gv.SkippedUnsafe = append(gv.SkippedUnsafe, rec)
		}
	}
	return gv
}

// GateRepairWiring is the daemon's wiring of the engine. Every field may be
// nil — the nil field takes the Manager-based default. Tests pass fakes.
type GateRepairWiring struct {
	// RunSyncDocs runs `make sync-docs` in the task's repo dir and returns
	// its output; nil uses the daemon's default.
	RunSyncDocs func(ctx context.Context, repoDir string) (string, error)
	// WidenAllowedList, AdjustPipes and NarrowTrackerIDCheck override the
	// default safe script edits; nil uses the defaults.
	WidenAllowedList     func(in GateRepairInput, lines []string) (before, after string, err error)
	AdjustPipes          func(in GateRepairInput) (before, after string, err error)
	NarrowTrackerIDCheck func(in GateRepairInput) (before, after string, err error)
	// RaiseHeldQuestion overrides the Manager's hold-question raise; nil
	// uses it.
	RaiseHeldQuestion func(automatonID, taskID string, class GateFailureClass, reason string) (string, error)
	// ResetGateRun overrides the Manager's failed-task reset; nil uses it.
	ResetGateRun func(automatonID, taskID, reason string) error
	// TaskGate overrides the Manager's failed-gate resolution; nil uses it.
	TaskGate func(automatonID, taskID string) (GateRepairInput, bool)
	// Alert and Log override the daemon's sinks; nil uses the Manager's
	// alert sink and the daemon log.
	Alert func(level, title, body string)
	Log   func(line string)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// BudgetPath persists the repair budget; empty keeps it in memory. The
	// daemon passes dataDir/gate_repair_budget.json.
	BudgetPath string
}

// WireGateRepair builds the engine on the Manager from the wiring and the
// live config, installs it on the Manager and returns it. It returns nil
// when the Manager or config is unavailable.
func WireGateRepair(m *Manager, cfg func() config.Config, w GateRepairWiring) *GateRepairer {
	if m == nil || cfg == nil {
		return nil
	}
	deps := m.gateRepairDefaults()
	deps.Mode = func() string { return cfg().Autonomous.GateRepair.Effective().Mode }
	if w.TaskGate != nil {
		deps.TaskGate = w.TaskGate
	}
	if w.RunSyncDocs != nil {
		deps.RunSyncDocs = w.RunSyncDocs
	}
	if w.WidenAllowedList != nil {
		deps.WidenAllowedList = w.WidenAllowedList
	}
	if w.AdjustPipes != nil {
		deps.AdjustPipes = w.AdjustPipes
	}
	if w.NarrowTrackerIDCheck != nil {
		deps.NarrowTrackerIDCheck = w.NarrowTrackerIDCheck
	}
	if w.RaiseHeldQuestion != nil {
		deps.RaiseHeldQuestion = w.RaiseHeldQuestion
	}
	if w.ResetGateRun != nil {
		deps.ResetGateRun = w.ResetGateRun
	}
	alert := w.Alert
	if alert == nil {
		alert = func(level, title, body string) {
			if sink := m.AlertSink(); sink != nil {
				sink(level, title, body)
			}
		}
	}
	logf := w.Log
	if logf == nil {
		logf = func(line string) { log.Printf("[gate-repair] %s", line) }
	}
	deps.Alert = alert
	deps.Log = logf
	if w.Now != nil {
		deps.Now = w.Now
	}
	g := NewGateRepairer(deps, w.BudgetPath)
	m.SetGateRepairer(g)
	return g
}

// gateRepairDefaults are the Manager-based defaults in the deps the engine
// is built from.
func (m *Manager) gateRepairDefaults() GateRepairDeps {
	return GateRepairDeps{
		TaskGate:             m.gateRepairResolveTask,
		RunSyncDocs:          gateRepairRunSyncDocs,
		WidenAllowedList:     gateRepairWidenAllowedList,
		AdjustPipes:          gateRepairAdjustPipes,
		NarrowTrackerIDCheck: gateRepairNarrowTrackerIDCheck,
		RaiseHeldQuestion:    m.RaiseGateRepairHoldQuestion,
		ResetGateRun:         m.gateRepairResetRun,
	}
}

// gateRepairResolveTask resolves the failure input of a failed task's gate
// from the stored state: the gate output is the task's failure text, the
// script is the story's verification script (when it is inside the task's
// .datawatch/verify folder). ok is false when nothing failed or the gate and
// its script cannot be resolved.
func (m *Manager) gateRepairResolveTask(automatonID, taskID string) (GateRepairInput, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prd, ok := m.store.GetPRD(automatonID)
	if !ok {
		return GateRepairInput{}, false
	}
	t, ok := m.store.GetTask(taskID)
	if !ok || t.Status != TaskFailed {
		return GateRepairInput{}, false
	}
	repoDir := m.resolveTaskProjectDir(prd, t)
	if repoDir == "" {
		repoDir = "."
	}
	verification := filepath.Join(repoDir, ".datawatch", "verify")
	scriptPath, scriptText := "", ""
	gateStoryIdx := -1
	for si, story := range prd.Story {
		for ti := range story.Tasks {
			if story.Tasks[ti].ID == taskID {
				gateStoryIdx = si
				rel := story.VerificationScript
				if rel != "" {
					abs := rel
					if !filepath.IsAbs(abs) {
						abs = filepath.Join(repoDir, rel)
					}
					if pathWithin(verification, abs) {
						scriptPath = abs
						if text, err := os.ReadFile(abs); err == nil {
							scriptText = string(text)
						}
					}
				}
			}
		}
	}
	signalKilled := strings.Contains(strings.ToLower(t.Error), "killed by a signal") ||
		strings.Contains(strings.ToLower(t.Error), "sigterm") ||
		strings.Contains(strings.ToLower(t.Error), "sigkill")
	in := GateRepairInput{
		AutomatonID: automatonID,
		TaskID:      taskID,
		Gate: GateFailureInput{
			Output:            t.Error,
			SignalKilled:      signalKilled,
			ScriptText:        scriptText,
			ScriptPath:        scriptPath,
			InOwnVerifyFolder: scriptPath != "",
			ScriptStoryIndex:  gateStoryIdx,
			GateStoryIndex:    gateStoryIdx,
		},
		VerifyFolder: verification,
	}
	return in, true
}

// gateRepairResetRun resets the failed task and its dependent tasks through
// the Manager reset path so the gate runs again.
func (m *Manager) gateRepairResetRun(automatonID, taskID, reason string) error {
	if reason == "" {
		reason = gateRepairResetActor
	}
	if _, err := m.ResetTask(automatonID, taskID, reason, true); err != nil {
		return err
	}
	return nil
}

// RaiseGateRepairHoldQuestion raises the one held operator question for a
// spent repair budget or an unknown class: it starts by saying what failed
// and why the repair was not applied, offers "retry the gate now" (reset
// the failed run) or "cancel the task". It returns the question id. Nothing
// is raised when the operator question channel is off or no store is wired;
// then an empty id and a nil error come back and the alert and the record
// carry the cause. A question already open for the same hold is not raised
// again.
func (m *Manager) RaiseGateRepairHoldQuestion(automatonID, taskID string, class GateFailureClass, reason string) (string, error) {
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return "", nil
	}
	m.mu.Lock()
	prd, ok := m.store.GetPRD(automatonID)
	t, okTask := m.store.GetTask(taskID)
	m.mu.Unlock()
	label := automatonID
	if ok {
		label = automatonLabel(prd)
	}
	title := taskID
	if okTask && strings.TrimSpace(t.Title) != "" {
		title = t.Title
	}
	q := Question{
		Label:     "decision",
		AskedBy:   "gate-repair",
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		PRDID:     automatonID,
		TaskID:    taskID,
		Text: fmt.Sprintf("Task %s in %s failed its gate: %s. The gate repair engine held it (%s). The repair would be: %s. Retry the gate now, or cancel the task?",
			title, label, reason, string(class), class.Repair()),
		Options: []QuestionOption{
			{ID: reviewActionRetry, Label: "Retry the gate now",
				Tradeoffs: "Resets the failed task and its dependent tasks so the gate runs again."},
			{ID: reviewActionCancel, Label: "Cancel the task",
				Tradeoffs: "Keeps the task failed; tasks that depend on it do not run."},
		},
		Default:          reviewActionRetry,
		Priority:         QuestionPriorityHigh,
		Blocking:         true,
		SourceLineSHA256: gateRepairHoldQuestionKey(automatonID, taskID, class),
	}
	created, _, err := store.RaiseUnlessOpenSourceLine(q)
	if err != nil {
		return "", err
	}
	if created.ID != "" {
		return created.ID, nil
	}
	return gateRepairHoldQuestionKey(automatonID, taskID, class), nil
}

// gateRepairHoldQuestionKey is the de-duplication key of one held question:
// a hash of the Automaton, the task and the failure class, so the same hold
// read twice raises one question while it is open.
func gateRepairHoldQuestionKey(automatonID, taskID string, class GateFailureClass) string {
	sum := sha256.Sum256([]byte(automatonID + "\x00" + taskID + "\x00" + string(class)))
	return hex.EncodeToString(sum[:])
}
