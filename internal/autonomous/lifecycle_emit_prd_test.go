// BL427 — PRD-level lifecycle events are emitted at every status write
// site. Table-driven over the PRD-level transition table in
// docs/plans/2026-10-13-prd-lifecycle-events.md ("Event source").

package autonomous

import (
	"context"
	"errors"
	"testing"
)

// newEmitPRD creates a PRD with one single-task story, forced to status.
func newEmitPRD(t *testing.T, m *Manager, status PRDStatus) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = status
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

func newEmitManager(t *testing.T, cfg Config, fn DecomposeFn) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func failingDecompose(DecomposeRequest) (string, error) {
	return "", errors.New("llm unavailable")
}

func failingVerify(context.Context, *PRD, *Task) (VerificationResult, error) {
	return VerificationResult{OK: false, Summary: "nope"}, nil
}

func TestLifecycleEmitPRD_Transitions(t *testing.T) {
	cases := []struct {
		name string
		// setup builds a Manager and PRD; drive performs the transition.
		setup    func(t *testing.T) (m *Manager, prdID string, drive func() error)
		want     PRDStatus
		old      PRDStatus
		decision string // expected ev.Decision.Kind; "" = no Decision
		verdict  bool   // expect a blocking ev.Verdict
	}{
		{
			name: "Approve", want: PRDApproved, old: PRDNeedsReview, decision: "approve",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDNeedsReview)
				return m, p.ID, func() error { _, err := m.Approve(p.ID, "op", ""); return err }
			},
		},
		{
			name: "Reject", want: PRDRejected, old: PRDNeedsReview, decision: "reject",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDNeedsReview)
				return m, p.ID, func() error { _, err := m.Reject(p.ID, "op", "no"); return err }
			},
		},
		{
			name: "RequestRevision", want: PRDRevisionsAsked, old: PRDApproved, decision: "request_revision",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDApproved)
				return m, p.ID, func() error { _, err := m.RequestRevision(p.ID, "op", "redo"); return err }
			},
		},
		{
			name: "Pause", want: PRDPaused, old: PRDRunning, decision: "pause",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDRunning)
				return m, p.ID, func() error { _, err := m.Pause(p.ID, "op"); return err }
			},
		},
		{
			name: "Resume", want: PRDRunning, old: PRDPaused, decision: "resume",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDPaused)
				return m, p.ID, func() error { _, err := m.Resume(p.ID, "op"); return err }
			},
		},
		{
			name: "Archive", want: PRDArchived, old: PRDCompleted, decision: "archive",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDCompleted)
				return m, p.ID, func() error { _, err := m.Archive(p.ID); return err }
			},
		},
		{
			name: "ResetToDraft", want: PRDDraft, old: PRDRejected, decision: "reset_to_draft",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDRejected)
				return m, p.ID, func() error { _, err := m.ResetToDraft(p.ID, "op"); return err }
			},
		},
		{
			name: "API.Cancel", want: PRDCancelled, old: PRDRunning, decision: "cancel",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDRunning)
				api := NewAPI(m)
				return m, p.ID, func() error { return api.Cancel(p.ID) }
			},
		},
		{
			// No executors wired → API.Run's status-only path.
			name: "API.Run", want: PRDRunning, old: PRDApproved,
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDApproved)
				api := NewAPI(m)
				return m, p.ID, func() error { return api.Run(p.ID) }
			},
		},
		{
			name: "Manager.Run start", want: PRDRunning, old: PRDApproved, decision: "run",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), fakeDecompose)
				p := newEmitPRD(t, m, PRDApproved)
				return m, p.ID, func() error { return m.Run(context.Background(), p.ID, fakeSpawn, fakeVerify) }
			},
		},
		{
			name: "decompose success", want: PRDNeedsReview, old: PRDPlanning, decision: "decompose",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), fakeDecompose)
				p, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
				return m, p.ID, func() error { _, err := m.Decompose(p.ID); return err }
			},
		},
		{
			name: "decompose planning", want: PRDPlanning, old: PRDDraft,
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), fakeDecompose)
				p, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
				return m, p.ID, func() error { _, err := m.Decompose(p.ID); return err }
			},
		},
		{
			name: "decompose failure", want: PRDDraft, old: PRDPlanning,
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), failingDecompose)
				p, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
				return m, p.ID, func() error {
					if _, err := m.Decompose(p.ID); err == nil {
						return errors.New("expected decompose error")
					}
					return nil
				}
			},
		},
		{
			name: "Manager.Run roll-up completed", want: PRDCompleted, old: PRDRunning,
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), fakeDecompose)
				p := newEmitPRD(t, m, PRDApproved)
				return m, p.ID, func() error { return m.Run(context.Background(), p.ID, fakeSpawn, fakeVerify) }
			},
		},
		{
			name: "Manager.Run roll-up failed", want: PRDFailed, old: PRDRunning,
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), fakeDecompose)
				p := newEmitPRD(t, m, PRDApproved)
				return m, p.ID, func() error { return m.Run(context.Background(), p.ID, fakeSpawn, failingVerify) }
			},
		},
		{
			name: "per-task guardrail block", want: PRDBlocked, old: PRDRunning, verdict: true,
			setup: func(t *testing.T) (*Manager, string, func() error) {
				cfg := DefaultConfig()
				cfg.PerTaskGuardrails = []string{"security"}
				m := newEmitManager(t, cfg, fakeDecompose)
				m.SetGuardrail(fakeGuardrailBlocking)
				p := newEmitPRD(t, m, PRDApproved)
				return m, p.ID, func() error { return m.Run(context.Background(), p.ID, fakeSpawn, fakeVerify) }
			},
		},
		{
			name: "ResetTask on failed PRD", want: PRDRunning, old: PRDFailed, decision: "reset_task",
			setup: func(t *testing.T) (*Manager, string, func() error) {
				m := newEmitManager(t, DefaultConfig(), nil)
				p := newEmitPRD(t, m, PRDFailed)
				p.Story[0].Tasks[0].Status = TaskFailed
				if err := m.Store().SavePRD(p); err != nil {
					t.Fatalf("SavePRD: %v", err)
				}
				tid := p.Story[0].Tasks[0].ID
				return m, p.ID, func() error { _, err := m.ResetTask(p.ID, tid, "op", false); return err }
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, prdID, drive := tc.setup(t)
			c := &collector{}
			cancel := m.Subscribe(LifecycleFilter{
				Level: LevelPRD,
				PRDID: prdID,
				Kinds: []string{PRDStatusKind(tc.want)},
			}, c.add)
			defer cancel()

			if err := drive(); err != nil {
				t.Fatalf("drive: %v", err)
			}
			waitFor(t, "event "+PRDStatusKind(tc.want), func() bool {
				for _, ev := range c.snapshot() {
					if ev.OldStatus == string(tc.old) {
						return true
					}
				}
				return false
			})
			var ev LifecycleEvent
			for _, e := range c.snapshot() {
				if e.OldStatus == string(tc.old) {
					ev = e
					break
				}
			}
			if ev.Kind != PRDStatusKind(tc.want) || ev.Level != LevelPRD || ev.PRDID != prdID {
				t.Fatalf("event = %+v, want kind=%s level=prd prd=%s", ev, PRDStatusKind(tc.want), prdID)
			}
			if ev.NewStatus != string(tc.want) {
				t.Errorf("NewStatus = %q, want %q", ev.NewStatus, tc.want)
			}
			if ev.ID == "" || ev.At.IsZero() {
				t.Errorf("event not stamped: id=%q at=%v", ev.ID, ev.At)
			}
			switch {
			case tc.decision == "" && ev.Decision != nil:
				t.Errorf("unexpected Decision %q", ev.Decision.Kind)
			case tc.decision != "" && (ev.Decision == nil || ev.Decision.Kind != tc.decision):
				t.Errorf("Decision = %+v, want kind %q", ev.Decision, tc.decision)
			}
			if tc.verdict && (ev.Verdict == nil || ev.Verdict.Outcome != "block") {
				t.Errorf("Verdict = %+v, want a block verdict", ev.Verdict)
			}
			if !tc.verdict && ev.Verdict != nil {
				t.Errorf("unexpected Verdict %+v", ev.Verdict)
			}
		})
	}
}

// Template instantiation creates a new PRD straight into needs_review:
// no prior status, so OldStatus is empty.
func TestLifecycleEmitPRD_TemplateInstantiate(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	tmpl := newEmitPRD(t, m, PRDDraft)
	tmpl.IsTemplate = true
	if err := m.Store().SavePRD(tmpl); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	c := &collector{}
	defer m.Subscribe(LifecycleFilter{Kinds: []string{PRDStatusKind(PRDNeedsReview)}}, c.add)()

	got, err := m.InstantiateTemplate(tmpl.ID, nil, "op")
	if err != nil {
		t.Fatalf("InstantiateTemplate: %v", err)
	}
	waitFor(t, "template needs_review event", func() bool { return len(c.snapshot()) > 0 })
	ev := c.snapshot()[0]
	if ev.PRDID != got.ID || ev.OldStatus != "" || ev.NewStatus != string(PRDNeedsReview) {
		t.Fatalf("event = %+v, want prd=%s old=\"\" new=needs_review", ev, got.ID)
	}
	if ev.Decision == nil || ev.Decision.Kind != "template_instantiate" {
		t.Fatalf("Decision = %+v, want template_instantiate", ev.Decision)
	}
}

// Child Automata carry their parent's ID on every PRD-level event.
func TestLifecycleEmitPRD_ChildCarriesParentPRDID(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	parent := newEmitPRD(t, m, PRDRunning)
	child, err := m.Store().CreatePRDWithParent("child", "/p", "claude", "", EffortNormal, parent.ID, "t1", 1)
	if err != nil {
		t.Fatalf("CreatePRDWithParent: %v", err)
	}
	if err := m.Store().SetStories(child.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	child, _ = m.Store().GetPRD(child.ID)
	child.Status = PRDNeedsReview
	_ = m.Store().SavePRD(child)

	c := &collector{}
	defer m.Subscribe(LifecycleFilter{PRDID: child.ID}, c.add)()
	if _, err := m.Approve(child.ID, "autonomous", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitFor(t, "child approve event", func() bool { return len(c.snapshot()) > 0 })
	if ev := c.snapshot()[0]; ev.ParentPRDID != parent.ID {
		t.Fatalf("ParentPRDID = %q, want %q", ev.ParentPRDID, parent.ID)
	}
}

// A rejected transition (precondition fails, nothing saved) emits nothing.
func TestLifecycleEmitPRD_NoEventOnRefusedTransition(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	p := newEmitPRD(t, m, PRDDraft)
	c := &collector{}
	defer m.Subscribe(LifecycleFilter{PRDID: p.ID}, c.add)()
	if _, err := m.Pause(p.ID, "op"); err == nil {
		t.Fatal("expected Pause on a draft PRD to fail")
	}
	// Sentinel: an emit after the refused call proves the queue drained.
	if _, err := m.Reject(p.ID, "op", "x"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	waitFor(t, "sentinel reject event", func() bool { return len(c.snapshot()) > 0 })
	evs := c.snapshot()
	if len(evs) != 1 || evs[0].Kind != PRDStatusKind(PRDRejected) {
		t.Fatalf("events = %+v, want only prd.rejected", evs)
	}
}
