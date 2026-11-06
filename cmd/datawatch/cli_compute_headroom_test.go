// Tests for `datawatch compute headroom measure|get <node>`. A local httptest
// server stands in for the daemon and records the method and path of the
// node's /headroom route.
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

type cliHeadroomDaemon struct {
	mu     sync.Mutex
	method string
	path   string
	calls  int
}

func withCLIHeadroomDaemon(t *testing.T) *cliHeadroomDaemon {
	t.Helper()
	d := &cliHeadroomDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		d.mu.Lock()
		d.calls++
		d.method, d.path = r.Method, r.URL.Path
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/headroom") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"node":"gpu-1","supported_cap":15,"fresh":true}`))
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d
}

// runComputeHeadroomCmd runs `compute headroom <args...>` and returns stdout.
func runComputeHeadroomCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("pipe: %v", perr)
	}
	orig := os.Stdout
	cmdErr := func() error {
		os.Stdout = w
		cmd := newComputeCmd()
		cmd.SetArgs(append([]string{"headroom"}, args...))
		err := cmd.Execute()
		_ = w.Close()
		os.Stdout = orig
		return err
	}()
	out, _ := io.ReadAll(r)
	return string(out), cmdErr
}

func TestComputeHeadroomCLI_MeasurePostsToRoute(t *testing.T) {
	d := withCLIHeadroomDaemon(t)
	if _, err := runComputeHeadroomCmd(t, "measure", "gpu-1"); err != nil {
		t.Fatalf("measure: %v", err)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-1/headroom" {
		t.Fatalf("request = %s %s, want POST /api/compute/nodes/gpu-1/headroom", d.method, d.path)
	}
}

func TestComputeHeadroomCLI_GetReadsRoute(t *testing.T) {
	d := withCLIHeadroomDaemon(t)
	if _, err := runComputeHeadroomCmd(t, "get", "gpu-1"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if d.method != http.MethodGet || d.path != "/api/compute/nodes/gpu-1/headroom" {
		t.Fatalf("request = %s %s, want GET /api/compute/nodes/gpu-1/headroom", d.method, d.path)
	}
}

// TestComputeHeadroomCLI_ArgsAreChecked: both subcommands take exactly one
// node name, and a missing or extra argument is refused before any request.
func TestComputeHeadroomCLI_ArgsAreChecked(t *testing.T) {
	d := withCLIHeadroomDaemon(t)
	for _, args := range [][]string{{"measure"}, {"get"}, {"get", "a", "b"}, {"measure", "a", "b"}} {
		if _, err := runComputeHeadroomCmd(t, args...); err == nil {
			t.Errorf("headroom %v: expected an argument error", args)
		}
	}
	if d.calls != 0 {
		t.Errorf("bad arguments reached the daemon %d times", d.calls)
	}
}

func TestComputeHeadroomCLI_SubcommandsDefined(t *testing.T) {
	cmd := newComputeCmd()
	for _, path := range [][]string{{"headroom", "measure"}, {"headroom", "get"}} {
		if c, _, err := cmd.Find(path); err != nil || c == nil || c.Name() != path[1] {
			t.Errorf("subcommand %v not defined: %v", path, err)
		}
	}
}
