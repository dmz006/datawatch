package router

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// controlCall is one request the fake daemon saw.
type controlCall struct {
	method, path string
	body         map[string]any
}

func TestAutomationCommControl(t *testing.T) {
	var calls []controlCall
	replyCode := http.StatusOK
	replyBody := `{"id":"ab12","status":"paused","version":7}`
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := controlCall{method: r.Method, path: r.URL.Path}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &c.body); err != nil {
				t.Errorf("body %q is not JSON: %v", raw, err)
			}
		}
		calls = append(calls, c)
		w.WriteHeader(replyCode)
		_, _ = w.Write([]byte(replyBody))
	})
	r := setupRetryRouter(t, port)

	cases := []struct {
		name, in, path string
		want           map[string]any
	}{
		{"pause", "automation pause ab12", "/api/automations/ab12/pause", map[string]any{"actor": "comm"}},
		{"pause with reason", "automation pause ab12 waiting on review", "/api/automations/ab12/pause",
			map[string]any{"actor": "comm", "reason": "waiting on review"}},
		{"resume", "automation resume ab12 review done", "/api/automations/ab12/resume",
			map[string]any{"actor": "comm", "reason": "review done"}},
		{"cancel in-flight true", "automation cancel ab12 in-flight=true", "/api/automations/ab12/cancel",
			map[string]any{"actor": "comm", "cancel_in_flight": true}},
		{"cancel in-flight false", "automation cancel ab12 in-flight=false", "/api/automations/ab12/cancel",
			map[string]any{"actor": "comm", "cancel_in_flight": false}},
		{"skip default edge", "automation skip ab12 m2", "/api/automations/ab12/skip_member",
			map[string]any{"actor": "comm", "member_id": "m2"}},
		{"skip with edge", "automation skip ab12 m2 on_failed", "/api/automations/ab12/skip_member",
			map[string]any{"actor": "comm", "member_id": "m2", "edge": "on_failed"}},
		{"insert", "automation insert ab12 prd-9 fix the flaky step", "/api/automations/ab12/insert_member",
			map[string]any{"actor": "comm", "member": map[string]any{"prd_id": "prd-9"}, "urgent": false, "reason": "fix the flaky step"}},
		{"insert urgent", "automation insert ab12 prd-9 urgent security fix", "/api/automations/ab12/insert_member",
			map[string]any{"actor": "comm", "member": map[string]any{"prd_id": "prd-9"}, "urgent": true, "reason": "security fix"}},
		{"remove", "automation remove ab12 m3", "/api/automations/ab12/remove_member",
			map[string]any{"actor": "comm", "member_id": "m3"}},
		{"edges", "automation edges ab12 m2 on_complete=m4 on_failed=m5", "/api/automations/ab12/reorder_edges",
			map[string]any{"actor": "comm", "member_id": "m2", "on_complete": "m4", "on_blocked": "", "on_failed": "m5"}},
		{"approve", "automation approve ab12 m6", "/api/automations/ab12/approve_insert",
			map[string]any{"actor": "comm", "member_id": "m6"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls = nil
			resp := r.HandleTestMessage(tc.in)
			if len(calls) != 1 {
				t.Fatalf("%q made %d calls, want 1 (reply %v)", tc.in, len(calls), resp)
			}
			got := calls[0]
			if got.method != http.MethodPost || got.path != tc.path {
				t.Errorf("request = %s %s, want POST %s", got.method, got.path, tc.path)
			}
			gotJSON, _ := json.Marshal(got.body)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("body = %s, want %s", gotJSON, wantJSON)
			}
			if len(resp) == 0 || !strings.Contains(resp[0], "automation ab12") || !strings.Contains(resp[0], "version 7") {
				t.Errorf("confirmation = %v", resp)
			}
		})
	}

	t.Run("cancel needs an in-flight choice", func(t *testing.T) {
		for _, in := range []string{
			"automation cancel ab12",
			"automation cancel ab12 in-flight=maybe",
			"automation cancel ab12 true",
			"automation cancel ab12 in-flight=true extra",
		} {
			calls = nil
			resp := r.HandleTestMessage(in)
			if len(calls) != 0 {
				t.Errorf("%q reached the daemon: %+v", in, calls)
			}
			if len(resp) == 0 || !strings.Contains(resp[0], "in-flight=true") || !strings.Contains(resp[0], "in-flight=false") {
				t.Errorf("%q reply = %v, want both in-flight examples", in, resp)
			}
		}
	})

	t.Run("usage errors never reach the daemon", func(t *testing.T) {
		for _, in := range []string{
			"automation pause", "automation resume",
			"automation skip ab12", "automation skip ab12 m2 sideways",
			"automation insert ab12", "automation insert ab12 prd-9", "automation insert ab12 prd-9 urgent",
			"automation remove ab12", "automation approve ab12",
			"automation edges ab12 m2", "automation edges ab12 m2 on_complete", "automation edges ab12 m2 bogus=m4",
		} {
			calls = nil
			resp := r.HandleTestMessage(in)
			if len(calls) != 0 {
				t.Errorf("%q reached the daemon: %+v", in, calls)
			}
			if len(resp) == 0 || !strings.Contains(resp[0], "usage:") {
				t.Errorf("%q reply = %v, want a usage message", in, resp)
			}
		}
	})

	t.Run("conflict shows the server message", func(t *testing.T) {
		replyCode = http.StatusConflict
		replyBody = "automation ab12 is at version 9, expected 7"
		defer func() {
			replyCode = http.StatusOK
			replyBody = `{"id":"ab12","status":"paused","version":7}`
		}()
		resp := r.HandleTestMessage("automation pause ab12")
		if len(resp) == 0 || !strings.Contains(resp[0], "409") || !strings.Contains(resp[0], "is at version 9, expected 7") {
			t.Errorf("conflict reply = %v", resp)
		}
	})
}
