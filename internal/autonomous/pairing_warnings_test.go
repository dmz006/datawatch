package autonomous

import (
	"strings"
	"testing"
)

func TestPairingWarnings_MatchingPairGivesNoWarning(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		refs map[string][]string
	}{
		{
			name: "unprefixed planning model with matching planning backend",
			cfg:  Config{PlanningBackend: "opencode", PlanningModel: "qwen3.8:27b"},
			refs: map[string][]string{"opencode": {"p1"}},
		},
		{
			name: "prefixed planning model whose prefix names the decompose backend",
			cfg:  Config{PlanningModel: "ollama/qwen3.8:27b"},
			refs: map[string][]string{"ollama": {"p1"}},
		},
		{
			name: "matching verification pair",
			cfg:  Config{VerificationBackend: "ollama", VerificationModel: "ollama/qwen3.8:27b"},
			refs: map[string][]string{"claude-code": {"p1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PairingWarnings(tt.cfg, tt.refs); len(got) != 0 {
				t.Fatalf("want no warnings, got %v", got)
			}
		})
	}
}

func TestPairingWarnings_MismatchedPrefixGivesOneWarningNamingBothBackends(t *testing.T) {
	cfg := Config{PlanningModel: "ollama/qwen3.8:27b"}
	got := PairingWarnings(cfg, map[string][]string{"claude-code": {"p1"}})
	if len(got) != 1 {
		t.Fatalf("want exactly 1 warning, got %d: %v", len(got), got)
	}
	for _, want := range []string{"ollama", "claude-code", "p1", "ollama/qwen3.8:27b"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("warning %q does not name %q", got[0], want)
		}
	}
}

func TestPairingWarnings_PlanningBackendMismatchWarns(t *testing.T) {
	cfg := Config{PlanningBackend: "opencode", PlanningModel: "qwen3.8:27b"}
	got := PairingWarnings(cfg, map[string][]string{"claude-code": {"p1", "p2"}})
	if len(got) != 1 {
		t.Fatalf("want exactly 1 warning, got %d: %v", len(got), got)
	}
	for _, want := range []string{"opencode", "claude-code", "PRDs p1, p2"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("warning %q does not name %q", got[0], want)
		}
	}
}

func TestPairingWarnings_VerificationMismatchWarns(t *testing.T) {
	cfg := Config{VerificationBackend: "claude-code", VerificationModel: "ollama/qwen3.8:27b"}
	got := PairingWarnings(cfg, nil)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 warning, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "claude-code") || !strings.Contains(got[0], "ollama") {
		t.Errorf("warning %q does not name both backends", got[0])
	}
}

func TestPairingWarnings_EmptyConfigGivesNone(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		refs map[string][]string
	}{
		{name: "empty config and no PRDs", cfg: Config{}, refs: nil},
		{name: "empty config with decompose backends", cfg: Config{}, refs: map[string][]string{"claude-code": {"p1"}}},
		{name: "planning model without decompose backends", cfg: Config{PlanningModel: "ollama/qwen3.8:27b"}, refs: nil},
		{name: "empty decompose backend is skipped", cfg: Config{PlanningModel: "ollama/qwen3.8:27b"}, refs: map[string][]string{"": {"p1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PairingWarnings(tt.cfg, tt.refs)
			if got == nil {
				t.Fatal("want a non-nil empty slice so the JSON field is an array")
			}
			if len(got) != 0 {
				t.Fatalf("want no warnings, got %v", got)
			}
		})
	}
}

func TestManagerPairingWarnings_FinishedPRDsAreExcluded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlanningBackend = "claude-code"
	cfg.PlanningModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if got := m.PairingWarnings(); len(got) != 1 {
		t.Fatalf("draft PRD should warn, got %v", got)
	}
	if got := m.Status().Warnings; len(got) != 1 {
		t.Fatalf("Status should carry the warning, got %v", got)
	}

	prd.Status = PRDCompleted
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if got := m.PairingWarnings(); len(got) != 0 {
		t.Fatalf("completed PRD should not warn, got %v", got)
	}
}

func TestManagerReportPairingWarnings_LogsEachWarningOnce(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlanningBackend = "claude-code"
	cfg.PlanningModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := m.CreatePRD("spec", t.TempDir(), "", "", ""); err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	for i := 0; i < 3; i++ {
		if got := m.ReportPairingWarnings(); len(got) != 1 {
			t.Fatalf("call %d: want 1 warning, got %v", i, got)
		}
	}
	if n := len(m.pairingLogged); n != 1 {
		t.Fatalf("want exactly one logged warning key, got %d", n)
	}
}
