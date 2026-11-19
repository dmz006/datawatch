package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

type fakeSlotLedger struct{ leases []capacity.Lease }

func (f *fakeSlotLedger) Leases() []capacity.Lease { return append([]capacity.Lease(nil), f.leases...) }
func (f *fakeSlotLedger) ReleaseIf(holder string, acquired time.Time) bool {
	for i, l := range f.leases {
		if l.Holder == holder && l.Acquired.Equal(acquired) {
			f.leases = append(f.leases[:i], f.leases[i+1:]...)
			return true
		}
	}
	return false
}

func TestSlotReconcileFreesOrphanAndKeepsLiveSession(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	led := &fakeSlotLedger{leases: []capacity.Lease{
		{Holder: "judge:orphan1", Pools: []string{autonomous.PiPromptPool}, Acquired: old},
		{Holder: "verify:live1", Pools: []string{autonomous.PiPromptPool}, Acquired: old, SessionID: "s-live"},
	}}
	var alerts, logs []string
	counted := 0
	freed := runSlotReconcile(slotReconcileDeps{
		Ledger:     led,
		Sessions:   sessionLiveFn(func(id string) bool { return id == "s-live" }),
		Nodes:      ledgerNodeLease{now: time.Now},
		Done:       automatonFinishedFn(func(string) bool { return false }),
		HolderName: func(h string) string { return "Judging rules: Ship the widget (orphan1)" },
		Alert:      func(title, body string) { alerts = append(alerts, title+": "+body) },
		Logf:       func(f string, a ...any) { logs = append(logs, f) },
		Count:      func() { counted++ },
	})
	if len(freed) != 1 || freed[0].Holder != "judge:orphan1" {
		t.Fatalf("freed = %+v, want only the orphan", freed)
	}
	if len(led.leases) != 1 || led.leases[0].Holder != "verify:live1" {
		t.Fatalf("live-session slot must remain, got %+v", led.leases)
	}
	if len(alerts) != 1 || len(logs) != 1 || counted != 1 {
		t.Fatalf("alerts=%d logs=%d counted=%d, want 1 each", len(alerts), len(logs), counted)
	}
	for _, want := range []string{"Ship the widget", "orphan1", autonomous.PiPromptPool, autonomous.SlotReasonNoSessionNoLease} {
		if !strings.Contains(alerts[0], want) {
			t.Errorf("alert %q missing %q", alerts[0], want)
		}
	}
	if again := runSlotReconcile(slotReconcileDeps{Ledger: led, Sessions: sessionLiveFn(func(id string) bool { return id == "s-live" }), Nodes: ledgerNodeLease{now: time.Now}}); len(again) != 0 {
		t.Fatalf("second pass freed %+v", again)
	}
}
