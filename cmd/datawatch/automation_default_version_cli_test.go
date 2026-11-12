// CLI default-version tests for `automation` controls: when the operator
// omits --expected-version the CLI reads the Automation's current version
// with GET /api/automations/<id>, sends it as expected_version, and retries
// exactly once after a 409 version mismatch by re-reading the version.
// Each test serves the daemon side with an httptest server that records
// every request (method, path, body) in order, overriding the package-level
// serverURL (same pattern as cli_autonomous_subscriptions_test.go).
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// autoVersionReq is one recorded request, in arrival order.
type autoVersionReq struct {
	Method string
	Path   string
	Body   []byte
}

// autoVersionReply is a canned response for one POST number (1-based).
type autoVersionReply struct {
	code int
	body string
}

// autoVersionFake is a fake automation daemon: it records every request in
// order, answers GET /api/automations/ab12 with the Automation record at
// its current version, and answers POST /api/automations/ab12/<op> with a
// per-POST canned reply when postReply provides one, else 200 with the same
// record shape.
type autoVersionFake struct {
	version    int64
	reqs       []autoVersionReq
	postReply  func(n int) (autoVersionReply, bool) // canned reply for the n-th POST
	afterReply func(n int)                          // hook after a canned reply
}

func (f *autoVersionFake) automationJSON() []byte {
	return []byte(`{"id":"ab12","name":"chain","status":"active","version":` +
		strconv.FormatInt(f.version, 10) + `,"members":[]}`)
}

// handler is the httptest server handler.
func (f *autoVersionFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		if r.Body != nil {
			raw, _ = io.ReadAll(r.Body)
		}
		f.reqs = append(f.reqs, autoVersionReq{Method: r.Method, Path: r.URL.Path, Body: raw})
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			_, _ = w.Write(f.automationJSON())
			return
		}

		postNumber := 0
		for _, req := range f.reqs {
			if req.Method == http.MethodPost {
				postNumber++
			}
		}
		if f.postReply != nil {
			if rep, ok := f.postReply(postNumber); ok {
				if rep.code != 0 {
					w.WriteHeader(rep.code)
				}
				_, _ = w.Write([]byte(rep.body))
				if f.afterReply != nil {
					f.afterReply(postNumber)
				}
				return
			}
		}
		_, _ = w.Write(f.automationJSON())
	}
}

// withAutoVersionFake points the CLI at the fake daemon via the package
// serverURL and restores it after the test (same pattern as withCaptureServer).
func withAutoVersionFake(t *testing.T, f *autoVersionFake) {
	t.Helper()
	old := serverURL
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
}

// autoFakeBody decodes the recorded body of request i (0-based) as JSON.
func autoFakeBody(t *testing.T, f *autoVersionFake, i int) map[string]any {
	t.Helper()
	if i >= len(f.reqs) {
		t.Fatalf("no recorded request %d (got %d requests)", i, len(f.reqs))
	}
	var m map[string]any
	if err := json.Unmarshal(f.reqs[i].Body, &m); err != nil {
		t.Fatalf("request %d body %q is not JSON: %v", i, f.reqs[i].Body, err)
	}
	return m
}

// autoFakeExpectedVersion returns the expected_version of request i as an
// int, failing the test when it is absent or not a whole number.
func autoFakeExpectedVersion(t *testing.T, f *autoVersionFake, i int) int64 {
	t.Helper()
	v, ok := autoFakeBody(t, f, i)["expected_version"].(float64)
	if !ok || v != float64(int64(v)) {
		t.Fatalf("request %d expected_version = %v, want a whole number", i, v)
	}
	return int64(v)
}

// TestAutomationCLIDefaultVersionReadsCurrent covers the implicit-version
// path for every control verb: with --expected-version omitted the CLI
// first does GET /api/automations/ab12 and then POSTs the control with
// expected_version read from that GET (3), in that order.
func TestAutomationCLIDefaultVersionReadsCurrent(t *testing.T) {
	ops := map[string]string{
		"insert":  "insert_member",
		"pause":   "pause",
		"resume":  "resume",
		"skip":    "skip_member",
		"remove":  "remove_member",
		"edges":   "reorder_edges",
		"approve": "approve_insert",
		"cancel":  "cancel",
	}
	cases := []struct {
		verb string
		args []string
	}{
		{"insert", []string{"insert", "ab12", "prd-1", "--reason", "r"}},
		{"pause", []string{"pause", "ab12"}},
		{"resume", []string{"resume", "ab12"}},
		{"skip", []string{"skip", "ab12", "m1"}},
		{"remove", []string{"remove", "ab12", "m1"}},
		{"edges", []string{"edges", "ab12", "m1", "--on-complete", "m2"}},
		{"approve", []string{"approve", "ab12", "m1"}},
		{"cancel", []string{"cancel", "ab12", "--cancel-in-flight", "false"}},
	}
	wantChecks := map[string]func(t *testing.T, body map[string]any){
		"insert": func(t *testing.T, body map[string]any) {
			if body["reason"] != "r" {
				t.Errorf("POST reason = %v, want r", body["reason"])
			}
			if m, ok := body["member"].(map[string]any); !ok || m["prd_id"] != "prd-1" {
				t.Errorf("POST member = %v, want {prd_id prd-1}", body["member"])
			}
		},
		"pause":  func(t *testing.T, _ map[string]any) {},
		"resume": func(t *testing.T, _ map[string]any) {},
		"skip": func(t *testing.T, body map[string]any) {
			if body["member_id"] != "m1" {
				t.Errorf("POST member_id = %v, want m1", body["member_id"])
			}
		},
		"remove": func(t *testing.T, body map[string]any) {
			if body["member_id"] != "m1" {
				t.Errorf("POST member_id = %v, want m1", body["member_id"])
			}
		},
		"edges": func(t *testing.T, body map[string]any) {
			if body["member_id"] != "m1" || body["on_complete"] != "m2" {
				t.Errorf("POST body = %v, want member_id m1 and on_complete m2", body)
			}
		},
		"approve": func(t *testing.T, body map[string]any) {
			if body["member_id"] != "m1" {
				t.Errorf("POST member_id = %v, want m1", body["member_id"])
			}
		},
		"cancel": func(t *testing.T, body map[string]any) {
			if v, ok := body["cancel_in_flight"].(bool); !ok || v {
				t.Errorf("POST cancel_in_flight = %v, want false", body["cancel_in_flight"])
			}
		},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			f := &autoVersionFake{version: 3}
			withAutoVersionFake(t, f)

			var err error
			out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), tc.args...) })
			if err != nil {
				t.Fatalf("execute: %v (output: %s)", err, out)
			}

			if len(f.reqs) != 2 {
				t.Fatalf("made %d requests, want exactly 2: %+v", len(f.reqs), f.reqs)
			}
			first, second := f.reqs[0], f.reqs[1]
			if first.Method != http.MethodGet || first.Path != "/api/automations/ab12" {
				t.Fatalf("first request = %s %s, want GET /api/automations/ab12", first.Method, first.Path)
			}
			wantPost := "/api/automations/ab12/" + ops[tc.verb]
			if second.Method != http.MethodPost || second.Path != wantPost {
				t.Fatalf("second request = %s %s, want POST %s", second.Method, second.Path, wantPost)
			}
			if got := autoFakeExpectedVersion(t, f, 1); got != 3 {
				t.Errorf("POST expected_version = %d, want 3 (read from the GET)", got)
			}
			wantChecks[tc.verb](t, autoFakeBody(t, f, 1))
		})
	}
}

// TestAutomationCLIDefaultVersionRetriesOnceOnRace covers the implicit
// version retry: the first POST is refused with a 409 version mismatch,
// the CLI re-reads the version (now 4) and posts again with
// expected_version 4, and the command succeeds. Total: two GETs, two POSTs.
func TestAutomationCLIDefaultVersionRetriesOnceOnRace(t *testing.T) {
	f := &autoVersionFake{version: 3}
	f.postReply = func(n int) (autoVersionReply, bool) {
		if n == 1 {
			return autoVersionReply{code: http.StatusConflict, body: "409: version mismatch: expected version 3, current version is 4"}, true
		}
		return autoVersionReply{}, false
	}
	f.afterReply = func(n int) {
		if n == 1 {
			f.version = 4 // the Automation moved to version 4 between read and write
		}
	}
	withAutoVersionFake(t, f)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "pause", "ab12") })
	if err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}

	gets := 0
	posts := 0
	for _, req := range f.reqs {
		if req.Method == http.MethodGet {
			gets++
		}
		if req.Method == http.MethodPost {
			posts++
		}
	}
	if gets != 2 || posts != 2 {
		t.Fatalf("made %d GETs and %d POSTs, want exactly 2 of each: %+v", gets, posts, f.reqs)
	}
	for i, want := range []struct {
		method, path string
	}{{http.MethodGet, "/api/automations/ab12"}, {http.MethodPost, "/api/automations/ab12/pause"}, {http.MethodGet, "/api/automations/ab12"}, {http.MethodPost, "/api/automations/ab12/pause"}} {
		if f.reqs[i].Method != want.method || f.reqs[i].Path != want.path {
			t.Errorf("request %d = %s %s, want %s %s", i, f.reqs[i].Method, f.reqs[i].Path, want.method, want.path)
		}
	}
	if got := autoFakeExpectedVersion(t, f, 1); got != 3 {
		t.Errorf("first POST expected_version = %d, want 3", got)
	}
	if got := autoFakeExpectedVersion(t, f, 3); got != 4 {
		t.Errorf("second POST expected_version = %d, want 4 (re-read from the GET)", got)
	}
}

// TestAutomationCLIDefaultVersionGivesUpAfterOneRetry covers the retry
// limit: every POST is refused with a 409 version mismatch, the CLI posts
// at most once after re-reading the version, and the command fails with
// the daemon's 409 message.
func TestAutomationCLIDefaultVersionGivesUpAfterOneRetry(t *testing.T) {
	f := &autoVersionFake{version: 3}
	f.postReply = func(int) (autoVersionReply, bool) {
		return autoVersionReply{code: http.StatusConflict, body: "409: version mismatch: expected version 3, current version is 4"}, true
	}
	withAutoVersionFake(t, f)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "pause", "ab12") })
	if err == nil {
		t.Fatalf("expected an error, got nil (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "version mismatch") {
		t.Errorf("error = %q, want it to contain %q and %q", err, "409", "version mismatch")
	}

	posts := 0
	for _, req := range f.reqs {
		if req.Method == http.MethodPost {
			posts++
		}
	}
	if posts != 2 {
		t.Fatalf("made %d POSTs, want exactly 2 (one original + one retry): %+v", posts, f.reqs)
	}
}
