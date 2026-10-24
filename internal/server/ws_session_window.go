// Default-view window for the WebSocket session list (see the header of
// ws_session_stream.go for the wire behaviour).
//
// A session is in the default view when it is not finished, or when it is
// finished (complete, failed or killed — the PWA's DONE_STATES) and was
// updated within the last server.recent_session_minutes minutes. Finished
// sessions older than that are never sent over the websocket; clients page
// them over GET /api/sessions?view=summary&limit=50 (and history=1).

package server

import (
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// defaultRecentSessionMinutes is the window used when
// server.recent_session_minutes is unset or not positive. The PWA applies
// the same fallback (`recent_session_minutes || 5`).
const defaultRecentSessionMinutes = 5

// recentWindowFor converts a server.recent_session_minutes value to the
// window; a value <= 0 means the default.
func recentWindowFor(minutes int) time.Duration {
	if minutes <= 0 {
		minutes = defaultRecentSessionMinutes
	}
	return time.Duration(minutes) * time.Minute
}

// recentSessionWindow is the live default-view window. It is the one reader
// of server.recent_session_minutes for the websocket path, so a config
// change applies without a restart.
func (s *Server) recentSessionWindow() time.Duration {
	if s.cfg != nil {
		return recentWindowFor(s.cfg.Server.RecentSessionMinutes)
	}
	return recentWindowFor(0)
}

// inDefaultView reports whether sess belongs in the default view at now.
// It is a cheap state / UpdatedAt check and touches nothing else, so it can
// run over every held session before any snapshot or marshal work. Like the
// PWA, a finished session exactly window old is out, and a session updated
// in the future is in.
func inDefaultView(sess *session.Session, now time.Time, window time.Duration) bool {
	if sess == nil {
		return false
	}
	return stateInDefaultView(sess.State, sess.UpdatedAt, now, window)
}

// stateInDefaultView is inDefaultView on the two fields it reads, for
// callers that test sessions while holding a lock and have no copy yet.
func stateInDefaultView(st session.State, updated, now time.Time, window time.Duration) bool {
	return !st.IsTerminal() || now.Sub(updated) < window
}

// splitDefaultView returns the default-view sessions (input order kept) and
// the total number of sessions held.
func splitDefaultView(sessions []*session.Session, now time.Time, window time.Duration) (view []*session.Session, total int) {
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		total++
		if inDefaultView(sess, now, window) {
			view = append(view, sess)
		}
	}
	return view, total
}

// sessionsRecordsData is the bounded legacy `sessions` frame: the same
// session objects as before, long text cut, truncated_fields set.
type sessionsRecordsData struct {
	Sessions      []sessionListRecord `json:"sessions"`
	Version       string              `json:"version,omitempty"`
	WindowMinutes int                 `json:"window_minutes"`
	Total         int                 `json:"total"`
	More          bool                `json:"more"`
}

// sessionsRowsData is the `sessions` frame a window client receives:
// summary rows, as GET /api/sessions?view=summary returns them.
type sessionsRowsData struct {
	Sessions      []sessionSummaryRow `json:"sessions"`
	Version       string              `json:"version,omitempty"`
	WindowMinutes int                 `json:"window_minutes"`
	Total         int                 `json:"total"`
	More          bool                `json:"more"`
}

// setSessionWindowConfig wires the live readers the window frames need.
// Must be called before Run starts; nil hooks fall back to built-in
// defaults (bare-hub tests).
func (h *Hub) setSessionWindowConfig(window func() time.Duration, row func(*session.Session, int) sessionSummaryRow, list func() []*session.Session) {
	h.recentWindowFn = window
	h.sessionRowFn = row
	h.listSessionsFn = list
}

func (h *Hub) recentWindow() time.Duration {
	if h.recentWindowFn != nil {
		return h.recentWindowFn()
	}
	return recentWindowFor(0)
}

func (h *Hub) summaryRow(sess *session.Session, textChars int) sessionSummaryRow {
	if h.sessionRowFn != nil {
		return h.sessionRowFn(sess, textChars)
	}
	return (&Server{}).sessionSummaryRow(sess, textChars)
}

func (h *Hub) heldSessions() []*session.Session {
	if h.listSessionsFn != nil {
		return h.listSessionsFn()
	}
	return nil
}

// boundedSessionsFrame is the connect-time `sessions` frame for a client
// that has not announced the window form: default-view sessions only, text
// cut at server.session_summary_text_chars.
func (h *Hub) boundedSessionsFrame() []byte {
	window := h.recentWindow()
	view, total := splitDefaultView(h.heldSessions(), time.Now(), window)
	textChars := h.summaryTextChars()
	recs := make([]sessionListRecord, 0, len(view))
	for _, sess := range view {
		rec := sessionListRecord{}
		rec.Session, rec.TruncatedFields = summarizeSession(sess, textChars)
		recs = append(recs, rec)
	}
	return encodeSessionsFrame(sessionsRecordsData{
		Sessions: recs, Version: h.version,
		WindowMinutes: int(window / time.Minute), Total: total, More: total > len(view),
	})
}

// windowSessionsFrame is the single `sessions` frame a window client gets
// after its hello: summary rows for the default-view sessions, newest
// first.
func (h *Hub) windowSessionsFrame() []byte {
	window := h.recentWindow()
	view, total := splitDefaultView(h.heldSessions(), time.Now(), window)
	sort.SliceStable(view, func(i, j int) bool { return sessionLess(view[i], view[j]) })
	textChars := h.summaryTextChars()
	rows := make([]sessionSummaryRow, 0, len(view))
	for _, sess := range view {
		rows = append(rows, h.summaryRow(sess, textChars))
	}
	return encodeSessionsFrame(sessionsRowsData{
		Sessions: rows, Version: h.version,
		WindowMinutes: int(window / time.Minute), Total: total, More: total > len(view),
	})
}

func encodeSessionsFrame(data any) []byte {
	return mustMarshal(WSMessage{Type: MsgSessions, Data: mustMarshal(data), Timestamp: time.Now()})
}
