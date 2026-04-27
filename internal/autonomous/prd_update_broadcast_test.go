// v5.24.0 — verifies Manager.SetOnPRDUpdate fires on every PRD
// persist so the WS broadcast (wired in main.go) reaches the PWA
// Autonomous tab without manual Refresh.

package autonomous

import (
	"sync"
	"testing"
)

type capture struct {
	mu     sync.Mutex
	events []captured
}

type captured struct {
	id  string
	prd *PRD
}

func (c *capture) add(id string, prd *PRD) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, captured{id: id, prd: prd})
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *capture) lastID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return ""
	}
	return c.events[len(c.events)-1].id
}

func TestEmitPRDUpdate_FiresWithCurrentPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", EffortNormal)

	c := &capture{}
	m.SetOnPRDUpdate(c.add)

	m.EmitPRDUpdate(prd.ID)

	if c.count() != 1 {
		t.Fatalf("expected 1 event, got %d", c.count())
	}
	if c.lastID() != prd.ID {
		t.Errorf("event id = %q, want %q", c.lastID(), prd.ID)
	}
	if c.events[0].prd == nil {
		t.Error("prd pointer should be non-nil for an existing PRD")
	}
}

func TestEmitPRDUpdate_NilForMissingPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	c := &capture{}
	m.SetOnPRDUpdate(c.add)

	m.EmitPRDUpdate("does-not-exist")

	if c.count() != 1 {
		t.Fatalf("expected 1 event for deletion, got %d", c.count())
	}
	if c.events[0].prd != nil {
		t.Error("prd pointer should be nil for a missing PRD (deletion semantics)")
	}
}

func TestEmitPRDUpdate_NoCallbackWhenUnset(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", EffortNormal)
	// Don't call SetOnPRDUpdate. Must not panic.
	m.EmitPRDUpdate(prd.ID)
}

func TestSetOnPRDUpdate_LastWriterWins(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", EffortNormal)

	first := &capture{}
	second := &capture{}
	m.SetOnPRDUpdate(first.add)
	m.SetOnPRDUpdate(second.add)

	m.EmitPRDUpdate(prd.ID)

	if first.count() != 0 {
		t.Errorf("first callback should have been replaced; got %d events", first.count())
	}
	if second.count() != 1 {
		t.Errorf("second callback should fire; got %d events", second.count())
	}
}
