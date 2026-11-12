package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// The gate is the ledger's NodeGate: every reason it can hand a wait reason
// must name the state (drained, in maintenance, consumer denied) so an
// operator can read the wait reason and act on it.
func TestDrainGateReasons(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	v := newNodeHealthView()
	v.now = func() time.Time { return now }
	v.refresh([]*compute.Node{
		{Name: "open"},
		{Name: "drained", Drained: true},
		{Name: "drained-expired", Drained: true, DrainUntil: now.Add(-time.Minute)},
		{Name: "maint", MaintenanceWindows: []compute.MaintenanceWindow{{From: now.Add(-time.Hour), To: now.Add(time.Hour)}}},
		// Drain beats maintenance: the reason must name the drain, not the window.
		{Name: "both", Drained: true, MaintenanceWindows: []compute.MaintenanceWindow{{From: now.Add(-time.Hour), To: now.Add(time.Hour)}}},
		// RBAC matrix straight from compute.Permissions semantics.
		{Name: "deny-spa", Permissions: compute.Permissions{DeniedConsumers: []string{"session_spawn"}}},
		{Name: "allow-council", Permissions: compute.Permissions{AllowedConsumers: []string{"council"}}},
		{Name: "wildcard-deny", Permissions: compute.Permissions{AllowedConsumers: []string{"*"}, DeniedConsumers: []string{"session_spawn"}}},
	})
	cases := []struct {
		node, consumer, wantReason string
		ok                         bool
	}{
		{node: "open", consumer: "session_spawn", ok: true},
		{node: "open", consumer: "", ok: true},
		{node: "drained", consumer: "session_spawn", wantReason: "drained"},
		{node: "drained", consumer: "ask", wantReason: "drained"},
		{node: "drained-expired", consumer: "session_spawn", ok: true},
		{node: "maint", consumer: "session_spawn", wantReason: "in maintenance"},
		{node: "both", consumer: "session_spawn", wantReason: "drained"},
		{node: "deny-spa", consumer: "session_spawn", wantReason: "consumer session_spawn denied"},
		{node: "deny-spa", consumer: "council", ok: true},
		{node: "allow-council", consumer: "session_spawn", wantReason: "consumer session_spawn denied"},
		{node: "allow-council", consumer: "council", ok: true},
		{node: "wildcard-deny", consumer: "session_spawn", wantReason: "consumer session_spawn denied"},
		{node: "wildcard-deny", consumer: "ask", ok: true},
		{node: "ghost", consumer: "ask", wantReason: "not in the compute registry"},
	}
	for _, c := range cases {
		t.Run(c.node+"/"+c.consumer, func(t *testing.T) {
			got, reason := v.gate(c.node, c.consumer)
			if got != c.ok {
				t.Fatalf("gate(%q, %q) = %v%s, want ok=%v", c.node, c.consumer, got, " ("+reason+")", c.ok)
			}
			if c.ok {
				if reason != "" {
					t.Errorf("gate(%q, %q) allowed with reason %q", c.node, c.consumer, reason)
				}
				return
			}
			if !strings.Contains(reason, c.wantReason) {
				t.Errorf("gate(%q, %q) reason = %q, want it to name %q", c.node, c.consumer, reason, c.wantReason)
			}
		})
	}

	// A drain set after the refresh is denied at call time (the copied record
	// carries the drain fields; no second tick is needed).
	v.mu.Lock()
	v.nodes["open"].Drained = true
	v.mu.Unlock()
	if ok, reason := v.gate("open", "session_spawn"); ok {
		t.Fatalf("a drain set after the refresh must be denied, got ok (%q)", reason)
	}
}

// The loaded-not-yet and strict rules of healthy() apply to the gate as well:
// non-strict opens every name, strict denies until the registry has been read.
func TestDrainGateNotYetLoaded(t *testing.T) {
	v := newNodeHealthView()
	v.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	if ok, _ := v.gate("anything", "session_spawn"); !ok {
		t.Fatal("a non-strict view must open every node before the registry has been read")
	}
	s := newStrictNodeHealthView()
	s.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	if ok, reason := s.gate("anything", "session_spawn"); ok {
		t.Fatalf("a strict view must not grant a node before the registry has been read (%q)", reason)
	} else if !strings.Contains(reason, "not loaded") {
		t.Errorf("strict pre-load reason = %q, want it to name the not-yet-loaded registry", reason)
	}
}
