// Comm-channel "automation list" and "automation get <id>" verbs for Phase 3a
// fork/join and nested Automations. They are read-only and proxy GET requests
// to the parent daemon; activate's routing is covered by automation_cmd_test.go.

package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestAutomationCmdListAndGetVerbs(t *testing.T) {
	var calls []string
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/automations":
			_, _ = w.Write([]byte(`[{"id":"ab12","name":"fj","status":"active","active_cursors":["api","n1"]}]`))
		case "/api/automations/ab12":
			_, _ = w.Write([]byte(`{"id":"ab12","name":"fj","status":"active","active_cursors":["api","n1"],` +
				`"members":[{"id":"join1","kind":"join","join_members":["api","n1"]}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := setupRetryRouter(t, port)

	resp := r.HandleTestMessage("automation list")
	if len(resp) == 0 || !strings.Contains(resp[0], `"fj"`) || !strings.Contains(resp[0], `"api","n1"`) {
		t.Errorf("list response = %v", resp)
	}

	resp = r.HandleTestMessage("automation get ab12")
	if len(resp) == 0 || !strings.Contains(resp[0], `"join_members"`) {
		t.Errorf("get response = %v", resp)
	}

	want := []string{"GET /api/automations", "GET /api/automations/ab12"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("parent calls = %v, want %v", calls, want)
	}
}

func TestAutomationCmdListGetUsageAndErrors(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/automations/nope":
			http.Error(w, "not found", http.StatusNotFound)
		case "/api/automations":
			http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	})
	r := setupRetryRouter(t, port)

	if resp := r.HandleTestMessage("automation get"); len(resp) == 0 || resp[0] != "usage: automation get <id>" {
		t.Errorf("missing-id get response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation"); len(resp) == 0 || !strings.Contains(resp[0], "usage:") ||
		!strings.Contains(resp[0], "list") || !strings.Contains(resp[0], "get <id>") {
		t.Errorf("bare automation response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation nope"); len(resp) == 0 || !strings.Contains(resp[0], "Try: activate, list, get") {
		t.Errorf("unknown-subcommand response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation get nope"); len(resp) == 0 ||
		!strings.HasPrefix(resp[0], "automation get error:") || !strings.Contains(resp[0], "HTTP 404") {
		t.Errorf("get not-found response = %v", resp)
	}
	if resp := r.HandleTestMessage("automation list"); len(resp) == 0 ||
		!strings.HasPrefix(resp[0], "automation list error:") || !strings.Contains(resp[0], "HTTP 503") {
		t.Errorf("list error response = %v", resp)
	}

	// Without a REST loopback the verbs report the error instead of panicking.
	offline := setupRetryRouter(t, 0)
	if resp := offline.HandleTestMessage("automation list"); len(resp) == 0 ||
		!strings.Contains(resp[0], "REST loopback not configured") {
		t.Errorf("no-loopback list response = %v", resp)
	}
}
