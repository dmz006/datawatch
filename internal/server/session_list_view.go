// Compact session-list view for GET /api/sessions (BL432 / GH#242).
//
// A busy daemon's unfiltered session list reached ~3 MB, most of it the
// long free-text fields (task, prompt context, last response, long
// summary). Clients that only render a list need none of that in full.
// ?view=summary truncates those fields and caps how many terminal
// sessions are returned; ?since= and ?states= narrow the list further.
//
// The unfiltered list (no view/since/states param) is intentionally left
// untouched so existing clients keep working; retention only applies to
// view=summary. GET /api/sessions/{id} still returns the full record.

package server

import (
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
	// sessionListViewSummary is the only accepted ?view= value.
	sessionListViewSummary = "summary"

	// Defaults used when the live config leaves the fields unset.
	defaultSessionSummaryTerminalLimit = 50
	defaultSessionSummaryTextChars     = 200
	minSessionSummaryTextChars         = 20

	// sessionsOmittedHeader reports how many terminal sessions the
	// summary view's cap dropped.
	sessionsOmittedHeader = "X-Datawatch-Sessions-Omitted"
)

// sessionListOptions holds the parsed view / since / states query params.
type sessionListOptions struct {
	summary  bool
	since    time.Time
	hasSince bool
	// states is nil when no ?states= filter was given.
	states map[session.State]bool
}

// active reports whether any of the new params were supplied. When false
// the handler must emit exactly the legacy response.
func (o sessionListOptions) active() bool {
	return o.summary || o.hasSince || o.states != nil
}

// parseSessionListOptions validates the view / since / states params.
// Any error is a client error (400).
func parseSessionListOptions(q url.Values) (sessionListOptions, error) {
	var o sessionListOptions
	if q.Has("view") {
		switch v := q.Get("view"); v {
		case sessionListViewSummary:
			o.summary = true
		default:
			return o, fmt.Errorf("unknown view %q (supported: %s)", v, sessionListViewSummary)
		}
	}
	if raw := strings.TrimSpace(q.Get("since")); raw != "" {
		t, err := parseSessionSince(raw)
		if err != nil {
			return o, err
		}
		o.since, o.hasSince = t, true
	}
	if raw := strings.TrimSpace(q.Get("states")); raw != "" {
		states, err := parseSessionStates(raw)
		if err != nil {
			return o, err
		}
		o.states = states
	}
	return o, nil
}

// parseSessionSince accepts RFC3339 (with or without fractional seconds)
// or integer unix seconds.
func parseSessionSince(raw string) (time.Time, error) {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(n, 0), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid since %q: want RFC3339 or unix seconds", raw)
}

// parseSessionStates expands a comma list of state names. "active" means
// every non-terminal state; any other token must be a known state name.
func parseSessionStates(raw string) (map[session.State]bool, error) {
	known := make(map[session.State]bool)
	for _, st := range session.KnownStates() {
		known[st] = true
	}
	out := make(map[session.State]bool)
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if tok == "active" {
			for st := range known {
				if !st.IsTerminal() {
					out[st] = true
				}
			}
			continue
		}
		st := session.State(tok)
		if !known[st] {
			return nil, fmt.Errorf("unknown state %q in states", tok)
		}
		out[st] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("states is empty")
	}
	return out, nil
}

// filterSessionList applies the since / states filters.
func filterSessionList(sessions []*session.Session, o sessionListOptions) []*session.Session {
	if !o.hasSince && o.states == nil {
		return sessions
	}
	out := make([]*session.Session, 0, len(sessions))
	for _, sess := range sessions {
		if o.hasSince && !sess.UpdatedAt.After(o.since) {
			continue
		}
		if o.states != nil && !o.states[sess.State] {
			continue
		}
		out = append(out, sess)
	}
	return out
}

// capTerminalSessions keeps every non-terminal session and only the
// newest limit terminal sessions by updated_at. Input order is preserved.
// limit <= 0 disables the cap. Returns the kept list and the drop count.
func capTerminalSessions(sessions []*session.Session, limit int) ([]*session.Session, int) {
	if limit <= 0 {
		return sessions, 0
	}
	var terminal []*session.Session
	for _, sess := range sessions {
		if sess.State.IsTerminal() {
			terminal = append(terminal, sess)
		}
	}
	if len(terminal) <= limit {
		return sessions, 0
	}
	sort.SliceStable(terminal, func(i, j int) bool {
		return terminal[i].UpdatedAt.After(terminal[j].UpdatedAt)
	})
	keep := make(map[*session.Session]bool, limit)
	for _, sess := range terminal[:limit] {
		keep[sess] = true
	}
	out := make([]*session.Session, 0, len(sessions)-len(terminal)+limit)
	for _, sess := range sessions {
		if !sess.State.IsTerminal() || keep[sess] {
			out = append(out, sess)
		}
	}
	return out, len(terminal) - limit
}

// truncateRunes cuts s to at most n runes, appending an ellipsis when it
// cuts. Reports whether it cut.
func truncateRunes(s string, n int) (string, bool) {
	if n <= 0 {
		return s, false
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…", true
		}
		count++
	}
	return s, false
}

// summarizeSession returns a copy of sess with the heavy text fields cut
// to n runes, plus the json names of the fields that were cut. The input
// is a live store record and is never modified.
func summarizeSession(sess *session.Session, n int) (*session.Session, []string) {
	c := *sess
	var cut []string
	for _, f := range []struct {
		name string
		val  *string
	}{
		{"task", &c.Task},
		{"prompt_context", &c.PromptContext},
		{"last_response", &c.LastResponse},
		{"last_summary_long", &c.LastSummaryLong},
	} {
		if v, ok := truncateRunes(*f.val, n); ok {
			*f.val = v
			cut = append(cut, f.name)
		}
	}
	return &c, cut
}

// sessionSummaryLimits reads the summary-view knobs from the live config
// so PUT /api/config takes effect without a restart.
func (s *Server) sessionSummaryLimits() (terminalLimit, textChars int) {
	terminalLimit, textChars = defaultSessionSummaryTerminalLimit, defaultSessionSummaryTextChars
	if s.cfg != nil {
		terminalLimit = s.cfg.Server.SessionSummaryTerminalLimit
		if s.cfg.Server.SessionSummaryTextChars > 0 {
			textChars = s.cfg.Server.SessionSummaryTextChars
		}
	}
	if terminalLimit < 0 {
		terminalLimit = 0
	}
	if textChars < minSessionSummaryTextChars {
		textChars = minSessionSummaryTextChars
	}
	return terminalLimit, textChars
}

// sessionListRecord is one element of a filtered or summary list. It keeps
// the legacy shape: the session's own fields, plus scheduled_count when
// the schedule store is wired, plus truncated_fields when anything was cut.
type sessionListRecord struct {
	*session.Session
	ScheduledCount  *int     `json:"scheduled_count,omitempty"`
	TruncatedFields []string `json:"truncated_fields,omitempty"`
}

// writeSessionListView encodes the session list for requests that used
// view / since / states.
func (s *Server) writeSessionListView(w http.ResponseWriter, sessions []*session.Session, o sessionListOptions) {
	sessions = filterSessionList(sessions, o)
	textChars := 0
	if o.summary {
		var limit, omitted int
		limit, textChars = s.sessionSummaryLimits()
		sessions, omitted = capTerminalSessions(sessions, limit)
		if omitted > 0 {
			w.Header().Set(sessionsOmittedHeader, strconv.Itoa(omitted))
		}
	}
	out := make([]sessionListRecord, 0, len(sessions))
	for _, sess := range sessions {
		rec := sessionListRecord{Session: sess}
		if o.summary {
			rec.Session, rec.TruncatedFields = summarizeSession(sess, textChars)
		}
		if s.schedStore != nil {
			n := s.schedStore.CountForSession(sess.FullID)
			rec.ScheduledCount = &n
		}
		out = append(out, rec)
	}
	_ = json.NewEncoder(w).Encode(out)
}
