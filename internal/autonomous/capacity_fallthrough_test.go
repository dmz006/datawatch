package autonomous

// Capacity fall-through on the fake ledger: a worker whose requested pool is
// full is admitted on the next idle alternate pool when
// autonomous.capacity_fallthrough is on, a task that is already waiting
// re-reads the setting on every wait pass, and the audit entry records where
// it came from, where it went and why. Every test runs on the manual step
// clock, so nothing depends on wall time. The fixtures are the ones in
// pool_fallthrough_test.go (llm:a held by "other", llm:b idle, a -> b).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
)

// ftRunAdmit starts m.admit in a goroutine and returns the channel its error
// arrives on, so a test can watch the task wait.
func ftRunAdmit(m *Manager, prd *PRD, task *Task, role capacity.Role) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- m.admit(context.Background(), prd, task, "a", "m1", role)
	}()
	return done
}

// ftPass lets the ledger's wait loop run n poll passes: it waits until the
// loop has registered its poll timer, fires it, and waits until the pass has
// run and the next timer is registered. (Two timers are pending while a
// worker waits: the wait deadline and the next poll tick.)
func ftPass(t *testing.T, clk *stepClock, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		clk.waitTimers(t, 2)
		clk.Advance(time.Second)
	}
	clk.waitTimers(t, 2)
}

// ftNotDone fails when admit has already returned.
func ftNotDone(t *testing.T, done <-chan error, why string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s: admit returned (err=%v), want it still waiting", why, err)
	default:
	}
}

// ftAwait returns admit's result, failing the test when it never returns.
func ftAwait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("admit did not return")
		return nil
	}
}

// ftSetFallthrough turns the setting on or off through the live config entry
// point, as the config API does.
func ftSetFallthrough(m *Manager, on bool) {
	cfg := m.Config()
	cfg.CapacityFallthrough = on
	m.SetConfig(cfg)
}

// ftClearExplicit removes every level that could make the task's backend
// explicit, leaving only the global default.
func ftClearExplicit(prd *PRD, task *Task) {
	prd.Backend, task.Backend = "", ""
	for i := range prd.Story {
		prd.Story[i].Backend = ""
	}
}

// ftAdmittedEntries returns the admitted audit entries recorded for the task.
func ftAdmittedEntries(sink *fallthroughAuditSink, taskID string) []audit.Entry {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var out []audit.Entry
	for _, e := range sink.entries {
		if e.Action == audit.ActionCapacityAdmitted && e.Subject == taskID {
			out = append(out, e)
		}
	}
	return out
}

func ftStrings(t *testing.T, d map[string]any, key string) []string {
	t.Helper()
	v, ok := d[key].([]string)
	if !ok {
		t.Fatalf("details[%q] = %#v, want []string", key, d[key])
	}
	return v
}

// The requested pool is full and the alternate is idle: the task is admitted
// on the alternate at once, and the audit entry carries fall_through,
// from_pools, to_pools and a note.
func TestCapacityFallthrough_AdmitsOnIdlePoolWithAuditDetails(t *testing.T) {
	m, task, prd, sink, led := fallthroughFixture(t, true)
	ftClearExplicit(prd, task)
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	ls := led.LeaseOf(task.ID)
	if ls == nil || len(ls.Pools) != 1 || ls.Pools[0] != "llm:b" {
		t.Fatalf("lease = %+v, want one lease on llm:b", ls)
	}
	ft := sink.fallThroughs()
	if len(ft) != 1 {
		t.Fatalf("fall-through entries = %d, want 1", len(ft))
	}
	e := ft[0]
	if e.Action != audit.ActionCapacityAdmitted || e.Subject != task.ID {
		t.Fatalf("entry = %s on %q, want %s on the task", e.Action, e.Subject, audit.ActionCapacityAdmitted)
	}
	if got := ftStrings(t, e.Details, "from_pools"); len(got) != 1 || got[0] != "llm:a" {
		t.Fatalf("from_pools = %v", got)
	}
	if got := ftStrings(t, e.Details, "to_pools"); len(got) != 1 || got[0] != "llm:b" {
		t.Fatalf("to_pools = %v", got)
	}
	note, _ := e.Details["note"].(string)
	if !strings.Contains(note, "fell through from a/m1 to b/m2") {
		t.Fatalf("note = %q", note)
	}
	if strings.Contains(note, "explicit backend") {
		t.Fatalf("a task with no explicit backend must not carry the explicit note: %q", note)
	}
	// Nothing waited, so there is no separate plain admitted entry.
	if n := len(ftAdmittedEntries(sink, task.ID)); n != 1 {
		t.Fatalf("admitted entries = %d, want only the fall-through one", n)
	}
}

// A full alternate is skipped: the task goes to the next alternate with room.
func TestCapacityFallthrough_FullAlternateIsSkippedForTheNext(t *testing.T) {
	m, task, prd, sink, led := fallthroughFixture(t, true)
	ftClearExplicit(prd, task)
	led.SetLimit("llm:c", 1)
	m.SetCapacityAlternates(func(backend, model string) []CapacityAlternate {
		return []CapacityAlternate{{Backend: "b", Model: "m2"}, {Backend: "c", Model: "m3"}}
	})
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other-b", Pools: []string{"llm:b"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy llm:b: %v", err)
	}
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	alt, ok := m.fallthroughOf(task.ID)
	if !ok || alt.Backend != "c" || alt.Model != "m3" {
		t.Fatalf("fell through to %+v (ok=%v), want c/m3", alt, ok)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Pools[0] != "llm:c" {
		t.Fatalf("lease = %+v, want llm:c", ls)
	}
	ft := sink.fallThroughs()
	if len(ft) != 1 {
		t.Fatalf("fall-through entries = %d, want 1", len(ft))
	}
	if got := ftStrings(t, ft[0].Details, "to_pools"); len(got) != 1 || got[0] != "llm:c" {
		t.Fatalf("to_pools = %v, want llm:c", got)
	}
	if note, _ := ft[0].Details["note"].(string); !strings.Contains(note, "to c/m3") {
		t.Fatalf("note = %q, want it to name c/m3", note)
	}
}

// A verifier, judge or planner request waits on its own pool: wait passes
// never move it, whatever the setting and however idle the alternate.
func TestCapacityFallthrough_VerifierWaitsAndNeverFallsThrough(t *testing.T) {
	for _, role := range []capacity.Role{capacity.RoleVerifier, capacity.RolePlanner} {
		m, task, prd, sink, led, step := fallthroughFixtureClock(t, true)
		done := ftRunAdmit(m, prd, task, role)
		ftPass(t, step, 3)
		ftNotDone(t, done, string(role)+" with the setting on")
		if _, ok := m.fallthroughOf(task.ID); ok {
			t.Fatalf("role %s: fell through while waiting", role)
		}
		led.Release("other")
		if err := ftAwait(t, done); err != nil {
			t.Fatalf("role %s: admit after the pool freed: %v", role, err)
		}
		if ls := led.LeaseOf(task.ID); ls == nil || ls.Pools[0] != "llm:a" {
			t.Fatalf("role %s: lease = %+v, want its own pool llm:a", role, ls)
		}
		if n := len(sink.fallThroughs()); n != 0 {
			t.Fatalf("role %s: fall-through entries = %d, want 0", role, n)
		}
	}
}

// A task that is already waiting falls through after the setting is turned on
// mid-wait: one admit call, no restart and no re-queue.
func TestCapacityFallthrough_WaitingTaskFallsThroughWhenSettingTurnedOnMidWait(t *testing.T) {
	m, task, prd, sink, led, step := fallthroughFixtureClock(t, false)
	ftClearExplicit(prd, task)
	done := ftRunAdmit(m, prd, task, capacity.RoleWorker)

	ftPass(t, step, 3)
	ftNotDone(t, done, "setting off")
	if _, ok := m.fallthroughOf(task.ID); ok || len(sink.fallThroughs()) != 0 {
		t.Fatal("must not fall through while the setting is off")
	}

	ftSetFallthrough(m, true)
	step.Advance(time.Second) // the next wait pass sees the setting
	if err := ftAwait(t, done); err != nil {
		t.Fatalf("admit after the setting was turned on: %v", err)
	}

	alt, ok := m.fallthroughOf(task.ID)
	if !ok || alt.Backend != "b" || alt.Model != "m2" {
		t.Fatalf("fell through to %+v (ok=%v), want b/m2", alt, ok)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Pools[0] != "llm:b" {
		t.Fatalf("lease = %+v, want llm:b", ls)
	}
	ft := sink.fallThroughs()
	if len(ft) != 1 {
		t.Fatalf("fall-through entries = %d, want 1", len(ft))
	}
	d := ft[0].Details
	if got := ftStrings(t, d, "from_pools"); len(got) != 1 || got[0] != "llm:a" {
		t.Fatalf("from_pools = %v, want the pool it was waiting on", got)
	}
	if got := ftStrings(t, d, "to_pools"); len(got) != 1 || got[0] != "llm:b" {
		t.Fatalf("to_pools = %v", got)
	}
	if note, _ := d["note"].(string); !strings.Contains(note, "fell through from a/m1 to b/m2") {
		t.Fatalf("note = %q", note)
	}
	// The task waited first, so the wait is also closed by a plain admitted
	// entry (waited_seconds) next to the fall-through one.
	if n := len(ftAdmittedEntries(sink, task.ID)); n != 2 {
		t.Fatalf("admitted entries = %d, want fall-through + waited", n)
	}
	// The held requested pool was never granted to this task.
	if !led.Held("other") {
		t.Fatal("the other holder must keep llm:a")
	}
}

// A waiting task whose alternate is full stays on its own wait when the
// setting is turned on, and is admitted on the requested pool when it frees.
func TestCapacityFallthrough_SettingOnWithFullAlternateKeepsWaiting(t *testing.T) {
	m, task, prd, sink, led, step := fallthroughFixtureClock(t, false)
	ftClearExplicit(prd, task)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other-b", Pools: []string{"llm:b"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy llm:b: %v", err)
	}
	done := ftRunAdmit(m, prd, task, capacity.RoleWorker)
	ftPass(t, step, 1)
	ftSetFallthrough(m, true)
	ftPass(t, step, 2)
	ftNotDone(t, done, "alternate full")
	led.Release("other")
	if err := ftAwait(t, done); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, ok := m.fallthroughOf(task.ID); ok || len(sink.fallThroughs()) != 0 {
		t.Fatal("no fall-through when the alternate was full")
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Pools[0] != "llm:a" {
		t.Fatalf("lease = %+v, want llm:a", ls)
	}
}

// An explicit-backend task waits with the setting off, and falls through with
// the setting on; the note says the backend was explicit.
func TestCapacityFallthrough_ExplicitBackendWaitsOffAndFallsThroughOn(t *testing.T) {
	m, task, prd, sink, _, step := fallthroughFixtureClock(t, false)
	task.Backend = "a" // set explicitly on the task
	done := ftRunAdmit(m, prd, task, capacity.RoleWorker)

	ftPass(t, step, 3)
	ftNotDone(t, done, "explicit backend, setting off")
	if _, ok := m.fallthroughOf(task.ID); ok || len(sink.fallThroughs()) != 0 {
		t.Fatal("an explicit-backend task must not fall through with the setting off")
	}

	ftSetFallthrough(m, true)
	step.Advance(time.Second)
	if err := ftAwait(t, done); err != nil {
		t.Fatalf("admit with the setting on: %v", err)
	}
	ft := sink.fallThroughs()
	if len(ft) != 1 {
		t.Fatalf("fall-through entries = %d, want 1", len(ft))
	}
	note, _ := ft[0].Details["note"].(string)
	if !strings.Contains(note, "explicit backend; fall-through setting on") {
		t.Fatalf("note = %q, want it to say the backend was explicit and the setting on", note)
	}
	if got := ftStrings(t, ft[0].Details, "to_pools"); len(got) != 1 || got[0] != "llm:b" {
		t.Fatalf("to_pools = %v", got)
	}
}

// An explicit backend set on the PRD (the default the fixture gives it) also
// reads as explicit when the task is admitted straight onto the alternate.
func TestCapacityFallthrough_ExplicitBackendNoteOnFirstPass(t *testing.T) {
	m, task, prd, sink, _ := fallthroughFixture(t, true)
	if prd.Backend == "" {
		t.Fatal("fixture PRD should carry an explicit backend")
	}
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	ft := sink.fallThroughs()
	if len(ft) != 1 {
		t.Fatalf("fall-through entries = %d, want 1", len(ft))
	}
	if note, _ := ft[0].Details["note"].(string); !strings.Contains(note, "explicit backend; fall-through setting on") {
		t.Fatalf("note = %q", note)
	}
}

// hasExplicitBackend reads task, story, PRD and Automaton levels, and nothing
// else.
func TestCapacityFallthrough_HasExplicitBackendLevels(t *testing.T) {
	m, task, prd, _, _ := fallthroughFixture(t, true)
	ftClearExplicit(prd, task)
	if m.hasExplicitBackend(prd, task) {
		t.Fatal("no level set: want not explicit")
	}

	task.Backend = "a"
	if !m.hasExplicitBackend(prd, task) {
		t.Fatal("task level: want explicit")
	}
	task.Backend = ""

	prd.Story[0].Backend = "a"
	if !m.hasExplicitBackend(prd, task) {
		t.Fatal("story level: want explicit")
	}
	prd.Story[0].Backend = ""

	prd.Backend = "a"
	if !m.hasExplicitBackend(prd, task) {
		t.Fatal("PRD level: want explicit")
	}
	prd.Backend = ""

	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "explicit backend",
		Backend: "a",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if m.AutomationForPRD(prd) == nil || !m.hasExplicitBackend(prd, task) {
		t.Fatalf("Automaton level (%s): want explicit", auto.ID)
	}
}
