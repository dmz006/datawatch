package router

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// restartCommRouter returns a router whose loopback serves a restart
// interruption report that needs force (a live session and a running task),
// and whose force-aware restart reports the force value it was given.
func restartCommRouter(t *testing.T) (*Router, chan bool, func()) {
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
	restarted := make(chan bool, 1)
	r := &Router{hostname: "h", webPort: port, backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetRestartFuncForce(func(force bool) { restarted <- force })
	return r, restarted, srv.Close
}

func TestRestartCommandParsesForceWord(t *testing.T) {
	cases := map[string]bool{
		"restart":            false,
		"restart force":      true,
		"Restart FORCE":      true,
		"restart now force":  true,
		"restart forcefully": false,
	}
	for text, want := range cases {
		cmd := Parse(text)
		if cmd.Type != CmdRestart {
			t.Fatalf("Parse(%q) type = %v; want CmdRestart", text, cmd.Type)
		}
		if cmd.RestartForce != want {
			t.Errorf("Parse(%q).RestartForce = %v; want %v", text, cmd.RestartForce, want)
		}
	}
}

func TestRestartCommandRefusesWithoutForceAndListsSessions(t *testing.T) {
	r, restarted, done := restartCommRouter(t)
	defer done()
	replies := r.HandleTestMessage("restart")
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "restart refused") {
		t.Fatalf("restart without force not refused: %q", joined)
	}
	for _, want := range []string{"sess-a", "sess-b", "prd-1/task-1", "restart force"} {
		if !strings.Contains(joined, want) {
			t.Errorf("refusal missing %q: %q", want, joined)
		}
	}
	select {
	case f := <-restarted:
		t.Fatalf("daemon restarted (force=%v) on a refused message", f)
	case <-time.After(900 * time.Millisecond):
	}
}

func TestRestartCommandProceedsWithForce(t *testing.T) {
	r, restarted, done := restartCommRouter(t)
	defer done()
	replies := r.HandleTestMessage("restart force")
	if strings.Contains(strings.Join(replies, "\n"), "restart refused") {
		t.Fatalf("restart force was refused: %q", replies)
	}
	select {
	case f := <-restarted:
		if !f {
			t.Fatal("restart ran with force=false; want force=true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart force never ran")
	}
}
