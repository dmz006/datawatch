// Test file for the `datawatch compute node drain|undrain <name>` CLI
// subcommands. A local httptest server stands in for the daemon and records
// the method, path and body of the /api/compute/nodes/<name>/drain request;
// it replies with the node health JSON the way the server handler does.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

const cliDrainTestUntil = "2026-11-02T09:30:00Z"

type computeDrainFakeDaemon struct {
	mu       sync.Mutex
	requests int
	method   string
	path     string
	body     map[string]any
}

func withComputeDrainDaemon(t *testing.T) *computeDrainFakeDaemon {
	t.Helper()
	d := &computeDrainFakeDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.HasPrefix(r.URL.Path, "/api/compute/nodes/") || !strings.HasSuffix(r.URL.Path, "/drain") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not a drain endpoint"}`))
			return
		}
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/compute/nodes/"), "/drain")
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		d.mu.Lock()
		d.requests++
		d.method, d.path = r.Method, r.URL.Path
		d.body = body
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"name":"` + name + `","drained":false,"drain_active":false}`))
			return
		}
		out := map[string]any{"name": name, "drained": true, "drain_active": true}
		if until, ok := body["until"].(string); ok && until != "" {
			out["drain_until"] = until
		}
		if reason, ok := body["reason"].(string); ok && reason != "" {
			out["drain_reason"] = reason
		}
		b, _ := json.Marshal(out)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d
}

// runComputeNodeDrainCmd runs `datawatch compute node <args…>` and returns
// the command's error and the captured program stdout (daemonJSON prints
// the health JSON to os.Stdout directly).
func runComputeNodeDrainCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("pipe: %v", perr)
	}
	orig := os.Stdout
	cmdErr := func() error {
		os.Stdout = w
		cmd := newComputeNodeCmd()
		cmd.SetArgs(args)
		err := cmd.Execute()
		_ = w.Close()
		os.Stdout = orig
		return err
	}()
	out, _ := io.ReadAll(r)
	return string(out), cmdErr
}

// TestComputeNodeDrainCLIFlagsDefined checks the drain subcommand defines
// --until and --reason as strings and undrain exists alongside it.
func TestComputeNodeDrainCLIFlagsDefined(t *testing.T) {
	node := newComputeNodeCmd()
	drain, _, err := node.Find([]string{"drain"})
	if err != nil {
		t.Fatalf("find drain: %v", err)
	}
	for _, flag := range []string{"until", "reason"} {
		f := drain.Flags().Lookup(flag)
		if f == nil {
			t.Errorf("drain: missing --%s flag", flag)
			continue
		}
		if f.Value.Type() != "string" {
			t.Errorf("drain --%s type = %s, want string", flag, f.Value.Type())
		}
	}
	if until := drain.Flags().Lookup("until"); until != nil && !strings.Contains(until.Usage, "RFC3339") {
		t.Errorf("--until usage = %q, want it to mention RFC3339", until.Usage)
	}
	if reason := drain.Flags().Lookup("reason"); reason != nil && !strings.Contains(reason.Usage, "reason") {
		t.Errorf("--reason usage = %q, want it to mention reason", reason.Usage)
	}
	if _, _, err := node.Find([]string{"undrain"}); err != nil {
		t.Errorf("find undrain: %v", err)
	}
}

// TestComputeNodeDrainCLIRequestPathAndBody runs the drain command and checks
// the daemon sees POST /api/compute/nodes/<name>/drain with the until and
// reason body keys, and that the health JSON is printed.
func TestComputeNodeDrainCLIRequestPathAndBody(t *testing.T) {
	d := withComputeDrainDaemon(t)
	out, err := runComputeNodeDrainCmd(t, "drain", "gpu-box-1", "--until", cliDrainTestUntil, "--reason", "maintenance")
	if err != nil {
		t.Fatalf("command: %v (out=%s)", err, out)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-box-1/drain" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-box-1/drain", d.method, d.path)
	}
	if d.body["until"] != cliDrainTestUntil {
		t.Errorf("body until = %v, want %v", d.body["until"], cliDrainTestUntil)
	}
	if d.body["reason"] != "maintenance" {
		t.Errorf("body reason = %v, want maintenance", d.body["reason"])
	}
	if !strings.Contains(out, `"drained": true`) || !strings.Contains(out, cliDrainTestUntil) {
		t.Errorf("printed health JSON = %q, want drained:true and the until", out)
	}
}

// TestComputeNodeDrainCLIWithoutFlags is the open-ended form: the body
// carries no until key.
func TestComputeNodeDrainCLIWithoutFlags(t *testing.T) {
	d := withComputeDrainDaemon(t)
	if _, err := runComputeNodeDrainCmd(t, "drain", "gpu-box-1"); err != nil {
		t.Fatalf("command: %v", err)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-box-1/drain" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-box-1/drain", d.method, d.path)
	}
	if until, present := d.body["until"]; present {
		t.Errorf("body carries until=%v, want absent for an open-ended drain", until)
	}
}

// TestComputeNodeDrainCLIBadUntilRejected checks a non-RFC3339 --until fails
// locally and never reaches the daemon.
func TestComputeNodeDrainCLIBadUntilRejected(t *testing.T) {
	d := withComputeDrainDaemon(t)
	_, err := runComputeNodeDrainCmd(t, "drain", "gpu-box-1", "--until", "soon-ish")
	if err == nil {
		t.Fatal("expected an error for a non-RFC3339 --until, got nil")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("error = %q, want it to mention RFC3339", err)
	}
	if d.requests != 0 {
		t.Errorf("daemon saw %d request(s); a bad --until must not reach it", d.requests)
	}
}

// TestComputeNodeUndrainCLIRequestPath runs undrain and checks the daemon
// sees DELETE /api/compute/nodes/<name>/drain and the drained:false health
// JSON is printed.
func TestComputeNodeUndrainCLIRequestPath(t *testing.T) {
	d := withComputeDrainDaemon(t)
	out, err := runComputeNodeDrainCmd(t, "undrain", "gpu-box-1")
	if err != nil {
		t.Fatalf("command: %v (out=%s)", err, out)
	}
	if d.method != http.MethodDelete || d.path != "/api/compute/nodes/gpu-box-1/drain" {
		t.Fatalf("request = %s %s, want DELETE /api/compute/nodes/gpu-box-1/drain", d.method, d.path)
	}
	if len(d.body) > 0 {
		t.Errorf("undrain carried a body %v, want none", d.body)
	}
	if !strings.Contains(out, `"drained": false`) {
		t.Errorf("printed health JSON = %q, want drained:false", out)
	}
}
