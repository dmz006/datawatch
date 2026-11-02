// Comm-channel parity tests for the operator console: the `dashboard
// console` command, its one-line summary of the five endpoint sections,
// and `configure dashboard.console.*` keys.

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const routerConsoleFixture = `{
	"generated_at": "2026-11-01T09:00:00Z",
	"sections": {
		"questions_waiting": {"available": true, "count": 2, "summary": "2 sessions waiting on questions", "link": "/console?section=questions"},
		"held_blocked": {"available": true, "count": 0, "summary": "no held or blocked sessions", "link": "/console?section=held"},
		"lanes": {"available": false, "count": 0, "summary": "lane fill is off", "link": "/lanes"},
		"usage": {"available": true, "count": 5, "summary": "$12.50 spent today", "link": "/usage"},
		"ci_release": {"available": true, "count": 1, "summary": "1 CI red on main", "link": "/ci-release"}
	}
}`

func TestParse_DashboardConsole(t *testing.T) {
	for _, in := range []string{"dashboard console", "DASHBOARD CONSOLE", "dashboard-console"} {
		if got := Parse(in).Type; got != CmdDashboardConsole {
			t.Errorf("Parse(%q) = %q, want dashboard console", in, got)
		}
	}
	for _, in := range []string{"dashboard", "dashboard consolex", "dashboard console extra"} {
		if got := Parse(in).Type; got == CmdDashboardConsole {
			t.Errorf("Parse(%q) must not be the console command, got %q", in, got)
		}
	}
}

func TestDashboardConsoleOneLiner(t *testing.T) {
	line, err := dashboardConsoleOneLiner(routerConsoleFixture)
	if err != nil {
		t.Fatalf("dashboardConsoleOneLiner: %v", err)
	}
	for _, want := range []string{
		"questions_waiting 2 — 2 sessions waiting on questions",
		"held_blocked 0 — no held or blocked sessions",
		"lanes off (lane fill is off)",
		"usage 5 — $12.50 spent today",
		"ci_release 1 — 1 CI red on main",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q:\n%s", want, line)
		}
	}
	// Endpoint section order.
	idx := []string{"questions_waiting", "held_blocked", "lanes", "usage", "ci_release"}
	pos := -1
	for _, id := range idx {
		at := strings.Index(line, id)
		if at < pos {
			t.Errorf("section %q out of order:\n%s", id, line)
		}
		pos = at
	}
}

func TestDashboardConsoleCommRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dashboard/console" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(routerConsoleFixture))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	r := loopbackRouter(t, srv)

	out := strings.Join(r.HandleTestMessage("dashboard console"), "\n")
	if !strings.Contains(out, "dashboard console:") {
		t.Fatalf("reply lacks the console label:\n%s", out)
	}
	if !strings.Contains(out, "lanes off (lane fill is off)") {
		t.Errorf("reply lacks the off-section summary:\n%s", out)
	}
	if !strings.Contains(out, "ci_release 1 — 1 CI red on main") {
		t.Errorf("reply lacks the ci_release count+summary:\n%s", out)
	}
}

func TestDashboardConsoleCommDisabledRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the operator console is disabled — set dashboard.console.enabled to true", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	r := loopbackRouter(t, srv)

	out := strings.Join(r.HandleTestMessage("dashboard console"), "\n")
	if !strings.Contains(out, "dashboard console failed") {
		t.Fatalf("reply lacks the failure label:\n%s", out)
	}
	if !strings.Contains(out, "dashboard.console.enabled") {
		t.Errorf("daemon refusal (which key to flip) not surfaced:\n%s", out)
	}
}

func TestHandleConfigure_DashboardConsoleKey(t *testing.T) {
	var gotKey, gotValue string
	r := &Router{hostname: "test"}
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	})

	out := strings.Join(r.HandleTestMessage("configure dashboard.console.enabled=false"), "\n")
	if gotKey != "dashboard.console.enabled" || gotValue != "false" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
	if !strings.Contains(out, "Set dashboard.console.enabled = false") {
		t.Errorf("confirmation missing:\n%s", out)
	}
}

// The documented toggle keys must be discoverable on the two operator
// surfaces (`configure help` and `configure list`).
func TestConfigureHelpAndListMentionDashboardConsoleKeys(t *testing.T) {
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(string, string) error { return nil })

	for _, text := range []string{"help", "list"} {
		out := strings.Join(r.HandleTestMessage("configure "+text), "\n")
		if !strings.Contains(out, "dashboard.console.enabled") {
			t.Errorf("configure %s lacks dashboard.console.enabled:\n%s", text, out)
		}
		if !strings.Contains(out, "dashboard.console.show_lanes") {
			t.Errorf("configure %s lacks dashboard.console.show_lanes:\n%s", text, out)
		}
	}
}
