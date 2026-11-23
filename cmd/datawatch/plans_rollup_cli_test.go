package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cliRollupReply = `{"project":"acme","dry_run":%DRY%,"applied":%APPLIED%,"changed":1,"kept":1,"changes":[
{"title":"Ship it","plan":"ship","from":"open","to":"done","reason":"all 2 linked items finished"},
{"title":"Hand set","plan":"hand","from":"cancelled","to":"done","reason":"all 1 linked items finished","set_by_hand_kept":true}]}`

func cliRollup(dry bool) string {
	if dry {
		return strings.NewReplacer("%DRY%", "true", "%APPLIED%", "false").Replace(cliRollupReply)
	}
	return strings.NewReplacer("%DRY%", "false", "%APPLIED%", "true").Replace(cliRollupReply)
}

func TestRenderPlansRollup(t *testing.T) {
	var out bytes.Buffer
	if err := renderPlansRollup(&out, []byte(cliRollup(true))); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Roll-up preview (project acme): would change 1, kept as set by hand 1",
		"Ship it  [ship]: open -> done - all 2 linked items finished",
		"Hand set  [hand]: cancelled -> done (kept: status set by hand)",
		"run the same command without --dry-run"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := renderPlansRollup(&out, []byte(cliRollup(false))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Roll-up (project acme): changed 1") || strings.Contains(out.String(), "--dry-run") {
		t.Errorf("apply output:\n%s", out.String())
	}
	out.Reset()
	if err := renderPlansRollup(&out, []byte(`{"dry_run":false,"applied":true,"changes":[]}`)); err != nil ||
		!strings.Contains(out.String(), "all projects") || !strings.Contains(out.String(), "Every plan already has the status") {
		t.Errorf("empty output %q %v", out.String(), err)
	}
	if err := renderPlansRollup(&out, []byte("nope")); err == nil {
		t.Error("bad JSON must be an error")
	}
}

func TestPlansRollupCommandSendsTheRightRequests(t *testing.T) {
	var method, path, query string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		body = nil
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cliRollup(r.Method == http.MethodGet)))
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	run := func(args ...string) (string, error) {
		cmd := newPlansCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		err := cmd.Execute()
		return out.String(), err
	}

	out, err := run("rollup", "--dry-run", "--project", "acme")
	if err != nil || method != http.MethodGet || path != "/api/plans/rollup" {
		t.Fatalf("dry run: %v %s %s", err, method, path)
	}
	if !strings.Contains(query, "dry_run=true") || !strings.Contains(query, "project=acme") {
		t.Errorf("query = %q", query)
	}
	if !strings.Contains(out, "would change 1") {
		t.Errorf("dry-run output:\n%s", out)
	}

	out, err = run("rollup", "--project", "acme")
	if err != nil || method != http.MethodPost || path != "/api/plans/rollup" {
		t.Fatalf("apply: %v %s %s", err, method, path)
	}
	if body["dry_run"] != false || body["project"] != "acme" || !strings.Contains(out, "changed 1") {
		t.Errorf("apply sent %v, printed:\n%s", body, out)
	}

	if _, err = run("rollup"); err != nil || body["project"] != nil {
		t.Errorf("no project: %v %v", err, body)
	}

	out, err = run("rollup", "--dry-run", "--json")
	if err != nil || !strings.Contains(out, `"changes"`) || strings.Contains(out, "would change") {
		t.Errorf("--json output %q %v", out, err)
	}
	if _, err = run("rollup", "extra"); err == nil {
		t.Error("an extra argument must fail")
	}
}

func TestPlansRollupCommandFlags(t *testing.T) {
	cmd := newPlansRollupCmd()
	for _, f := range []string{"project", "dry-run", "json"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("--%s is missing", f)
		}
	}
	if f := cmd.Flags().Lookup("dry-run"); f.DefValue != "false" {
		t.Errorf("--dry-run default = %s", f.DefValue)
	}
}

func TestRenderPlansListMarksAStatusSetByHand(t *testing.T) {
	body := []byte(`{"project":"acme","plans":[` +
		`{"title":"Held","id":"held","state":"cancelled","set_by_hand":true,"progress":{"done":0,"total":1}},` +
		`{"title":"Derived","id":"derived","state":"done","set_by_hand":false,"progress":{"done":1,"total":1}}],"backlog":{"plans":[]}}`)
	var out bytes.Buffer
	if err := renderPlansList(&out, body); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1. Held  [held]  cancelled (by hand)  0/1 done", "2. Derived  [derived]  done  1/1 done"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "done (by hand)") {
		t.Errorf("a derived status must not be marked:\n%s", out.String())
	}
}
