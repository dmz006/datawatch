package autonomous

import (
	"bytes"
	"encoding/json"
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

// TestPairingWarnings_PiPromptProviderServedNoWarning pins the acceptance case:
// a provider-served model (ollama/ prefix) is forwarded to pi and pi-prompt
// decompose sessions, so no warning must fire for those backends.
func TestPairingWarnings_PiPromptProviderServedNoWarning(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		refs map[string][]string
	}{
		{
			name: "pi-prompt planning backend with ollama model and pi-prompt PRDs",
			cfg:  Config{PlanningBackend: "pi-prompt", PlanningModel: "ollama/qwen3.8-64k:27b"},
			refs: map[string][]string{"pi-prompt": {"p1", "p2"}},
		},
		{
			name: "pi planning backend with ollama model and pi PRDs",
			cfg:  Config{PlanningBackend: "pi", PlanningModel: "ollama/qwen3.8-64k:27b"},
			refs: map[string][]string{"pi": {"p1", "p2"}},
		},
		{
			name: "empty planning backend with ollama model and pi-prompt PRDs",
			cfg:  Config{PlanningModel: "ollama/qwen3.8-64k:27b"},
			refs: map[string][]string{"pi-prompt": {"p1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PairingWarnings(tt.cfg, tt.refs)
			if got == nil {
				t.Fatal("want a non-nil empty slice so the JSON field is an array")
			}
			if len(got) != 0 {
				t.Fatalf("want zero warnings for a pi-family backend serving provider models, got %v", got)
			}
		})
	}
}

func TestPairingWarnings_VerificationPiPromptNoWarning(t *testing.T) {
	cfg := Config{VerificationBackend: "pi-prompt", VerificationModel: "ollama/qwen3.8-64k:27b"}
	if got := PairingWarnings(cfg, nil); len(got) != 0 {
		t.Fatalf("want zero warnings, the pi-family backend serves provider-served models, got %v", got)
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

// TestPairingWarnings_EachPRDNamedOnceWithReason pins the compact rendering:
// one warning per (planning model, decompose backend) group, each PRD named
// at most once, more than three PRDs abbreviated to the first three plus a
// count of the rest, and the reason (model, its backend, and the decompose
// backend) still stated.
func TestPairingWarnings_EachPRDNamedOnceWithReason(t *testing.T) {
	cfg := Config{PlanningBackend: "opencode", PlanningModel: "qwen3.8:27b"}

	got := PairingWarnings(cfg, map[string][]string{"claude-code": {"p1", "p2", "p3", "p4", "p5"}})
	if len(got) != 1 {
		t.Fatalf("want exactly 1 warning, got %d: %v", len(got), got)
	}
	w := got[0]
	for _, id := range []string{"p1", "p2", "p3", "p4", "p5"} {
		if n := strings.Count(w, id); n != 0 && n != 1 {
			t.Errorf("id %q appears %d times in warning %q, want 0 or 1", id, n, w)
		}
	}
	for _, want := range []string{"and 2 more", "qwen3.8:27b", "opencode", "claude-code"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q does not name %q", w, want)
		}
	}

	single := PairingWarnings(cfg, map[string][]string{"claude-code": {"p1"}})
	if len(single) != 1 {
		t.Fatalf("single-PRD group: want exactly 1 warning, got %d: %v", len(single), single)
	}
	sw := single[0]
	if n := strings.Count(sw, "p1"); n != 1 {
		t.Errorf("single-PRD warning names %q %d times, want once: %q", "p1", n, sw)
	}
	for _, want := range []string{"qwen3.8:27b", "opencode", "claude-code"} {
		if !strings.Contains(sw, want) {
			t.Errorf("single-PRD warning %q does not name %q", sw, want)
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

// TestStatusWarningsPayloadShape pins the wire shape of LoopStatus.Warnings:
// it must stay a plain JSON array of warning strings (a []string in Go),
// never an object. (1) With one non-finished PRD and a planning model that
// does not pair with the PRD's decompose backend, the marshaled status has a
// "warnings" key whose value is an array of exactly that one string.
// (2) With a clean config and zero non-finished PRDs, the field is an empty
// non-nil []string, and the current omitempty tag omits the key (Go treats a
// len-0 slice as empty) — the field stays a plain []string in both cases.
func TestStatusWarningsPayloadShape(t *testing.T) {
	t.Run("one warning present, warnings key is a plain string array", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.PlanningBackend = "claude-code"
		cfg.PlanningModel = "ollama/qwen3.8-64k:27b"
		m, err := NewManager(t.TempDir(), cfg, nil)
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		if _, err := m.CreatePRD("spec", t.TempDir(), "", "", ""); err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}

		st := m.Status()
		if st.Warnings == nil {
			t.Fatal("Status().Warnings must be a non-nil []string")
		}
		if len(st.Warnings) != 1 {
			t.Fatalf("want exactly 1 warning, got %d: %v", len(st.Warnings), st.Warnings)
		}

		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatalf("Marshal status: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("Marshal produced no status object: %v", err)
		}
		wraw, ok := fields["warnings"]
		if !ok {
			t.Fatalf(`marshaled status is missing the "warnings" key: %s`, raw)
		}
		body := bytes.TrimSpace(wraw)
		if len(body) == 0 || body[0] != '[' {
			t.Fatalf(`"warnings" must be a JSON array (a plain []string), got %s`, body)
		}
		var items []string
		if err := json.Unmarshal(wraw, &items); err != nil {
			t.Fatalf(`"warnings" is not an array of strings: %v: %s`, err, body)
		}
		if len(items) != 1 {
			t.Fatalf(`"warnings" must hold exactly 1 string, got %d: %s`, len(items), body)
		}
		if items[0] != st.Warnings[0] {
			t.Errorf("marshaled warning %q does not match in-memory warning %q", items[0], st.Warnings[0])
		}
	})

	t.Run("clean config, no warnings key, field stays a plain []string", func(t *testing.T) {
		cfg := DefaultConfig() // no planning model
		m, err := NewManager(t.TempDir(), cfg, nil)
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		// zero PRDs at all, so zero non-finished PRDs

		st := m.Status()
		if st.Warnings == nil {
			t.Fatal("Status().Warnings must be a non-nil []string (PairingWarnings never returns nil)")
		}
		if len(st.Warnings) != 0 {
			t.Fatalf("clean config with no PRDs: want a warnings slice of len 0, got %v", st.Warnings)
		}

		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatalf("Marshal status: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("Marshal produced no status object: %v", err)
		}
		if wraw, ok := fields["warnings"]; ok {
			// Current behavior: the json tag is warnings,omitempty and Go
			// treats a len-0 slice as empty, so the key is omitted entirely.
			t.Fatalf(`with the current omitempty tag, a len-0 warnings slice omits the key, but "warnings" is present: %s`, wraw)
		}
	})
}
