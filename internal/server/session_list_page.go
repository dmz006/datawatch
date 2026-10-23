// Row-only summary, paging, filters and ETag for GET /api/sessions.
//
// view=summary used to return every Session field with the long text cut
// short, which still shipped task text, prompt context and transcripts
// summaries to a list that renders none of them. The summary view now
// returns sessionSummaryRow, only what a list row shows. limit / cursor
// page it, status / active / q filter it, and the body carries an ETag.
// A request with none of these params still gets the legacy array.

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

const (
	defaultSessionPageLimit = 50
	maxSessionPageLimit     = 500
	// sessionTaskSearchChars is how much of the task q matches against.
	sessionTaskSearchChars = 256
	// sessionsTotalHeader carries the filtered total for array responses.
	sessionsTotalHeader = "X-Datawatch-Sessions-Total"
)

// sessionSummaryRow is one row of the summary view.
type sessionSummaryRow struct {
	ID             string        `json:"id"`
	FullID         string        `json:"full_id"`
	Name           string        `json:"name,omitempty"`
	State          session.State `json:"state"`
	Backend        string        `json:"backend,omitempty"`
	LLMRef         string        `json:"llm_ref,omitempty"`
	ProjectDir     string        `json:"project_dir,omitempty"`
	Hostname       string        `json:"hostname,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	LastActivity   time.Time     `json:"last_activity"`
	ParentID       string        `json:"parent_id,omitempty"`
	EstCostUSD     float64       `json:"est_cost_usd,omitempty"`
	TokensIn       int           `json:"tokens_in,omitempty"`
	TokensOut      int           `json:"tokens_out,omitempty"`
	ScheduledCount *int          `json:"scheduled_count,omitempty"`
	StatusLine     string        `json:"status_line,omitempty"`
}

// sessionListPage is the paged envelope.
type sessionListPage struct {
	Sessions   []sessionSummaryRow `json:"sessions"`
	NextCursor string              `json:"next_cursor,omitempty"`
	Total      int                 `json:"total"`
}

// sessionPageOptions holds the paging and filter params.
type sessionPageOptions struct {
	paged    bool // limit or cursor supplied -> envelope response
	limit    int
	after    *sessionCursor
	statuses map[session.State]bool
	active   bool
	q        string
}

// any reports whether any paging or filter param was supplied.
func (o sessionPageOptions) any() bool {
	return o.paged || o.statuses != nil || o.active || o.q != ""
}

type sessionCursor struct {
	T  int64  `json:"t"` // updated_at unix nanos
	ID string `json:"id"`
}

func encodeSessionCursor(sess *session.Session) string {
	b, _ := json.Marshal(sessionCursor{T: sess.UpdatedAt.UnixNano(), ID: sess.FullID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeSessionCursor(raw string) (*sessionCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	var c sessionCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &c, nil
}

// parseSessionPageOptions validates limit / cursor / status / active / q.
func parseSessionPageOptions(q url.Values) (sessionPageOptions, error) {
	var o sessionPageOptions
	o.limit = defaultSessionPageLimit
	if q.Has("limit") {
		n, err := strconv.Atoi(strings.TrimSpace(q.Get("limit")))
		if err != nil || n < 1 {
			return o, fmt.Errorf("invalid limit %q: want a positive integer", q.Get("limit"))
		}
		if n > maxSessionPageLimit {
			n = maxSessionPageLimit
		}
		o.limit, o.paged = n, true
	}
	if raw := q.Get("cursor"); raw != "" {
		c, err := decodeSessionCursor(raw)
		if err != nil {
			return o, err
		}
		o.after, o.paged = c, true
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		st, err := parseSessionStates(raw)
		if err != nil {
			return o, fmt.Errorf("status: %w", err)
		}
		o.statuses = st
	}
	o.active = q.Get("active") == "1"
	o.q = strings.ToLower(strings.TrimSpace(q.Get("q")))
	return o, nil
}

// sessionLess is the stable list order: newest update first, ties by id.
func sessionLess(a, b *session.Session) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	return a.FullID < b.FullID
}

func matchSessionQuery(sess *session.Session, q string) bool {
	if q == "" {
		return true
	}
	task := sess.Task
	if r := []rune(task); len(r) > sessionTaskSearchChars {
		task = string(r[:sessionTaskSearchChars])
	}
	for _, f := range []string{sess.Name, sess.ID, sess.FullID, task} {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

func filterSessionPage(sessions []*session.Session, o sessionPageOptions) []*session.Session {
	out := make([]*session.Session, 0, len(sessions))
	for _, sess := range sessions {
		if o.active && sess.State.IsTerminal() {
			continue
		}
		if o.statuses != nil && !o.statuses[sess.State] {
			continue
		}
		if !matchSessionQuery(sess, o.q) {
			continue
		}
		out = append(out, sess)
	}
	return out
}

// pageSessions sorts sessions and returns the page after the cursor plus
// the next cursor ("" on the last page).
func pageSessions(sessions []*session.Session, o sessionPageOptions) ([]*session.Session, string) {
	sort.SliceStable(sessions, func(i, j int) bool { return sessionLess(sessions[i], sessions[j]) })
	start := 0
	if o.after != nil {
		start = sort.Search(len(sessions), func(i int) bool {
			t := sessions[i].UpdatedAt.UnixNano()
			if t != o.after.T {
				return t < o.after.T
			}
			return sessions[i].FullID > o.after.ID
		})
	}
	end := start + o.limit
	if end >= len(sessions) {
		return sessions[start:], ""
	}
	return sessions[start:end], encodeSessionCursor(sessions[end-1])
}

// sessionStatusLine is the first non-empty line of the latest response (or
// the pending prompt), cut to n runes.
func sessionStatusLine(sess *session.Session, n int) string {
	src := sess.LastResponse
	if sess.State == session.StateWaitingInput && sess.PendingInput != "" {
		src = sess.PendingInput
	}
	for _, line := range strings.Split(src, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out, _ := truncateRunes(line, n)
			return out
		}
	}
	return ""
}

func (s *Server) sessionSummaryRow(sess *session.Session, textChars int) sessionSummaryRow {
	row := sessionSummaryRow{
		ID: sess.ID, FullID: sess.FullID, Name: sess.Name, State: sess.State,
		Backend: sess.BackendFamily, LLMRef: sess.LLMRef, ProjectDir: sess.ProjectDir,
		Hostname: sess.Hostname, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
		LastActivity: sess.UpdatedAt, ParentID: sess.ParentID,
		EstCostUSD: sess.EstCostUSD, TokensIn: sess.TokensIn, TokensOut: sess.TokensOut,
		StatusLine: sessionStatusLine(sess, textChars),
	}
	if sess.LastChannelEventAt.After(row.LastActivity) {
		row.LastActivity = sess.LastChannelEventAt
	}
	if s.schedStore != nil {
		n := s.schedStore.CountForSession(sess.FullID)
		row.ScheduledCount = &n
	}
	return row
}

// writeJSONETag marshals v, sets an ETag from the body hash and answers a
// matching If-None-Match with 304 and no body.
func writeJSONETag(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:])[:32] + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// writeSessionSummaryList serves the row-only list: the legacy array shape
// (default terminal cap) unless limit/cursor ask for the paged envelope.
func (s *Server) writeSessionSummaryList(w http.ResponseWriter, r *http.Request, sessions []*session.Session, o sessionListOptions, p sessionPageOptions) {
	sessions = filterSessionList(sessions, o)
	sessions = filterSessionPage(sessions, p)
	terminalLimit, textChars := s.sessionSummaryLimits()
	total := len(sessions)
	var next string
	if p.paged {
		sessions, next = pageSessions(sessions, p)
	} else {
		var omitted int
		sessions, omitted = capTerminalSessions(sessions, terminalLimit)
		if omitted > 0 {
			w.Header().Set(sessionsOmittedHeader, strconv.Itoa(omitted))
		}
		total = len(sessions)
		sort.SliceStable(sessions, func(i, j int) bool { return sessionLess(sessions[i], sessions[j]) })
	}
	rows := make([]sessionSummaryRow, 0, len(sessions))
	for _, sess := range sessions {
		rows = append(rows, s.sessionSummaryRow(sess, textChars))
	}
	w.Header().Set(sessionsTotalHeader, strconv.Itoa(total))
	if p.paged {
		writeJSONETag(w, r, sessionListPage{Sessions: rows, NextCursor: next, Total: total})
		return
	}
	writeJSONETag(w, r, rows)
}
