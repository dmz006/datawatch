// BL410 Phase 1 remediation — chat-channel parity tests for `retry …`,
// mirroring peers_test.go's fakeParentSrv/HandleTestMessage pattern
// (the same one guardrails.go's own comm verbs would use, had they
// shipped with tests).
package router

import (
	"net/http"
	"strings"
	"testing"
)

// ── Parser ────────────────────────────────────────────────────────────────

func TestParse_Retry_Bare(t *testing.T) {
	cmd := Parse("retry")
	if cmd.Type != CmdRetry {
		t.Errorf("type = %v want CmdRetry", cmd.Type)
	}
	if cmd.Text != "" {
		t.Errorf("text = %q want empty", cmd.Text)
	}
}

func TestParse_Retry_ProfileList(t *testing.T) {
	cmd := Parse("retry profile list")
	if cmd.Type != CmdRetry || cmd.Text != "profile list" {
		t.Errorf("got %+v", cmd)
	}
}

func TestParse_Retry_AutomatonSet(t *testing.T) {
	cmd := Parse("retry automaton set prd1 profile=release")
	if cmd.Type != CmdRetry || cmd.Text != "automaton set prd1 profile=release" {
		t.Errorf("got %+v", cmd)
	}
}

// ── Handler (via HandleTestMessage + fake parent) ──────────────────────────

func setupRetryRouter(t *testing.T, port int) *Router {
	t.Helper()
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	return r
}

func TestRetry_ProfileList_HitsParent(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/autonomous/retry_profiles" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"p1","name":"cve-work"}]`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("retry profile list")
	if calls != 1 {
		t.Errorf("expected 1 call to parent, got %d", calls)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "cve-work") {
		t.Errorf("response did not include profile name: %v", resp)
	}
}

func TestRetry_ProfileGet_ByID(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/retry_profiles/p1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"p1","name":"cve-work"}`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("retry profile get p1")
	if len(resp) == 0 || !strings.Contains(resp[0], "cve-work") {
		t.Errorf("response missing profile: %v", resp)
	}
}

func TestRetry_ProfileCreate_PostsBudgetsAndCeiling(t *testing.T) {
	captured := ""
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/autonomous/retry_profiles" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		captured = string(buf[:n])
		_, _ = w.Write([]byte(`{"id":"p2","name":"cve-work"}`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("retry profile create cve-work infra=6,content=2 ceiling=30")
	if !strings.Contains(captured, `"name":"cve-work"`) {
		t.Errorf("body missing name: %q", captured)
	}
	if !strings.Contains(captured, `"infra":6`) || !strings.Contains(captured, `"content":2`) {
		t.Errorf("body missing budgets: %q", captured)
	}
	if !strings.Contains(captured, `"ceiling_total":30`) {
		t.Errorf("body missing ceiling_total: %q", captured)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "p2") {
		t.Errorf("response missing id: %v", resp)
	}
}

func TestRetry_ProfileCreate_RejectsMissingName(t *testing.T) {
	r := setupRetryRouter(t, 0)
	resp := r.HandleTestMessage("retry profile create")
	if len(resp) == 0 || !strings.Contains(strings.ToLower(resp[0]), "usage") {
		t.Errorf("missing-name should explain usage, got %v", resp)
	}
}

func TestRetry_ProfileDelete_HitsParent(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/api/autonomous/retry_profiles/p1" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("retry profile delete p1")
	if calls != 1 {
		t.Errorf("expected 1 call to parent, got %d", calls)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "deleted") {
		t.Errorf("response should confirm deletion: %v", resp)
	}
}

func TestRetry_AutomatonSet_PutsPolicyProfileCeiling(t *testing.T) {
	captured := ""
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/autonomous/prds/prd1/set_retry_policy" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		captured = string(buf[:n])
		_, _ = w.Write([]byte(`{"id":"prd1","retry_profile":"release"}`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("retry automaton set prd1 profile=release policy=infra=4 ceiling=10")
	if !strings.Contains(captured, `"retry_profile":"release"`) {
		t.Errorf("body missing retry_profile: %q", captured)
	}
	if !strings.Contains(captured, `"infra":4`) {
		t.Errorf("body missing retry_policy: %q", captured)
	}
	if !strings.Contains(captured, `"retry_ceiling_total":10`) {
		t.Errorf("body missing retry_ceiling_total: %q", captured)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "prd1") {
		t.Errorf("response missing id: %v", resp)
	}
}

func TestRetry_AutomatonSet_RejectsMissingArgs(t *testing.T) {
	r := setupRetryRouter(t, 0)
	resp := r.HandleTestMessage("retry automaton set")
	if len(resp) == 0 || !strings.Contains(strings.ToLower(resp[0]), "usage") {
		t.Errorf("missing-args should explain usage, got %v", resp)
	}
}

func TestRetry_UnknownSubcommand(t *testing.T) {
	r := setupRetryRouter(t, 0)
	resp := r.HandleTestMessage("retry bogus")
	if len(resp) == 0 || !strings.Contains(resp[0], "unknown subcommand") {
		t.Errorf("expected unknown-subcommand message, got %v", resp)
	}
}
