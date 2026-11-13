package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderPlansMigrate(t *testing.T) {
	body := []byte(`{"note":"Preview only.","result":{"applied":false,
"would_create":[{"source":"arc","title":"Plans tab","project":"datawatch","link_kind":"automation","link_id":"aaaa1111","order":1}],
"would_link":[],"skipped_existing":[{"source":"registry-row","title":"X","reason":"already linked"}],
"conflicts":[{"source":"arc","title":"Y","reason":"title taken"}]}}`)
	var out bytes.Buffer
	if err := renderPlansMigrate(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Preview: would create 1, would link 0, already there 1, conflicts 1",
		"would create: Plans tab (arc, datawatch, order 1) → automation aaaa1111",
		"left alone: X", "conflict: Y", "--apply"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if err := renderPlansMigrate(&out, []byte("nope")); err == nil {
		t.Error("bad JSON must be an error")
	}
}

func TestPlansMigrateCommandDefaultsToDryRun(t *testing.T) {
	cmd := newPlansMigrateCmd()
	f := cmd.Flags().Lookup("apply")
	if f == nil || f.DefValue != "false" {
		t.Fatalf("--apply must exist and default to false: %+v", f)
	}
}

// plansFake records the requests the plans commands send.
type plansFake struct {
	reqs []string
	body map[string]any
}

func (f *plansFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.reqs = []string{r.Method + " " + r.URL.Path}
	f.body = nil
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&f.body)
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/plans/p1/automata/order":
		_, _ = w.Write([]byte(`{"plan":{"title":"Ship","id":"p1"},"next":{"automaton_id":"a4","title":"Four"},"automata":[` +
			`{"title":"Three","id":"a3","run":"blocked","skipped":true},{"title":"Four","id":"a4","run":"next","is_next":true}]}`))
	case "/api/plans/p2/attach":
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"automaton \"a1\" is already linked to p1; detach it first"}`))
	default:
		_, _ = w.Write([]byte(`{"plan":{"title":"New","id":"new"},"plans":[{"title":"A","id":"a","order":1}]}`))
	}
}

func runPlansCmd(t *testing.T, f *plansFake, args ...string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	cmd := newPlansCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

func TestPlansCommandsSendTheRightRequests(t *testing.T) {
	f := &plansFake{}
	if _, err := runPlansCmd(t, f, "create", "acme", "Alpha", "--goal", "g"); err != nil {
		t.Fatal(err)
	}
	if f.reqs[0] != "POST /api/plans" || f.body["project"] != "acme" || f.body["title"] != "Alpha" || f.body["goal"] != "g" {
		t.Fatalf("create sent %v %v", f.reqs, f.body)
	}
	out, err := runPlansCmd(t, f, "reorder", "b", "a")
	if err != nil || f.reqs[0] != "PUT /api/plans/order" || !strings.Contains(out, "1. A") {
		t.Fatalf("reorder: %v %v %q", err, f.reqs, out)
	}
	out, err = runPlansCmd(t, f, "reorder-automata", "p1", "a3", "a4")
	if err != nil || f.reqs[0] != "PUT /api/plans/p1/automata/order" {
		t.Fatalf("reorder-automata: %v %v", err, f.reqs)
	}
	if !strings.Contains(out, "<- next") || !strings.Contains(out, "skipped: blocked") {
		t.Fatalf("run view: %q", out)
	}
	if _, err = runPlansCmd(t, f, "attach", "p1", "--finding", "f1"); err != nil || f.body["kind"] != "finding" || f.body["id"] != "f1" {
		t.Fatalf("attach finding: %v %v", err, f.body)
	}
	if _, err = runPlansCmd(t, f, "detach", "p1", "--automation", "au1"); err != nil || f.reqs[0] != "POST /api/plans/p1/detach" || f.body["kind"] != "automation" {
		t.Fatalf("detach: %v %v %v", err, f.reqs, f.body)
	}
	if _, err = runPlansCmd(t, f, "attach", "p2", "--automaton", "a1"); err == nil || !strings.Contains(err.Error(), "detach it first") {
		t.Fatalf("a conflict must be surfaced, got %v", err)
	}
	if _, err = runPlansCmd(t, f, "attach", "p1"); err == nil {
		t.Fatal("attach with no kind must fail")
	}
	if _, err = runPlansCmd(t, f, "attach", "p1", "--automaton", "a", "--finding", "f"); err == nil {
		t.Fatal("attach with two kinds must fail")
	}
}

func TestRenderPlansList(t *testing.T) {
	body := []byte(`{"project":"acme","plans":[{"title":"Ship it","id":"ship","state":"in_progress","progress":{"done":1,"total":3},` +
		`"next":{"automaton_id":"a2","title":"Two"},"finding_count":2}],"backlog":{"plans":[{"title":"Later","id":"later","state":"open","progress":{"done":0,"total":0}}]}}`)
	var out bytes.Buffer
	if err := renderPlansList(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1. Ship it  [ship]  in_progress  1/3 done  2 findings  next: Two", "Backlog:", "1. Later"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
}
