// Tests for `datawatch schedule-results` (list / history / post).

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- shared helpers for this file -------------------------------------------

// unsetEnvForTest removes an env var for the test duration, restoring it
// (t.Setenv cannot unset).
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key) //nolint:errcheck
		}
	})
}

// recordingScheduleResultsDaemon stands up a fake daemon that records each
// request (method, path, headers, body) and answers with the canned body.
// serverURL is pointed at it for the test.
func recordingScheduleResultsDaemon(t *testing.T, status int, body string) *[]recordedReq {
	t.Helper()
	var mu sync.Mutex
	var seen []recordedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, recordedReq{
			Method: r.Method,
			Path:   r.URL.EscapedPath(),
			Auth:   r.Header.Get("Authorization"),
			Body:   string(b),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return &seen
}

type recordedReq struct {
	Method string
	Path   string
	Auth   string
	Body   string
}

// --- payload builder --------------------------------------------------------

func TestScheduleResultsCLIPayloadBuild(t *testing.T) {
	payload, err := buildScheduleResultsPayload(scheduleResultsPostFlags{
		Title:    "40 active rules",
		Status:   "ok",
		Summary:  "2 messages actioned",
		Metrics:  `{"rules": 40, "actioned": 2}`,
		Tables:   []string{`{"title":"Actions","columns":["msg","action"],"rows":[[1,"flagged"]]}`},
		Markdown: "details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["title"] != "40 active rules" || payload["status"] != "ok" || payload["summary"] != "2 messages actioned" {
		t.Errorf("scalar fields wrong: %v", payload)
	}
	m, ok := payload["metrics"].(map[string]any)
	if !ok || m["rules"] != float64(40) || m["actioned"] != float64(2) {
		t.Errorf("metrics = %#v, want object with rules=40 actioned=2", payload["metrics"])
	}
	tables, ok := payload["tables"].([]any)
	if !ok || len(tables) != 1 {
		t.Fatalf("tables = %#v, want 1 object", payload["tables"])
	}
	tbl, _ := tables[0].(map[string]any)
	if tbl["title"] != "Actions" {
		t.Errorf("table[0] title = %v", tbl["title"])
	}
	if payload["markdown"] != "details" {
		t.Errorf("markdown = %v", payload["markdown"])
	}
}

func TestScheduleResultsCLIPayloadBuildRejects(t *testing.T) {
	cases := []struct {
		name    string
		flags   scheduleResultsPostFlags
		contain string
	}{
		{
			name:    "missing title",
			flags:   scheduleResultsPostFlags{Summary: "no title"},
			contain: "title",
		},
		{
			name:    "bad status",
			flags:   scheduleResultsPostFlags{Title: "t", Status: "sometimes-works"},
			contain: `--status "sometimes-works"`,
		},
		{
			name:    "metrics not an object",
			flags:   scheduleResultsPostFlags{Title: "t", Metrics: `[1,2]`},
			contain: "--metrics",
		},
		{
			name:    "table not an object",
			flags:   scheduleResultsPostFlags{Title: "t", Tables: []string{`"nope"`}},
			contain: "--table[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildScheduleResultsPayload(tc.flags)
			if err == nil || !strings.Contains(err.Error(), tc.contain) {
				t.Errorf("err = %v, want containing %q", err, tc.contain)
			}
		})
	}
}

func TestScheduleResultsCLIPayloadBuildFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(file, []byte(`{
		"title": "From file",
		"status": "warn",
		"summary": "file summary",
		"metrics": {"from": "file"}
	}`), 0644); err != nil {
		t.Fatal(err)
	}

	// File alone is a complete payload.
	payload, err := buildScheduleResultsPayload(scheduleResultsPostFlags{File: file})
	if err != nil {
		t.Fatal(err)
	}
	if payload["title"] != "From file" || payload["status"] != "warn" || payload["summary"] != "file summary" {
		t.Errorf("file payload not honored: %v", payload)
	}
	if m := payload["metrics"].(map[string]any)["from"]; m != "file" {
		t.Errorf("file metrics = %v", payload["metrics"])
	}

	// Flags override the file's fields.
	payload, err = buildScheduleResultsPayload(scheduleResultsPostFlags{
		File:    file,
		Status:  "ok",
		Summary: "flag summary",
		Tables:  []string{`{"title":"override"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ok" || payload["summary"] != "flag summary" {
		t.Errorf("flags must override file: %v", payload)
	}
	if tables := payload["tables"].([]any); len(tables) != 1 || tables[0].(map[string]any)["title"] != "override" {
		t.Errorf("--tables must replace file tables: %v", payload["tables"])
	}
	// Title untouched by flags still comes from the file.
	if payload["title"] != "From file" {
		t.Errorf("title = %v", payload["title"])
	}

	// Flags alone can supply the title for a title-less file.
	titleless := filepath.Join(dir, "titleless.json")
	mustWrite(t, titleless, `{"summary": "s"}`)
	if _, err := buildScheduleResultsPayload(scheduleResultsPostFlags{File: titleless}); err == nil {
		t.Error("titleless file without --title: want error, got nil")
	}
	if _, err := buildScheduleResultsPayload(scheduleResultsPostFlags{File: titleless, Title: "added"}); err != nil {
		t.Errorf("file + --title should succeed: %v", err)
	}

	// Non-object and missing files are rejected.
	bad := filepath.Join(dir, "bad.json")
	mustWrite(t, bad, `[1]`)
	if _, err := buildScheduleResultsPayload(scheduleResultsPostFlags{File: bad, Title: "t"}); err == nil {
		t.Error("non-object --file: want error, got nil")
	}
	if _, err := buildScheduleResultsPayload(scheduleResultsPostFlags{File: filepath.Join(dir, "missing.json")}); err == nil {
		t.Error("missing --file: want error, got nil")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- env defaults -----------------------------------------------------------

func TestScheduleResultsCLIEnvDefaults(t *testing.T) {
	// --session flag wins over the env.
	t.Setenv("DATAWATCH_SESSION_ID", "env-session")
	id, err := scheduleResultsPostSession("flag-session")
	if err != nil || id != "flag-session" {
		t.Errorf("flag session = %q, err = %v", id, err)
	}
	// Env default when the flag is absent.
	id, err = scheduleResultsPostSession("")
	if err != nil || id != "env-session" {
		t.Errorf("env session = %q, err = %v", id, err)
	}

	unsetEnvForTest(t, "DATAWATCH_SESSION_ID")
	if _, err := scheduleResultsPostSession(""); err == nil || !strings.Contains(err.Error(), "--session") {
		t.Errorf("missing session: err = %v, want --session error", err)
	}
}

func TestScheduleResultsCLIPostTokenPreference(t *testing.T) {
	// DATAWATCH_RESULT_TOKEN takes precedence over DATAWATCH_TOKEN, even when
	// both are set: the spawn-scoped credential must not be shadowed by the
	// generic daemon token.
	t.Setenv("DATAWATCH_TOKEN", "generic-daemon-token")
	t.Setenv("DATAWATCH_RESULT_TOKEN", "scoped-result-token")
	if got := scheduleResultsPostToken(); got != "scoped-result-token" {
		t.Errorf("token = %q, want scoped-result-token", got)
	}

	unsetEnvForTest(t, "DATAWATCH_RESULT_TOKEN")
	if got := scheduleResultsPostToken(); got != "generic-daemon-token" {
		t.Errorf("token fallback = %q, want generic-daemon-token", got)
	}
}

// --- command-level tests against an httptest daemon --------------------------

func TestScheduleResultsCLIListFormatted(t *testing.T) {
	body := `[
	  {"schedule_name": "imap-mcp run-rules",
	   "latest": {"posted_at": "2026-12-01T06:30:00Z",
	           "session_id": "host-a1b2",
	           "schedule_name": "imap-mcp run-rules",
	           "payload": {"title": "40 active rules", "status": "ok", "summary": "2 messages actioned", "metrics": {"rules": 40}}},
	   "count": 3, "recent_statuses": ["ok", "warn", "ok"]}
	]`
	seen := recordingScheduleResultsDaemon(t, 200, body)
	t.Setenv("DATAWATCH_TOKEN", "read-token")

	out := captureStdout(t, func() {
		_ = runCmd(t, newScheduleResultsCmd(), "list")
	})

	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	req := (*seen)[0]
	if req.Method != "GET" || req.Path != "/api/schedule-results" {
		t.Errorf("request = %#v", req)
	}
	if req.Auth != "Bearer read-token" {
		t.Errorf("Authorization = %q", req.Auth)
	}

	for _, want := range []string{
		"SCHEDULE", "STATUS", "TITLE", "SUMMARY", "POSTED", "RETAINED", "RECENT",
		"imap-mcp run-rules", "40 active rules", "2 messages actioned",
		"2026-12-01 06:30:00 UTC", "ok,warn,ok",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q\noutput:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "3") {
		t.Errorf("list output missing retained count 3:\n%s", out)
	}
}

func TestScheduleResultsCLIListEmpty(t *testing.T) {
	recordingScheduleResultsDaemon(t, 200, `[]`)
	t.Setenv("DATAWATCH_TOKEN", "read-token")
	out := captureStdout(t, func() {
		_ = runCmd(t, newScheduleResultsCmd(), "list")
	})
	if !strings.Contains(out, "No scheduled-job results have been posted yet.") {
		t.Errorf("empty list output = %q", out)
	}
}

func TestScheduleResultsCLIHistoryFormatted(t *testing.T) {
	body := `[
	  {"posted_at": "2026-12-01T05:30:00Z", "session_id": "host-old",
	   "schedule_name": "nightly",
	   "payload": {"title": "Old run", "status": "warn", "summary": "flaky",
	            "metrics": {}, "tables": [], "markdown": ""}},
	  {"posted_at": "2026-12-01T06:30:00Z", "session_id": "host-a1b2",
	   "schedule_name": "nightly",
	   "payload": {"title": "Build OK", "status": "ok", "summary": "40 tests",
	            "metrics": {"score": 1},
	            "tables": [{"title": "a"}, {"title": "b"}],
	            "markdown": "short notes"}}
	]`
	seen := recordingScheduleResultsDaemon(t, 200, body)
	t.Setenv("DATAWATCH_TOKEN", "read-token")

	out := captureStdout(t, func() {
		_ = runCmd(t, newScheduleResultsCmd(), "history", "nightly")
	})

	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	if got := (*seen)[0].Path; got != "/api/schedule-results/nightly" {
		t.Errorf("path = %q, want /api/schedule-results/nightly", got)
	}
	for _, want := range []string{
		"2026-12-01 05:30:00 UTC", "Old run",
		"2026-12-01 06:30:00 UTC", "Build OK",
		"session: host-a1b2", "summary: 40 tests",
		`metrics: {"score":1}`, "tables: 2 table(s)", "markdown: short notes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("history output missing %q\noutput:\n%s", want, out)
		}
	}
	// Oldest first: the server order must be preserved.
	iOld, iNew := strings.Index(out, "Old run"), strings.Index(out, "Build OK")
	if iOld < 0 || iNew < 0 || iOld > iNew {
		t.Errorf("entries must render oldest first:\n%s", out)
	}
}

func TestScheduleResultsCLIHistoryEscapesName(t *testing.T) {
	seen := recordingScheduleResultsDaemon(t, 200, `[]`)
	t.Setenv("DATAWATCH_TOKEN", "read-token")
	captureStdout(t, func() {
		_ = runCmd(t, newScheduleResultsCmd(), "history", "imap-mcp run-rules")
	})
	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	if got := (*seen)[0].Path; got != "/api/schedule-results/imap-mcp%20run-rules" {
		t.Errorf("path = %q, want escaped space", got)
	}
}

func TestScheduleResultsCLIHistoryReports404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `no results recorded for schedule "ghost"`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	t.Setenv("DATAWATCH_TOKEN", "read-token")

	err := runCmd(t, newScheduleResultsCmd(), "history", "ghost")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no results recorded") {
		t.Errorf("err = %v, want 404 with server body", err)
	}
}

func TestScheduleResultsCLIPostSendsPayload(t *testing.T) {
	seen := recordingScheduleResultsDaemon(t, 200, `{"accepted": true}`)
	t.Setenv("DATAWATCH_TOKEN", "generic-daemon-token")
	t.Setenv("DATAWATCH_RESULT_TOKEN", "scoped-result-token")
	t.Setenv("DATAWATCH_SESSION_ID", "host-a1b2")

	out := captureStdout(t, func() {
		_ = runCmd(t, newScheduleResultsCmd(),
			"post",
			"--title", "40 active rules",
			"--status", "ok",
			"--summary", "2 messages actioned",
			"--metrics", `{"rules": 40, "actioned": 2}`,
			"--table", `{"title":"Actions","columns":["n"],"rows":[[2]]}`,
		)
	})

	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	req := (*seen)[0]
	if req.Method != "POST" || req.Path != "/api/sessions/host-a1b2/result" {
		t.Errorf("request = %#v", req)
	}
	if req.Auth != "Bearer scoped-result-token" {
		t.Errorf("Authorization = %q, want the result token", req.Auth)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(req.Body), &sent); err != nil {
		t.Fatalf("body is not JSON: %v\nbody=%s", err, req.Body)
	}
	if sent["title"] != "40 active rules" || sent["status"] != "ok" || sent["summary"] != "2 messages actioned" {
		t.Errorf("body = %v", sent)
	}
	if m := sent["metrics"].(map[string]any)["rules"]; m != float64(40) {
		t.Errorf("metrics = %v", sent["metrics"])
	}
	if !strings.Contains(out, "accepted") {
		t.Errorf("response was not formatted: %q", out)
	}
}

func TestScheduleResultsCLIPostExplicitSessionOverridesEnv(t *testing.T) {
	seen := recordingScheduleResultsDaemon(t, 200, `{"accepted": true}`)
	t.Setenv("DATAWATCH_SESSION_ID", "env-session")
	t.Setenv("DATAWATCH_RESULT_TOKEN", "tok")

	captureStdout(t, func() {
		_ = runCmd(t, newScheduleResultsCmd(), "post", "--session", "flag-session", "--title", "t")
	})
	if got := (*seen)[0].Path; got != "/api/sessions/flag-session/result" {
		t.Errorf("path = %q, want /api/sessions/flag-session/result", got)
	}
}

func TestScheduleResultsCLIPostErrors(t *testing.T) {
	t.Setenv("DATAWATCH_RESULT_TOKEN", "tok")

	// Missing title: a clear client-side error, no HTTP request sent.
	seen := recordingScheduleResultsDaemon(t, 200, `{}`)
	t.Setenv("DATAWATCH_SESSION_ID", "sess-1")
	err := runCmd(t, newScheduleResultsCmd(), "post", "--summary", "no title at all")
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("missing title: err = %v, want title error", err)
	}
	if got := len(*seen); got != 0 {
		t.Errorf("missing title: %d requests sent, want 0 (client-side validation)", got)
	}

	unsetEnvForTest(t, "DATAWATCH_SESSION_ID")
	err = runCmd(t, newScheduleResultsCmd(), "post", "--title", "t")
	if err == nil || !strings.Contains(err.Error(), "--session") {
		t.Errorf("missing session: err = %v, want --session error", err)
	}

	// Server rejection surfaced verbatim.
	recordingScheduleResultsDaemon(t, 403, `{"error":"not authorized to write this session's result"}`)
	t.Setenv("DATAWATCH_SESSION_ID", "sess-1")
	err = runCmd(t, newScheduleResultsCmd(), "post", "--session", "sess-1", "--title", "t")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("403: err = %v, want status + body", err)
	}
}
