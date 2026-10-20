// Refusal tests for model/backend pairs at the Manager setters. A pair the
// backend cannot serve is refused with ErrModelBackendMismatch before any
// field of the PRD changes: no mutation, no Decision entry, no save.

package autonomous

import (
	"errors"
	"testing"
)

// refusalSnapshot holds every worker, verification and guardrail pair a
// refused call could touch, plus the decision-log length, as plain values.
type refusalSnapshot struct {
	prdBackend, prdModel                     string
	prdVerificationBackend                   string
	prdVerificationModel                     string
	prdGuardrailBackend, prdGuardrailModel   string
	storyBackend, storyModel                 string
	storyVerificationBackend                 string
	storyVerificationModel                   string
	taskBackend, taskModel                   string
	taskVerificationBackend                  string
	taskVerificationModel                    string
	taskGuardrailBackend, taskGuardrailModel string
	decisions                                int
}

func newRefusalManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// seedRefusalPRD creates a PRD in review with one story and one task. It
// sets the PRD's worker pair directly, bypassing the setters, so a test can
// seed a pair the setters would refuse.
func seedRefusalPRD(t *testing.T, m *Manager, backend, model string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("refusal spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Backend, prd.Model = backend, model
	prd.Story = []Story{{ID: "s1", Title: "story", Tasks: []Task{{ID: "t1", Title: "task"}}}}
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

func takeRefusalSnapshot(t *testing.T, m *Manager, prdID string) refusalSnapshot {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %q not found", prdID)
	}
	task, story := findTaskInPRD(prd, "t1")
	if task == nil {
		t.Fatalf("task t1 not found")
	}
	return refusalSnapshot{
		prdBackend:               prd.Backend,
		prdModel:                 prd.Model,
		prdVerificationBackend:   prd.VerificationBackend,
		prdVerificationModel:     prd.VerificationModel,
		prdGuardrailBackend:      prd.GuardrailBackend,
		prdGuardrailModel:        prd.GuardrailModel,
		storyBackend:             story.Backend,
		storyModel:               story.Model,
		storyVerificationBackend: story.VerificationBackend,
		storyVerificationModel:   story.VerificationModel,
		taskBackend:              task.Backend,
		taskModel:                task.Model,
		taskVerificationBackend:  task.VerificationBackend,
		taskVerificationModel:    task.VerificationModel,
		taskGuardrailBackend:     task.GuardrailBackend,
		taskGuardrailModel:       task.GuardrailModel,
		decisions:                len(prd.Decisions),
	}
}

func assertRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected refusal, got nil error")
	}
	if !errors.Is(err, ErrModelBackendMismatch) {
		t.Fatalf("error %v does not wrap ErrModelBackendMismatch", err)
	}
}

func assertSnapshotUnchanged(t *testing.T, before, after refusalSnapshot) {
	t.Helper()
	if before != after {
		t.Fatalf("refused call changed the record:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestModelBackendRefusalCreatePRDClaudeAliasOnPi(t *testing.T) {
	m := newRefusalManager(t)

	_, err := m.CreatePRD("spec", t.TempDir(), "pi", "claude-haiku-5-5", "")
	assertRefused(t, err)
	if n := len(m.Store().ListPRDs()); n != 0 {
		t.Fatalf("refused CreatePRD still stored %d PRD(s); want 0", n)
	}

	// The same model with a provider tag, or on claude-code, is accepted.
	if _, err := m.CreatePRD("spec", t.TempDir(), "pi", "anthropic/claude-haiku-5-5", ""); err != nil {
		t.Fatalf("provider-qualified pi model refused: %v", err)
	}
	if _, err := m.CreatePRD("spec", t.TempDir(), "claude-code", "claude-opus-5-5", ""); err != nil {
		t.Fatalf("bare claude-code model refused: %v", err)
	}
	if n := len(m.Store().ListPRDs()); n != 2 {
		t.Fatalf("accepted CreatePRD calls stored %d PRD(s); want 2", n)
	}
}

func TestModelBackendRefusalSetPRDLLMLeavesRecordUnchanged(t *testing.T) {
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
	before := takeRefusalSnapshot(t, m, prd.ID)

	// The new backend is pi, and the model in the call is a bare Claude alias.
	_, err := m.SetPRDLLM(prd.ID, strp("pi"), nil, strp("claude-haiku-5-5"), nil, nil, "test")
	assertRefused(t, err)
	assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))

	// A call that sets only a field the pair does not depend on is not
	// refused, even though the record's own pair is a valid one.
	if _, err := m.SetPRDLLM(prd.ID, nil, strp("high"), nil, nil, nil, "test"); err != nil {
		t.Fatalf("effort-only SetPRDLLM refused: %v", err)
	}
}

func TestModelBackendRefusalDecompositionModelNotChecked(t *testing.T) {
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "pi", "anthropic/claude-haiku-5-5")

	// decomposition_model belongs to the planning backend, which is
	// resolved separately, so a bare Claude name there is not refused.
	if _, err := m.SetPRDLLM(prd.ID, nil, nil, nil, nil, strp("claude-opus-5-5"), "test"); err != nil {
		t.Fatalf("decomposition-only SetPRDLLM refused: %v", err)
	}
}

func TestModelBackendRefusalSetStoryLLMLeavesRecordUnchanged(t *testing.T) {
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
	before := takeRefusalSnapshot(t, m, prd.ID)

	_, err := m.SetStoryLLM(prd.ID, "s1", "pi", "", "claude-haiku-5-5", "test")
	assertRefused(t, err)
	assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))
}

func TestModelBackendRefusalSetTaskLLMUsesEffectiveBackend(t *testing.T) {
	m := newRefusalManager(t)

	// Case 1: the task names no backend, so it inherits the PRD's pi. A bare
	// Claude alias is refused even though the task's own backend is empty.
	prd := seedRefusalPRD(t, m, "pi", "anthropic/qwen3.8:27b")
	before := takeRefusalSnapshot(t, m, prd.ID)
	_, err := m.SetTaskLLM(prd.ID, "t1", "", "", "claude-haiku-5-5", "test")
	assertRefused(t, err)
	assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))

	// Case 2: the same call on a PRD running claude-code is accepted, because
	// the effective backend is claude-code and the alias is valid there.
	prd2 := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
	if _, err := m.SetTaskLLM(prd2.ID, "t1", "", "", "claude-haiku-5-5", "test"); err != nil {
		t.Fatalf("SetTaskLLM on claude-code PRD refused: %v", err)
	}

	// Case 3: a task backend of its own decides the pair, so pi with a bare
	// alias is refused even on a claude-code PRD.
	prd3 := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
	before3 := takeRefusalSnapshot(t, m, prd3.ID)
	_, err = m.SetTaskLLM(prd3.ID, "t1", "pi", "", "claude-haiku-5-5", "test")
	assertRefused(t, err)
	assertSnapshotUnchanged(t, before3, takeRefusalSnapshot(t, m, prd3.ID))
}

func TestModelBackendRefusalVerificationSetterRefused(t *testing.T) {
	bareAlias := "claude-haiku-5-5"
	cases := []struct {
		name string
		call func(m *Manager, prdID string) error
	}{
		{
			name: "PRD verification",
			call: func(m *Manager, prdID string) error {
				_, err := m.SetPRDVerificationLLM(prdID, strp("pi"), strp(bareAlias), nil, "test")
				return err
			},
		},
		{
			name: "story verification",
			call: func(m *Manager, prdID string) error {
				_, err := m.SetStoryVerificationLLM(prdID, "s1", strp("pi"), strp(bareAlias), nil, "test")
				return err
			},
		},
		{
			name: "task verification",
			call: func(m *Manager, prdID string) error {
				_, err := m.SetTaskVerificationLLM(prdID, "t1", strp("pi"), strp(bareAlias), nil, "test")
				return err
			},
		},
		{
			name: "PRD guardrail",
			call: func(m *Manager, prdID string) error {
				_, err := m.SetPRDGuardrailLLM(prdID, strp("pi"), strp(bareAlias), "test")
				return err
			},
		},
		{
			name: "task guardrail",
			call: func(m *Manager, prdID string) error {
				_, err := m.SetTaskGuardrailLLM(prdID, "t1", strp("pi"), strp(bareAlias), "test")
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRefusalManager(t)
			prd := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")
			before := takeRefusalSnapshot(t, m, prd.ID)

			assertRefused(t, tc.call(m, prd.ID))
			assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))
		})
	}
}

func TestModelBackendRefusalAcceptsValidVerificationPair(t *testing.T) {
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "claude-code", "claude-opus-5-5")

	// A provider-qualified pi model on a pi verifier is accepted.
	if _, err := m.SetPRDVerificationLLM(prd.ID, strp("pi"), strp("anthropic/claude-haiku-5-5"), nil, "test"); err != nil {
		t.Fatalf("valid pi verification pair refused: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.VerificationBackend != "pi" || stored.VerificationModel != "anthropic/claude-haiku-5-5" {
		t.Fatalf("stored verification = %q/%q; want pi/anthropic/claude-haiku-5-5",
			stored.VerificationBackend, stored.VerificationModel)
	}
}
