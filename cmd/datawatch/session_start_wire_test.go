package main

import (
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The poller shares the gate of the other two dialog responders, so a dialog
// is never answered by two of them at once.
func TestStartupDialogPollerWaitsForTheOtherResponder(t *testing.T) {
	const id = "testhost-poll"
	claudeStartupAccepted.Store(id, struct{}{}) // another responder is at work
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		pollStartupDialog(id, func(string) bool { calls.Add(1); return true }, time.Millisecond, 5*time.Second)
	}()
	time.Sleep(30 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("answered %d times beside the other responder", n)
	}
	claudeStartupAccepted.Delete(id)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("the poller did not finish")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("answers = %d, want 1", n)
	}
	if _, held := claudeStartupAccepted.Load(id); held {
		t.Fatal("the gate was not given back")
	}
}

// With TLS on its own port the main port only redirects, and the hook script's
// POST does not follow a redirect.
func TestHookURLIsWhereTheDaemonAnswers(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Port, cfg.Server.TLSPort = 8080, 8443
	if got := hookDaemonURL(cfg); got != "http://localhost:8080" {
		t.Errorf("plain: %s", got)
	}
	cfg.Server.TLSEnabled = true
	if got := hookDaemonURL(cfg); got != "https://localhost:8443" {
		t.Errorf("TLS port: %s", got)
	}
	cfg.Server.TLSPort = 0
	if got := hookDaemonURL(cfg); got != "https://localhost:8080" {
		t.Errorf("TLS on the main port: %s", got)
	}
}

// The daemon's wiring: both start callbacks are added (neither replaces the
// other), one of them raises the start alert, and the boot notice is sent
// after the listener that carries alerts to the web and chat is in place.
func TestDaemonStartWiring(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if n := strings.Count(src, "mgr.AddOnSessionStart("); n != 2 {
		t.Errorf("start callbacks added = %d, want 2", n)
	}
	if n := strings.Count(src, `"%s: started (%s)"`); n != 1 {
		t.Errorf("start alerts = %d, want 1", n)
	}
	listener, report := strings.Index(src, "alertStore.AddListener(func"), strings.Index(src, "reportSessionNotices(mgr)")
	if listener < 0 || report < listener {
		t.Errorf("the boot notice is not sent after the alert listener (listener at %d, report at %d)", listener, report)
	}
	if strings.Contains(src, `"memory-wakeup"`) {
		t.Error("memory text is still typed into new sessions")
	}
}
