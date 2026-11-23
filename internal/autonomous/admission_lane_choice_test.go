package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// admissionLaneRig builds a manager whose ledger has one slot on llm:a and one
// on llm:b, a fake alternates resolver (a/m1 -> b/m2) and the fall-through
// setting as asked. llm:a is already held by another holder, so a worker whose
// effective backend is "a" finds its requested pool full.
func admissionLaneRig(t *testing.T, fallthroughOn bool) (*Manager, *capacity.Ledger, *PRD, *Task) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond, Clock: newFakeCapClock()})
	led.SetLimit("llm:a", 1)
	led.SetLimit("llm:b", 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"llm:" + backend}, ""
	})
	m.SetCapacityAlternates(func(backend, model string) []CapacityAlternate {
		if backend == "a" {
			return []CapacityAlternate{{Backend: "b", Model: "m2"}}
		}
		return nil
	})
	m.mu.Lock()
	m.cfg.CapacityFallthrough = fallthroughOn
	m.cfg.CapacityWaitTimeoutSeconds = 60
	m.mu.Unlock()
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other", Pools: []string{"llm:a"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy llm:a: %v", err)
	}
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := &cur.Story[0].Tasks[0]
	return m, led, cur, task
}

// laneDecisions returns the capacity_fallthrough entries on the Automaton's
// decision log.
func laneDecisions(t *testing.T, m *Manager, prdID string) []Decision {
	t.Helper()
	cur, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %s missing", prdID)
	}
	var out []Decision
	m.Store().WithLock(func() {
		for _, d := range cur.Decisions {
			if d.Kind == admissionLaneDecisionKind {
				out = append(out, d)
			}
		}
	})
	return out
}

func TestAdmissionLane_RequestedFullAlternateFreeAdmitsOnAlternateAndLogs(t *testing.T) {
	m, led, prd, task := admissionLaneRig(t, true)
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	alt, ok := m.fallthroughOf(task.ID)
	if !ok || alt.Backend != "b" || alt.Model != "m2" {
		t.Fatalf("want admission on b/m2, got %+v ok=%v", alt, ok)
	}
	ls := led.LeaseOf(task.ID)
	if ls == nil || len(ls.Pools) != 1 || ls.Pools[0] != "llm:b" {
		t.Fatalf("lease = %+v, want pool llm:b only", ls)
	}
	if got := nodeHeld(led, "llm:a"); got != 1 {
		t.Fatalf("llm:a held = %d, the requested pool must be left to its own holder", got)
	}

	got := laneDecisions(t, m, prd.ID)
	if len(got) != 1 {
		t.Fatalf("want 1 decision-log entry, got %d: %+v", len(got), got)
	}
	d := got[0]
	if d.Backend != "b" || d.Model != "m2" || d.Actor != "autonomous" {
		t.Fatalf("decision = %+v, want backend b model m2 actor autonomous", d)
	}
	for _, want := range []string{task.ID, "b/m2", "pool llm:b", "a/m1", "pool llm:a", "requested pool full, alternate pool free"} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("note %q missing %q", d.Note, want)
		}
	}
}

func TestAdmissionLane_CapacityPageReportsPoolRequestSitsOn(t *testing.T) {
	m, led, prd, task := admissionLaneRig(t, true)
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	hold := m.runningHoldView(visPRD{id: prd.ID, title: "S"}, &visTask{id: task.ID, title: "T0", updatedAt: time.Now()}, led)
	if len(hold.Pools) != 1 || hold.Pools[0] != "llm:b" {
		t.Fatalf("running view pools = %v, want [llm:b] (the pool actually held)", hold.Pools)
	}
	body, err := json.Marshal(map[string]any{
		"pools": []capacity.PoolStatus{},
		"tasks": map[string]any{"running": []TaskHoldView{hold}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text, err := FormatCapacityText(body)
	if err != nil {
		t.Fatalf("FormatCapacityText: %v", err)
	}
	if !strings.Contains(text, "llm:b") || strings.Contains(text, "llm:a") {
		t.Fatalf("capacity text must name the pool held (llm:b) and not the requested one:\n%s", text)
	}
}

func TestAdmissionLane_RequestedPoolFreeStaysOnItWithNoNote(t *testing.T) {
	m, led, prd, task := admissionLaneRig(t, true)
	led.Release("other") // the requested pool has room
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, ok := m.fallthroughOf(task.ID); ok {
		t.Fatal("must stay on the requested pool when it has room")
	}
	ls := led.LeaseOf(task.ID)
	if ls == nil || len(ls.Pools) != 1 || ls.Pools[0] != "llm:a" {
		t.Fatalf("lease = %+v, want pool llm:a", ls)
	}
	if got := laneDecisions(t, m, prd.ID); len(got) != 0 {
		t.Fatalf("no fall-through note expected, got %+v", got)
	}
}

func TestAdmissionLane_FallthroughOffKeepsWaiting(t *testing.T) {
	m, led, prd, task := admissionLaneRig(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.admit(ctx, prd, task, "a", "m1", capacity.RoleWorker); err == nil {
		t.Fatal("alternate free but fall-through off: admit must keep waiting, got success")
	}
	if _, ok := m.fallthroughOf(task.ID); ok {
		t.Fatal("no fall-through when the setting is off")
	}
	if led.Held(task.ID) {
		t.Fatal("the waiting task must hold no lease")
	}
	if got := nodeHeld(led, "llm:b"); got != 0 {
		t.Fatalf("llm:b held = %d, the alternate must stay free", got)
	}
	if got := laneDecisions(t, m, prd.ID); len(got) != 0 {
		t.Fatalf("no decision-log entry expected while waiting, got %+v", got)
	}
}

// Both pools full: the task waits and leaves no note, even with the setting on.
func TestAdmissionLane_AlternateFullKeepsWaitingWithNoNote(t *testing.T) {
	m, led, prd, task := admissionLaneRig(t, true)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other-b", Pools: []string{"llm:b"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy llm:b: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.admit(ctx, prd, task, "a", "m1", capacity.RoleWorker); err == nil {
		t.Fatal("both pools full: admit must wait, got success")
	}
	if got := laneDecisions(t, m, prd.ID); len(got) != 0 {
		t.Fatalf("no decision-log entry expected while waiting, got %+v", got)
	}
}

// The lane follows the backend the caller resolved at the moment of admission:
// the same task asked for backend "b" (an uncontended pool) is admitted there
// with no fall-through, and the next admission, on "a" with its pool full,
// falls through. The two admissions are independent.
func TestAdmissionLane_LaneFollowsBackendResolvedAtAdmission(t *testing.T) {
	m, led, prd, task := admissionLaneRig(t, true)
	if err := m.admit(context.Background(), prd, task, "b", "m2", capacity.RoleWorker); err != nil {
		t.Fatalf("admit on b: %v", err)
	}
	if _, ok := m.fallthroughOf(task.ID); ok {
		t.Fatal("a task whose effective backend pool is free must not fall through")
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Pools[0] != "llm:b" {
		t.Fatalf("lease = %+v, want llm:b", ls)
	}
	led.Release(task.ID)
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit on a: %v", err)
	}
	if alt, ok := m.fallthroughOf(task.ID); !ok || alt.Backend != "b" {
		t.Fatalf("second admission: want fall-through to b, got %+v ok=%v", alt, ok)
	}
	if got := laneDecisions(t, m, prd.ID); len(got) != 1 {
		t.Fatalf("want exactly the second admission logged, got %d entries", len(got))
	}
}
