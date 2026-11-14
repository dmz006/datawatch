package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
)

type fallthroughAuditSink struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (s *fallthroughAuditSink) add(e audit.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
}

func (s *fallthroughAuditSink) fallThroughs() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []audit.Entry
	for _, e := range s.entries {
		if ft, _ := e.Details["fall_through"].(bool); ft {
			out = append(out, e)
		}
	}
	return out
}

// fallthroughFixture builds a manager with llm:a and llm:b pools of one slot
// each, llm:a already held by another holder, a fake alternates resolver
// (a -> b) and a fixed audit clock.
func fallthroughFixture(t *testing.T, enabled bool) (*Manager, *Task, *PRD, *fallthroughAuditSink, *capacity.Ledger) {
	t.Helper()
	m, task, prd, sink, led, _ := fallthroughFixtureClock(t, enabled)
	return m, task, prd, sink, led
}

// fallthroughFixtureClock is fallthroughFixture that also returns the step
// clock the ledger runs on, for tests that drive wait passes by hand.
func fallthroughFixtureClock(t *testing.T, enabled bool) (*Manager, *Task, *PRD, *fallthroughAuditSink, *capacity.Ledger, *stepClock) {
	t.Helper()
	m, _, clk, led := backoffFixture(t)
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
	m.cfg.CapacityFallthrough = enabled
	m.cfg.CapacityWaitTimeoutSeconds = 60
	m.mu.Unlock()
	sink := &fallthroughAuditSink{}
	fixed := clk.Now()
	rec := NewCapacityAudit(sink.add)
	rec.Clock = func() time.Time { return fixed }
	m.SetCapacityAudit(rec)

	// Occupy the requested pool.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other", Pools: []string{"llm:a"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy llm:a: %v", err)
	}
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	return m, &task, cur, sink, led, clk
}

func TestPoolFallthrough_AdmitsOnIdleAlternateAndAudits(t *testing.T) {
	m, task, prd, sink, led := fallthroughFixture(t, true)
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	alt, ok := m.fallthroughOf(task.ID)
	if !ok || alt.Backend != "b" || alt.Model != "m2" {
		t.Fatalf("want fall-through to b/m2, got %+v ok=%v", alt, ok)
	}
	if !led.Held(task.ID) {
		t.Fatal("task should hold a lease on the alternate pool")
	}
	got := sink.fallThroughs()
	if len(got) != 1 {
		t.Fatalf("want 1 fall-through audit entry, got %d", len(got))
	}
	d := got[0].Details
	if from, _ := d["from_pools"].([]string); len(from) != 1 || from[0] != "llm:a" {
		t.Fatalf("from_pools = %v", d["from_pools"])
	}
	if to, _ := d["to_pools"].([]string); len(to) != 1 || to[0] != "llm:b" {
		t.Fatalf("to_pools = %v", d["to_pools"])
	}
	if note, _ := d["note"].(string); !strings.Contains(note, "fell through from a/m1 to b/m2") {
		t.Fatalf("note = %q", note)
	}
	if got[0].Action != audit.ActionCapacityAdmitted {
		t.Fatalf("action = %q", got[0].Action)
	}
}

func TestPoolFallthrough_OffNeverFallsThrough(t *testing.T) {
	m, task, prd, sink, _ := fallthroughFixture(t, false)
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 1
	m.mu.Unlock()
	task.Backend = "a" // explicitly requested backend
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := m.admit(ctx, prd, task, "a", "m1", capacity.RoleWorker)
	if err == nil {
		t.Fatal("admit must keep waiting on the full pool, got success")
	}
	if _, ok := m.fallthroughOf(task.ID); ok {
		t.Fatal("no fall-through when the setting is off")
	}
	if n := len(sink.fallThroughs()); n != 0 {
		t.Fatalf("no fall-through audit expected, got %d", n)
	}
}

func TestPoolFallthrough_RequestedPoolIdleStaysPut(t *testing.T) {
	m, task, prd, sink, led := fallthroughFixture(t, true)
	led.Release("other") // requested pool is idle again
	if err := m.admit(context.Background(), prd, task, "a", "m1", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, ok := m.fallthroughOf(task.ID); ok {
		t.Fatal("must not fall through when the requested pool has room")
	}
	if n := len(sink.fallThroughs()); n != 0 {
		t.Fatalf("no fall-through audit expected, got %d", n)
	}
}

func TestPoolFallthrough_AlternateFullKeepsWaiting(t *testing.T) {
	m, task, prd, _, led := fallthroughFixture(t, true)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other-b", Pools: []string{"llm:b"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy llm:b: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.admit(ctx, prd, task, "a", "m1", capacity.RoleWorker); err == nil {
		t.Fatal("both pools full: admit must wait, got success")
	}
	if _, ok := m.fallthroughOf(task.ID); ok {
		t.Fatal("no fall-through when the alternate is also full")
	}
}

func TestPoolFallthrough_NoResolverIsInert(t *testing.T) {
	m, task, prd, _, _ := fallthroughFixture(t, true)
	m.SetCapacityAlternates(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.admit(ctx, prd, task, "a", "m1", capacity.RoleWorker); err == nil {
		t.Fatal("no alternates wired: admit must wait")
	}
}

// A verifier, judge or planner request never falls through, even when the
// setting is on, an alternate is idle and the requested pool is full.
func TestPoolFallthrough_NonWorkerRolesNeverFallThrough(t *testing.T) {
	for _, role := range []capacity.Role{capacity.RoleVerifier, capacity.RolePlanner} {
		m, task, prd, sink, _ := fallthroughFixture(t, true)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := m.admit(ctx, prd, task, "a", "m1", role)
		cancel()
		if err == nil {
			t.Fatalf("role %s: admit must keep waiting on the full pool, got success", role)
		}
		if _, ok := m.fallthroughOf(task.ID); ok {
			t.Fatalf("role %s: must not fall through", role)
		}
		if n := len(sink.fallThroughs()); n != 0 {
			t.Fatalf("role %s: no fall-through audit expected, got %d", role, n)
		}
		// The picker itself refuses a non-worker request.
		base := capacity.Request{Holder: task.ID, Pools: []string{"llm:a"}, Role: role}
		if alt, _ := m.pickFallthrough(m.Capacity(), base, "a", "m1"); alt != nil {
			t.Fatalf("role %s: pickFallthrough returned %+v", role, alt)
		}
	}
}
