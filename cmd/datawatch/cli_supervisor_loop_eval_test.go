package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const supervisorLoopEvalBody = `{"job":"pick_backend","models":[
 {"model":"m1","cases":2,"passed":1,"validator_failures":3,"retries":1,"median_duration_ms":120,"max_duration_ms":300,"escalations":1,"call_errors":0}]}`

func TestSupervisorLoopEvalCLIPostsAndRenders(t *testing.T) {
	method, path, _, body := supervisorLoopDaemon(t, http.StatusOK, supervisorLoopEvalBody)
	s, err := runSupervisorLoopCmd(t, "eval", "--job", "pick_backend", "--model", "m1", "--model", "m2", "--cases", "x/y")
	if err != nil {
		t.Fatalf("execute: %v (%q)", err, s)
	}
	if method() != http.MethodPost || path() != "/api/supervisor-loop/eval" {
		t.Errorf("request = %s %s", method(), path())
	}
	var got struct {
		Job      string   `json:"job"`
		Models   []string `json:"models"`
		CasesDir string   `json:"cases_dir"`
	}
	if err := json.Unmarshal([]byte(body()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Job != "pick_backend" || len(got.Models) != 2 || got.CasesDir != "x/y" {
		t.Errorf("body = %+v", got)
	}
	for _, want := range []string{"m1", "passed 1/2", "retries 1", "escalations 1", "median 120ms", "max 300ms"} {
		if !strings.Contains(s, want) {
			t.Errorf("output %q lacks %q", s, want)
		}
	}
}

func TestSupervisorLoopEvalCLIJSONFormatUntouched(t *testing.T) {
	supervisorLoopDaemon(t, http.StatusOK, supervisorLoopEvalBody)
	s, err := runSupervisorLoopCmd(t, "eval", "--job", "pick_backend", "--model", "m1", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if s != supervisorLoopEvalBody {
		t.Errorf("json output changed: %q", s)
	}
}

func TestSupervisorLoopEvalCLIRequiresJobAndModel(t *testing.T) {
	if _, err := runSupervisorLoopCmd(t, "eval", "--model", "m1"); err == nil {
		t.Error("missing --job accepted")
	}
	if _, err := runSupervisorLoopCmd(t, "eval", "--job", "draft_spec"); err == nil {
		t.Error("missing --model accepted")
	}
}

func TestSupervisorLoopEvalCLIUnreachableError(t *testing.T) {
	supervisorLoopDaemon(t, http.StatusBadGateway, "no listed model is reachable: m1 (refused)")
	_, err := runSupervisorLoopCmd(t, "eval", "--job", "draft_spec", "--model", "m1")
	if err == nil || !strings.Contains(err.Error(), "m1") {
		t.Errorf("err = %v, want the daemon's unreachable message naming m1", err)
	}
}
