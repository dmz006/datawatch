package router

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const commBudgetReply = `{"id":"ab12","name":"nightly","status":"active","version":8,"members":[{"id":"m2"},{"id":"m3"}],
"budget":{"scope":"automation","ceiling_type":"dollar","ceiling_usd":40,"spent_usd":12.4,"remaining_usd":27.6,
"window":"week","window_start":"2026-10-19T00:00:00Z","window_end":"2026-10-26T00:00:00Z","on_breach":"pause","pacing":"none",
"ceiling_reached":false,"reset_at":"2026-10-26T00:00:00Z","reset_known":true,"reset_source":"window",
"allocation_estimate":{"allocation_pct":25,"estimated":true,"allocation_usd":40,"plan_tier":"max_5x"},
"peers":[{"peer":"ci-box","spend_usd":3.2,"counted":true}]}}`

func TestAutomationSetBudgetComm(t *testing.T) {
	var calls []controlCall
	replyBody := commBudgetReply
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := controlCall{method: r.Method, path: r.URL.Path}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &c.body); err != nil {
				t.Errorf("body %q is not JSON: %v", raw, err)
			}
		}
		calls = append(calls, c)
		_, _ = w.Write([]byte(replyBody))
	})
	r := setupRetryRouter(t, port)

	cases := []struct {
		name, in string
		want     map[string]any
	}{
		{"documented example", "automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week on_breach=pause expected_version=7",
			map[string]any{"actor": "comm", "ceiling_type": "dollar", "ceiling_value": float64(40), "window": "week", "on_breach": "pause", "expected_version": float64(7)}},
		{"every argument", "automation set-budget ab12cd34 member=m2 ceiling_type=dollar ceiling_value=5.5 window=week pacing=even on_breach=block " +
			"backend=claude-code allocation_estimate=true peer_window=ci-box,laptop reset_time=06:00 reset_weekday=monday " +
			"reset_zone=America/New_York estimated_cost_usd=2.5 expected_version=3",
			map[string]any{"actor": "comm", "member_id": "m2", "ceiling_type": "dollar", "ceiling_value": 5.5, "window": "week", "pacing": "even",
				"on_breach": "block", "backend": "claude-code", "allocation_estimate": true, "peer_window": []any{"ci-box", "laptop"},
				"reset_time": "06:00", "reset_weekday": "monday", "reset_zone": "America/New_York", "estimated_cost_usd": 2.5, "expected_version": float64(3)}},
		{"month anchor", "automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=100 window=month reset_day=15",
			map[string]any{"actor": "comm", "ceiling_type": "dollar", "ceiling_value": float64(100), "window": "month", "reset_day": float64(15)}},
		{"member_id spelling", "automation set-budget ab12cd34 member_id=m2 estimated_cost_usd=0",
			map[string]any{"actor": "comm", "member_id": "m2", "estimated_cost_usd": float64(0)}},
		{"clear", "automation set-budget ab12cd34 clear=true expected_version=8",
			map[string]any{"actor": "comm", "clear": true, "expected_version": float64(8)}},
		{"clear a member", "automation set-budget ab12cd34 clear=true member=m2",
			map[string]any{"actor": "comm", "clear": true, "member_id": "m2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls = nil
			resp := r.HandleTestMessage(tc.in)
			if len(calls) != 1 {
				t.Fatalf("%q made %d calls, want 1 (reply %v)", tc.in, len(calls), resp)
			}
			got := calls[0]
			if got.method != http.MethodPost || got.path != "/api/automations/ab12cd34/set_budget" {
				t.Errorf("request = %s %s, want POST /api/automations/ab12cd34/set_budget", got.method, got.path)
			}
			if !reflect.DeepEqual(got.body, tc.want) {
				t.Errorf("body = %v, want %v", got.body, tc.want)
			}
			if len(resp) == 0 {
				t.Fatal("no reply")
			}
			for _, want := range []string{
				"automation ab12 (nightly): status active, version 8, 2 members",
				"budget (automation)",
				"spent: $12.40 of $40.00 ceiling (week window)",
				"allocation (estimated): ~25% of $40.00 (max_5x)",
				"peer ci-box: counted, $3.20",
			} {
				if !strings.Contains(resp[0], want) {
					t.Errorf("reply lacks %q:\n%s", want, resp[0])
				}
			}
		})
	}

	t.Run("a refusal carries the daemon message", func(t *testing.T) {
		replyBody = commBudgetReply
		_, port := newFakePeerParent(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "409: version mismatch: expected version 7, current version is 9", http.StatusConflict)
		})
		resp := setupRetryRouter(t, port).HandleTestMessage("automation set-budget ab12cd34 clear=true expected_version=7")
		if len(resp) == 0 || !strings.Contains(resp[0], "409") || !strings.Contains(resp[0], "current version is 9") {
			t.Errorf("reply = %v", resp)
		}
	})
}

func TestAutomationSetBudgetCommRejectsBadInput(t *testing.T) {
	var calls int
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(commBudgetReply))
	})
	r := setupRetryRouter(t, port)

	inputs := []string{
		"automation set-budget",
		"automation set-budget ab12cd34",
		"automation set-budget ab12cd34 ceiling_type",
		"automation set-budget ab12cd34 ceiling_type=",
		"automation set-budget ab12cd34 bogus=1",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_type=dollar ceiling_value=40 window=week",
		"automation set-budget ab12cd34 ceiling_type=euro ceiling_value=40 window=week",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=forty window=week",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=0 window=week",
		"automation set-budget ab12cd34 ceiling_type=dollar window=week",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=year",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week pacing=fast",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week on_breach=explode",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week allocation_estimate=maybe",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week expected_version=seven",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=month reset_day=fifteenth",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week peer_window=ci-box,,laptop",
		"automation set-budget ab12cd34 ceiling_type=dollar ceiling_value=40 window=week peer_window=ci-box,ci-box",
		"automation set-budget ab12cd34 clear=true ceiling_type=dollar",
		"automation set-budget ab12cd34 clear=maybe",
		"automation set-budget ab12cd34 estimated_cost_usd=2",
		"automation set-budget ab12cd34 member=m2 estimated_cost_usd=-1",
		"automation set-budget ab12cd34 clear=false",
	}
	for _, in := range inputs {
		before := calls
		resp := r.HandleTestMessage(in)
		if calls != before {
			t.Errorf("%q reached the daemon", in)
		}
		if len(resp) == 0 || !strings.Contains(resp[0], "usage: automation set-budget") {
			t.Errorf("%q reply = %v, want the usage message", in, resp)
			continue
		}
		// The first line names the problem (when the input had one) and the
		// usage message is a single line.
		lines := strings.Split(strings.TrimSpace(resp[0]), "\n")
		if last := lines[len(lines)-1]; !strings.HasPrefix(last, "usage: automation set-budget") {
			t.Errorf("%q: usage must be the last line, got %q", in, last)
		}
		if len(lines) > 2 {
			t.Errorf("%q: reply is %d lines, want a problem line and one usage line", in, len(lines))
		}
	}
}

func TestAutomationStatusCommShowsBudget(t *testing.T) {
	var gotMethod, gotPath string
	reply := commBudgetReply
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(reply))
	})
	r := setupRetryRouter(t, port)

	resp := r.HandleTestMessage("automation status ab12")
	if gotMethod != http.MethodGet || gotPath != "/api/automations/ab12" {
		t.Errorf("request = %s %s, want GET /api/automations/ab12", gotMethod, gotPath)
	}
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	for _, want := range []string{
		"automation ab12 (nightly): status active, version 8, 2 members",
		"budget (automation)",
		"spent: $12.40 of $40.00 ceiling (week window)",
		"reset: 2026-10-26T00:00:00Z (window)",
		"on breach: pauses and resumes by itself at the reset",
		"allocation (estimated): ~25% of $40.00 (max_5x)",
		"peer ci-box: counted, $3.20",
	} {
		if !strings.Contains(resp[0], want) {
			t.Errorf("status lacks %q:\n%s", want, resp[0])
		}
	}

	reply = `{"id":"ab12","name":"nightly","status":"paused","version":9,"members":[]}`
	resp = r.HandleTestMessage("automation status ab12")
	if len(resp) == 0 || !strings.Contains(resp[0], "status paused, version 9, 0 members") || !strings.Contains(resp[0], "budget: none set") {
		t.Errorf("status without a budget = %v", resp)
	}

	for _, in := range []string{"automation status", "automation status ab12 extra"} {
		if resp := r.HandleTestMessage(in); len(resp) == 0 || !strings.Contains(resp[0], "usage: automation status <id>") {
			t.Errorf("%q reply = %v", in, resp)
		}
	}

	t.Run("help lists the new verbs", func(t *testing.T) {
		resp := r.HandleTestMessage("automation")
		if len(resp) == 0 || !strings.Contains(resp[0], "status <id>") || !strings.Contains(resp[0], "set-budget <id>") {
			t.Errorf("help = %v", resp)
		}
	})
}
