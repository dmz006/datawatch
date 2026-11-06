package router

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

const deployCommStatus = `{"settings":{"build_command":"make build","health_timeout":"2m"},"phase":"done","outcome":"healthy","snapshot":{"time":"t","sha256":"abc"},"running":false}`

func deployCommSay(t *testing.T, status int, body, msg string) (method, path, reply string, calls int) {
	t.Helper()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		method, path = r.Method, r.URL.Path
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	reply = strings.Join(setupPeerRouter(t, port).HandleTestMessage(msg), " ")
	return
}

func TestDeployComm_Show(t *testing.T) {
	for _, msg := range []string{"autonomous deploy", "autonomous deploy show"} {
		m, p, got, _ := deployCommSay(t, 200, deployCommStatus, msg)
		if m != http.MethodGet || p != "/api/autonomous/deploy" {
			t.Errorf("%q sent %s %s", msg, m, p)
		}
		for _, want := range []string{"build_command: make build", "outcome: healthy", "sha256 abc"} {
			if !strings.Contains(got, want) {
				t.Errorf("%q lacks %q: %q", msg, want, got)
			}
		}
	}
}

func TestDeployComm_Run(t *testing.T) {
	m, p, got, _ := deployCommSay(t, 200, `{"status":"deployed","note":"ok"}`, "autonomous deploy run")
	if m != http.MethodPost || p != "/api/autonomous/deploy/run" {
		t.Errorf("sent %s %s", m, p)
	}
	if !strings.Contains(got, "Deploy deployed: ok") {
		t.Errorf("reply %q", got)
	}
}

func TestDeployComm_RunConflictReported(t *testing.T) {
	_, _, got, _ := deployCommSay(t, 409, "a merge or deploy is already running", "autonomous deploy run")
	if !strings.Contains(got, "autonomous deploy failed") || !strings.Contains(got, "already running") {
		t.Errorf("reply %q", got)
	}
}

func TestDeployComm_UsageErrorsMakeNoRequest(t *testing.T) {
	for _, msg := range []string{"autonomous deploy bogus", "autonomous deploy run now", "autonomous deploy show x"} {
		_, _, got, calls := deployCommSay(t, 200, "{}", msg)
		if calls != 0 || !strings.Contains(got, "usage: autonomous deploy") {
			t.Errorf("%q: calls %d reply %q", msg, calls, got)
		}
		if strings.Contains(got, "bogus") {
			t.Errorf("reply echoes command text: %q", got)
		}
	}
}

func TestDeployComm_ConfigureAcceptsKeys(t *testing.T) {
	for _, k := range DeployConfigureKeys() {
		_, port := newFakePeerParent(t, func(http.ResponseWriter, *http.Request) {})
		r := setupPeerRouter(t, port)
		var gotK, gotV string
		r.SetConfigureFunc(func(key, value string) error { gotK, gotV = key, value; return nil })
		out := strings.Join(r.HandleTestMessage("configure "+k+"=abc"), " ")
		if gotK != k || gotV != "abc" || !strings.Contains(out, "Set "+k) {
			t.Errorf("%s: key %q value %q reply %q", k, gotK, gotV, out)
		}
	}
}

func TestDeployComm_VerbListed(t *testing.T) {
	if v := DeployCommVerbs(); len(v) != 1 || v[0] != "deploy" {
		t.Errorf("verbs %v", v)
	}
}
