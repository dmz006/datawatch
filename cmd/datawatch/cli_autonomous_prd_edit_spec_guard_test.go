// CLI spec-guard tests for the PRD edit commands: prd-edit, prd-edit-task
// and prd-edit-story refuse text carrying the capped-view truncation marker
// before any request is sent, using the daemon's own refusal wording
// (autonomous.TruncationRefusal, delegated by the daemon's truncationRefusal
// 400). A clean spec/description goes through as the full text, and no read
// request with a capped detail is ever issued by the edit path.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// editGuardReq is one request the fake daemon received, in arrival order.
type editGuardReq struct {
	method string
	path   string
	query  string
	raw    []byte
}

// editGuardFake records every request and answers with a defaulting-200
// JSON reply (same pattern as withCaptureServer in
// cli_autonomous_subscriptions_test.go).
type editGuardFake struct {
	reqs      []editGuardReq
	replyCode int
	replyBody string
}

func (f *editGuardFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.reqs = append(f.reqs, editGuardReq{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, raw: raw})
		w.Header().Set("Content-Type", "application/json")
		if f.replyCode > 0 {
			w.WriteHeader(f.replyCode)
		}
		if f.replyBody != "" {
			_, _ = w.Write([]byte(f.replyBody))
		}
	}
}

func (f *editGuardFake) withServer(t *testing.T) {
	t.Helper()
	old := serverURL
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
}

func (f *editGuardFake) body(t *testing.T, i int) map[string]any {
	t.Helper()
	if i >= len(f.reqs) {
		t.Fatalf("no recorded request %d (got %d requests)", i, len(f.reqs))
	}
	var m map[string]any
	if err := json.Unmarshal(f.reqs[i].raw, &m); err != nil {
		t.Fatalf("request %d body %q is not JSON: %v", i, f.reqs[i].raw, err)
	}
	return m
}

// truncationSample is spec text copied out of a capped view: it carries the
// marker capText appends to a cut field.
const truncationSample = "Goal\n\nBuild the widget.\n…[truncated: kept 4096 of 32768 bytes]"

// refusalText is the daemon's refusal message for a field, from the shared
// source the CLI, the MCP tools and the 400 all use.
func refusalText(t *testing.T, field, value string) string {
	t.Helper()
	msg, refuse := autonomous.TruncationRefusal(field, value)
	if !refuse {
		t.Fatalf("TruncationRefusal(%q) should refuse marker text", field)
	}
	return msg
}

// TestAutonomousPRDEditFullSpecSentAsWritten covers the happy path: the
// full spec text goes to the daemon exactly as the operator passed it — and
// the edit path issues no read request at all, let alone one with a capped
// detail.
func TestAutonomousPRDEditFullSpecSentAsWritten(t *testing.T) {
	f := &editGuardFake{replyBody: `{"id":"prd-1","title":"w","spec":"kept"}`}
	f.withServer(t)

	fullSpec := "Goal\n\nBuild the widget.\n\nAcceptance\n- the widget builds\n- tests pass"
	cmd := newAutonomousPRDEditCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"prd-1", "--spec", fullSpec})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}

	if len(f.reqs) != 1 {
		t.Fatalf("got %d requests, want exactly the edit write:\n%v", len(f.reqs), f.reqs)
	}
	r := f.reqs[0]
	if r.method != http.MethodPatch || r.path != "/api/autonomous/prds/prd-1" {
		t.Errorf("route = %s %s, want PATCH /api/autonomous/prds/prd-1", r.method, r.path)
	}
	if strings.Contains(r.query, "detail") {
		t.Errorf("edit write carries a detail query: %q (a capped read)", r.query)
	}
	body := f.body(t, 0)
	if body["spec"] != fullSpec {
		t.Errorf("spec %q != the full spec text the operator passed", body["spec"])
	}
}

// TestAutonomousPRDEditMarkerSpecRefusedWithoutRequest covers the guard: a
// spec copied from a capped view is refused client-side with the daemon's
// own wording and no request is sent.
func TestAutonomousPRDEditMarkerSpecRefusedWithoutRequest(t *testing.T) {
	f := &editGuardFake{replyCode: http.StatusOK, replyBody: `{}`}
	f.withServer(t)

	cmd := newAutonomousPRDEditCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"prd-1", "--spec", truncationSample})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("marker spec accepted (sent %d requests)", len(f.reqs))
	}
	want := refusalText(t, "spec", truncationSample)
	if err.Error() != want {
		t.Errorf("refusal = %q, want the daemon's wording %q", err.Error(), want)
	}
	if len(f.reqs) != 0 {
		t.Errorf("marker spec sent %d requests, want none:\n%v", len(f.reqs), f.reqs)
	}
}

// TestPRDEditTaskFullSpecSentAsWritten covers the task-edit happy path the
// same way: the full spec text, no capped read.
func TestPRDEditTaskFullSpecSentAsWritten(t *testing.T) {
	f := &editGuardFake{replyBody: `{"id":"prd-1"}`}
	f.withServer(t)

	fullSpec := "Task full spec\n\n- step one\n- step two\n- acceptance"
	cmd := newAutonomousPRDEditTaskCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"prd-1", "--task", "task-7", "--spec", fullSpec})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}

	if len(f.reqs) != 1 {
		t.Fatalf("got %d requests, want exactly the edit write:\n%v", len(f.reqs), f.reqs)
	}
	r := f.reqs[0]
	if r.method != http.MethodPost || r.path != "/api/autonomous/prds/prd-1/edit_task" {
		t.Errorf("route = %s %s, want POST /api/autonomous/prds/prd-1/edit_task", r.method, r.path)
	}
	if strings.Contains(r.query, "detail") {
		t.Errorf("edit write carries a detail query: %q (a capped read)", r.query)
	}
	body := f.body(t, 0)
	if body["new_spec"] != fullSpec {
		t.Errorf("new_spec %q != the full spec text the operator passed", body["new_spec"])
	}
}

// TestPRDEditTaskMarkerSpecRefusedWithoutRequest is the guard half.
func TestPRDEditTaskMarkerSpecRefusedWithoutRequest(t *testing.T) {
	f := &editGuardFake{replyBody: `{}`}
	f.withServer(t)

	cmd := newAutonomousPRDEditTaskCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"prd-1", "--task", "task-7", "--spec", truncationSample})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("marker spec accepted (sent %d requests)", len(f.reqs))
	}
	want := refusalText(t, "new_spec", truncationSample)
	if err.Error() != want {
		t.Errorf("refusal = %q, want the daemon's wording %q", err.Error(), want)
	}
	if len(f.reqs) != 0 {
		t.Errorf("marker spec sent %d requests, want none:\n%v", len(f.reqs), f.reqs)
	}
}

// TestPRDEditStoryFullDescriptionSentAsWritten is the story-description
// happy path.
func TestPRDEditStoryFullDescriptionSentAsWritten(t *testing.T) {
	f := &editGuardFake{replyBody: `{"id":"prd-1"}`}
	f.withServer(t)

	description := "Story description\n\nFull text of what the story covers and why."
	cmd := newAutonomousPRDEditStoryCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"prd-1", "--story", "story-3", "--description", description})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}

	if len(f.reqs) != 1 {
		t.Fatalf("got %d requests, want exactly the edit write:\n%v", len(f.reqs), f.reqs)
	}
	r := f.reqs[0]
	if r.method != http.MethodPost || r.path != "/api/autonomous/prds/prd-1/edit_story" {
		t.Errorf("route = %s %s, want POST /api/autonomous/prds/prd-1/edit_story", r.method, r.path)
	}
	body := f.body(t, 0)
	if body["new_description"] != description {
		t.Errorf("new_description %q != the full description the operator passed", body["new_description"])
	}
}

// TestPRDEditStoryMarkerDescriptionRefusedWithoutRequest is the guard half.
func TestPRDEditStoryMarkerDescriptionRefusedWithoutRequest(t *testing.T) {
	f := &editGuardFake{replyBody: `{}`}
	f.withServer(t)

	markerDesc := "Story description\n…[truncated: kept 512 of 40960 bytes]"
	cmd := newAutonomousPRDEditStoryCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"prd-1", "--story", "story-3", "--description", markerDesc})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("marker description accepted (sent %d requests)", len(f.reqs))
	}
	want := refusalText(t, "new_description", markerDesc)
	if err.Error() != want {
		t.Errorf("refusal = %q, want the daemon's wording %q", err.Error(), want)
	}
	if len(f.reqs) != 0 {
		t.Errorf("marker description sent %d requests, want none:\n%v", len(f.reqs), f.reqs)
	}
}

// TestPRDEditDescriptionsAdvertiseFullReads pins the operator guidance the
// edit commands' flag help and prd-get's detail flag must carry: the full
// view is the level that returns the spec/description text whole, and text
// carrying the truncation marker is refused.
func TestPRDEditDescriptionsAdvertiseFullReads(t *testing.T) {
	cases := []struct {
		name string
		u    string
	}{
		{"prd-edit --spec", newAutonomousPRDEditCmd().Flags().Lookup("spec").Usage},
		{"prd-edit-task --spec", newAutonomousPRDEditTaskCmd().Flags().Lookup("spec").Usage},
		{"prd-edit-story --description", newAutonomousPRDEditStoryCmd().Flags().Lookup("description").Usage},
		{"prd-get --detail", newAutonomousPRDGetCmd().Flags().Lookup("detail").Usage},
	}
	for _, c := range cases {
		if !strings.Contains(c.u, "truncation marker") {
			t.Errorf("%s flag help does not mention the truncation marker: %q", c.name, c.u)
		}
		if !strings.Contains(strings.ToLower(c.u), "full") {
			t.Errorf("%s flag help does not point at the full read: %q", c.name, c.u)
		}
	}
}
