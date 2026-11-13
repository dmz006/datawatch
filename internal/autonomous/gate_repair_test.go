package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// gateRepairTestDeps builds fake deps for the engine, recording every call
// so the tests can assert which collaborator ran (or was refused).
type gateRepairTestDeps struct {
	base     GateRepairDeps
	input    GateRepairInput
	mode     string
	syncDir  string
	syncErr  string
	resetIDs []string
	resetErr string
	widenIn  GateRepairInput
	widenLn  []string
	widenErr string
	pipeErr  string
	narrowIn GateRepairInput
	narrowEr string
	holds    []string
	holdErr  string
	alerts   []string
	records  []RepairRecord
	logs     []string
}

func newGateRepairTestDeps(mode string) *gateRepairTestDeps {
	d := &gateRepairTestDeps{mode: mode}
	d.base = GateRepairDeps{
		Mode: func() string { return d.mode },
		TaskGate: func(automatonID, taskID string) (GateRepairInput, bool) {
			in := d.input
			if in.AutomatonID == "" {
				in.AutomatonID = automatonID
			}
			if in.TaskID == "" {
				in.TaskID = taskID
			}
			return in, true
		},
		RunSyncDocs: func(ctx context.Context, repoDir string) (string, error) {
			d.syncDir = repoDir
			if d.syncErr != "" {
				return "", errText(d.syncErr)
			}
			return "synced", nil
		},
		ResetGateRun: func(automatonID, taskID, reason string) error {
			d.resetIDs = append(d.resetIDs, automatonID+"/"+taskID+"/"+reason)
			if d.resetErr != "" {
				return errText(d.resetErr)
			}
			return nil
		},
		WidenAllowedList: func(in GateRepairInput, lines []string) (string, string, error) {
			d.widenIn = in
			d.widenLn = lines
			if d.widenErr != "" {
				return "", "", errText(d.widenErr)
			}
			return "before", "after", nil
		},
		AdjustPipes: func(in GateRepairInput) (string, string, error) {
			if d.pipeErr != "" {
				return "", "", errText(d.pipeErr)
			}
			return "before", "after", nil
		},
		NarrowTrackerIDCheck: func(in GateRepairInput) (string, string, error) {
			d.narrowIn = in
			if d.narrowEr != "" {
				return "", "", errText(d.narrowEr)
			}
			return "before", "after", nil
		},
		RaiseHeldQuestion: func(automatonID, taskID string, class GateFailureClass, reason string) (string, error) {
			if d.holdErr != "" {
				return "", errText(d.holdErr)
			}
			id := "held-" + string(class)
			d.holds = append(d.holds, id)
			return id, nil
		},
		Alert: func(level, title, body string) {
			d.alerts = append(d.alerts, level+": "+title)
		},
		Record: func(rec RepairRecord) { d.records = append(d.records, rec) },
		Log:    func(line string) { d.logs = append(d.logs, line) },
	}
	return d
}

func errText(s string) error { return gateErrText(s) }

type gateErrText string

func (e gateErrText) Error() string { return string(e) }

func TestGateRepairReportModeRecordsAndAlertsOnce(t *testing.T) {
	d := newGateRepairTestDeps("report")
	d.input = GateRepairInput{
		VerifyFolder: t.TempDir(),
		Gate: GateFailureInput{
			Output:     "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage",
			ScriptPath: ".datawatch/verify/final.sh",
		},
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")

	if out.Mode != "report" || out.Class != GateFailureDocsMirrorStale || out.Kind != RepairWould {
		t.Fatalf("report outcome wrong: %+v", out)
	}
	if len(d.alerts) != 1 {
		t.Fatalf("expected exactly one alert, got %d: %v", len(d.alerts), d.alerts)
	}
	// Report mode must not touch the world.
	if d.syncDir != "" || len(d.resetIDs) != 0 || len(d.widenIn.Gate.Output) != 0 {
		t.Fatalf("report mode changed something: sync=%q reset=%v", d.syncDir, d.resetIDs)
	}
	// The on-mode budget must not be spent by report mode.
	if _, spent := g.spendCheck("task-1", GateFailureDocsMirrorStale); spent {
		t.Fatal("report mode must not spend the on-mode budget")
	}
	// A second failure of the same subject raises no second alert.
	g.Run(context.Background(), "prd-1", "task-1")
	if len(d.alerts) != 1 {
		t.Fatalf("second run raised another alert: %v", d.alerts)
	}
	// One bounded record with the would kind exists.
	recs := g.Records()
	if len(recs) != 2 || recs[0].Group != GateRepairGroup || recs[0].Kind != RepairWould {
		t.Fatalf("records wrong: %v", recs)
	}
	// A decision-log line was written per run.
	if len(d.logs) != 2 {
		t.Fatalf("decision-log lines: got %d, want 2", len(d.logs))
	}
}

func TestGateRepairOffModeRecordsNothing(t *testing.T) {
	d := newGateRepairTestDeps("off")
	d.input = GateRepairInput{
		VerifyFolder: t.TempDir(),
		Gate:         GateFailureInput{Output: "sync-docs docs drift"},
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")
	if out.Mode != "off" || out.Err == "" {
		t.Fatalf("off outcome wrong: %+v", out)
	}
	if len(d.alerts) != 0 || len(g.Records()) != 0 || len(d.logs) != 0 {
		t.Fatalf("off mode left side effects: alerts=%v records=%v logs=%v", d.alerts, g.Records(), d.logs)
	}
}

func TestGateRepairOnAppliesDocsMirrorAndResets(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.input = GateRepairInput{
		Gate: GateFailureInput{
			Output:     "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage",
			ScriptPath: ".datawatch/verify/final.sh",
		},
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairDid || out.Err != "" {
		t.Fatalf("expected did, got %+v", out)
	}
	if d.syncDir == "" {
		t.Fatal("sync-docs was not run")
	}
	if len(d.resetIDs) != 1 {
		t.Fatalf("expected one gate-run reset, got %v", d.resetIDs)
	}
	gotReset := d.resetIDs[0]
	if !strings.Contains(gotReset, "task-1") || !strings.Contains(gotReset, string(GateFailureDocsMirrorStale)) {
		t.Fatalf("reset id wrong: %q", gotReset)
	}
	if _, spent := g.spendCheck("task-1", GateFailureDocsMirrorStale); !spent {
		t.Fatal("budget was not spent on a successful repair")
	}
	recs := g.Records()
	if len(recs) != 1 || recs[0].Kind != RepairDid || recs[0].After == "" {
		t.Fatalf("record wrong: %v", recs)
	}
}

func TestGateRepairOnAppliesAllowedListWiden(t *testing.T) {
	d := newGateRepairTestDeps("on")
	vf := t.TempDir()
	d.input = GateRepairInput{
		VerifyFolder: vf,
		Gate: GateFailureInput{
			ScriptPath:   filepath.Join(vf, "scope.sh"),
			ScriptText:   "allowed_list=( ... )\n",
			Output:       "ERROR: docs/CHANGELOG.md is not in the allowed list — out of scope",
			ChangedFiles: []string{"docs/CHANGELOG.md", "internal/x/y.go"},
		},
	}
	if got := ClassifyGateFailure(d.input.Gate); got != GateFailureAllowedListRejects {
		t.Fatalf("classifier fixture wrong: %v", got)
	}
	gw := NewGateRepairer(d.base, "")
	out := gw.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairDid || out.Err != "" {
		t.Fatalf("expected did, got %+v", out)
	}
	if !gateRepairStrSliceHas(d.widenLn, "docs/CHANGELOG.md") {
		t.Fatalf("widen lines missing the rejected file: %v", d.widenLn)
	}
	if out.Before != "before" || out.After != "after" {
		t.Fatalf("before/after pair not stored: %+v", out)
	}
}

func TestGateRepairOnAppliesTrackerNarrowAndPipeFix(t *testing.T) {
	dn := newGateRepairTestDeps("on")
	vf := t.TempDir()
	dn.input = GateRepairInput{
		VerifyFolder: vf,
		Gate: GateFailureInput{
			ScriptPath: filepath.Join(vf, "no-internal-refs.sh"),
			Output:     "found tracker-like token #B40044 in style.css",
		},
	}
	if got := ClassifyGateFailure(dn.input.Gate); got != GateFailureTrackerIDCSSHex {
		t.Fatalf("classifier fixture wrong: %v", got)
	}
	gn := NewGateRepairer(dn.base, "")
	out := gn.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairDid || out.Err != "" {
		t.Fatalf("expected did, got %+v", out)
	}

	vp := t.TempDir()
	dp := newGateRepairTestDeps("on")
	dp.input = GateRepairInput{
		VerifyFolder: vp,
		Gate: GateFailureInput{
			ScriptPath:   filepath.Join(vp, "tests.sh"),
			ExitCode:     141,
			SignalKilled: true,
			Output:       "go test ./...",
		},
	}
	if got := ClassifyGateFailure(dp.input.Gate); got != GateFailurePipeSigpipe {
		t.Fatalf("classifier fixture wrong: %v", got)
	}
	gp := NewGateRepairer(dp.base, "")
	outp := gp.Run(context.Background(), "prd-1", "task-1")
	if outp.Kind != RepairDid || outp.Err != "" {
		t.Fatalf("expected did, got %+v", outp)
	}
}

func TestGateRepairOnReRunOnlyClasses(t *testing.T) {
	// restart_killed and later_story_script are re-run-only: reset, no edit.
	dr := newGateRepairTestDeps("on")
	dr.input = GateRepairInput{
		Gate: GateFailureInput{SignalKilled: true, ExitCode: -15},
	}
	if got := ClassifyGateFailure(dr.input.Gate); got != GateFailureRestartKilled {
		t.Fatalf("classifier fixture wrong: %v", got)
	}
	gr := NewGateRepairer(dr.base, "")
	out := gr.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairDid || out.Err != "" || len(dr.resetIDs) != 1 {
		t.Fatalf("restart_killed: %+v; resets=%v", out, dr.resetIDs)
	}

	vl := t.TempDir()
	dl := newGateRepairTestDeps("on")
	dl.input = GateRepairInput{
		VerifyFolder: vl,
		Gate: GateFailureInput{
			InOwnVerifyFolder: true,
			ScriptStoryIndex:  2,
			GateStoryIndex:    0,
			Output:            "sh: story2.sh: not found",
			ScriptPath:        filepath.Join(vl, "story2.sh"),
		},
	}
	if got := ClassifyGateFailure(dl.input.Gate); got != GateFailureLaterStoryScript {
		t.Fatalf("classifier fixture wrong: %v", got)
	}
	gl := NewGateRepairer(dl.base, "")
	outl := gl.Run(context.Background(), "prd-1", "task-1")
	if outl.Kind != RepairDid || outl.Err != "" || len(dl.resetIDs) != 1 || dl.syncDir != "" {
		t.Fatalf("later_story_script: %+v; resets=%v; sync=%q", outl, dl.resetIDs, dl.syncDir)
	}
}

func TestGateRepairOnRefusesOutsideVerifyFolder(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.input = GateRepairInput{
		VerifyFolder: filepath.Join(t.TempDir(), ".datawatch", "verify"),
		Gate: GateFailureInput{
			ScriptPath: filepath.Join(t.TempDir(), "scripts", "check-scope.sh"), // outside the verify folder
			ScriptText: "allowed_list=( ... )\n",
			Output:     "not in the allowed list: docs/CHANGELOG.md",
			ChangedFiles: []string{
				"docs/CHANGELOG.md",
			},
		},
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairSkippedUnsafe {
		t.Fatalf("expected a refused repair, got %+v", out)
	}
	if len(d.widenIn.Gate.Output) != 0 {
		t.Fatalf("the widen ran for a script outside the verify folder")
	}
	if len(d.resetIDs) != 0 {
		t.Fatalf("a refused repair must not reset the gate run: %v", d.resetIDs)
	}
	if len(d.holds) != 1 || d.holds[0] != "held-"+string(GateFailureAllowedListRejects) {
		t.Fatalf("held question not raised for the refusal: %v", d.holds)
	}
	if _, spent := g.spendCheck("task-1", GateFailureAllowedListRejects); spent {
		t.Fatal("a refused repair must not spend the budget")
	}
}

func TestGateRepairOnSecondAttemptIsHeld(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.input = GateRepairInput{
		VerifyFolder: t.TempDir(),
		Gate: GateFailureInput{
			Output:     "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage",
			ScriptPath: ".datawatch/verify/final.sh",
		},
	}
	g := NewGateRepairer(d.base, "")
	if out := g.Run(context.Background(), "prd-1", "task-1"); out.Kind != RepairDid {
		t.Fatalf("first attempt: %+v", out)
	}
	d.holds = nil
	d.resetIDs = nil
	out2 := g.Run(context.Background(), "prd-1", "task-1")
	if out2.Kind != RepairSkippedUnsafe {
		t.Fatalf("second attempt: %+v", out2)
	}
	if len(d.holds) != 1 {
		t.Fatalf("second attempt must raise the held question: %v", d.holds)
	}
	if len(d.resetIDs) != 0 {
		t.Fatalf("a held second attempt must not reset the gate run: %v", d.resetIDs)
	}
}

func TestGateRepairOnUnknownClassIsHeld(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.input = GateRepairInput{
		VerifyFolder: t.TempDir(),
		Gate: GateFailureInput{
			Output:   "assertion failed: expected 2 files, got 3",
			ExitCode: 1,
		},
	}
	if got := ClassifyGateFailure(d.input.Gate); got != GateFailureUnknown {
		t.Fatalf("classifier fixture wrong: %v", got)
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairSkippedUnsafe || out.Held == "" {
		t.Fatalf("unknown class: %+v", out)
	}
	if len(d.holds) != 1 {
		t.Fatalf("unknown class must raise the held question: %v", d.holds)
	}
	if len(d.resetIDs) != 0 || d.syncDir != "" {
		t.Fatalf("unknown class must not act: resets=%v sync=%q", d.resetIDs, d.syncDir)
	}
}

func TestGateRepairBudgetPersistsAcrossInstances(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.input = GateRepairInput{
		Gate: GateFailureInput{
			Output:     "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage",
			ScriptPath: ".datawatch/verify/final.sh",
		},
	}
	path := filepath.Join(t.TempDir(), "budget.json")
	g1 := NewGateRepairer(d.base, path)
	if out := g1.Run(context.Background(), "prd-1", "task-1"); out.Kind != RepairDid {
		t.Fatalf("first instance: %+v", out)
	}
	d2 := newGateRepairTestDeps("on")
	d2.input = d.input
	g2 := NewGateRepairer(d2.base, path)
	if _, spent := g2.spendCheck("task-1", GateFailureDocsMirrorStale); !spent {
		t.Fatal("budget did not survive the new instance")
	}
	if out := g2.Run(context.Background(), "prd-1", "task-1"); out.Kind != RepairSkippedUnsafe {
		t.Fatalf("spend in the fresh instance should hold: %+v", out)
	}
}

func TestGateRepairSyncDocsFailureFailsApply(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.syncErr = "make: No rule to make target"
	d.input = GateRepairInput{
		Gate: GateFailureInput{
			Output:     "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage",
			ScriptPath: ".datawatch/verify/final.sh",
		},
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairFailed {
		t.Fatalf("expected repair failed, got %+v", out)
	}
	if len(d.resetIDs) != 0 {
		t.Fatalf("a failed sync must not reset the gate run: %v", d.resetIDs)
	}
	if _, spent := g.spendCheck("task-1", GateFailureDocsMirrorStale); spent {
		t.Fatal("a failed repair must not spend the budget")
	}
}

func TestGateRepairResetFailureFailsApply(t *testing.T) {
	d := newGateRepairTestDeps("on")
	d.resetErr = "the task is not failed"
	d.input = GateRepairInput{
		Gate: GateFailureInput{
			Output:     "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage",
			ScriptPath: ".datawatch/verify/final.sh",
		},
	}
	g := NewGateRepairer(d.base, "")
	out := g.Run(context.Background(), "prd-1", "task-1")
	if out.Kind != RepairFailed {
		t.Fatalf("expected repair failed, got %+v", out)
	}
	if _, spent := g.spendCheck("task-1", GateFailureDocsMirrorStale); spent {
		t.Fatal("a failed reset must not spend the budget")
	}
}

func gateRepairStrSliceHas(l []string, want string) bool {
	for _, v := range l {
		if v == want {
			return true
		}
	}
	return false
}
