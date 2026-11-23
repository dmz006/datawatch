package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const deployTestStatus = `{"settings":{"build_command":"make build","binary_path":"","health_timeout":"2m"},"phase":"deferred","outcome":"deferred","deferred":{"reason":"tasks running","at":"2026-11-06T10:00:00Z"},"snapshot":{"time":"2026-11-06T09:00:00Z","sha256":"abc123"},"running":false}`

func deployTestDaemon(t *testing.T, status int, body string, rec *[2]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec[0], rec[1] = r.Method, r.URL.Path
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
}

func deployRun(args ...string) (string, error) {
	c := newAutonomousDeployCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestDeployCLI_ShowPrintsSettingsAndRecord(t *testing.T) {
	var rec [2]string
	deployTestDaemon(t, 200, deployTestStatus, &rec)
	out, err := deployRun("show")
	if err != nil {
		t.Fatal(err)
	}
	if rec != [2]string{"GET", "/api/autonomous/deploy"} {
		t.Errorf("sent %v", rec)
	}
	for _, want := range []string{"build_command: make build", "binary_path: (not set)", "phase: deferred", "tasks running", "sha256 abc123"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestDeployCLI_ShowJSONUntouched(t *testing.T) {
	var rec [2]string
	deployTestDaemon(t, 200, deployTestStatus, &rec)
	out, err := deployRun("show", "--json")
	if err != nil || out != deployTestStatus {
		t.Errorf("out %q err %v", out, err)
	}
}

func TestDeployCLI_RunPosts(t *testing.T) {
	var rec [2]string
	deployTestDaemon(t, 200, `{"status":"deployed","note":"ok"}`, &rec)
	out, err := deployRun("run")
	if err != nil {
		t.Fatal(err)
	}
	if rec != [2]string{"POST", "/api/autonomous/deploy/run"} {
		t.Errorf("sent %v", rec)
	}
	if !strings.Contains(out, "Deploy deployed: ok") {
		t.Errorf("out %q", out)
	}
}

func TestDeployCLI_RunConflictIsAnError(t *testing.T) {
	var rec [2]string
	deployTestDaemon(t, 409, "a merge or deploy is already running", &rec)
	if _, err := deployRun("run"); err == nil || !strings.Contains(err.Error(), "409") {
		t.Errorf("err %v", err)
	}
}

func TestDeployCLI_UsageErrors(t *testing.T) {
	for _, args := range [][]string{{"show", "extra"}, {"run", "extra"}, {"bogus"}} {
		if _, err := deployRun(args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestDeployCLI_ConfigureKeysAccepted(t *testing.T) {
	for _, k := range []string{"build_command", "install_command", "binary_path", "restart_command", "health_check", "drain_timeout", "health_timeout"} {
		got := configureRequestBody("autonomous.deploy."+k, "x")
		if got != `{"autonomous.deploy.`+k+`":"x"}` {
			t.Errorf("%s: body %s", k, got)
		}
	}
}

func TestDeployCLI_WiredUnderAutonomous(t *testing.T) {
	found := false
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() == "deploy" {
			found = true
		}
	}
	if !found {
		t.Error("deploy is not under autonomous")
	}
}
