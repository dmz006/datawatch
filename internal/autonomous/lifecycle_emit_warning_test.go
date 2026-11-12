// Emission tests for the two opt-in lifecycle warning kinds at their call
// sites (lifecycle_emit_warning.go): scope_warning from the file-drift /
// undeclared-edit checks in file_drift.go, and guardrail_warn from
// recordGuardrailVerdict in guardrail_verdict_record.go. They cover the
// emission path itself — the event fires with the right kind, level and
// IDs — and the "informational only" contract: emitting never blocks,
// records or alters task, story or Automaton behavior (the existing
// decision, audit and alert behavior is asserted unchanged alongside).

package autonomous

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// undeclaredAuditCount counts the undeclared-edit audit events rec carried.
func undeclaredAuditCount(rec *driftAuditRecorder) int {
	n := 0
	for _, a := range rec.acts {
		if a == "autonomous_undeclared_edit_detected" {
			n++
		}
	}
	return n
}

// scopeCollector subscribes a collector to scope_warning events only and
// closes the manager's dispatcher on test cleanup.
func scopeCollector(t *testing.T, m *Manager) *collector {
	t.Helper()
	col := &collector{}
	cancel := m.Subscribe(LifecycleFilter{Kinds: []string{KindScopeWarning}}, col.add)
	t.Cleanup(func() {
		cancel()
		m.lifecycleBus().Close()
	})
	return col
}

// TestScopeWarning_EmittedOnUndeclaredEdit — a worker commit that touches
// an unplanned file (b.go) in warn mode emits exactly one scope_warning
// event carrying the Automaton, task, finding files and the
// undeclared_edit_detected Decision the same path recorded.
func TestScopeWarning_EmittedOnUndeclaredEdit(t *testing.T) {
	m, prd, task, dir, _, alerts := undeclaredFixture(t, "warn", []string{"a.go"})
	col := scopeCollector(t, m)
	workerCommit(t, dir)

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("warn mode must never block")
	}
	waitFor(t, "scope_warning delivery", func() bool { return len(col.snapshot()) == 1 })
	evs := col.snapshot()
	if len(evs) != 1 {
		t.Fatalf("scope_warning events = %d, want exactly 1", len(evs))
	}
	ev := evs[0]
	if ev.Kind != KindScopeWarning {
		t.Errorf("kind = %q, want %q", ev.Kind, KindScopeWarning)
	}
	if ev.PRDID != prd.ID {
		t.Errorf("PRDID = %q, want %q", ev.PRDID, prd.ID)
	}
	if ev.TaskID != task.ID {
		t.Errorf("TaskID = %q, want %q", ev.TaskID, task.ID)
	}
	if ev.Level != LevelTask {
		t.Errorf("level = %q, want %q", ev.Level, LevelTask)
	}
	if !reflect.DeepEqual(ev.Files, []string{"b.go"}) {
		t.Errorf("files = %v, want [b.go]", ev.Files)
	}
	if ev.Decision == nil || ev.Decision.Kind != "undeclared_edit_detected" {
		t.Errorf("decision = %+v, want the undeclared_edit_detected Decision", ev.Decision)
	} else if !strings.Contains(ev.Decision.Note, "b.go") {
		t.Errorf("decision note = %q, naming the flagged file", ev.Decision.Note)
	}
	// Link is non-empty: the deep link to the alert the same path raised,
	// in the "?alert=<id>" form that DescribeLifecycleEvent renders, with
	// the id the sink reported for that alert.
	if ev.Link == "" {
		t.Fatal("Link is empty, want the non-empty alert deep link \"?alert=<id>\"")
	}
	want := "?alert=" + alerts.lastID()
	if ev.Link != want {
		t.Errorf("Link = %q, want %q", ev.Link, want)
	}
	if !strings.Contains(DescribeLifecycleEvent(ev), ev.Link) {
		t.Errorf("DescribeLifecycleEvent = %q, want it to carry the link %q", DescribeLifecycleEvent(ev), ev.Link)
	}
}

// TestScopeWarning_EmittedOnForeignDrift — pre-existing foreign drift,
// once the worker's sweep commits it, emits a scope_warning naming the
// pre-existing files; in-run-only commits emit nothing. In every case the
// pre-existing decision, dirty-file record and blocking behavior the check
// already had (block vs. explicit opt-out) are unchanged.
func TestScopeWarning_EmittedOnForeignDrift(t *testing.T) {
	sweep := func(dir string) {
		// the worker's sweep: its own planned work plus the pre-existing
		// dirty file dragged into the commit.
		driftWrite(t, dir, "own.txt", "task work\n")
		driftGit(t, dir, "add", "own.txt", "pre.txt")
		driftGit(t, dir, "commit", "-q", "-m", "task commit")
	}

	t.Run("pre_existing_warn_behavior_still_blocks", func(t *testing.T) {
		m, prd, task, dir, _ := driftCheckFixture(t, []string{"own.txt"})
		col := scopeCollector(t, m)
		sweep(dir)

		if !m.checkForeignFileDrift(context.Background(), prd, task) {
			t.Fatal("default drift mode must still block on pre-existing drift")
		}
		if task.Status != TaskBlocked {
			t.Fatalf("status = %s, want blocked", task.Status)
		}
		if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
			t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
		}
		waitFor(t, "scope_warning delivery", func() bool { return len(col.snapshot()) == 1 })
		ev := col.snapshot()[0]
		if ev.Kind != KindScopeWarning || ev.PRDID != prd.ID || ev.TaskID != task.ID {
			t.Errorf("event = %+v, want scope_warning for prd %s task %s", ev, prd.ID, task.ID)
		}
		if !reflect.DeepEqual(ev.Files, []string{"pre.txt"}) {
			t.Errorf("files = %v, want [pre.txt] (the pre-existing drift)", ev.Files)
		}
		if ev.Decision == nil || ev.Decision.Kind != "file_drift_detected" {
			t.Errorf("decision = %+v, want the file_drift_detected Decision", ev.Decision)
		}
		// The foreign-drift path records no alert that could be named, so
		// the Link (whose convention is to point at the alert raised
		// alongside the warning) stays empty.
		if ev.Link != "" {
			t.Errorf("Link = %q, want empty (no alert raised on this path)", ev.Link)
		}
	})

	t.Run("pre_existing_opt_out_warns", func(t *testing.T) {
		m, prd, task, dir, _ := driftCheckFixture(t, []string{"own.txt"})
		off := false
		prd.BlockOnFileDrift = &off // explicit opt-out: warn, don't block
		col := scopeCollector(t, m)
		sweep(dir)

		if m.checkForeignFileDrift(context.Background(), prd, task) {
			t.Fatal("explicit opt-out must not block")
		}
		if task.Status == TaskBlocked {
			t.Fatalf("status = %s, must not be blocked with the opt-out set", task.Status)
		}
		if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
			t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
		}
		waitFor(t, "scope_warning delivery", func() bool { return len(col.snapshot()) == 1 })
		ev := col.snapshot()[0]
		if !reflect.DeepEqual(ev.Files, []string{"pre.txt"}) {
			t.Errorf("files = %v, want [pre.txt]", ev.Files)
		}
	})

	t.Run("in_run_only_never_emits", func(t *testing.T) {
		m, prd, task, dir, _ := driftCheckFixture(t, []string{"own.txt"})
		col := scopeCollector(t, m)
		// The in-run commit is own.txt (planned) plus new.txt — a brand-new
		// file the worker itself created, so the run has no PRE-EXISTING
		// drift even though it committed a foreign file.
		driftWrite(t, dir, "own.txt", "task work\n")
		driftWrite(t, dir, "new.txt", "worker created\n")
		driftGit(t, dir, "add", "own.txt", "new.txt")
		driftGit(t, dir, "commit", "-q", "-m", "task commit")

		if m.checkForeignFileDrift(context.Background(), prd, task) {
			t.Fatal("in-run-only commits never blocked (and still must not)")
		}
		if task.ForeignFileDrift != nil {
			t.Errorf("ForeignFileDrift = %v, want nil (no pre-existing drift)", task.ForeignFileDrift)
		}
		time.Sleep(100 * time.Millisecond) // let the dispatcher drain
		if got := col.snapshot(); len(got) != 0 {
			t.Errorf("scope_warning events = %d for in-run-only drift, want 0: %v", len(got), got)
		}
	})
}

// TestScopeWarning_DoesNotChangeBehavior — the emission is strictly
// additive per undeclared-edit mode: warn mode reports (and emits) without
// blocking; block mode blocks (and emits); off mode stays silent. In all
// three the pre-existing decision + audit behavior is untouched.
func TestScopeWarning_DoesNotChangeBehavior(t *testing.T) {
	t.Run("warn_emits_and_leaves_task_running", func(t *testing.T) {
		m, prd, task, dir, audit, alerts := undeclaredFixture(t, "warn", []string{"a.go"})
		col := scopeCollector(t, m)
		workerCommit(t, dir)

		if m.checkUndeclaredEdits(context.Background(), prd, task) {
			t.Fatal("warn mode must not block")
		}
		if task.Status == TaskBlocked {
			t.Fatalf("warn mode left the task %s", task.Status)
		}
		if len(task.UndeclaredEdits) != 1 {
			t.Fatalf("UndeclaredEdits = %v, want the finding recorded", task.UndeclaredEdits)
		}
		if got := undeclaredAuditCount(audit); got != 1 {
			t.Errorf("audits = %d, want 1 (unchanged by the emission)", got)
		}
		if got := len(alerts.all()); got != 1 {
			t.Errorf("alerts = %d, want 1 (unchanged by the emission)", got)
		}
		waitFor(t, "scope_warning delivery", func() bool { return len(col.snapshot()) == 1 })
		if len(col.snapshot()) != 1 {
			t.Fatalf("warn mode: scope_warning events = %d, want 1", len(col.snapshot()))
		}
	})

	t.Run("block_still_blocks_and_emits", func(t *testing.T) {
		m, prd, task, dir, audit, alerts := undeclaredFixture(t, "block", []string{"a.go"})
		col := scopeCollector(t, m)
		workerCommit(t, dir)

		if !m.checkUndeclaredEdits(context.Background(), prd, task) {
			t.Fatal("block mode must still block")
		}
		if task.Status != TaskBlocked {
			t.Fatalf("status = %s, want blocked", task.Status)
		}
		if got := undeclaredAuditCount(audit); got != 1 {
			t.Errorf("audits = %d, want 1 (unchanged by the emission)", got)
		}
		if got := len(alerts.all()); got != 1 {
			t.Errorf("alerts = %d, want 1 (unchanged by the emission)", got)
		}
		waitFor(t, "scope_warning delivery", func() bool { return len(col.snapshot()) == 1 })
		if len(col.snapshot()) != 1 {
			t.Fatalf("block mode: scope_warning events = %d, want 1", len(col.snapshot()))
		}
	})

	t.Run("off_mode_is_silent", func(t *testing.T) {
		m, prd, task, dir, audit, alerts := undeclaredFixture(t, "off", []string{"a.go"})
		col := scopeCollector(t, m)
		workerCommit(t, dir)

		if m.checkUndeclaredEdits(context.Background(), prd, task) {
			t.Fatal("off mode must neither report nor block")
		}
		if task.Status == TaskBlocked || len(task.UndeclaredEdits) != 0 {
			t.Fatalf("off mode must record nothing: status=%s edits=%v", task.Status, task.UndeclaredEdits)
		}
		if got := undeclaredAuditCount(audit); got != 0 {
			t.Errorf("audits = %d, want 0 in off mode", got)
		}
		if got := len(alerts.all()); got != 0 {
			t.Errorf("alerts = %d, want 0 in off mode", got)
		}
		time.Sleep(100 * time.Millisecond) // let the dispatcher drain
		if got := col.snapshot(); len(got) != 0 {
			t.Errorf("scope_warning events = %d in off mode, want 0: %v", len(got), got)
		}
	})
}

// warningVerdict is the shared verdict shape for the guardrail tests: one
// guardrail, one outcome (set per case), a summary and an issue so the
// note and the event both carry detail.
func warningVerdict(outcome string) GuardrailVerdict {
	return GuardrailVerdict{
		Guardrail: "rules",
		Outcome:   outcome,
		Summary:   "one finding",
		Issues:    []string{"line 3: forbidden import"},
		VerdictAt: time.Now(),
	}
}

// guardWarnPRD is a Manager with one story and one task and a collector
// subscribed to guardrail_warn only, returning the stored PRD plus the
// story and task IDs for the three unit levels.
func guardWarnPRD(t *testing.T) (*Manager, *PRD, string, string, *collector) {
	t.Helper()
	m := newEmitManager(t, DefaultConfig(), nil)
	prd := newEmitPRD(t, m, PRDActive)
	storyID := prd.Story[0].ID
	taskID := prd.Story[0].Tasks[0].ID
	col := &collector{}
	cancel := m.Subscribe(LifecycleFilter{Kinds: []string{KindGuardrailWarn}}, col.add)
	t.Cleanup(func() {
		cancel()
		m.lifecycleBus().Close()
	})
	return m, prd, storyID, taskID, col
}

func guardVerdicts(prd *PRD) []Decision {
	var out []Decision
	for _, d := range prd.Decisions {
		if d.Kind == "guardrail_verdict" {
			out = append(out, d)
		}
	}
	return out
}

// TestGuardrailWarn_EmittedOnWarnVerdictOnly — the warn outcome records
// the verdict AND emits exactly one guardrail_warn; pass and block record
// the verdict (their existing behavior) and emit nothing. The emitted
// event's level and IDs follow the recorded unit: task, story (reported
// at Automaton level with StoryID) and Automaton.
func TestGuardrailWarn_EmittedOnWarnVerdictOnly(t *testing.T) {
	cases := []struct {
		name      string
		unit      string // "task" | "story" | "automaton"
		outcome   string
		wantLevel LifecycleLevel
		wantTask  bool
		wantStory bool
	}{
		{"warn_taskLevel", "task", "warn", LevelTask, true, true},
		{"warn_storyLevel", "story", "warn", LevelPRD, false, true},
		{"warn_automatonLevel", "automaton", "warn", LevelPRD, false, false},
		{"pass_taskLevel", "task", "pass", LevelTask, true, true},
		{"block_taskLevel", "task", "block", LevelTask, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Fresh Automaton + collector per case: the collector accumulates
			// across subtests, so shared state would cross assertions.
			m, prd, storyID, taskID, col := guardWarnPRD(t)
			unitID := taskID
			switch c.unit {
			case "story":
				unitID = storyID
			case "automaton":
				unitID = prd.ID
			}
			if err := m.recordGuardrailVerdict(prd, unitID, warningVerdict(c.outcome)); err != nil {
				t.Fatalf("recordGuardrailVerdict: %v", err)
			}

			// Behavior is unchanged for every outcome: the verdict is
			// recorded on the stored PRD either way.
			stored, ok := m.Store().GetPRD(prd.ID)
			if !ok {
				t.Fatal("GetPRD")
			}
			got := guardVerdicts(stored)
			if len(got) != 1 {
				t.Fatalf("%s: verdicts recorded = %d, want exactly 1: %+v", c.outcome, len(got), got)
			}
			last := got[0]
			if last.VerdictOutcome != c.outcome || !strings.Contains(last.Note, c.outcome) {
				t.Errorf("%s: decision = %+v, want outcome %q in the note", c.outcome, last, c.outcome)
			}

			if c.outcome != "warn" {
				time.Sleep(100 * time.Millisecond) // let the dispatcher drain
				if evs := col.snapshot(); len(evs) != 0 {
					t.Errorf("%s emitted %d guardrail_warn event(s), want none: %v", c.outcome, len(evs), evs)
				}
				return
			}

			waitFor(t, "guardrail_warn delivery", func() bool { return len(col.snapshot()) == 1 })
			evs := col.snapshot()
			if len(evs) != 1 {
				t.Fatalf("warn emitted %d event(s), want exactly 1: %v", len(evs), evs)
			}
			ev := evs[0]
			if ev.Kind != KindGuardrailWarn {
				t.Errorf("kind = %q, want %q", ev.Kind, KindGuardrailWarn)
			}
			if ev.PRDID != prd.ID {
				t.Errorf("PRDID = %q, want %q", ev.PRDID, prd.ID)
			}
			if ev.Level != c.wantLevel {
				t.Errorf("level = %q, want %q", ev.Level, c.wantLevel)
			}
			if (ev.TaskID != "") != c.wantTask {
				t.Errorf("TaskID = %q, wantTask set = %v", ev.TaskID, c.wantTask)
			}
			if (ev.StoryID != "") != c.wantStory {
				t.Errorf("StoryID = %q, wantStory set = %v", ev.StoryID, c.wantStory)
			}
			if ev.Guardrail != "rules" {
				t.Errorf("guardrail = %q, want \"rules\"", ev.Guardrail)
			}
			// The verdict is deep-copied into the event...
			if ev.Verdict == nil || ev.Verdict.Outcome != "warn" ||
				ev.Verdict.Guardrail != "rules" || !reflect.DeepEqual(ev.Verdict.Issues, warningVerdict("warn").Issues) {
				t.Errorf("verdict in event = %+v, want the copied warn verdict", ev.Verdict)
			}
			// ...and the same code path's Decision rides along too.
			if ev.Decision == nil || ev.Decision.Kind != "guardrail_verdict" || ev.Decision.VerdictOutcome != "warn" {
				t.Errorf("decision in event = %+v, want the guardrail_verdict Decision", ev.Decision)
			}
		})
	}
}
