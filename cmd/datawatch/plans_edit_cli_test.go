package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// plansEditFake answers PATCH /api/plans/{ref} the way the daemon does.
type plansEditFake struct {
	method, path string
	body         map[string]any
	status       int
	reply        string
}

func (f *plansEditFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.method, f.path, f.body = r.Method, r.URL.EscapedPath(), nil
	_ = json.NewDecoder(r.Body).Decode(&f.body)
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_, _ = w.Write([]byte(f.reply))
}

func runPlansEditCmd(t *testing.T, f *plansEditFake, args ...string) (string, error) {
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

func TestPlansRenameSendsAPatch(t *testing.T) {
	f := &plansEditFake{reply: `{"plan":{"title":"Better name","id":"plan-1"},"goal":""}`}
	out, err := runPlansEditCmd(t, f, "rename", "Old name", "Better", "name")
	if err != nil {
		t.Fatal(err)
	}
	if f.method != http.MethodPatch || f.path != "/api/plans/Old%20name" {
		t.Fatalf("sent %s %s", f.method, f.path)
	}
	if f.body["title"] != "Better name" || len(f.body) != 1 {
		t.Fatalf("body = %v (words after the plan join into the title)", f.body)
	}
	if !strings.Contains(out, "Renamed plan plan-1 to Better name") {
		t.Fatalf("output = %q", out)
	}
	if _, err = runPlansEditCmd(t, f, "rename", "only-a-plan"); err == nil {
		t.Fatal("rename with no title must fail")
	}
}

func TestPlansRenameSurfacesTheConflictAndValidation(t *testing.T) {
	f := &plansEditFake{status: http.StatusConflict, reply: `{"error":"the title \"Alpha\" is already used in project acme by \"Alpha\" (plan-1)"}`}
	_, err := runPlansEditCmd(t, f, "rename", "plan-2", "alpha")
	if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "Alpha") || !strings.Contains(err.Error(), "plan-1") {
		t.Fatalf("conflict must be surfaced and name the other plan, got %v", err)
	}
	f = &plansEditFake{status: http.StatusBadRequest, reply: `{"error":"a plan title is at most 100 characters"}`}
	if _, err = runPlansEditCmd(t, f, "rename", "plan-2", strings.Repeat("x", 101)); err == nil || !strings.Contains(err.Error(), "100 characters") {
		t.Fatalf("validation must be surfaced, got %v", err)
	}
}

func TestPlansEditSendsTheGoal(t *testing.T) {
	f := &plansEditFake{reply: `{"plan":{"title":"Ship","id":"plan-1"},"goal":"get it out"}`}
	out, err := runPlansEditCmd(t, f, "edit", "plan-1", "--goal", "get it out")
	if err != nil {
		t.Fatal(err)
	}
	if f.method != http.MethodPatch || f.path != "/api/plans/plan-1" || f.body["goal"] != "get it out" || len(f.body) != 1 {
		t.Fatalf("sent %s %s %v", f.method, f.path, f.body)
	}
	if !strings.Contains(out, "Set the goal of plan Ship") || !strings.Contains(out, "get it out") {
		t.Fatalf("output = %q", out)
	}
	// An empty goal is sent and clears it.
	f.reply = `{"plan":{"title":"Ship","id":"plan-1"},"goal":""}`
	out, err = runPlansEditCmd(t, f, "edit", "plan-1", "--goal", "")
	if err != nil || f.body["goal"] != "" || !strings.Contains(out, "Cleared the goal") {
		t.Fatalf("clear: %v %v %q", err, f.body, out)
	}
	// Without --goal there is nothing to change and the daemon is not called.
	f.method = ""
	if _, err = runPlansEditCmd(t, f, "edit", "plan-1"); err == nil || f.method != "" {
		t.Fatalf("edit with no --goal must fail before calling the daemon: %v %q", err, f.method)
	}
}

func TestPlansEditHelpMentionsRenameAndEdit(t *testing.T) {
	cmd := newPlansCmd()
	for _, want := range []string{"plans rename <plan> <new title>", "plans edit <plan> --goal TEXT"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("plans help does not mention %q", want)
		}
	}
}
