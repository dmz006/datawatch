package main

import (
	"strings"
	"sync"
	"testing"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/session"
)

// The notice for sessions found dead at start is one system alert: the alert
// list holds it, and the alert listener, which feeds the web app and, for an
// alert with no session, the chat channels, receives it once.
func TestBootNoticeIsOneSystemAlertForEverySurface(t *testing.T) {
	mgr, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.WithFakeTmux()
	for _, id := range []string{"aa01", "aa02"} {
		if err := mgr.SaveSession(&session.Session{ID: id, FullID: "testhost-" + id, Hostname: "testhost",
			TmuxSession: "cs-" + id, State: session.StateRunning}); err != nil {
			t.Fatal(err)
		}
	}
	mgr.ResumeMonitors(t.Context()) // before alerts can be delivered

	store, err := alertspkg.NewStore(t.TempDir() + "/alerts.json")
	if err != nil {
		t.Fatal(err)
	}
	alertspkg.SetGlobal(store)
	t.Cleanup(func() { alertspkg.SetGlobal(nil) })
	var mu sync.Mutex
	var got []alertspkg.Alert
	store.AddListener(func(a *alertspkg.Alert) { mu.Lock(); got = append(got, *a); mu.Unlock() })

	reportSessionNotices(mgr)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want 1: %+v", len(got), got)
	}
	a := got[0]
	if a.SessionID != "" || a.SkipCommFanout {
		t.Errorf("would not reach the chat channels: %+v", a)
	}
	if !strings.Contains(a.Title, "2 session(s) found dead") || !strings.Contains(a.Body, "aa01") || !strings.Contains(a.Body, "aa02") {
		t.Errorf("%s / %s", a.Title, a.Body)
	}
}
