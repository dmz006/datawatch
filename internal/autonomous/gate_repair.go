package autonomous

// Gate repair engine: when a gate fails, classify the failure shape and
// either report it (the shipped default) or, in mode on, apply the safe
// repair for that class — once per class per task. A spent budget or an
// unknown class raises one held question instead.
//
// Mode — autonomous.gate_repair.mode:
//
//	report (default) — record the finding under the housekeeping group
//		gate_repair, write one decision-log line, raise one alert per failed
//		gate until it stops failing. Nothing is changed.
//	on — apply the safe repair for the class, then reset the failed task and
//		its dependent tasks through the Manager reset path so the gate runs
//		again. Per-class repairs:
//
//		docs_mirror_stale    → make sync-docs, then re-run the gate
//		allowed_list_rejects → add the rejected file to the script's
//			cumulative allowed-files list, then re-run
//		tracker_id_css_hex   → narrow the tracker-ID pattern so it cannot
//			match a '#' hex token, then re-run
//		pipe_sigpipe         → stop the pipe/broken invocation, then re-run
//		restart_killed       → re-run the gate once (it was killed, not
//			failed)
//		later_story_script   → reset the gate run; the gate passes when the
//			later story lands, nothing is created now
//
// Hard limits:
//   - never edits a tracked repo file: only the gate script's own
//     .datawatch/verify path, or a generated mirror (make sync-docs), is
//     touched; a script path outside .datawatch/verify is a refusal, not a
//     repair;
//   - one repair per class per task (the budget is persisted across
//     restarts); a second attempt for the same class, and every unknown
//     class, is a held operator question;
//   - every applied repair stores the before/after text pair on its record.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GateRepairGroup is the housekeeping group the gate repairs report under:
// same records shape and bounded cap as the other five groups, and the same
// view grouping in the housekeeping repairs view.
const GateRepairGroup = "gate_repair"

const (
	// gateRepairRecordCap bounds the engine's own last-run record, in
	// memory and on disk (the bounded record).
	gateRepairRecordCap = 200
	// gateRepairHoldAsker is the asked_by of the held question raised for a
	// spent budget or an unknown class; it is also how the question is
	// found again.
	gateRepairHoldAsker = "gate-repair"
	// gateRepairHoldLabel is the held question's label.
	gateRepairHoldLabel = "decision"
	// gateRepairResetActor is the actor recorded by the task reset the
	// repair performs.
	gateRepairResetActor = "gate-repair"
)

// GateRepairInput is one failed gate the engine may act on: the automaton
// and its task, the classifier input carried from the gate result, and the
// absolute path of the gate's verify folder (the only editable scope).
type GateRepairInput struct {
	// AutomatonID is the failed gate's Automaton (PRD).
	AutomatonID string
	// TaskID is the task the gate failed on.
	TaskID string
	// Gate is the classifier input (output, exit code, the script's path
	// and text, the changed files, the story indices).
	Gate GateFailureInput
	// VerifyFolder is the absolute .datawatch/verify folder the gate
	// scripts live in (the repair's only editable scope).
	VerifyFolder string
}

// GateRepairOutcome is what one Run did: the mode, the class that was
// proposed, and — depending on the outcome — the before/after pair of a
// repair, the id of a raised held question, an error, or the refusal
// reason.
type GateRepairOutcome struct {
	Mode string
	// Class is the failure class that was proposed.
	Class GateFailureClass
	// Would says what report mode would do (the class's proposed repair).
	Would string
	// Did says what on mode actually did.
	Did string
	// Done is the on-mode outcome text for a hold, refusal, or failed repair
	// (what report mode would call Would; the record's done line when the
	// repair did not land).
	Done string
	// Before and After are the stored text pair of an applied file repair
	// (empty for re-run-only repairs such as sync-docs and task reset).
	Before string
	After  string
	// Held is the id of the held operator question raised, if any.
	Held string
	// Err is the outcome error text (a failed repair or a refusal).
	Err string
	// Kind is the housekeeping record kind: would, did, failed or
	// skipped_unsafe.
	Kind string
	// Subject is the record/alert subject (the automaton and task).
	Subject string
	// Found is the record's finding line (class plus the leading output).
	Found string
}

// GateRepairDeps are the engine's collaborators, all injectable so tests
// run them with fakes. The daemon wiring (gate_repair_wire.go) fills the
// defaults from the Manager; a test may set any subset.
type GateRepairDeps struct {
	// Mode returns the live autonomous.gate_repair.mode ("off", "report"
	// or "on").
	Mode func() string

	// TaskGate resolves the failure input of one failed gate from the
	// automaton and task ids; ok is false when the task has no failed gate
	// to repair.
	TaskGate func(automatonID, taskID string) (GateRepairInput, bool)

	// RunSyncDocs runs `make sync-docs` in the task's repo dir (the
	// docs_mirror_stale repair) and returns its output.
	RunSyncDocs func(ctx context.Context, repoDir string) (string, error)

	// ResetGateRun resets the failed task and its dependent tasks through
	// the Manager reset path; the gate then runs again.
	ResetGateRun func(automatonID, taskID, reason string) error

	// WidenAllowedList adds the rejected files to the gate script's
	// cumulative allowed-files list and returns the before/after text.
	WidenAllowedList func(in GateRepairInput, lines []string) (before, after string, err error)

	// AdjustPipes rewrites the gate script so it no longer dies of a
	// broken pipe or a sh/bashism mismatch, and returns before/after text.
	AdjustPipes func(in GateRepairInput) (before, after string, err error)

	// NarrowTrackerIDCheck rewrites the gate script's tracker-ID pattern
	// so it cannot match a '#' hex token, and returns before/after text.
	NarrowTrackerIDCheck func(in GateRepairInput) (before, after string, err error)

	// RaiseHeldQuestion raises the one held operator question for a spent
	// budget or an unknown class and returns its id.
	RaiseHeldQuestion func(automatonID, taskID string, class GateFailureClass, reason string) (string, error)

	// Alert raises one alert (report mode / held questions); nil skips.
	Alert func(level, title, body string)

	// Record appends one housekeeping repair record (the direct call-site
	// path; the housekeeping pass records through the Repairer itself);
	// nil skips.
	Record func(rec RepairRecord)

	// Log writes one decision-log line; nil skips.
	Log func(line string)

	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// GateRepairer is one daemon's gate repair engine: the mode gate, the
// classification, the budget, and the outcome records. It is stateless
// except for the persisted budget map and its last-run records.
type GateRepairer struct {
	mu         sync.Mutex
	deps       GateRepairDeps
	budget     map[string]time.Time // "taskID\x00class" → spent at
	budgetPath string
	// records is the engine's own bounded record of the direct call-site
	// runs (the housekeeping-pass runs live on the Repairer).
	records []RepairRecord
	// alerted de-dups the alerts: one per subject and class until that
	// subject stops being found.
	alerted map[string]bool
	nowFn   func() time.Time
}

// NewGateRepairer builds the engine over the given dependencies (all of
// them injectable for tests) and loads the persisted repair budget from
// budgetPath (an empty path keeps it in memory only).
func NewGateRepairer(deps GateRepairDeps, budgetPath string) *GateRepairer {
	g := &GateRepairer{
		deps:       deps,
		budget:     map[string]time.Time{},
		budgetPath: budgetPath,
		alerted:    map[string]bool{},
	}
	if deps.Now != nil {
		g.nowFn = deps.Now
	} else {
		g.nowFn = time.Now
	}
	g.loadBudget()
	return g
}

// Status is the engine's read state: the live mode and the spent budget
// keys (task id, class), sorted.
type GateRepairStatus struct {
	Mode   string   `json:"mode"`
	Budget []string `json:"budget"`
	// Records is the engine's own bounded record, newest last.
	Records []RepairRecord `json:"records"`
}

// Status returns the engine's read state.
func (g *GateRepairer) Status() GateRepairStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	var keys []string
	for k := range g.budget {
		keys = append(keys, k)
	}
	return GateRepairStatus{
		Mode:    g.mode(),
		Budget:  keys,
		Records: append([]RepairRecord(nil), g.records...),
	}
}

// Run acts on one failed gate, given its automaton and task ids, in the
// live mode: report records and alerts only; on applies the repair for the
// class (after the hard-limit checks) and resets the failed task and its
// dependents so the gate runs again. It never returns without a record and
// a decision-log line, unless the mode is off or there is nothing to act on.
func (g *GateRepairer) Run(ctx context.Context, automatonID, taskID string) GateRepairOutcome {
	mode := g.mode()
	if mode == "off" {
		return GateRepairOutcome{Mode: mode, Err: "gate repair is off; nothing recorded"}
	}
	in, ok := g.deps.TaskGate(automatonID, taskID)
	if !ok {
		return GateRepairOutcome{Mode: mode, Err: fmt.Sprintf("no failed gate found for task %s", taskID)}
	}
	class := ClassifyGateFailure(in.Gate)
	subject := in.AutomatonID + " / " + in.TaskID
	found := gateFindingLine(class, in.Gate.Output)

	switch mode {
	case "report":
		out := GateRepairOutcome{
			Mode: mode, Class: class, Subject: subject, Found: found,
			Would: class.Repair(), Kind: RepairWould,
		}
		g.report(in, out)
		return out
	case "on":
		return g.apply(ctx, in, class, subject, found)
	default:
		return GateRepairOutcome{Mode: mode, Err: "unknown gate repair mode " + mode}
	}
}

// report (report mode) records the finding, writes the decision-log line,
// and raises the one alert for this subject until it stops failing. Nothing
// is changed, and the on-mode budget is not touched.
func (g *GateRepairer) report(in GateRepairInput, out GateRepairOutcome) {
	g.mu.Lock()
	g.appendRecord(RepairRecord{
		At:      g.nowFn(),
		Group:   GateRepairGroup,
		Subject: out.Subject,
		Kind:    RepairWould,
		Found:   out.Found,
		Done:    "would repair (report mode; nothing changed)",
		Undo:    "nothing was changed",
	})
	g.alertSeen(out.Subject+"\x00"+string(out.Class), in, out)
	g.mu.Unlock()
	g.log(out)
}

// apply (on mode) makes the one repair allowed for this class on this task
// and resets the failed task and its dependents, or raises the held
// question when the budget is spent, the class is unknown, or a hard limit
// refuses the repair.
func (g *GateRepairer) apply(ctx context.Context, in GateRepairInput, class GateFailureClass, subject, found string) GateRepairOutcome {
	spentAt, spent := g.spendCheck(in.TaskID, class)
	if spent {
		out := GateRepairOutcome{
			Mode: "on", Class: class, Subject: subject, Found: found,
			Kind: RepairSkippedUnsafe,
			Done: fmt.Sprintf("held: the budget for %s on task %s is already spent (%s); no second repair is attempted", class, in.TaskID, spentAt),
		}
		out.Held = g.held(ctx, in, class, "the repair budget for this class on this task is already spent; a second repair is raised as a held question instead")
		g.finishApplied(ctx, in, class, out, false)
		return out
	}
	if class == GateFailureUnknown {
		out := GateRepairOutcome{
			Mode: "on", Class: class, Subject: subject, Found: found,
			Kind: RepairSkippedUnsafe,
			Done: "held: no known failure shape matched the gate output",
		}
		out.Held = g.held(ctx, in, class, "no known failure shape matched the gate output, so the gate repair engine has no safe repair to apply")
		g.finishApplied(ctx, in, class, out, false)
		return out
	}

	// Hard limit: the editable scope is the gate script's own
	// .datawatch/verify folder. Any class whose repair edits a file must
	// resolve a script path inside that folder, or the repair is refused.
	if classEditsScript(class) {
		if in.Gate.ScriptPath == "" || in.VerifyFolder == "" {
			return g.refuse(ctx, in, class, subject, found, fmt.Sprintf("refused: no gate script path (or verify folder) to repair, and %s must not edit any tracked repo file", class))
		}
		if !pathWithin(in.VerifyFolder, in.Gate.ScriptPath) {
			return g.refuse(ctx, in, class, subject, found, fmt.Sprintf("refused: the gate script %q is outside the .datawatch/verify folder; the gate repair engine does not edit tracked repo files outside it", in.Gate.ScriptPath))
		}
	}

	out := GateRepairOutcome{Mode: "on", Class: class, Subject: subject, Found: found, Kind: RepairDid}
	switch class {
	case GateFailureDocsMirrorStale:
		var syncErr error
		out.After, syncErr = runSyncDocs(ctx, g.deps, in)
		if syncErr != nil {
			return g.failApply(in, out, "sync-docs: "+syncErr.Error())
		}
		out.After = truncate(out.After, 2000)
	case GateFailureAllowedListRejects:
		lines := allowedRejectLines(in.Gate.Output)
		if len(lines) == 0 {
			return g.refuse(ctx, in, class, subject, found, "refused: the rejected files could not be read from the gate output, so the allowed-files list cannot be widened safely")
		}
		var err error
		out.Before, out.After, err = g.deps.WidenAllowedList(in, lines)
		if err != nil {
			return g.failApply(in, out, "allowed-list widen: "+err.Error())
		}
	case GateFailureTrackerIDCSSHex:
		var err error
		out.Before, out.After, err = g.deps.NarrowTrackerIDCheck(in)
		if err != nil {
			return g.failApply(in, out, "tracker-id narrow: "+err.Error())
		}
	case GateFailurePipeSigpipe:
		var err error
		out.Before, out.After, err = g.deps.AdjustPipes(in)
		if err != nil {
			return g.failApply(in, out, "pipe fix: "+err.Error())
		}
	case GateFailureRestartKilled, GateFailureLaterStoryScript:
		// re-run only: nothing to edit; the reset below is the repair.
	default:
		return GateRepairOutcome{Mode: "on", Class: class, Subject: subject, Found: found, Kind: RepairFailed, Err: "no known repair for class " + string(class)}
	}

	resetReason := string(class)
	if err := g.deps.ResetGateRun(in.AutomatonID, in.TaskID, resetReason); err != nil {
		return g.failApply(in, out, "reset the failed task and its dependents: "+err.Error())
	}
	out.Did = "repaired " + string(class) + " and reset the failed task and its dependent tasks; the gate runs again"
	g.finishApplied(ctx, in, class, out, true)
	return out
}

// refuse records a hard-limit refusal (a script outside the editable
// scope, or a repair that could not be made safely) and raises the held
// question instead of touching anything.
func (g *GateRepairer) refuse(ctx context.Context, in GateRepairInput, class GateFailureClass, subject, found, reason string) GateRepairOutcome {
	out := GateRepairOutcome{
		Mode: "on", Class: class, Subject: subject, Found: found,
		Kind: RepairSkippedUnsafe, Err: reason,
		Done: reason,
	}
	out.Held = g.held(ctx, in, class, reason)
	g.finishApplied(ctx, in, class, out, false)
	return out
}

// finishApplied records the outcome, writes the decision-log line, spends
// the budget on success, and raises the one alert for the subject.
func (g *GateRepairer) finishApplied(ctx context.Context, in GateRepairInput, class GateFailureClass, out GateRepairOutcome, success bool) {
	g.mu.Lock()
	g.appendRecord(RepairRecord{
		At:      g.nowFn(),
		Group:   GateRepairGroup,
		Subject: out.Subject,
		Kind:    out.Kind,
		Found:   out.Found,
		Done:    out.Did,
		Before:  out.Before,
		After:   out.After,
		Undo:    gateRepairUndoHint(out),
	})
	if success {
		g.budget[gateBudgetKey(in.TaskID, class)] = g.nowFn()
		g.saveBudgetLocked()
	}
	if out.Held != "" || out.Kind == RepairWould || out.Kind == RepairSkippedUnsafe {
		g.alertSeen(out.Subject+"\x00"+string(class), in, out)
	}
	g.mu.Unlock()
	g.log(out)
}

func (g *GateRepairer) failApply(in GateRepairInput, out GateRepairOutcome, why string) GateRepairOutcome {
	out.Kind = RepairFailed
	out.Err = why
	out.Done = "repair failed: " + why
	g.mu.Lock()
	g.appendRecord(RepairRecord{
		At:      g.nowFn(),
		Group:   GateRepairGroup,
		Subject: out.Subject,
		Kind:    out.Kind,
		Found:   out.Found,
		Done:    out.Done,
		Before:  out.Before,
		After:   out.After,
	})
	g.mu.Unlock()
	g.log(out)
	return out
}

// held raises the one held operator question and returns its id ("" when
// the raise is unavailable or fails — the hold is then only the record).
func (g *GateRepairer) held(ctx context.Context, in GateRepairInput, class GateFailureClass, reason string) string {
	if g.deps.RaiseHeldQuestion == nil {
		return ""
	}
	id, err := g.deps.RaiseHeldQuestion(in.AutomatonID, in.TaskID, class, reason)
	if err != nil {
		return ""
	}
	_ = ctx
	return id
}

// alertSeen raises the one alert for a subject and class, until that
// subject stops being found. Call with g.mu held. The text depends on the
// outcome: report mode says what would be repaired, a hold says who must
// look, an applied repair does not alert (the record and decision line are
// enough).
func (g *GateRepairer) alertSeen(key string, in GateRepairInput, out GateRepairOutcome) {
	if g.alerted[key] || g.deps.Alert == nil {
		return
	}
	g.alerted[key] = true
	switch out.Kind {
	case RepairWould:
		g.deps.Alert("warn", "Gate repair found: "+out.Subject,
			fmt.Sprintf("The gate failed with the %q shape: %s. In report mode the engine would: %s. Nothing was changed.", out.Class, out.Found, out.Would))
	case RepairSkippedUnsafe:
		g.deps.Alert("warn", "Gate repair held: "+out.Subject,
			fmt.Sprintf("The gate failed with the %q shape: %s. %s A person needs to look at it.", out.Class, out.Found, out.Done))
	}
}

// spendCheck reports whether the on-mode budget for this task and class was
// already spent in a previous run.
func (g *GateRepairer) spendCheck(taskID string, class GateFailureClass) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	at, ok := g.budget[gateBudgetKey(taskID, class)]
	return at, ok
}

func (g *GateRepairer) mode() string {
	if g.deps.Mode != nil {
		return strings.ToLower(strings.TrimSpace(g.deps.Mode()))
	}
	return "report"
}

// log writes the one decision-log line for this outcome.
func (g *GateRepairer) log(out GateRepairOutcome) {
	if g.deps.Log == nil {
		return
	}
	g.deps.Log(DecisionLogLine(RepairRecord{
		At: g.nowFn(), Group: GateRepairGroup, Subject: out.Subject,
		Kind: out.Kind, Found: out.Found, Done: out.Done,
		Before: out.Before, After: out.After,
	}))
}

// appendRecord appends one record to the engine's bounded record, under
// g.mu which the caller must hold.
func (g *GateRepairer) appendRecord(rec RepairRecord) {
	g.records = append(g.records, rec)
	if len(g.records) > gateRepairRecordCap {
		g.records = g.records[len(g.records)-gateRepairRecordCap:]
	}
}

// Records returns a copy of the engine's bounded record.
func (g *GateRepairer) Records() []RepairRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]RepairRecord(nil), g.records...)
}

func gateBudgetKey(taskID string, c GateFailureClass) string {
	return taskID + "\x00" + string(c)
}

// classEditsScript reports whether the safe repair for this class edits the
// gate script's text (as opposed to re-running the gate, syncing the docs
// mirror, or just resetting the run).
func classEditsScript(c GateFailureClass) bool {
	switch c {
	case GateFailureAllowedListRejects, GateFailureTrackerIDCSSHex, GateFailurePipeSigpipe:
		return true
	}
	return false
}

// gateFindingLine is the record's found line: the class plus the first
// 200 runes of the gate output, on one line.
func gateFindingLine(class GateFailureClass, output string) string {
	s := strings.Join(strings.Fields(output), " ")
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		s = "(no captured output)"
	}
	return string(class) + ": " + s
}

func gateRepairUndoHint(out GateRepairOutcome) string {
	if out.Before == "" && out.Did == "" {
		return "nothing was changed"
	}
	if out.Before != "" {
		return "restore the before text recorded beside this; if the task was reset, put it back to failed to re-open the failed gate"
	}
	return "the repair only re-runs the gate and resets the task; set the task back to failed to re-open the failed gate"
}

func runSyncDocs(ctx context.Context, deps GateRepairDeps, in GateRepairInput) (string, error) {
	if deps.RunSyncDocs == nil {
		return "", fmt.Errorf("no sync-docs runner is wired for this engine")
	}
	dir := ""
	if abs, err := filepath.Abs(in.VerifyFolder); err == nil {
		// .datawatch/verify lives in the repo root's .datawatch folder.
		dir = filepath.Dir(filepath.Dir(abs))
	}
	return deps.RunSyncDocs(ctx, dir)
}

// allowedRejectLines extracts the files the allowed-files check rejected
// from the gate output, one per line (trimmed, de-duplicated).
func allowedRejectLines(output string) []string {
	var lines []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "reject") || strings.Contains(low, "not in allowed") || strings.Contains(low, "not allowed") || strings.Contains(low, "scope") {
			for _, tok := range strings.Fields(line) {
				tok = strings.Trim(tok, `",:()[]`)
				if strings.Contains(tok, "/") || strings.HasSuffix(tok, ".go") || strings.HasSuffix(tok, ".md") || strings.HasSuffix(tok, ".json") || strings.HasSuffix(tok, ".sh") || strings.HasSuffix(tok, ".css") || strings.HasSuffix(tok, ".js") {
					if !seen[tok] {
						seen[tok] = true
						lines = append(lines, tok)
					}
				}
			}
		}
	}
	return lines
}

// --- budget persistence (json file; path empty = in-memory only) ---------

type gateBudgetFile struct {
	Spent map[string]time.Time `json:"spent"`
}

func (g *GateRepairer) loadBudget() {
	if g.budgetPath == "" {
		return
	}
	b, err := os.ReadFile(g.budgetPath)
	if err != nil {
		return
	}
	var f gateBudgetFile
	if err := json.Unmarshal(b, &f); err != nil {
		return
	}
	for k, at := range f.Spent {
		g.budget[k] = at
	}
}

func (g *GateRepairer) saveBudgetLocked() {
	if g.budgetPath == "" {
		return
	}
	b, err := json.MarshalIndent(gateBudgetFile{Spent: g.budget}, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(g.budgetPath), 0o755)
	_ = os.WriteFile(g.budgetPath, b, 0o644)
}

// gateHoldQuestionKey is the dedup key of the engine's held question:
// one question per failed gate and class, no matter which run raised it.
func gateHoldQuestionKey(automatonID, taskID string, c GateFailureClass) string {
	sum := sha256.Sum256([]byte(automatonID + "\x00" + taskID + "\x00" + string(c)))
	return string(c) + "-" + hex.EncodeToString(sum[:6])
}
