// Parser, formatting and loopback tests for the schedule-results
// comm-channel command (read-only list + per-schedule history of
// scheduled jobs' structured results).

package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

// ── Parser tests ──────────────────────────────────────────────────────────────

func TestScheduleResultsCommandParseBare(t *testing.T) {
	c := Parse("schedule-results")
	if c.Type != CmdScheduleResults {
		t.Fatalf("type=%v want %v", c.Type, CmdScheduleResults)
	}
	if c.ScheduleResultsVerb != "list" {
		t.Errorf("verb=%q want list", c.ScheduleResultsVerb)
	}
}

func TestScheduleResultsCommandParseList(t *testing.T) {
	c := Parse("schedule-results list")
	if c.Type != CmdScheduleResults || c.ScheduleResultsVerb != "list" {
		t.Fatalf("got %+v", c)
	}
	if c.ScheduleResultsName != "" {
		t.Errorf("name=%q want empty", c.ScheduleResultsName)
	}
}

func TestScheduleResultsCommandParseName(t *testing.T) {
	c := Parse("schedule-results imap-hourly-rules")
	if c.Type != CmdScheduleResults {
		t.Fatalf("type=%v", c.Type)
	}
	if c.ScheduleResultsVerb != "history" {
		t.Errorf("verb=%q want history", c.ScheduleResultsVerb)
	}
	if c.ScheduleResultsName != "imap-hourly-rules" {
		t.Errorf("name=%q", c.ScheduleResultsName)
	}
}

func TestScheduleResultsCommandParseNameWithSpaces(t *testing.T) {
	c := Parse("schedule-results imap-mcp run-rules")
	if c.ScheduleResultsName != "imap-mcp run-rules" {
		t.Errorf("name=%q want %q", c.ScheduleResultsName, "imap-mcp run-rules")
	}
}

func TestScheduleResultsCommandParseHistoryAlias(t *testing.T) {
	c := Parse("schedule-results history nightly-report")
	if c.ScheduleResultsVerb != "history" || c.ScheduleResultsName != "nightly-report" {
		t.Errorf("got %+v", c)
	}
}

func TestScheduleResultsCommandParseNotConfusedWithSchedule(t *testing.T) {
	c := Parse("schedule-results")
	if c.Type == CmdSchedule || c.Type == CmdScheduleSpawn {
		t.Fatalf("schedule-results parsed as %v — must not collide with schedule", c.Type)
	}
}

// ── Formatting tests ─────────────────────────────────────────────────────────

func TestScheduleResultsCommandFormatListOkWarnError(t *testing.T) {
	now := time.Now()
	mk := func(name, status, summary, title string, d time.Duration) scheduleResultListEntry {
		e := scheduleResultListEntry{ScheduleName: name, Count: 1}
		e.Latest = &scheduleResultEntry{
			Payload:  resultPayload{Title: title, Status: status, Summary: summary},
			PostedAt: now.Add(d),
		}
		return e
	}
	entries := []scheduleResultListEntry{
		mk("disk-check", "error", "mount missing", "check", -3*24*time.Hour),
		mk("imap-hourly-rules", "warn", "2 messages actioned", "40 active rules", -45*time.Minute),
		mk("nightly-report", "ok", "all quiet", "daily digest", -2*time.Minute),
	}
	out := formatScheduleResultsList(entries, now)
	for _, want := range []string{
		"✓ ok", "⚠ warn", "✗ error",
		"nightly-report", "imap-hourly-rules", "disk-check",
		"all quiet", "2 messages actioned",
		"2m ago", "45m ago", "3d ago",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestScheduleResultsCommandFormatListUnsetStatus(t *testing.T) {
	now := time.Now()
	e := scheduleResultListEntry{ScheduleName: "mystery", Count: 1}
	e.Latest = &scheduleResultEntry{
		Payload:  resultPayload{Title: "no status set"},
		PostedAt: now,
	}
	out := formatScheduleResultsList([]scheduleResultListEntry{e}, now)
	if !strings.Contains(out, "• unset") {
		t.Errorf("missing unset marker in:\n%s", out)
	}
}

func TestScheduleResultsCommandFormatListTitleFallback(t *testing.T) {
	now := time.Now()
	e := scheduleResultListEntry{ScheduleName: "x", Count: 1}
	e.Latest = &scheduleResultEntry{
		Payload:  resultPayload{Title: "the title", Status: "ok"},
		PostedAt: now,
	}
	out := formatScheduleResultsList([]scheduleResultListEntry{e}, now)
	if !strings.Contains(out, "the title") {
		t.Errorf("expected title fallback in:\n%s", out)
	}
}

func TestScheduleResultsCommandFormatHistoryNewestFirst(t *testing.T) {
	now := time.Now()
	mk := func(d time.Duration, s, txt string) scheduleResultEntry {
		return scheduleResultEntry{
			Payload:  resultPayload{Status: s, Summary: txt},
			PostedAt: now.Add(d),
		}
	}
	// Deliberately oldest-first, as the API returns it; the formatter
	// must sort newest first.
	entries := []scheduleResultEntry{mk(-3*time.Hour, "ok", "old-entry"), mk(-1*time.Hour, "warn", "mid-entry"), mk(-5*time.Minute, "error", "new-entry")}
	out := formatScheduleResultHistory("nightly-report", entries, now)
	iNew, iMid, iOld := strings.Index(out, "new-entry"), strings.Index(out, "mid-entry"), strings.Index(out, "old-entry")
	if !(iNew < iMid && iMid < iOld) {
		t.Errorf("expected newest first:\n%s", out)
	}
	if !strings.Contains(out, `"nightly-report"`) || !strings.Contains(out, "3 retained result") {
		t.Errorf("missing header info:\n%s", out)
	}
}

func TestScheduleResultsCommandFormatHistoryTruncation(t *testing.T) {
	now := time.Now()
	entries := make([]scheduleResultEntry, 0, scheduleResultsHistoryLimit+5)
	for i := 0; i < scheduleResultsHistoryLimit+5; i++ {
		entries = append(entries, scheduleResultEntry{
			Payload:  resultPayload{Status: "ok", Summary: "e"},
			PostedAt: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	out := formatScheduleResultHistory("big", entries, now)
	if !strings.Contains(out, "of 15 retained") {
		t.Errorf("expected truncation note:\n%s", out)
	}
}

// ── Handler tests over a fake loopback ───────────────────────────────────────

func TestScheduleResultsCommandHandlerListEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schedule-results", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := captureSends(router, router.handleScheduleResultsList)
	if !strings.Contains(out, "No schedule results recorded yet") {
		t.Errorf("expected empty-state message, got:\n%s", out)
	}
}

func TestScheduleResultsCommandHandlerList(t *testing.T) {
	body := `[
	  {"schedule_name":"nightly-report","latest":{"payload":{"title":"daily digest","status":"ok","summary":"all quiet"},"posted_at":"2026-05-01T00:00:00Z"},"count":12},
	  {"schedule_name":"disk-check","latest":{"payload":{"title":"check","status":"error","summary":"mount missing"},"posted_at":"2026-05-01T00:00:00Z"},"count":4}
	]`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schedule-results", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := captureSends(router, router.handleScheduleResultsList)
	for _, want := range []string{"✓ ok", "✗ error", "nightly-report", "disk-check", "all quiet"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestScheduleResultsCommandHandlerHistory(t *testing.T) {
	body := `[
	  {"payload":{"title":"t1","status":"warn","summary":"first-entry"},"posted_at":"2026-04-30T00:00:00Z"},
	  {"payload":{"title":"t2","status":"ok","summary":"second-entry"},"posted_at":"2026-05-01T00:00:00Z"}
	]`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schedule-results/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") != "nightly-report" {
			t.Errorf("path name = %q", r.PathValue("name"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := captureSends(router, func() {
		router.handleScheduleResultsHistory(Command{Type: CmdScheduleResults, ScheduleResultsVerb: "history", ScheduleResultsName: "nightly-report"})
	})
	for _, want := range []string{"⚠ warn", "✓ ok", "first-entry", "second-entry"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if iSecond, iFirst := strings.Index(out, "second-entry"), strings.Index(out, "first-entry"); !(iSecond < iFirst) {
		t.Errorf("expected newest first:\n%s", out)
	}
}

func TestScheduleResultsCommandHandlerUnknownSchedule(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schedule-results/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no results recorded for this schedule"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := captureSends(router, func() {
		router.handleScheduleResultsHistory(Command{Type: CmdScheduleResults, ScheduleResultsVerb: "history", ScheduleResultsName: "nope-missing"})
	})
	if !strings.Contains(out, "No results recorded for") {
		t.Errorf("expected unknown-schedule message, got:\n%s", out)
	}
	if strings.Contains(out, "HTTP 404") {
		t.Errorf("raw HTTP error leaked to chat:\n%s", out)
	}
}

func TestScheduleResultsCommandHandlerLoopbackUnconfigured(t *testing.T) {
	// webPort == 0 — every form must reply with a failure, not panic.
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		r.handleScheduleResultsCmd(Command{Type: CmdScheduleResults, ScheduleResultsVerb: "list"})
		r.handleScheduleResultsCmd(Command{Type: CmdScheduleResults, ScheduleResultsVerb: "history", ScheduleResultsName: "x"})
	})
	if !strings.Contains(out, "loopback not configured") {
		t.Errorf("expected loopback failure reply, got:\n%s", out)
	}
}

func TestScheduleResultsCommandHandlerFullMessage(t *testing.T) {
	// End-to-end through the public test harness: parse + dispatch + send.
	body := `[
	  {"schedule_name":"nightly-report","latest":{"payload":{"title":"daily digest","status":"ok","summary":"all quiet"},"posted_at":"2026-05-01T00:00:00Z"},"count":12}
	]`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schedule-results", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	router := loopbackRouter(t, srv)
	out := strings.Join(router.HandleTestMessage("schedule-results list"), "\n")
	for _, want := range []string{"✓ ok", "nightly-report", "all quiet"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// ── Test helpers ─────────────────────────────────────────────────────────────

// loopbackRouter builds a router pointed at the given test HTTP server.
func loopbackRouter(t *testing.T, srv *httptest.Server) *Router {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "http://")
	portStr := host[strings.LastIndexByte(host, ':')+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("cannot parse port from %s: %v", srv.URL, err)
	}
	return &Router{hostname: "h", webPort: port}
}

// captureSends swaps in a draining backend, runs fn and returns everything
// that was sent. r.send dispatches on a goroutine, so a short settle sleep
// is required (same technique HandleTestMessage uses).
func captureSends(r *Router, fn func()) string {
	db := &drainBackend{}
	r.backend = db
	fn()
	time.Sleep(120 * time.Millisecond)
	db.mu.Lock()
	defer db.mu.Unlock()
	return strings.Join(db.parts, "\n---\n")
}

// drainBackend records every sent message.
type drainBackend struct {
	mu    sync.Mutex
	parts []string
}

func (b *drainBackend) Name() string { return "drain" }
func (b *drainBackend) Send(_, message string) error {
	b.mu.Lock()
	b.parts = append(b.parts, message)
	b.mu.Unlock()
	return nil
}
func (b *drainBackend) Subscribe(_ context.Context, _ func(messaging.Message)) error { return nil }
func (b *drainBackend) Link(_ string, _ func(string)) error                          { return nil }
func (b *drainBackend) SelfID() string                                               { return "test" }
func (b *drainBackend) Close() error                                                 { return nil }
