// Tests for the default-view window on the WebSocket connect path and the
// hello handshake (ws_session_window.go, ws_session_stream.go).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func windowTestSession(id string, st session.State, updated time.Time) *session.Session {
	return &session.Session{
		ID: id, FullID: "testhost-" + id, Name: id, State: st,
		Task: "task " + id, CreatedAt: updated.Add(-time.Hour), UpdatedAt: updated,
	}
}

// seedWindowSessions stores 50 finished-old, 3 finished-fresh and 5 active
// sessions, and returns the ids the default view must hold.
func seedWindowSessions(t *testing.T, s *Server, now time.Time) (wantIDs []string) {
	t.Helper()
	long := strings.Repeat("y", 400)
	save := func(sess *session.Session) {
		t.Helper()
		if err := s.manager.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
	}
	for i := 0; i < 50; i++ {
		id := "old" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		state := []session.State{session.StateComplete, session.StateFailed, session.StateKilled}[i%3]
		save(windowTestSession(id, state, now.Add(-time.Hour-time.Duration(i)*time.Minute)))
	}
	for i, id := range []string{"fresh1", "fresh2", "fresh3"} {
		sess := windowTestSession(id, session.StateComplete, now.Add(-time.Duration(i+1)*time.Minute))
		save(sess)
		wantIDs = append(wantIDs, sess.FullID)
	}
	for i, id := range []string{"act1", "act2", "act3", "act4", "act5"} {
		st := session.StateRunning
		if i == 1 {
			st = session.StateWaitingInput
		}
		if i == 2 {
			st = session.StateRateLimited
		}
		sess := windowTestSession(id, st, now.Add(-24*time.Hour)) // old, but not finished
		sess.Task = long
		sess.LastResponse = long
		save(sess)
		wantIDs = append(wantIDs, sess.FullID)
	}
	return wantIDs
}

func TestWSSessionWindow_Boundaries(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	window := 5 * time.Minute
	cases := []struct {
		name string
		st   session.State
		age  time.Duration
		want bool
	}{
		{"finished just inside", session.StateComplete, window - time.Second, true},
		{"finished exactly at window", session.StateComplete, window, false},
		{"finished just outside", session.StateComplete, window + time.Second, false},
		{"failed just inside", session.StateFailed, window - time.Second, true},
		{"failed just outside", session.StateFailed, window + time.Second, false},
		{"killed just inside", session.StateKilled, window - time.Second, true},
		{"killed just outside", session.StateKilled, window + time.Second, false},
		{"finished with a future update", session.StateComplete, -time.Minute, true},
		{"running, ancient", session.StateRunning, 365 * 24 * time.Hour, true},
		{"waiting_input, ancient", session.StateWaitingInput, 365 * 24 * time.Hour, true},
		{"rate_limited, ancient", session.StateRateLimited, 365 * 24 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := windowTestSession("x", tc.st, now.Add(-tc.age))
			if got := inDefaultView(sess, now, window); got != tc.want {
				t.Errorf("inDefaultView = %v, want %v", got, tc.want)
			}
		})
	}
	if inDefaultView(windowTestSession("z", session.StateComplete, time.Time{}), now, window) {
		t.Error("a finished session with no updated_at must be outside the window")
	}
	if inDefaultView(nil, now, window) {
		t.Error("nil session must be outside the window")
	}

	all := []*session.Session{
		windowTestSession("a", session.StateRunning, now.Add(-time.Hour)),
		windowTestSession("b", session.StateComplete, now.Add(-time.Hour)),
		windowTestSession("c", session.StateComplete, now.Add(-time.Minute)),
		nil,
	}
	view, total := splitDefaultView(all, now, window)
	if total != 3 || len(view) != 2 || view[0].ID != "a" || view[1].ID != "c" {
		t.Errorf("splitDefaultView = %d held, view %v", total, view)
	}
}

// The finished states must be the PWA's DONE_STATES.
func TestWSSessionWindow_StateSetMatchesPWA(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("web", "app.js"))
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	m := regexp.MustCompile(`const DONE_STATES = new Set\(\[([^\]]*)\]\)`).FindSubmatch(src)
	if m == nil {
		t.Fatal("DONE_STATES not found in app.js")
	}
	pwa := map[session.State]bool{}
	for _, tok := range strings.Split(string(m[1]), ",") {
		if tok = strings.Trim(strings.TrimSpace(tok), `'"`); tok != "" {
			pwa[session.State(tok)] = true
		}
	}
	for _, st := range session.KnownStates() {
		if st.IsTerminal() != pwa[st] {
			t.Errorf("state %s: server finished=%v, PWA DONE_STATES=%v", st, st.IsTerminal(), pwa[st])
		}
	}
	if len(pwa) != 3 {
		t.Errorf("PWA DONE_STATES = %v, want 3 states", pwa)
	}
}

func TestWSSessionWindow_RecentWindowSetting(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		want    time.Duration
	}{{0, 5 * time.Minute}, {-3, 5 * time.Minute}, {1, time.Minute}, {30, 30 * time.Minute}} {
		s, _ := newSessionStreamServer(t, "")
		s.cfg.Server.RecentSessionMinutes = tc.minutes
		if got := s.recentSessionWindow(); got != tc.want {
			t.Errorf("recent_session_minutes=%d: window = %v, want %v", tc.minutes, got, tc.want)
		}
	}
	if got := (&Server{}).recentSessionWindow(); got != 5*time.Minute {
		t.Errorf("no config: window = %v, want 5m", got)
	}
}

func decodeRecordsFrame(t *testing.T, m WSMessage) (d struct {
	Sessions      []map[string]any `json:"sessions"`
	WindowMinutes int              `json:"window_minutes"`
	Total         int              `json:"total"`
	More          bool             `json:"more"`
}) {
	t.Helper()
	if m.Type != MsgSessions {
		t.Fatalf("frame = %s, want sessions", m.Type)
	}
	if err := json.Unmarshal(m.Data, &d); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	return d
}

func TestWSSessionWindow_LegacyConnectFrameIsBounded(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	want := seedWindowSessions(t, s, now)

	conn := dialSessionStream(t, srv)
	d := decodeRecordsFrame(t, readWSFrame(t, conn))

	if d.Total != 58 {
		t.Errorf("total = %d, want 58", d.Total)
	}
	if !d.More {
		t.Error("more = false, want true (50 old finished sessions omitted)")
	}
	if d.WindowMinutes != 5 {
		t.Errorf("window_minutes = %d, want 5", d.WindowMinutes)
	}
	got := map[string]bool{}
	for _, row := range d.Sessions {
		got[row["full_id"].(string)] = true
	}
	if len(d.Sessions) != len(want) {
		t.Errorf("%d sessions in frame, want %d: %v", len(d.Sessions), len(want), got)
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("frame misses default-view session %s", id)
		}
	}
	for _, row := range d.Sessions {
		id := row["full_id"].(string)
		if strings.HasPrefix(id, "testhost-old") {
			t.Errorf("finished-old session %s must not be in the frame", id)
		}
		if strings.HasPrefix(id, "testhost-act") {
			task := row["task"].(string)
			if len([]rune(task)) > 31 { // 30 chars + ellipsis
				t.Errorf("%s: task not cut (%d runes)", id, len([]rune(task)))
			}
			tf, _ := row["truncated_fields"].([]any)
			if len(tf) == 0 {
				t.Errorf("%s: truncated_fields not set: %v", id, row["truncated_fields"])
			}
		}
		if strings.HasPrefix(id, "testhost-fresh") {
			if _, ok := row["truncated_fields"]; ok {
				t.Errorf("%s: short text must not set truncated_fields", id)
			}
		}
	}
}

func TestWSSessionWindow_LegacyHelloFullStaysLegacy(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	seedWindowSessions(t, s, time.Now())
	conn := dialSessionStream(t, srv)
	readWSFrame(t, conn) // connect-time frame
	if ack := sayHello(t, conn, `{"session_list":"full"}`); ack.SessionList != "full" {
		t.Fatalf("ack = %+v", ack)
	}
	s.hub.BroadcastSessions([]*session.Session{testSession("a", "one", session.StateRunning)})
	frames := drainUntilSentinel(t, s.hub, conn)[0]
	if len(frames) != 1 || frames[0].Type != MsgSessions {
		t.Fatalf("frames = %v, want only the legacy sessions broadcast (no rows frame)", frameTypes(frames))
	}
}

func TestWSSessionWindow_HelloWindowAckAndRowsFrame(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	s.cfg.Server.RecentSessionMinutes = 7
	st, err := session.NewScheduleStore(filepath.Join(t.TempDir(), "sched.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetScheduleStore(st)
	now := time.Now()
	want := seedWindowSessions(t, s, now)

	conn := dialSessionStream(t, srv)
	first := readWSFrame(t, conn) // provisional bounded legacy frame
	if first.Type != MsgSessions {
		t.Fatalf("connect frame = %s", first.Type)
	}
	ack := sayHello(t, conn, `{"session_list":"window"}`)
	if ack.SessionList != "window" {
		t.Errorf("ack.session_list = %q, want window", ack.SessionList)
	}
	has := false
	for _, c := range ack.Capabilities {
		has = has || c == "ws.session_list_window"
	}
	if !has {
		t.Errorf("hello_ack capabilities %v lack ws.session_list_window", ack.Capabilities)
	}

	rows := readWSFrame(t, conn)
	d := decodeRecordsFrame(t, rows)
	if d.WindowMinutes != 7 {
		t.Errorf("window_minutes = %d, want the configured 7", d.WindowMinutes)
	}
	if d.Total != 58 || !d.More {
		t.Errorf("total=%d more=%v, want 58 / true", d.Total, d.More)
	}
	if len(d.Sessions) != len(want) {
		t.Fatalf("%d rows, want %d", len(d.Sessions), len(want))
	}
	for _, row := range d.Sessions {
		// Summary-row shape: no heavy text, but status_line and scheduled_count.
		for _, banned := range []string{"task", "last_response", "prompt_context", "truncated_fields"} {
			if _, ok := row[banned]; ok {
				t.Errorf("row %v carries %q; want the summary-row shape", row["full_id"], banned)
			}
		}
		for _, need := range []string{"id", "full_id", "state", "created_at", "updated_at", "last_activity", "scheduled_count"} {
			if _, ok := row[need]; !ok {
				t.Errorf("row %v lacks %q", row["full_id"], need)
			}
		}
		if strings.HasPrefix(row["full_id"].(string), "testhost-act") && row["status_line"] == nil {
			// LastResponse was set on the active sessions
			t.Errorf("row %v lacks status_line", row["full_id"])
		}
	}
	// Newest first, like the REST summary list.
	prev := ""
	for _, row := range d.Sessions {
		u := row["updated_at"].(string)
		if prev != "" && u > prev {
			t.Errorf("rows not newest-first: %s after %s", u, prev)
		}
		prev = u
	}

	// Exactly one rows frame: nothing else follows the hello.
	if extra := drainUntilSentinel(t, s.hub, conn)[0]; len(extra) != 0 {
		t.Errorf("extra frames after the rows frame: %v", frameTypes(extra))
	}
}

func TestWSSessionWindow_ChangingSettingChangesWindowLive(t *testing.T) {
	s, srv := newSessionStreamServer(t, "")
	now := time.Now()
	if err := s.manager.SaveSession(windowTestSession("ten", session.StateComplete, now.Add(-10*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := s.manager.SaveSession(windowTestSession("run", session.StateRunning, now.Add(-10*time.Minute))); err != nil {
		t.Fatal(err)
	}

	rowsFor := func() (ids []string, window int, more bool) {
		conn := dialSessionStream(t, srv)
		readWSFrame(t, conn)
		sayHello(t, conn, `{"session_list":"window"}`)
		d := decodeRecordsFrame(t, readWSFrame(t, conn))
		for _, r := range d.Sessions {
			ids = append(ids, r["full_id"].(string))
		}
		return ids, d.WindowMinutes, d.More
	}

	ids, win, more := rowsFor()
	if win != 5 || len(ids) != 1 || ids[0] != "testhost-run" || !more {
		t.Errorf("default window: ids=%v window=%d more=%v", ids, win, more)
	}
	s.cfg.Server.RecentSessionMinutes = 15 // as PUT /api/config would
	ids, win, more = rowsFor()
	if win != 15 || len(ids) != 2 || more {
		t.Errorf("after setting 15: ids=%v window=%d more=%v", ids, win, more)
	}
}

func TestWSSessionWindow_DeltaDefaultSkipsConnectFrame(t *testing.T) {
	s, srv := newSessionStreamServer(t, "delta")
	seedWindowSessions(t, s, time.Now())
	conn := dialSessionStream(t, srv)
	if ack := sayHello(t, conn, `{"session_list":"delta"}`); ack.SessionList != "delta" {
		t.Fatalf("ack = %+v", ack)
	}
	if extra := drainUntilSentinel(t, s.hub, conn)[0]; len(extra) != 0 {
		t.Errorf("delta client got frames %v after hello, want none", frameTypes(extra))
	}
}

// "window" is only a hello mode: it is not a valid ws_session_list_default.
func TestWSSessionWindow_NotAValidDefault(t *testing.T) {
	s, srv := newSessionStreamServer(t, "window")
	if got := s.wsSessionListDefault(); got != "full" {
		t.Errorf("wsSessionListDefault = %q, want full", got)
	}
	conn := dialSessionStream(t, srv)
	if m := readWSFrame(t, conn); m.Type != MsgSessions {
		t.Fatalf("first frame = %s", m.Type)
	}
	if ack := sayHello(t, conn, `{}`); ack.SessionList != "full" {
		t.Errorf("ack = %+v, want full", ack)
	}
}

func TestWSSessionWindow_BareHubDefaults(t *testing.T) {
	h := NewHub()
	var m WSMessage
	if err := json.Unmarshal(h.windowSessionsFrame(), &m); err != nil {
		t.Fatal(err)
	}
	d := decodeRecordsFrame(t, m)
	if d.WindowMinutes != 5 || d.Total != 0 || d.More || len(d.Sessions) != 0 {
		t.Errorf("bare hub window frame = %+v", d)
	}
}

func TestWSSessionWindow_InfoAdvertisesCapability(t *testing.T) {
	s := bl90Server(t)
	rr := httptest.NewRecorder()
	s.handleInfo(rr, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	var v struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Capabilities {
		if c == "ws.session_list_window" {
			return
		}
	}
	t.Errorf("/api/info capabilities %v lack ws.session_list_window", v.Capabilities)
}
