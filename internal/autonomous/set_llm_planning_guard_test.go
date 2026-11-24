package autonomous

import (
	"errors"
	"strings"
	"testing"
)

// While an Automaton is planning, the worker LLM of its Automation, its
// stories and its tasks must not move, the same rule SetPRDLLM already holds.
// The plan was written against the settings in force when it started.

func TestSetAutomationLLM_RefusedWhilePlanning(t *testing.T) {
	h, a := llmAuto(t)
	h.setStatus(0, PRDPlanning)
	before := h.get(a)

	for name, call := range map[string]func() error{
		"backend": func() error {
			_, _, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), nil, nil, nil, nil, "alice", 0)
			return err
		},
		"model": func() error {
			_, _, err := h.m.SetAutomationLLM(a.ID, nil, strp("claude-opus-5-5"), nil, nil, nil, "alice", 0)
			return err
		},
		"effort": func() error {
			_, _, err := h.m.SetAutomationLLM(a.ID, nil, nil, strp("high"), nil, nil, "alice", 0)
			return err
		},
	} {
		err := call()
		if err == nil {
			t.Fatalf("%s change while a member plans: want a refusal, got nil", name)
		}
		if !errors.Is(err, ErrPlanInProgress) {
			t.Fatalf("%s change: error %v is not ErrPlanInProgress", name, err)
		}
		if !strings.HasPrefix(err.Error(), "409: ") {
			t.Fatalf("%s change: error %q must carry the 409 prefix the REST layer maps", name, err)
		}
	}

	after := h.get(a)
	if after.Version != before.Version || after.Backend != before.Backend || after.Model != before.Model || after.Effort != before.Effort {
		t.Fatalf("a refused call changed the Automation: before %+v after %+v", before, after)
	}
	if countDecisions(after, controlSetLLM) != 0 {
		t.Fatal("a refused call left a decision record")
	}

	// A planning-only change stays allowed: it goes through the requeue path.
	if _, _, err := h.m.SetAutomationLLM(a.ID, nil, nil, nil, strp("pi-prompt"), strp("qwen3.8:27b"), "alice", 0); err != nil {
		t.Fatalf("planning-pair change while planning must stay allowed: %v", err)
	}

	// Once planning ends the same call succeeds.
	h.setStatus(0, PRDNeedsReview)
	if _, _, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), nil, nil, nil, nil, "alice", 0); err != nil {
		t.Fatalf("worker change after planning ended: %v", err)
	}
}

func TestSetStoryLLM_RefusedWhilePlanning(t *testing.T) {
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
	prd.Status = PRDPlanning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	before := takeRefusalSnapshot(t, m, prd.ID)

	_, _, err := m.SetStoryLLM(prd.ID, "s1", "claude-code", "", "claude-haiku-5-5", "test")
	if !errors.Is(err, ErrPlanInProgress) {
		t.Fatalf("SetStoryLLM while planning: error %v, want ErrPlanInProgress", err)
	}
	assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))

	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.SetStoryLLM(prd.ID, "s1", "claude-code", "", "claude-haiku-5-5", "test"); err != nil {
		t.Fatalf("SetStoryLLM after planning ended: %v", err)
	}
}

func TestSetTaskLLM_RefusedWhilePlanning(t *testing.T) {
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
	prd.Status = PRDPlanning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	before := takeRefusalSnapshot(t, m, prd.ID)

	_, _, err := m.SetTaskLLM(prd.ID, "t1", "claude-code", "", "claude-haiku-5-5", "test")
	if !errors.Is(err, ErrPlanInProgress) {
		t.Fatalf("SetTaskLLM while planning: error %v, want ErrPlanInProgress", err)
	}
	assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))

	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "claude-code", "", "claude-haiku-5-5", "test"); err != nil {
		t.Fatalf("SetTaskLLM after planning ended: %v", err)
	}
}
