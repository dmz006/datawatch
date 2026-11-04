package server

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

// fixedHealthClock is the server clock the node-health drain tests run
// against.
var fixedHealthClock = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

// newHealthDrainServer builds a Server with a registry holding one node
// ("gpu-m", mutated by mutate) and pins the server clock to fixedHealthClock.
func newHealthDrainServer(t *testing.T, mutate func(n *compute.Node)) (*Server, *compute.Registry) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	n := &compute.Node{
		Name:    "gpu-m",
		Kind:    compute.KindRemote,
		Address: "https://gpu-m:11434",
	}
	if mutate != nil {
		mutate(n)
	}
	if err := reg.Add(n); err != nil {
		t.Fatalf("Add node: %v", err)
	}
	prev := serverNow
	serverNow = func() time.Time { return fixedHealthClock }
	t.Cleanup(func() { serverNow = prev })
	return &Server{computeReg: reg}, reg
}

// healthDrainBody returns the decoded JSON object of a GET health for name.
func healthDrainBody(t *testing.T, srv *Server, name string) map[string]any {
	t.Helper()
	code, out := drainRequest(t, srv, http.MethodGet, "/api/compute/nodes/"+name+"/health", "")
	if code != http.StatusOK {
		t.Fatalf("health of %s: got %d, want 200", name, code)
	}
	return out
}

func TestNodeHealthDrainFields(t *testing.T) {
	// The node holds one admission lease, so the health view reports it.
	led := capacity.New(capacity.Options{})
	led.SetLimit("node:gpu-m", 2)
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "task-a", PRDID: "prd-a", Pools: []string{"node:gpu-m"}, Node: "gpu-m",
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	srv, _ := newHealthDrainServer(t, nil)
	srv.capacityLedger = led

	code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-m/drain",
		`{"until":"`+fixedHealthClock.Add(time.Hour).Format(time.RFC3339)+`","reason":"maintenance"}`)
	if code != http.StatusOK {
		t.Fatalf("drain: got %d, want 200", code)
	}

	out := healthDrainBody(t, srv, "gpu-m")
	if out["drained"] != true || out["drain_active"] != true {
		t.Fatalf("drain state = drained=%v active=%v: %v", out["drained"], out["drain_active"], out)
	}
	if out["drain_until"] != fixedHealthClock.Add(time.Hour).Format(time.RFC3339) {
		t.Fatalf("drain_until = %v: %v", out["drain_until"], out)
	}
	if out["drain_reason"] != "maintenance" {
		t.Fatalf("drain_reason = %v", out["drain_reason"])
	}
	if out["held_tasks"] != float64(1) {
		t.Fatalf("held_tasks = %v, want 1 (the node's lease)", out["held_tasks"])
	}
	// The fields the view already carried are unchanged.
	if out["in_maintenance"] != false || out["name"] != "gpu-m" {
		t.Fatalf("existing fields changed: %v", out)
	}
}

func TestNodeHealthHeldTasksWithoutLedger(t *testing.T) {
	// A server with no admission ledger wired reports held_tasks 0.
	srv, _ := newHealthDrainServer(t, nil)
	out := healthDrainBody(t, srv, "gpu-m")
	if out["held_tasks"] != float64(0) {
		t.Fatalf("held_tasks = %v, want 0 with no ledger", out["held_tasks"])
	}
	if denied := out["denied_consumers"].([]any); len(denied) != 0 {
		t.Fatalf("denied_consumers = %v, want an empty list", out["denied_consumers"])
	}
	if windows := out["maintenance_windows_active"].([]any); len(windows) != 0 {
		t.Fatalf("maintenance_windows_active = %v, want an empty list", out["maintenance_windows_active"])
	}
}

func TestNodeHealthExpiredDrain(t *testing.T) {
	srv, _ := newHealthDrainServer(t, nil)
	until := fixedHealthClock.Add(time.Hour)
	if code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-m/drain",
		`{"until":"`+until.Format(time.RFC3339)+`","reason":"patching"}`); code != http.StatusOK {
		t.Fatalf("drain: got %d, want 200", code)
	}
	// The drain's until passes: the stored drift is still there, but the
	// drain is no longer active and the remaining fields read as not drained
	// at the server's now.
	serverNow = func() time.Time { return fixedHealthClock.Add(2 * time.Hour) }

	out := healthDrainBody(t, srv, "gpu-m")
	if out["drained"] != true {
		t.Fatalf("the stored drain is still on the node: drained = %v", out["drained"])
	}
	if out["drain_active"] != false {
		t.Fatalf("an expired drain must not be active: %v", out)
	}
	if out["drain_until"] != until.Format(time.RFC3339) {
		t.Fatalf("drain_until = %v", out["drain_until"])
	}
	if out["drain_reason"] != "patching" {
		t.Fatalf("drain_reason = %v", out["drain_reason"])
	}
}

func TestNodeHealthDrainAndMaintenanceWindow(t *testing.T) {
	srv, _ := newHealthDrainServer(t, func(n *compute.Node) {
		n.MaintenanceWindows = []compute.MaintenanceWindow{
			// Active: covers fixedHealthClock.
			{From: fixedHealthClock.Add(-time.Hour), To: fixedHealthClock.Add(45 * time.Minute), Reason: "kernel patch"},
			// Past: fully before now, must not appear.
			{From: fixedHealthClock.Add(-5 * time.Hour), To: fixedHealthClock.Add(-4 * time.Hour), Reason: "done"},
			// Future: starts after now, must not appear.
			{From: fixedHealthClock.Add(2 * time.Hour), Reason: "later"},
			// Open-ended, from in the past: active with no to.
			{From: fixedHealthClock.Add(-time.Minute), Reason: "unclosed"},
		}
	})

	// Drain alongside the window; the two states are reported side by side.
	if code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-m/drain",
		`{"reason":"rolling restart"}`); code != http.StatusOK {
		t.Fatalf("drain: got %d, want 200", code)
	}

	out := healthDrainBody(t, srv, "gpu-m")
	if out["drained"] != true || out["drain_active"] != true {
		t.Fatalf("drain state = %v / %v", out["drained"], out["drain_active"])
	}
	if out["drain_reason"] != "rolling restart" {
		t.Fatalf("drain_reason = %v", out["drain_reason"])
	}
	if out["in_maintenance"] != true {
		t.Fatalf("in_maintenance = %v, want true (an active window covers now)", out["in_maintenance"])
	}

	active, ok := out["maintenance_windows_active"].([]any)
	if !ok || len(active) != 2 {
		t.Fatalf("maintenance_windows_active = %v, want the two active windows", out["maintenance_windows_active"])
	}
	wantFrom := map[string]bool{}
	wantTo := map[string]bool{}
	for _, e := range active {
		w, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("window entry %v is not an object", e)
		}
		from, _ := w["from"].(string)
		wantFrom[from] = true
		if to, ok := w["to"].(string); ok {
			wantTo[to] = true
		}
		if _, ok := w["reason"]; !ok {
			t.Fatalf("window entry %v lacks reason", w)
		}
	}
	// The active finite window: from+to+reason.
	if !wantFrom[fixedHealthClock.Add(-time.Hour).UTC().Format(time.RFC3339)] {
		t.Fatalf("active window from missing: %v", out["maintenance_windows_active"])
	}
	if !wantTo[fixedHealthClock.Add(45*time.Minute).UTC().Format(time.RFC3339)] {
		t.Fatalf("active window to missing: %v", out["maintenance_windows_active"])
	}
	// The open-ended window carries from and reason but no to.
	open, found := false, false
	for _, e := range active {
		w, _ := e.(map[string]any)
		if w != nil && w["from"] == fixedHealthClock.Add(-time.Minute).UTC().Format(time.RFC3339) {
			found = true
			if _, has := w["to"]; has {
				t.Fatalf("open-ended window must not carry to: %v", w)
			}
			if w["reason"] != "unclosed" {
				t.Fatalf("open-ended window reason = %v", w["reason"])
			}
			open = true
		}
	}
	if !found || !open {
		t.Fatalf("open-ended active window missing: %v", out["maintenance_windows_active"])
	}
	// The past and future windows must not appear.
	past := fixedHealthClock.Add(-5 * time.Hour).UTC().Format(time.RFC3339)
	future := fixedHealthClock.Add(2 * time.Hour).UTC().Format(time.RFC3339)
	for _, e := range active {
		w, _ := e.(map[string]any)
		if w == nil {
			continue
		}
		if w["from"] == past || w["from"] == future {
			t.Fatalf("inactive window leaked into active list: %v", w)
		}
	}
}

func TestNodeHealthDrainDenialList(t *testing.T) {
	srv, _ := newHealthDrainServer(t, func(n *compute.Node) {
		n.Permissions = compute.Permissions{DeniedConsumers: []string{"council", "ask"}}
	})
	if code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-m/drain",
		`{"reason":"security review"}`); code != http.StatusOK {
		t.Fatalf("drain: got %d, want 200", code)
	}

	out := healthDrainBody(t, srv, "gpu-m")
	if out["drained"] != true {
		t.Fatalf("drained = %v", out["drained"])
	}
	denied, ok := out["denied_consumers"].([]any)
	if !ok {
		t.Fatalf("denied_consumers is not a list: %v", out["denied_consumers"])
	}
	want := []string{"council", "ask"}
	if len(denied) != len(want) {
		t.Fatalf("denied_consumers = %v, want %v", denied, want)
	}
	for i := range want {
		if denied[i] != want[i] {
			t.Fatalf("denied_consumers = %v, want %v", denied, want)
		}
	}
}
