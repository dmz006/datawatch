// ci-watch completion guardrail plumbing tests
// (config default, PRD record round-trip, library entry). The detection
// logic itself lives elsewhere.

package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCIWatchTimeout_DefaultAndConfigured(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if got := m.ciWatchTimeout(); got != 1500*time.Second {
		t.Errorf("unset: ciWatchTimeout() = %v, want 1500s", got)
	}

	cfg := m.Config()
	cfg.CIWatchTimeoutSeconds = -5
	m.SetConfig(cfg)
	if got := m.ciWatchTimeout(); got != 1500*time.Second {
		t.Errorf("negative: ciWatchTimeout() = %v, want 1500s", got)
	}

	cfg.CIWatchTimeoutSeconds = 90
	m.SetConfig(cfg)
	if got := m.ciWatchTimeout(); got != 90*time.Second {
		t.Errorf("configured: ciWatchTimeout() = %v, want 90s", got)
	}
}

func TestCIWatchTimeout_ConfigJSONTag(t *testing.T) {
	raw, err := json.Marshal(Config{CIWatchTimeoutSeconds: 600})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"ci_watch_timeout_seconds":600`) {
		t.Errorf("Config JSON missing ci_watch_timeout_seconds: %s", raw)
	}
	zero, _ := json.Marshal(Config{})
	if strings.Contains(string(zero), "ci_watch_timeout_seconds") {
		t.Errorf("zero Config should omit ci_watch_timeout_seconds: %s", zero)
	}
}

func TestPRDCIWatch_JSONRoundTrip(t *testing.T) {
	// Zero value: field omitted entirely.
	raw, err := json.Marshal(PRD{ID: "p1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"ci_watch"`) {
		t.Errorf("nil CIWatch should be omitted: %s", raw)
	}

	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	in := PRD{
		ID: "p2",
		CIWatch: &CIWatchResult{
			Status:   "failed_content",
			SHA:      "abc123",
			Repo:     "owner/repo",
			Summary:  "1 of 2 runs failed",
			Category: "content",
			Issues:   []string{"unit test TestFoo failed"},
			Runs: []CIWatchRun{
				{ID: 42, Name: "CI", Status: "completed", Conclusion: "failure", URL: "https://example.invalid/runs/42"},
				{ID: 43, Name: "Lint", Status: "completed", Conclusion: "success"},
			},
			CheckedAt: at,
		},
	}
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, k := range []string{`"ci_watch"`, `"status":"failed_content"`, `"sha":"abc123"`, `"runs"`, `"checked_at"`, `"conclusion":"failure"`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("marshal missing %s: %s", k, raw)
		}
	}
	var out PRD
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in.CIWatch, out.CIWatch) {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", in.CIWatch, out.CIWatch)
	}
}

func TestCIWatchRun_ZeroOptionalFieldsOmitted(t *testing.T) {
	raw, _ := json.Marshal(CIWatchResult{Status: "skipped"})
	for _, k := range []string{`"sha"`, `"repo"`, `"runs"`, `"summary"`, `"category"`, `"issues"`} {
		if strings.Contains(string(raw), k) {
			t.Errorf("zero CIWatchResult should omit %s: %s", k, raw)
		}
	}
	if !strings.Contains(string(raw), `"checked_at"`) {
		t.Errorf("checked_at must always be present: %s", raw)
	}
}

func TestGuardrailLibrary_CIWatchPresentAndOffByDefault(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	e, ok := m.lookupGuardrailEntry("ci-watch")
	if !ok {
		t.Fatal("ci-watch missing from guardrail library")
	}
	if e.Type != "judgment" || e.ScanType != "" {
		t.Errorf("ci-watch entry type = %q scan=%q, want judgment/no scan type", e.Type, e.ScanType)
	}
	if !strings.Contains(e.Description, "CI runs") {
		t.Errorf("ci-watch description unexpected: %q", e.Description)
	}
	// Library order: ci-watch immediately follows rules-review.
	lib := m.GuardrailLibrary()
	for i, g := range lib {
		if g.Name == "rules-review" {
			if i+1 >= len(lib) || lib[i+1].Name != "ci-watch" {
				t.Errorf("ci-watch should directly follow rules-review in the library")
			}
		}
	}
	// Off by default: the resolved PRD-level list is empty.
	if got := m.resolveGuardrails(&PRD{ID: "p"}, "prd"); len(got) != 0 {
		t.Errorf("default PRD-level guardrails = %v, want empty", got)
	}
}
