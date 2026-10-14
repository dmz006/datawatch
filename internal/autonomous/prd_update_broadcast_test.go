// v5.24.0 — verifies EmitPRDUpdate fires on every PRD persist so the
// WS broadcast (wired in main.go) reaches the PWA Autonomous tab
// without manual Refresh.
//
// BL427 — EmitPRDUpdate now publishes a KindPRDUpdated LifecycleEvent
// through the dispatcher instead of a single-slot callback. Delivery is
// async per subscriber, so these tests wait with a bounded timeout. The
// payload regression tests pin the client-visible prd_update frame to
// the legacy main.go closure byte-for-byte.

package autonomous

import (
	"encoding/json"
	"testing"
	"time"
)

// prdUpdated subscribes a collector to prd.updated only, matching the
// main.go WS adapter's filter.
func prdUpdated(t *testing.T, m *Manager) *collector {
	t.Helper()
	c := &collector{}
	cancel := m.Subscribe(LifecycleFilter{Kinds: []string{KindPRDUpdated}}, c.add)
	t.Cleanup(cancel)
	return c
}

func TestEmitPRDUpdate_FiresWithCurrentPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	c := prdUpdated(t, m)

	m.EmitPRDUpdate(prd.ID)

	waitFor(t, "prd.updated", func() bool { return len(c.snapshot()) == 1 })
	ev := c.snapshot()[0]
	if ev.PRDID != prd.ID {
		t.Errorf("event prd_id = %q, want %q", ev.PRDID, prd.ID)
	}
	if ev.Deleted {
		t.Error("existing PRD must not be reported deleted")
	}
	if ev.NewStatus != string(prd.Status) {
		t.Errorf("NewStatus = %q, want %q", ev.NewStatus, prd.Status)
	}
	if ev.Kind != KindPRDUpdated || ev.Level != LevelPRD {
		t.Errorf("kind/level = %s/%s", ev.Kind, ev.Level)
	}
}

func TestEmitPRDUpdate_DeletedForMissingPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	c := prdUpdated(t, m)

	m.EmitPRDUpdate("does-not-exist")

	waitFor(t, "prd.updated (deleted)", func() bool { return len(c.snapshot()) == 1 })
	ev := c.snapshot()[0]
	if !ev.Deleted {
		t.Error("missing PRD must be reported deleted")
	}
	if ev.PRDID != "does-not-exist" {
		t.Errorf("event prd_id = %q", ev.PRDID)
	}
}

func TestEmitPRDUpdate_NoPanicWhenNothingSubscribed(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	m.EmitPRDUpdate(prd.ID)
	m.EmitPRDUpdate("does-not-exist")
}

// legacyPRDUpdatePayload is a verbatim copy of the pre-dispatcher
// main.go SetOnPRDUpdate closure body (minus the broadcast call), the
// reference for client-visible output.
func legacyPRDUpdatePayload(prdID string, prd *PRD) map[string]any {
	payload := map[string]any{"prd_id": prdID}
	if prd != nil {
		payload["status"] = string(prd.Status)
	} else {
		payload["deleted"] = true
	}
	return payload
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestPRDUpdatePayload_MatchesLegacyClosure(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	c := prdUpdated(t, m)

	cases := []struct {
		name string
		id   string
	}{
		{"existing", prd.ID},
		{"deleted", "does-not-exist"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, _ := m.Store().GetPRD(tc.id)
			want := mustJSON(t, legacyPRDUpdatePayload(tc.id, ref))

			m.EmitPRDUpdate(tc.id)
			waitFor(t, "prd.updated", func() bool { return len(c.snapshot()) == i+1 })
			got := mustJSON(t, PRDUpdatePayload(c.snapshot()[i]))
			if got != want {
				t.Errorf("payload JSON mismatch\n got: %s\nwant: %s", got, want)
			}
		})
	}
	// Spot-check the exact legacy shapes.
	if got := mustJSON(t, PRDUpdatePayload(LifecycleEvent{PRDID: "x", Deleted: true})); got != `{"deleted":true,"prd_id":"x"}` {
		t.Errorf("deleted payload = %s", got)
	}
	// An empty status on an existing PRD still carries "status":"" as before.
	if got := mustJSON(t, PRDUpdatePayload(LifecycleEvent{PRDID: "x"})); got != `{"prd_id":"x","status":""}` {
		t.Errorf("empty-status payload = %s", got)
	}
}

// One EmitPRDUpdate → exactly one prd.updated; status-change events
// (Approve) add no prd.updated deliveries of their own.
func TestEmitPRDUpdate_OneDeliveryAndNoExtraFromStatusEvents(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	all := &collector{}
	t.Cleanup(m.Subscribe(LifecycleFilter{}, all.add))
	upd := prdUpdated(t, m)

	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	approved := PRDStatusKind(PRDApproved)
	waitFor(t, approved, func() bool {
		for _, ev := range all.snapshot() {
			if ev.Kind == approved {
				return true
			}
		}
		return false
	})

	m.EmitPRDUpdate(prd.ID)
	// Per-subscriber ordering: once the wildcard subscriber has seen the
	// prd.updated, everything published before it has been delivered too.
	waitFor(t, "prd.updated on wildcard", func() bool {
		for _, ev := range all.snapshot() {
			if ev.Kind == KindPRDUpdated {
				return true
			}
		}
		return false
	})
	waitFor(t, "prd.updated on adapter", func() bool { return len(upd.snapshot()) >= 1 })
	time.Sleep(50 * time.Millisecond) // let any stray extra delivery land

	n := 0
	for _, ev := range all.snapshot() {
		if ev.Kind == KindPRDUpdated {
			n++
		}
	}
	if n != 1 {
		t.Errorf("wildcard saw %d prd.updated, want 1", n)
	}
	if got := len(upd.snapshot()); got != 1 {
		t.Errorf("prd.updated subscriber got %d deliveries, want exactly 1", got)
	}
	for _, ev := range upd.snapshot() {
		if ev.Kind != KindPRDUpdated {
			t.Errorf("prd.updated subscriber received %s", ev.Kind)
		}
	}
}
