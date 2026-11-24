package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestSupervisorWatchPassEmitsStallWakeAndCarriesBlocker — the production
// pass wakes once per blocker on the lifecycle bus and the finding carries
// the blocker text and action, serialized for the read route.
func TestSupervisorWatchPassEmitsStallWakeAndCarriesBlocker(t *testing.T) {
	m := swNewManager(t)
	p := &PRD{ID: "held-1", Title: "held-1", Spec: "HOLD until later", Status: PRDApproved,
		Priority: 90, CreatedAt: time.Now().Add(-45 * time.Minute)}
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got := make(chan LifecycleEvent, 4)
	cancel := m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) {
		if ev.Kind == KindPriorityStall {
			got <- ev
		}
	})
	defer cancel()

	cfg := SupervisorWatchConfig{}
	prds := m.supervisorWatchPRDs()
	raw := m.supervisorWatchRawFindings(time.Now(), cfg, prds)
	m.supervisorWatchRawFindings(time.Now(), cfg, prds) // same blocker: no second wake

	var f *SupervisorWatchFinding
	for i := range raw {
		if raw[i].Kind == SupervisorWatchPriorityStall {
			f = &raw[i]
		}
	}
	if f == nil || f.BlockerText == "" || f.Action == "" || !strings.Contains(f.Text, f.Action) {
		t.Fatalf("finding lacks blocker/action: %+v", f)
	}
	b, _ := json.Marshal(f)
	if !strings.Contains(string(b), `"blocker_text"`) || !strings.Contains(string(b), `"action"`) {
		t.Errorf("read route JSON lacks blocker fields: %s", b)
	}
	select {
	case ev := <-got:
		var st PriorityStall
		if err := json.Unmarshal([]byte(ev.Details), &st); err != nil || st.AutomatonID != "held-1" ||
			st.Priority != 90 || st.BlockerCode != StallBlockerHeld || st.BlockerText != f.BlockerText || st.Action != f.Action {
			t.Fatalf("wake payload %+v err %v", st, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no priority_stall event reached the bus")
	}
	select {
	case ev := <-got:
		t.Fatalf("second wake for the same blocker: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestSupervisorWatchPassStallWakeOff — stall_wake off emits nothing.
func TestSupervisorWatchPassStallWakeOff(t *testing.T) {
	m := swNewManager(t)
	p := &PRD{ID: "held-2", Title: "held-2", Spec: "HOLD", Status: PRDApproved,
		Priority: 90, CreatedAt: time.Now().Add(-45 * time.Minute)}
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got := make(chan LifecycleEvent, 1)
	cancel := m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) {
		if ev.Kind == KindPriorityStall {
			got <- ev
		}
	})
	defer cancel()
	off := false
	m.supervisorWatchRawFindings(time.Now(), SupervisorWatchConfig{StallWake: &off}, m.supervisorWatchPRDs())
	select {
	case ev := <-got:
		t.Fatalf("wake while off: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}
