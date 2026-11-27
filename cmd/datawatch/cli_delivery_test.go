package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/delivery"
)

type deliveryCLICall struct {
	method, path string
	body         map[string]any
}

// deliveryCLIServer points the CLI at a fake daemon that answers every
// delivery route; result is the report's result.
func deliveryCLIServer(t *testing.T, result string, status int) *[]deliveryCLICall {
	t.Helper()
	calls := &[]deliveryCLICall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := deliveryCLICall{method: r.Method, path: r.URL.RequestURI()}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			_ = json.Unmarshal(data, &call.body)
		}
		*calls = append(*calls, call)
		if status != 0 {
			http.Error(w, "the delivery check is not available", status)
			return
		}
		switch r.URL.Path {
		case "/api/delivery", "/api/delivery/check":
			_ = json.NewEncoder(w).Encode(delivery.Report{ReportOnly: true, Result: result, Pass: 3,
				Items: []delivery.Entry{{Key: "task:t1:failed", Invariant: "I3", Reason: "failed"}}})
		case "/api/delivery/test-message":
			_, _ = io.WriteString(w, `{"results":[{"channel":"signal","ok":true},{"channel":"ntfy","ok":false,"code":"send_failed"}]}`)
		default:
			_, _ = io.WriteString(w, `{"changed":2}`)
		}
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	// No real config is read.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DATAWATCH_TOKEN", "t")
	return calls
}

func runDeliveryCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDeliveryCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

func TestDeliveryCLIWiring(t *testing.T) {
	cmd := newDeliveryCmd()
	if cmd.Name() != "delivery" {
		t.Fatalf("name = %q", cmd.Name())
	}
	for _, n := range []string{"status", "check", "ack", "accept", "drop", "test-message"} {
		findSubCommand(t, cmd, n)
	}
}

func TestDeliveryCLICalls(t *testing.T) {
	cases := []struct {
		args         []string
		method, path string
		body         map[string]any
		out          []string
	}{
		{[]string{"status"}, "GET", "/api/delivery", nil, []string{"Delivery check: green", "task:t1:failed"}},
		{[]string{"check"}, "POST", "/api/delivery/check", nil, []string{"Delivery check: green", "Pass 3"}},
		{[]string{"ack", "--key", "task:t1:failed", "--key", "bug:b1:open"}, "POST", "/api/delivery/ack",
			map[string]any{"keys": []any{"task:t1:failed", "bug:b1:open"}, "actor": "cli"}, []string{"2", "seen"}},
		{[]string{"accept", "--kind", "automaton", "--state", "held", "--match-reason", "no_date", "--reason", "parked", "--until", "2026-12-01"},
			"POST", "/api/delivery/accept",
			map[string]any{"kind": "automaton", "state": "held", "match_reason": "no_date", "reason": "parked", "until": "2026-12-01", "actor": "cli"},
			[]string{"2", "accepted", "2026-12-01"}},
		{[]string{"drop", "--backlog", "--reason", "obsolete"}, "POST", "/api/delivery/drop",
			map[string]any{"backlog": true, "reason": "obsolete", "actor": "cli"}, []string{"2", "dropped"}},
		{[]string{"test-message"}, "POST", "/api/delivery/test-message", nil, []string{"signal: sent", "ntfy: failed (send_failed)"}},
	}
	for _, c := range cases {
		calls := deliveryCLIServer(t, delivery.ResultGreen, 0)
		out, err := runDeliveryCLI(t, c.args...)
		if err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if len(*calls) != 1 || (*calls)[0].method != c.method || (*calls)[0].path != c.path || !reflect.DeepEqual((*calls)[0].body, c.body) {
			t.Errorf("%v: calls = %+v", c.args, *calls)
		}
		for _, want := range c.out {
			if !strings.Contains(out, want) {
				t.Errorf("%v: output lacks %q: %q", c.args, want, out)
			}
		}
	}
}

// check exits non-zero when the result is red, with or without --json; status
// only shows it.
func TestDeliveryCLICheckExitsOnRed(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "--json"}} {
		deliveryCLIServer(t, delivery.ResultRed, 0)
		out, err := runDeliveryCLI(t, args...)
		if err == nil || !strings.Contains(err.Error(), "red") {
			t.Errorf("%v on red: err = %v, want an error", args, err)
		}
		if !strings.Contains(out, "task:t1:failed") {
			t.Errorf("%v on red printed no report: %q", args, out)
		}
	}
	// Anything but green fails: an answer with no result is not a pass.
	deliveryCLIServer(t, "", 0)
	if _, err := runDeliveryCLI(t, "check"); err == nil {
		t.Error("check with no result must not exit 0")
	}
	deliveryCLIServer(t, delivery.ResultRed, 0)
	if _, err := runDeliveryCLI(t, "status"); err != nil {
		t.Errorf("status on red: %v", err)
	}
}

func TestDeliveryCLIJSON(t *testing.T) {
	deliveryCLIServer(t, delivery.ResultGreen, 0)
	for _, args := range [][]string{{"status", "--json"}, {"check", "--json"}, {"ack", "--backlog", "--json"}, {"test-message", "--json"}} {
		out, err := runDeliveryCLI(t, args...)
		var v map[string]any
		if err != nil || json.Unmarshal([]byte(out), &v) != nil {
			t.Errorf("%v: err=%v out=%q, want JSON", args, err, out)
		}
	}
}

// Nothing is sent for a command that names no entries, lacks a required flag or
// carries a stray word: a dropped word would widen what the action applies to.
func TestDeliveryCLIRefusals(t *testing.T) {
	for _, args := range [][]string{
		{"ack"}, {"ack", "task:t1:failed", "--kind", "task"},
		{"accept", "--backlog", "--reason", "parked"}, {"accept", "--backlog", "--until", "2026-12-01"},
		{"accept", "--reason", "parked", "--until", "2026-12-01"},
		{"drop", "--backlog"}, {"drop", "--reason", "obsolete"},
		{"drop", "--backlog", "--reason", "obsolete", "extra"},
		{"ack", "--key", " ", "--kind", "task"},
		{"ack", "--backlog", "--reason", "x"}, {"drop", "--backlog", "--reason", "x", "--until", "2026-12-01"},
		{"status", "now"}, {"check", "now"}, {"test-message", "now"},
	} {
		calls := deliveryCLIServer(t, delivery.ResultGreen, 0)
		if _, err := runDeliveryCLI(t, args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
		if len(*calls) != 0 {
			t.Errorf("%v reached the daemon: %+v", args, *calls)
		}
	}
}

func TestDeliveryCLIDaemonError(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"check"}, {"ack", "--backlog"}, {"test-message"}} {
		deliveryCLIServer(t, "", http.StatusServiceUnavailable)
		_, err := runDeliveryCLI(t, args...)
		if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "the delivery check is not available") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}
