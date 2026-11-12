package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type defectCLICall struct {
	method, path, query string
	body                map[string]any
}

// defectCLIServer points the CLI at a fake daemon and records requests.
func defectCLIServer(t *testing.T, reply string) *[]defectCLICall {
	t.Helper()
	var calls []defectCLICall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&b)
		}
		calls = append(calls, defectCLICall{r.Method, r.URL.Path, r.URL.RawQuery, b})
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	t.Setenv("DATAWATCH_TOKEN", "t")
	return &calls
}

func runDefectCLI(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newAutonomousDefectCmd()
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func TestDefectCLIWiring(t *testing.T) {
	cmd := newAutonomousDefectCmd()
	if cmd.Name() != "defect" {
		t.Fatalf("name = %q", cmd.Name())
	}
	for _, n := range []string{"intake", "batches", "split", "tiny-fix-check"} {
		findSubCommand(t, cmd, n)
	}
}

func TestDefectCLIIntake(t *testing.T) {
	calls := defectCLIServer(t, `{"route":"tiny_fix","reason":"one file"}`)
	if err := runDefectCLI(t, "intake", "--title", "typo", "--project", "web", "--kind", "bug", "--file", "a.go", "--file", "b.go", "--priority", "3"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %d", len(*calls))
	}
	c := (*calls)[0]
	if c.method != http.MethodPost || c.path != "/api/autonomous/defects/intake" {
		t.Errorf("call = %+v", c)
	}
	if c.body["title"] != "typo" || c.body["project"] != "web" || c.body["kind"] != "bug" || c.body["priority"] != float64(3) {
		t.Errorf("body = %+v", c.body)
	}
	if id, _ := c.body["id"].(string); !strings.HasPrefix(id, "defect-") {
		t.Errorf("id = %v", c.body["id"])
	}
	if fs, _ := c.body["files"].([]any); len(fs) != 2 {
		t.Errorf("files = %v", c.body["files"])
	}
}

func TestDefectCLIIntakeValidation(t *testing.T) {
	calls := defectCLIServer(t, `{}`)
	if err := runDefectCLI(t, "intake", "--project", "web"); err == nil {
		t.Error("missing --title should fail")
	}
	if err := runDefectCLI(t, "intake", "--title", "x"); err == nil {
		t.Error("missing --project should fail")
	}
	if len(*calls) != 0 {
		t.Errorf("validation failures made %d calls", len(*calls))
	}
}

func TestDefectCLIBatchesAndSplit(t *testing.T) {
	calls := defectCLIServer(t, `{"batches":[],"total":0}`)
	if err := runDefectCLI(t, "batches", "--project", "web app"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.method != http.MethodGet || c.path != "/api/autonomous/defects/batches" || c.query != "project=web+app" {
		t.Errorf("batches call = %+v", c)
	}
	if err := runDefectCLI(t, "split", "b1", "--story", "s2"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[1]; c.method != http.MethodPost || c.path != "/api/autonomous/defects/batches/b1/split" || c.body["story_id"] != "s2" {
		t.Errorf("split call = %+v", c)
	}
	n := len(*calls)
	if err := runDefectCLI(t, "split", "b1"); err == nil {
		t.Error("split without --story should fail")
	}
	if len(*calls) != n {
		t.Error("failed split made a call")
	}
}

func TestDefectCLITinyFixCheck(t *testing.T) {
	calls := defectCLIServer(t, `{"eligible":true,"reason":"ok"}`)
	if err := runDefectCLI(t, "tiny-fix-check", "--file", "a.go", "--kind", "bug", "--project", "web"); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.method != http.MethodPost || c.path != "/api/autonomous/defects/tiny-fix/check" || c.body["kind"] != "bug" || c.body["project"] != "web" {
		t.Errorf("call = %+v", c)
	}
	if err := runDefectCLI(t, "tiny-fix-check", "--kind", "bug"); err == nil {
		t.Error("missing --file should fail")
	}
	if err := runDefectCLI(t, "tiny-fix-check", "--file", "a.go"); err == nil {
		t.Error("missing --kind should fail")
	}
}

func TestDefectCLIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "disabled", http.StatusConflict)
	}))
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	defer func() { serverURL = old }()
	if err := runDefectCLI(t, "intake", "--title", "x", "--project", "p"); err == nil || !strings.Contains(err.Error(), "409") {
		t.Errorf("err = %v", err)
	}
}
