// BL311 — Gap #7: DecompositionBackend with a named registry LLM string.
//
// Verifies that Manager.SetConfig accepts a named LLM string for
// DecompositionBackend and that Config() returns it unchanged.
// This pins the data-flow so regressions (e.g. validation code that
// strips non-adapter-kind strings) are caught immediately.

package autonomous

import (
	"strings"
	"testing"
)

// TestAutonomousManager_PlanningBackend_NamedLLM sets DecompositionBackend
// to a named LLM string (e.g. "ollama-datawatch") and verifies it is stored
// and returned correctly by Config().
func TestAutonomousManager_PlanningBackend_NamedLLM(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const namedLLM = "ollama-datawatch"

	cfg := m.Config()
	cfg.PlanningBackend = namedLLM
	m.SetConfig(cfg)

	got := m.Config()
	if got.PlanningBackend != namedLLM {
		t.Errorf("DecompositionBackend: got %q, want %q", got.PlanningBackend, namedLLM)
	}
}

// TestAutonomousManager_PlanningBackend_AdapterKindVsNamed verifies that both
// a plain adapter-kind string ("claude-code") and a dotted named LLM string
// ("ollama-datawatch") survive a SetConfig/Config round-trip without
// modification — the manager must not validate or transform these strings.
func TestAutonomousManager_PlanningBackend_AdapterKindVsNamed(t *testing.T) {
	cases := []struct {
		label   string
		backend string
	}{
		{"adapter-kind", "claude-code"},
		{"named-llm", "ollama-datawatch"},
		{"named-with-dots", "my.custom.llm"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			cfg := m.Config()
			cfg.PlanningBackend = tc.backend
			m.SetConfig(cfg)

			got := m.Config()
			if got.PlanningBackend != tc.backend {
				t.Errorf("DecompositionBackend round-trip: got %q, want %q",
					got.PlanningBackend, tc.backend)
			}
		})
	}
}

// TestAutonomousManager_PlanningBackend_UsedInDecompose verifies that when
// a PRD has no explicit Backend set, Decompose reads DecompositionBackend
// from the manager config and passes it to the decompose function.
func TestAutonomousManager_PlanningBackend_UsedInDecompose(t *testing.T) {
	const namedLLM = "ollama-datawatch"

	var capturedBackend string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedBackend = req.Backend
		// Return a minimal valid JSON decomposition (the parser expects JSON, not markdown).
		return `{"title":"Test PRD","stories":[{"title":"Story 1","tasks":[{"title":"Task 1","spec":"do the thing"}]}]}`, nil
	}

	cfg := DefaultConfig()
	cfg.PlanningBackend = namedLLM

	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a PRD without an explicit backend so the manager falls back
	// to cfg.PlanningBackend.
	prd, err := m.CreatePRD("build something", t.TempDir(), "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	if capturedBackend != namedLLM {
		t.Errorf("Decompose received backend %q, want %q", capturedBackend, namedLLM)
	}
}

// TestParsePlanningTaskKeyBackfill pins ParsePlanning's fallback path:
// tasks emitted with "name"/"description" keys (instead of
// "title"/"spec") are backfilled from the same cleaned input, and input
// that cannot be parsed at all errors out instead of returning a half
// populated plan.
func TestParsePlanningTaskKeyBackfill(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantTitle string
		wantTask  struct{ title, spec string }
		wantErr   string
	}{
		{
			name:      "title-and-spec-keys",
			raw:       `{"title":"Plan A","stories":[{"id":"s1","title":"Story 1","tasks":[{"id":"t1","title":"Task 1","spec":"do the thing"}]}]}`,
			wantTitle: "Plan A",
			wantTask:  struct{ title, spec string }{"Task 1", "do the thing"},
		},
		{
			name:      "name-and-description-backfilled",
			raw:       `{"title":"Plan B","stories":[{"id":"s1","title":"Story 1","tasks":[{"id":"t1","name":"Task B","description":"that thing"}]}]}`,
			wantTitle: "Plan B",
			wantTask:  struct{ title, spec string }{"Task B", "that thing"},
		},
		{
			name:    "task-missing-spec-still-fails",
			raw:     `{"title":"Plan C","stories":[{"id":"s1","title":"Story 1","tasks":[{"id":"t1","name":"name only"}]}]}`,
			wantErr: "empty title or spec",
		},
		{
			name:    "malformed-json-fails",
			raw:     "this is not json at all",
			wantErr: "parse",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, stories, err := ParsePlanning(tc.raw)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ParsePlanning: want error containing %q, got success (stories=%d)", tc.wantErr, len(stories))
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePlanning: %v", err)
			}
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
			if len(stories) != 1 || len(stories[0].Tasks) != 1 {
				t.Fatalf("stories = %+v, want exactly one story with one task", stories)
			}
			got := stories[0].Tasks[0]
			if got.Title != tc.wantTask.title || got.Spec != tc.wantTask.spec {
				t.Errorf("task = {%q %q}, want {%q %q}", got.Title, got.Spec, tc.wantTask.title, tc.wantTask.spec)
			}
		})
	}
}
