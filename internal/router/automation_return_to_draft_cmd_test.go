// Comm-channel coverage for "automation return-to-draft <id>": parse, parent
// dispatch with the comm actor, and the usage/conflict refusals.
package router

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationReturnToDraftParse(t *testing.T) {
	cmd := Parse("automation return-to-draft ab12")
	if cmd.Type != CmdAutomation || cmd.Text != "return-to-draft ab12" {
		t.Fatalf("Parse = %+v", cmd)
	}
	found := false
	for _, v := range AutomationCommVerbs() {
		if v == "return-to-draft" {
			found = true
		}
	}
	if !found {
		t.Errorf("AutomationCommVerbs = %v, missing return-to-draft", AutomationCommVerbs())
	}
}

func TestAutomationReturnToDraftHitsParent(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/automations/ab12/return_to_draft" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(string(body), `"actor":"comm"`) {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"id":"ab12","status":"draft","version":4}`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("automation return-to-draft ab12")
	if calls != 1 {
		t.Fatalf("expected 1 call to parent, got %d", calls)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], `"draft"`) {
		t.Errorf("response = %v", resp)
	}
}

func TestAutomationReturnToDraftUsageAndErrors(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "automation ab12 is active; only a blocked automation with nothing started can go back to draft", http.StatusConflict)
	})
	r := setupRetryRouter(t, port)
	if resp := r.HandleTestMessage("automation return-to-draft"); len(resp) == 0 || !strings.Contains(resp[0], "usage:") {
		t.Errorf("missing-id response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation return-to-draft ab12"); len(resp) == 0 || !strings.Contains(resp[0], "only a blocked") {
		t.Errorf("conflict response = %v", resp)
	}
}
