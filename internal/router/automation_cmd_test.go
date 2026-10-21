package router

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationActivateParse(t *testing.T) {
	cmd := Parse("automation activate ab12")
	if cmd.Type != CmdAutomation || cmd.Text != "activate ab12" {
		t.Fatalf("Parse = %+v", cmd)
	}
	if bare := Parse("automation"); bare.Type != CmdAutomation || bare.Text != "" {
		t.Fatalf("bare Parse = %+v", bare)
	}
}

func TestAutomationActivateHitsParent(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/automations/ab12/activate" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(string(body), `"actor":"comm"`) {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"id":"ab12","status":"active"}`))
	})
	r := setupRetryRouter(t, port)
	resp := r.HandleTestMessage("automation activate ab12")
	if calls != 1 {
		t.Fatalf("expected 1 call to parent, got %d", calls)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], `"active"`) {
		t.Errorf("response = %v", resp)
	}
}

func TestAutomationActivateUsageAndErrors(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "automation ab12 is active, only a draft can be activated", http.StatusConflict)
	})
	r := setupRetryRouter(t, port)
	if resp := r.HandleTestMessage("automation activate"); len(resp) == 0 || !strings.Contains(resp[0], "usage:") {
		t.Errorf("missing-id response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation nope"); len(resp) == 0 || !strings.Contains(resp[0], "unknown subcommand") {
		t.Errorf("unknown-subcommand response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation activate ab12"); len(resp) == 0 || !strings.Contains(resp[0], "only a draft") {
		t.Errorf("conflict response = %v", resp)
	}
}

func TestConfigureHelpListsAutomationIdleAlert(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, in := range []string{"configure help", "configure list"} {
		resp := r.HandleTestMessage(in)
		if len(resp) == 0 || !strings.Contains(resp[0], "autonomous.automation.idle_alert_minutes") {
			t.Errorf("%q output lacks the key: %v", in, resp)
		}
	}
}
