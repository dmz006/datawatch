package router

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// holdDaemonCall is one request the fake daemon saw.
type holdDaemonCall struct {
	method, path string
	body         map[string]any
}

// holdVerbRouter returns a router wired to a fake daemon that answers every
// request with status and reply, and the list of requests it saw.
func holdVerbRouter(t *testing.T, status int, reply string) (*Router, *[]holdDaemonCall) {
	t.Helper()
	calls := &[]holdDaemonCall{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, req *http.Request) {
		c := holdDaemonCall{method: req.Method, path: req.URL.Path}
		if b, _ := io.ReadAll(req.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &c.body); err != nil {
				t.Errorf("request body is not JSON: %q", b)
			}
		}
		*calls = append(*calls, c)
		if status != http.StatusOK {
			http.Error(w, reply, status)
			return
		}
		_, _ = w.Write([]byte(reply))
	})
	return setupRetryRouter(t, port), calls
}

func TestHoldVerbsParseAndReply(t *testing.T) {
	t.Run("hold reads the reason from the words left after until and condition", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{"id":"aaaa0001","status":"held","title":"Zeta release"}`)
		out := joinReplies(r.HandleTestMessage(`autonomous hold aaaa0001 wait for the release until=72h condition="until after v10" a=b`))
		if strings.Contains(out, "failed") || !strings.Contains(out, "Zeta release") {
			t.Fatalf("reply should carry the daemon's answer, got %q", out)
		}
		if len(*calls) != 1 {
			t.Fatalf("daemon calls = %d, want 1", len(*calls))
		}
		c := (*calls)[0]
		if c.method != http.MethodPost || c.path != "/api/autonomous/prds/aaaa0001/hold" {
			t.Fatalf("proxied %s %s", c.method, c.path)
		}
		want := map[string]any{"reason": "wait for the release a=b", "until": "72h", "condition": "until after v10", "actor": "operator"}
		if !reflect.DeepEqual(c.body, want) {
			t.Errorf("body = %v, want %v", c.body, want)
		}
	})

	t.Run("hold with a reason only, and the tokens in any order", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{}`)
		r.HandleTestMessage("autonomous hold aaaa0001 parked for now")
		r.HandleTestMessage("autonomous hold aaaa0001 until=2026-12-01 condition=release parked for now")
		want := []map[string]any{
			{"reason": "parked for now", "actor": "operator"},
			{"reason": "parked for now", "until": "2026-12-01", "condition": "release", "actor": "operator"},
		}
		if len(*calls) != 2 {
			t.Fatalf("daemon calls = %d, want 2", len(*calls))
		}
		for i, w := range want {
			if !reflect.DeepEqual((*calls)[i].body, w) {
				t.Errorf("call %d body = %v, want %v", i, (*calls)[i].body, w)
			}
		}
	})

	t.Run("hold without a reason answers with the usage and sends nothing", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{}`)
		for _, msg := range []string{
			"autonomous hold",
			"autonomous hold aaaa0001",
			`autonomous hold aaaa0001 condition="open`, // unclosed quote
			"autonomous hold aaaa0001 until=",          // empty value
		} {
			out := joinReplies(r.HandleTestMessage(msg))
			if !strings.Contains(out, "usage") || !strings.Contains(out, "autonomous hold") || !strings.Contains(out, "failed") {
				t.Errorf("%q: want the usage, got %q", msg, out)
			}
		}
		if len(*calls) != 0 {
			t.Errorf("bad input made %d daemon call(s); want 0", len(*calls))
		}
	})

	t.Run("hold with only a new until is sent so an existing hold can be extended", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{"status":"held"}`)
		r.HandleTestMessage("autonomous hold aaaa0001 until=2026-12-01")
		if len(*calls) != 1 || (*calls)[0].body["reason"] != "" || (*calls)[0].body["until"] != "2026-12-01" {
			t.Fatalf("calls = %+v", *calls)
		}
	})

	t.Run("hold of an unknown id replies with the daemon's refusal", func(t *testing.T) {
		r, _ := holdVerbRouter(t, http.StatusBadRequest, "no such Automaton: zzzz0000")
		out := joinReplies(r.HandleTestMessage("autonomous hold zzzz0000 parked"))
		if !strings.Contains(out, "failed") || !strings.Contains(out, "no such Automaton: zzzz0000") {
			t.Errorf("reply = %q; want the daemon's refusal", out)
		}
	})

	t.Run("release-hold sends the note and needs an id", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{"id":"aaaa0001","status":"blocked"}`)
		out := joinReplies(r.HandleTestMessage("autonomous release-hold aaaa0001 the release shipped"))
		if strings.Contains(out, "failed") {
			t.Fatalf("reply = %q", out)
		}
		c := (*calls)[0]
		if c.method != http.MethodPost || c.path != "/api/autonomous/prds/aaaa0001/release_hold" {
			t.Fatalf("proxied %s %s", c.method, c.path)
		}
		if !reflect.DeepEqual(c.body, map[string]any{"note": "the release shipped", "actor": "operator"}) {
			t.Errorf("body = %v", c.body)
		}
		before := len(*calls)
		out = joinReplies(r.HandleTestMessage("autonomous release-hold"))
		if !strings.Contains(out, "usage") || len(*calls) != before {
			t.Errorf("missing id: reply %q, calls %d -> %d; want usage and no call", out, before, len(*calls))
		}
	})

	t.Run("release-hold of an Automaton that is not held replies with the refusal", func(t *testing.T) {
		r, _ := holdVerbRouter(t, http.StatusBadRequest, "Automaton aaaa0001 is not held")
		out := joinReplies(r.HandleTestMessage("autonomous release-hold aaaa0001"))
		if !strings.Contains(out, "failed") || !strings.Contains(out, "is not held") {
			t.Errorf("reply = %q", out)
		}
	})

	t.Run("held lists the group", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{"count":1,"items":[{"id":"aaaa0001","title":"Zeta release","reason":"after v10"}]}`)
		out := joinReplies(r.HandleTestMessage("autonomous held"))
		if strings.Contains(out, "failed") || !strings.Contains(out, "Zeta release") || !strings.Contains(out, "after v10") {
			t.Fatalf("reply = %q; want the held group", out)
		}
		c := (*calls)[0]
		if c.method != http.MethodGet || c.path != "/api/autonomous/held" || c.body != nil {
			t.Errorf("proxied %+v", c)
		}
		out = joinReplies(r.HandleTestMessage("autonomous held please"))
		if !strings.Contains(out, "usage") || len(*calls) != 1 {
			t.Errorf("extra words: reply %q, calls %d; want usage and no new call", out, len(*calls))
		}
	})

	t.Run("held-migrate dry run lists and sends no ids", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{"dry_run":true,"candidates":[{"id":"aaaa0001","title":"Eval matrix","rule":"eval_measurement"}]}`)
		out := joinReplies(r.HandleTestMessage("autonomous held-migrate dry_run=true"))
		if strings.Contains(out, "failed") || !strings.Contains(out, "Eval matrix") {
			t.Fatalf("reply = %q; want the candidate list", out)
		}
		c := (*calls)[0]
		if c.method != http.MethodPost || c.path != "/api/autonomous/held/migrate" {
			t.Fatalf("proxied %s %s", c.method, c.path)
		}
		if !reflect.DeepEqual(c.body, map[string]any{"dry_run": true, "actor": "operator"}) {
			t.Errorf("body = %v", c.body)
		}
		r.HandleTestMessage("autonomous held-migrate")
		if _, sent := (*calls)[1].body["dry_run"]; sent {
			t.Errorf("dry_run must be left out when not given: %v", (*calls)[1].body)
		}
		r.HandleTestMessage("autonomous held-migrate dry_run=false id=aaaa0001 reason=x")
		if v, sent := (*calls)[2].body["dry_run"]; !sent || v != false {
			t.Errorf("dry_run=false must be sent as false: %v", (*calls)[2].body)
		}
	})

	t.Run("held-migrate real run reads repeated ids and reason words", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{"dry_run":false,"held":2}`)
		out := joinReplies(r.HandleTestMessage(`autonomous held-migrate id=aaaa0001 id=bbbb0002,cccc0003 reason=parked until after the release until=72h condition="after v10"`))
		if strings.Contains(out, "failed") {
			t.Fatalf("reply = %q", out)
		}
		want := map[string]any{
			"ids": []any{"aaaa0001", "bbbb0002", "cccc0003"}, "reason": "parked until after the release",
			"until": "72h", "condition": "after v10", "actor": "operator",
		}
		if !reflect.DeepEqual((*calls)[0].body, want) {
			t.Errorf("body = %v, want %v", (*calls)[0].body, want)
		}
	})

	t.Run("held-migrate answers bad input with the usage and sends nothing", func(t *testing.T) {
		r, calls := holdVerbRouter(t, http.StatusOK, `{}`)
		for _, msg := range []string{
			"autonomous held-migrate please",
			"autonomous held-migrate dry_run=maybe",
			"autonomous held-migrate bogus=1",
			"autonomous held-migrate id=",
			`autonomous held-migrate id=a reason="unclosed`,
		} {
			out := joinReplies(r.HandleTestMessage(msg))
			if !strings.Contains(out, "usage") || !strings.Contains(out, "held-migrate") {
				t.Errorf("%q: want the usage, got %q", msg, out)
			}
		}
		if len(*calls) != 0 {
			t.Errorf("bad input made %d daemon call(s); want 0", len(*calls))
		}
	})

	t.Run("held-migrate busy refusal replies with the daemon's plain message", func(t *testing.T) {
		r, _ := holdVerbRouter(t, http.StatusConflict, "held migration: a final gate is running")
		out := joinReplies(r.HandleTestMessage("autonomous held-migrate id=a reason=r"))
		if !strings.Contains(out, "a final gate is running") || strings.Contains(out, "HTTP 409") {
			t.Errorf("busy reply = %q", out)
		}
	})

	t.Run("the verbs are listed in the autonomous help", func(t *testing.T) {
		r, _ := holdVerbRouter(t, http.StatusOK, `{}`)
		out := joinReplies(r.HandleTestMessage("autonomous"))
		for _, verb := range []string{"hold <id>", "release-hold <id>", "held|", "held-migrate"} {
			if !strings.Contains(out, verb) {
				t.Errorf("autonomous help lacks %q", verb)
			}
		}
		help := joinReplies(r.HandleTestMessage("help"))
		for _, verb := range []string{"autonomous hold <id>", "autonomous release-hold <id>", "autonomous held ", "autonomous held-migrate"} {
			if !strings.Contains(help, verb) {
				t.Errorf("help text lacks %q", verb)
			}
		}
	})
}
