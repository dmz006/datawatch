package router

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// supervisorWatchCommSample is the supervisor watch read body for the
// comm tests: one shadow would-alert finding, one resolved finding with
// no operator line, and a last comparison where the watch raised one
// entry the script did not.
const supervisorWatchCommSample = `{
  "mode": "shadow",
  "last_poll_at": "2026-10-29T10:15:00Z",
  "findings": [
    {"kind": "task_failed", "subject": "a1b2c3d4",
     "text": "a task in Automaton a1b2c3d4 failed: read the verifier verdict before retrying",
     "first_seen_at": "2026-10-29T09:40:00Z", "last_seen_at": "2026-10-29T10:15:00Z",
     "would_alert": true, "would_alert_at": "2026-10-29T10:10:00Z", "resolved": false},
    {"kind": "worktree_silent", "subject": "85720001",
     "first_seen_at": "2026-10-29T08:00:00Z", "last_seen_at": "2026-10-29T09:00:00Z",
     "resolved": true}
  ],
  "comparison": {
    "last": {"at": "2026-10-29T10:15:00Z", "agreed": false},
    "agreed_days": 2,
    "only_watch": ["task_failed a1b2c3d4"],
    "only_script": []
  }
}`

// supervisorWatchCommLines runs the verb and returns the body lines after
// the "[h] autonomous supervisor-watch" header.
func supervisorWatchCommLines(t *testing.T, r *Router, text string) []string {
	t.Helper()
	out := strings.Join(r.HandleTestMessage(text), "\n")
	header := "[h] autonomous supervisor-watch\n"
	i := strings.Index(out, header)
	if i == -1 {
		t.Fatalf("reply carries no supervisor watch header: %q", out)
	}
	return strings.Split(out[i+len(header):], "\n")
}

// supervisorWatchCLILines renders the same body with the renderer the CLI
// calls, as the expected lines for the comm reply.
func supervisorWatchCLILines(t *testing.T, body string) []string {
	t.Helper()
	var st autonomous.SupervisorWatchStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("sample body: %v", err)
	}
	var b strings.Builder
	autonomous.RenderSupervisorWatch(&b, &st)
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

func TestSupervisorWatchComm_VerbOutput(t *testing.T) {
	if !slices.Contains(SupervisorWatchCommVerbs(), "supervisor-watch") {
		t.Fatalf("SupervisorWatchCommVerbs() = %v, must contain %q", SupervisorWatchCommVerbs(), "supervisor-watch")
	}
	for _, verb := range SupervisorWatchCommVerbs() {
		c := Parse("autonomous " + verb)
		if c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}

	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorWatchCommSample)
	r := setupPeerRouter(t, port)
	lines := supervisorWatchCommLines(t, r, "autonomous supervisor-watch")
	if log.path != "/api/autonomous/supervisor-watch" || log.query != "" {
		t.Errorf("path=%s query=%q; want the supervisor watch route with no filters", log.path, log.query)
	}
	if log.calls != 1 {
		t.Errorf("the read is one call, got %d", log.calls)
	}

	// The reply is the CLI's lines, line for line.
	want := supervisorWatchCLILines(t, supervisorWatchCommSample)
	if !slices.Equal(lines, want) {
		t.Errorf("comm lines differ from the CLI renderer:\n got: %q\nwant: %q", lines, want)
	}

	got := strings.Join(lines, "\n")
	for _, frag := range []string{
		"Supervisor watch mode: shadow",
		"Last poll:",
		"Findings: 2",
		"task_failed",
		"would alert",
		"a task in Automaton a1b2c3d4 failed",
		"worktree_silent",
		"resolved",
		"85720001", // a finding with no operator line falls back to its subject
		"Comparison: differs",
		"days of full agreement: 2",
		"only the watch raised: task_failed a1b2c3d4",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("reply lacks %q:\n%s", frag, got)
		}
	}
	if strings.Contains(got, "only the script raised") {
		t.Errorf("an empty script side must not print a line:\n%s", got)
	}

	// The underscore alias answers the same read.
	_, port = laneFillCommDaemon(t, http.StatusOK, supervisorWatchCommSample)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous supervisor_watch"), " ")
	if !strings.Contains(got, "Supervisor watch mode: shadow") {
		t.Errorf("the underscore alias must answer the read, got: %q", got)
	}
}

func TestSupervisorWatchComm_Empty(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK,
		`{"mode":"shadow","last_poll_at":"0001-01-01T00:00:00Z","findings":[],"comparison":{"last":null,"agreed_days":0,"only_watch":[],"only_script":[]}}`)
	r := setupPeerRouter(t, port)
	lines := supervisorWatchCommLines(t, r, "autonomous supervisor-watch")
	want := []string{
		"Supervisor watch mode: shadow",
		"Last poll: never",
		"No supervisor watch findings.",
		"Comparison: none recorded yet (days of full agreement: 0)",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("empty reply:\n got: %q\nwant: %q", lines, want)
	}

	// The reply never uses the internal name for an Automaton.
	for _, body := range []string{supervisorWatchCommSample, `{"mode":"off","findings":[]}`} {
		_, port = laneFillCommDaemon(t, http.StatusOK, body)
		r = setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous supervisor-watch"), "\n")
		for _, word := range []string{"PRD", "prd"} {
			if strings.Contains(got, word) {
				t.Errorf("the reply must be free of the word %q: %q", word, got)
			}
		}
	}
}

func TestSupervisorWatchComm_UnknownArgumentHelp(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorWatchCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor-watch extra"), " ")
	if !strings.Contains(got, "autonomous supervisor-watch failed") ||
		!strings.Contains(got, "unknown argument extra") ||
		!strings.Contains(got, supervisorWatchUsage) {
		t.Errorf("an unknown argument must be refused naming it, with the usage; got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
	}
}

func TestSupervisorWatchComm_DaemonRefusalPassesThrough(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor-watch"), " ")
	if !strings.Contains(got, "HTTP 503") || !strings.Contains(got, "autonomous disabled") {
		t.Errorf("the daemon refusal must pass through, got: %q", got)
	}
}

// The parity table (sx2_parity.go) lists the verb in the autonomous help
// and dispatches both spellings.
func TestSupervisorWatchComm_ParityTableEntry(t *testing.T) {
	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "supervisor-watch") {
		t.Errorf("the autonomous help must list the supervisor-watch verb; got:\n%s", got)
	}
	for _, verb := range SupervisorWatchCommVerbs() {
		_, port := laneFillCommDaemon(t, http.StatusOK, supervisorWatchCommSample)
		r := setupPeerRouter(t, port)
		out := strings.Join(r.HandleTestMessage("autonomous "+verb), " ")
		if strings.Contains(out, "unknown") || !strings.Contains(out, "Supervisor watch mode") {
			t.Errorf("verb %q must be dispatched by the parity table; got: %q", verb, out)
		}
	}
}

// The alert text the pass raises is a system alert (no session); the
// existing fan-out sends it to the chat channels verbatim through
// Router.SendAlert, so the watch needs no channel code of its own.
func TestSupervisorWatchComm_AlertReachesChatThroughFanOut(t *testing.T) {
	got := make(chan string, 1)
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(s string) { got <- s }},
	}
	a := &alerts.Alert{
		Level:  alerts.LevelWarn,
		Title:  "supervisor watch: task_failed — a1b2c3d4",
		Body:   "The supervisor watch (mode on) has an open finding:\na task in Automaton a1b2c3d4 failed\nopen for 31 minute(s), first seen 2026-10-29 09:40:00.",
		Source: "system",
	}
	r.SendAlert(a)
	select {
	case text := <-got:
		if text != AlertText("h", a) {
			t.Errorf("the chat text must be the alert text verbatim:\n got: %q\nwant: %q", text, AlertText("h", a))
		}
		if !strings.Contains(text, "open for 31 minute(s)") || strings.Contains(text, "PRD") {
			t.Errorf("alert text lost its body or names the internal term: %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the alert never reached the chat backend")
	}
}
