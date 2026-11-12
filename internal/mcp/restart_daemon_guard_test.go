package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// restartReportServer serves the restart-interruptions report with running
// work that needs force, and counts how often the report was read.
func restartReportServer(t *testing.T, reads *atomic.Int32) (int, func()) {
	t.Helper()
	rep := autonomous.RestartInterruption{
		Policy:       config.RestartPolicyImmediate,
		LiveSessions: []string{"sess-a", "sess-b"},
		RunningTasks: []autonomous.InterruptedTask{{PRDID: "prd-1", TaskID: "task-1", Status: "running", SessionID: "sess-a"}},
		NeedsForce:   true,
	}
	raw, _ := json.Marshal(rep)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/autonomous/restart-interruptions" {
			reads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
			return
		}
		http.NotFound(w, r)
	}))
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	return port, srv.Close
}

func restartDaemonCall(force bool) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"force": force}
	return req
}

func TestRestartDaemonToolRefusesWithoutForceAndListsSessions(t *testing.T) {
	var reads atomic.Int32
	port, done := restartReportServer(t, &reads)
	defer done()
	restarted := make(chan bool, 1)
	s := &Server{webPort: port, restartForceFn: func(force bool) { restarted <- force }}

	res, err := s.handleRestartDaemon(context.Background(), restartDaemonCall(false))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("restart_daemon without force succeeded; want a refusal")
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	for _, want := range []string{"force=true", "sess-a", "sess-b", "prd-1/task-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal result missing %q:\n%s", want, text)
		}
	}
	select {
	case f := <-restarted:
		t.Fatalf("daemon restarted (force=%v) on a refused call", f)
	case <-time.After(900 * time.Millisecond):
	}
}

func TestRestartDaemonToolProceedsWithForce(t *testing.T) {
	var reads atomic.Int32
	port, done := restartReportServer(t, &reads)
	defer done()
	restarted := make(chan bool, 1)
	s := &Server{webPort: port, restartForceFn: func(force bool) { restarted <- force }}

	res, err := s.handleRestartDaemon(context.Background(), restartDaemonCall(true))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatal("restart_daemon with force=true was refused")
	}
	select {
	case f := <-restarted:
		if !f {
			t.Fatal("restart ran with force=false; want force=true passed through")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart with force=true never ran")
	}
}
