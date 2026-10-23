// Tests for the compact session-list view (session_list_view.go) and the
// /api/info capability list (api_capabilities.go). BL432 / GH#242.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		n       int
		want    string
		wantCut bool
	}{
		{"short untouched", "hello", 10, "hello", false},
		{"exact length untouched", "hello", 5, "hello", false},
		{"empty untouched", "", 5, "", false},
		{"ascii cut", "hello world", 5, "hello…", true},
		{"multi-byte cut on rune boundary", "héllo wörld", 7, "héllo w…", true},
		{"cjk cut", "日本語のテキスト", 3, "日本語…", true},
		{"emoji cut", "🙂🙂🙂🙂", 2, "🙂🙂…", true},
		{"cjk exact untouched", "日本語", 3, "日本語", false},
		{"zero n disables", "hello", 0, "hello", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cut := truncateRunes(tc.in, tc.n)
			if got != tc.want || cut != tc.wantCut {
				t.Errorf("truncateRunes(%q,%d) = %q,%v want %q,%v", tc.in, tc.n, got, cut, tc.want, tc.wantCut)
			}
		})
	}
}

func TestSummarizeSession_TruncatedFieldsAndNoMutation(t *testing.T) {
	long := strings.Repeat("ü", 50)
	cases := []struct {
		name    string
		sess    session.Session
		wantCut []string
	}{
		{"nothing cut", session.Session{Task: "short", LastResponse: "ok"}, nil},
		{"task only", session.Session{Task: long, LastResponse: "ok"}, []string{"task"}},
		{"all four", session.Session{Task: long, PromptContext: long, LastResponse: long, LastSummaryLong: long},
			[]string{"task", "prompt_context", "last_response", "last_summary_long"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := tc.sess
			in := &tc.sess
			out, cut := summarizeSession(in, 20)
			if strings.Join(cut, ",") != strings.Join(tc.wantCut, ",") {
				t.Errorf("cut=%v want %v", cut, tc.wantCut)
			}
			if out == in {
				t.Fatal("summarizeSession returned the input pointer, want a copy")
			}
			if in.Task != orig.Task || in.PromptContext != orig.PromptContext ||
				in.LastResponse != orig.LastResponse || in.LastSummaryLong != orig.LastSummaryLong {
				t.Error("input session was mutated")
			}
			for _, f := range []string{out.Task, out.PromptContext, out.LastResponse, out.LastSummaryLong} {
				if n := len([]rune(f)); n > 21 {
					t.Errorf("field has %d runes, want <= 21", n)
				}
			}
		})
	}
}

func TestParseSessionListOptions(t *testing.T) {
	ts := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		query      string
		wantErr    bool
		wantSum    bool
		wantSince  time.Time
		wantStates []string
	}{
		{query: "", wantSum: false},
		{query: "view=summary", wantSum: true},
		{query: "view=full", wantErr: true},
		{query: "view=", wantErr: true},
		{query: "since=2026-10-01T12:00:00Z", wantSince: ts},
		{query: "since=" + strconv.FormatInt(ts.Unix(), 10), wantSince: ts},
		{query: "since=2026-10-01T12:00:00.5Z", wantSince: ts.Add(500 * time.Millisecond)},
		{query: "since=yesterday", wantErr: true},
		{query: "states=active", wantStates: []string{"rate_limited", "running", "waiting_input"}},
		{query: "states=complete,%20failed", wantStates: []string{"complete", "failed"}},
		{query: "states=active,killed", wantStates: []string{"killed", "rate_limited", "running", "waiting_input"}},
		{query: "states=bogus", wantErr: true},
		{query: "states=,", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.query)
			o, err := parseSessionListOptions(q)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", o)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if o.summary != tc.wantSum {
				t.Errorf("summary=%v want %v", o.summary, tc.wantSum)
			}
			if !tc.wantSince.IsZero() && !o.since.Equal(tc.wantSince) {
				t.Errorf("since=%v want %v", o.since, tc.wantSince)
			}
			if tc.wantStates != nil {
				var got []string
				for st := range o.states {
					got = append(got, string(st))
				}
				sort.Strings(got)
				if strings.Join(got, ",") != strings.Join(tc.wantStates, ",") {
					t.Errorf("states=%v want %v", got, tc.wantStates)
				}
			}
			if tc.query == "" && o.active() {
				t.Error("no params must not activate the view path")
			}
		})
	}
}

func TestStateIsTerminal(t *testing.T) {
	want := map[session.State]bool{
		session.StateRunning: false, session.StateWaitingInput: false, session.StateRateLimited: false,
		session.StateComplete: true, session.StateFailed: true, session.StateKilled: true,
	}
	for st, w := range want {
		if st.IsTerminal() != w {
			t.Errorf("%s.IsTerminal()=%v want %v", st, !w, w)
		}
	}
}

func TestFilterSessionList(t *testing.T) {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	sessions := []*session.Session{
		{FullID: "a", State: session.StateRunning, UpdatedAt: base.Add(-time.Hour)},
		{FullID: "b", State: session.StateComplete, UpdatedAt: base},
		{FullID: "c", State: session.StateWaitingInput, UpdatedAt: base.Add(time.Hour)},
		{FullID: "d", State: session.StateKilled, UpdatedAt: base.Add(2 * time.Hour)},
	}
	cases := []struct {
		query string
		want  string
	}{
		{"", "a,b,c,d"},
		{"since=2026-10-01T00:00:00Z", "c,d"}, // strictly after: b excluded
		{"states=active", "a,c"},
		{"states=complete,killed", "b,d"},
		{"since=2026-10-01T00:00:00Z&states=active", "c"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.query)
			o, err := parseSessionListOptions(q)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, s := range filterSessionList(sessions, o) {
				ids = append(ids, s.FullID)
			}
			if got := strings.Join(ids, ","); got != tc.want {
				t.Errorf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestCapTerminalSessions(t *testing.T) {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }
	sessions := []*session.Session{
		{FullID: "old-done", State: session.StateComplete, UpdatedAt: at(1)},
		{FullID: "run-old", State: session.StateRunning, UpdatedAt: at(0)},
		{FullID: "new-fail", State: session.StateFailed, UpdatedAt: at(5)},
		{FullID: "mid-kill", State: session.StateKilled, UpdatedAt: at(3)},
		{FullID: "rl", State: session.StateRateLimited, UpdatedAt: at(-10)},
	}
	cases := []struct {
		limit       int
		want        string
		wantOmitted int
	}{
		{0, "old-done,run-old,new-fail,mid-kill,rl", 0},
		{3, "old-done,run-old,new-fail,mid-kill,rl", 0},
		{10, "old-done,run-old,new-fail,mid-kill,rl", 0},
		{2, "run-old,new-fail,mid-kill,rl", 1},
		{1, "run-old,new-fail,rl", 2},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.limit), func(t *testing.T) {
			got, omitted := capTerminalSessions(sessions, tc.limit)
			var ids []string
			for _, s := range got {
				ids = append(ids, s.FullID)
			}
			if strings.Join(ids, ",") != tc.want || omitted != tc.wantOmitted {
				t.Errorf("limit %d: got %v omitted %d, want %s omitted %d", tc.limit, ids, omitted, tc.want, tc.wantOmitted)
			}
		})
	}
}

// sessionViewServer returns a bl90 server seeded with the given sessions
// and summary limits.
func sessionViewServer(t *testing.T, terminalLimit, textChars int, sessions ...*session.Session) *Server {
	t.Helper()
	s := bl90Server(t)
	s.cfg.Server.SessionSummaryTerminalLimit = terminalLimit
	s.cfg.Server.SessionSummaryTextChars = textChars
	for _, sess := range sessions {
		if err := s.manager.SaveSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func getSessions(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?"+query, nil)
	rr := httptest.NewRecorder()
	s.handleSessions(rr, req)
	return rr
}

func TestHandleSessions_SummaryView(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	longTask := strings.Repeat("タスク", 100)
	s := sessionViewServer(t, 1, 20,
		&session.Session{ID: "a1", FullID: "h-a1", State: session.StateRunning, Task: longTask, LastResponse: "fine", UpdatedAt: base},
		&session.Session{ID: "b2", FullID: "h-b2", State: session.StateComplete, Task: "done", UpdatedAt: base.Add(time.Minute)},
		&session.Session{ID: "c3", FullID: "h-c3", State: session.StateFailed, Task: "older", UpdatedAt: base.Add(-time.Minute)},
	)

	rr := getSessions(t, s, "view=summary")
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get(sessionsOmittedHeader); got != "1" {
		t.Errorf("%s=%q want 1", sessionsOmittedHeader, got)
	}
	var recs []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &recs); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, r := range recs {
		byID[r["full_id"].(string)] = r
	}
	if _, ok := byID["h-c3"]; ok {
		t.Error("older terminal session should have been capped")
	}
	if _, ok := byID["h-b2"]; !ok {
		t.Error("newest terminal session missing")
	}
	a := byID["h-a1"]
	if a == nil {
		t.Fatal("running session missing")
	}
	for _, f := range []string{"task", "prompt_context", "last_response", "last_summary_long", "truncated_fields"} {
		if _, ok := a[f]; ok {
			t.Errorf("summary row must not carry %q", f)
		}
	}
	if a["status_line"] != "fine" {
		t.Errorf("status_line=%v want fine", a["status_line"])
	}

	// The manager's live record must still hold the full task.
	live, ok := s.manager.GetSession("h-a1")
	if !ok || live.Task != longTask {
		t.Error("summary view mutated the manager's session record")
	}
}

func TestHandleSessions_SummaryNoCapNoHeader(t *testing.T) {
	s := sessionViewServer(t, 0, 200,
		&session.Session{ID: "a1", FullID: "h-a1", State: session.StateComplete, UpdatedAt: time.Now()},
		&session.Session{ID: "b2", FullID: "h-b2", State: session.StateKilled, UpdatedAt: time.Now()},
	)
	rr := getSessions(t, s, "view=summary")
	if rr.Header().Get(sessionsOmittedHeader) != "" {
		t.Error("omitted header set with cap disabled")
	}
	var recs []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &recs)
	if len(recs) != 2 {
		t.Errorf("got %d records want 2", len(recs))
	}
}

func TestHandleSessions_BadParams400(t *testing.T) {
	s := sessionViewServer(t, 50, 200)
	for _, q := range []string{"view=full", "view=", "since=notatime", "states=nope"} {
		if rr := getSessions(t, s, q); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: code=%d want 400", q, rr.Code)
		}
	}
}

func TestHandleSessions_SinceStatesWithoutSummaryKeepsFullText(t *testing.T) {
	longTask := strings.Repeat("x", 500)
	s := sessionViewServer(t, 1, 20,
		&session.Session{ID: "a1", FullID: "h-a1", State: session.StateRunning, Task: longTask, UpdatedAt: time.Now()},
		&session.Session{ID: "b2", FullID: "h-b2", State: session.StateComplete, UpdatedAt: time.Now()},
	)
	rr := getSessions(t, s, "states=active")
	var recs []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0]["task"] != longTask {
		t.Errorf("states=active without view should return the full running session only: %v", recs)
	}
}

// legacySessionsBody is what handleSessions encoded before the summary
// view existed, for the single-session case (map order is then fixed).
func legacySessionsBody(t *testing.T, s *Server, sessions []*session.Session) string {
	t.Helper()
	var buf bytes.Buffer
	if s.schedStore == nil {
		_ = json.NewEncoder(&buf).Encode(sessions)
		return buf.String()
	}
	type sessionWithCounts struct {
		*session.Session
		ScheduledCount int `json:"scheduled_count"`
	}
	var enriched []sessionWithCounts
	for _, sess := range sessions {
		enriched = append(enriched, sessionWithCounts{sess, s.schedStore.CountForSession(sess.FullID)})
	}
	_ = json.NewEncoder(&buf).Encode(enriched)
	return buf.String()
}

func TestHandleSessions_DefaultTreeOrphanedUnchanged(t *testing.T) {
	sess := &session.Session{
		ID: "a1", FullID: "h-a1", State: session.StateComplete,
		Task: strings.Repeat("long task ", 100), UpdatedAt: time.Now(), ParentID: "gone",
	}
	for _, withSched := range []bool{false, true} {
		s := sessionViewServer(t, 1, 20, sess)
		if withSched {
			st, err := session.NewScheduleStore(filepath.Join(t.TempDir(), "sched.json"))
			if err != nil {
				t.Fatal(err)
			}
			s.SetScheduleStore(st)
		}
		live := s.manager.ListSessions()

		if got, want := getSessions(t, s, "").Body.String(), legacySessionsBody(t, s, live); got != want {
			t.Errorf("sched=%v default body changed:\n got %s\nwant %s", withSched, got, want)
		}
		if strings.Contains(getSessions(t, s, "").Body.String(), "truncated_fields") {
			t.Error("default body must not carry truncated_fields")
		}

		var tree bytes.Buffer
		_ = json.NewEncoder(&tree).Encode(buildSessionTree(live))
		if got := getSessions(t, s, "tree=1").Body.String(); got != tree.String() {
			t.Errorf("tree body changed:\n got %s\nwant %s", got, tree.String())
		}

		var orphans bytes.Buffer
		_ = json.NewEncoder(&orphans).Encode(live)
		if got := getSessions(t, s, "orphaned=1").Body.String(); got != orphans.String() {
			t.Errorf("orphaned body changed:\n got %s\nwant %s", got, orphans.String())
		}
	}
}

func TestHandleSessions_SummaryKeepsScheduledCount(t *testing.T) {
	s := sessionViewServer(t, 50, 200,
		&session.Session{ID: "a1", FullID: "h-a1", State: session.StateRunning, UpdatedAt: time.Now()})
	st, err := session.NewScheduleStore(filepath.Join(t.TempDir(), "sched.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetScheduleStore(st)
	var recs []map[string]any
	_ = json.Unmarshal(getSessions(t, s, "view=summary").Body.Bytes(), &recs)
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	if v, ok := recs[0]["scheduled_count"]; !ok || v.(float64) != 0 {
		t.Errorf("scheduled_count=%v present=%v, want 0 present", v, ok)
	}
}

func TestSessionSummaryLimits_LiveConfig(t *testing.T) {
	s := bl90Server(t)
	cases := []struct {
		limit, chars         int
		wantLimit, wantChars int
	}{
		{50, 200, 50, 200},
		{0, 0, 0, defaultSessionSummaryTextChars},
		{-3, 5, 0, minSessionSummaryTextChars},
		{7, 1000, 7, 1000},
	}
	for _, tc := range cases {
		s.cfg.Server.SessionSummaryTerminalLimit = tc.limit
		s.cfg.Server.SessionSummaryTextChars = tc.chars
		l, c := s.sessionSummaryLimits()
		if l != tc.wantLimit || c != tc.wantChars {
			t.Errorf("(%d,%d) -> (%d,%d) want (%d,%d)", tc.limit, tc.chars, l, c, tc.wantLimit, tc.wantChars)
		}
	}
	s.cfg = nil
	if l, c := s.sessionSummaryLimits(); l != defaultSessionSummaryTerminalLimit || c != defaultSessionSummaryTextChars {
		t.Errorf("nil cfg -> (%d,%d)", l, c)
	}
}

func TestApplyConfigPatch_SessionSummaryKeys(t *testing.T) {
	cases := []struct {
		name                 string
		patch                map[string]interface{}
		wantLimit, wantChars int
	}{
		{"set both", map[string]interface{}{
			"server.session_summary_terminal_limit": float64(25),
			"server.session_summary_text_chars":     float64(120),
		}, 25, 120},
		{"zero limit disables cap", map[string]interface{}{
			"server.session_summary_terminal_limit": float64(0),
		}, 0, 200},
		{"negative limit ignored", map[string]interface{}{
			"server.session_summary_terminal_limit": float64(-1),
		}, 50, 200},
		{"chars clamped to minimum", map[string]interface{}{
			"server.session_summary_text_chars": float64(3),
		}, 50, minSessionSummaryTextChars},
		// MCP config_set / CLI config set send the raw value as JSON, so
		// numbers arrive as float64; in-process callers may pass int.
		{"int values", map[string]interface{}{
			"server.session_summary_terminal_limit": 10,
			"server.session_summary_text_chars":     300,
		}, 10, 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			applyConfigPatch(cfg, tc.patch)
			if cfg.Server.SessionSummaryTerminalLimit != tc.wantLimit || cfg.Server.SessionSummaryTextChars != tc.wantChars {
				t.Errorf("got (%d,%d) want (%d,%d)", cfg.Server.SessionSummaryTerminalLimit,
					cfg.Server.SessionSummaryTextChars, tc.wantLimit, tc.wantChars)
			}
		})
	}
}

func TestHandleGetConfig_SessionSummaryKeys(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Server.SessionSummaryTerminalLimit = 33
	s.cfg.Server.SessionSummaryTextChars = 77
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	s.handleConfig(rr, req)
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	srv, _ := v["server"].(map[string]any)
	if srv["session_summary_terminal_limit"] != float64(33) || srv["session_summary_text_chars"] != float64(77) {
		t.Errorf("server map missing summary keys: %v", srv)
	}
}

func TestHandleInfo_AdvertisesSessionsViewSummary(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	rr := httptest.NewRecorder()
	s.handleInfo(rr, req)
	var v struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.Capabilities {
		if c == "sessions.view_summary" {
			found = true
		}
	}
	if !found {
		t.Errorf("capabilities=%v missing sessions.view_summary", v.Capabilities)
	}
}
