// Comm-channel read surface for scheduled jobs' structured result panel.
//
// `schedule-results` lists the latest result for every schedule (status
// icon, summary, age); `schedule-results <name>` shows that schedule's
// retained history, newest first. Both are read-only — posting results
// stays on the CLI / MCP / API paths. The data comes from the same REST
// surface the PWA result panel reads (GET /api/schedule-results and
// GET /api/schedule-results/<name>), fetched over the router's in-process
// loopback (webPort) — the same access pattern the other read commands
// use (see commGet in sx2_parity.go).

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// scheduleResultsHistoryLimit caps how many retained entries a single
// chat reply shows per schedule; older ones stay available via the
// CLI / PWA panel.
const scheduleResultsHistoryLimit = 10

// scheduleResultListEntry is one element of GET /api/schedule-results:
// the schedule's latest posted result plus the count and recent-status
// sequence of its retained history.
type scheduleResultListEntry struct {
	ScheduleName   string               `json:"schedule_name"`
	Latest         *scheduleResultEntry `json:"latest,omitempty"`
	Count          int                  `json:"count"`
	RecentStatuses []string             `json:"recent_statuses,omitempty"`
}

// resultPayload is the subset of the stored result payload the chat
// reply renders.
type resultPayload struct {
	Title   string `json:"title,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// scheduleResultEntry is one posted result (element of
// GET /api/schedule-results/<name>, and scheduleResultListEntry.Latest).
// Only the fields the chat reply renders are needed here.
type scheduleResultEntry struct {
	Payload  resultPayload `json:"payload"`
	PostedAt time.Time     `json:"posted_at"`
}

// handleScheduleResultsCmd dispatches schedule-results commands over
// the comm channel.
func (r *Router) handleScheduleResultsCmd(cmd Command) {
	switch cmd.ScheduleResultsVerb {
	case "", "list":
		r.handleScheduleResultsList()
	case "history":
		r.handleScheduleResultsHistory(cmd)
	default:
		r.send(fmt.Sprintf("[%s] Usage: schedule-results | schedule-results list | schedule-results <name>", r.hostname))
	}
}

func (r *Router) handleScheduleResultsList() {
	body, err := r.commGet("/api/schedule-results", nil)
	if err != nil {
		r.send(fmt.Sprintf("[%s] schedule-results failed: %v", r.hostname, err))
		return
	}
	var entries []scheduleResultListEntry
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		r.send(fmt.Sprintf("[%s] schedule-results: unexpected response: %v", r.hostname, err))
		return
	}
	if len(entries) == 0 {
		r.send(fmt.Sprintf("[%s] No schedule results recorded yet. Scheduled jobs post results with: datawatch schedule-results post", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s]\n%s", r.hostname, formatScheduleResultsList(entries, time.Now())))
}

func (r *Router) handleScheduleResultsHistory(cmd Command) {
	name := strings.TrimSpace(cmd.ScheduleResultsName)
	if name == "" {
		r.send(fmt.Sprintf("[%s] Usage: schedule-results <name>\nList known schedules with: schedule-results list", r.hostname))
		return
	}
	body, err := r.commGet("/api/schedule-results/"+url.PathEscape(name), nil)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			r.send(fmt.Sprintf("[%s] No results recorded for %q. See all schedules with: schedule-results list", r.hostname, name))
			return
		}
		r.send(fmt.Sprintf("[%s] schedule-results %q failed: %v", r.hostname, name, err))
		return
	}
	var entries []scheduleResultEntry
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		r.send(fmt.Sprintf("[%s] schedule-results %q: unexpected response: %v", r.hostname, name, err))
		return
	}
	if len(entries) == 0 {
		r.send(fmt.Sprintf("[%s] No results recorded for %q.", r.hostname, name))
		return
	}
	r.send(fmt.Sprintf("[%s]\n%s", r.hostname, formatScheduleResultHistory(name, entries, time.Now())))
}

// statusIcon renders a result status as icon + word. Unset maps to a
// neutral bullet.
func statusIcon(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok":
		return "✓ ok"
	case "warn":
		return "⚠ warn"
	case "error":
		return "✗ error"
	default:
		return "• unset"
	}
}

// relativeAge renders a posted-at time as a compact "N<unit> ago".
func relativeAge(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// formatScheduleResultsList renders the list view: one line per
// schedule (most recently posted first) with the latest status icon,
// a one-line summary (falling back to the title), and the age.
func formatScheduleResultsList(entries []scheduleResultListEntry, now time.Time) string {
	sorted := append([]scheduleResultListEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i].Latest, sorted[j].Latest
		if a == nil && b == nil {
			return sorted[i].ScheduleName < sorted[j].ScheduleName
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.PostedAt.After(b.PostedAt)
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "Schedule results (%d schedule%s):\n", len(sorted), pluralS(len(sorted)))
	for _, e := range sorted {
		if e.Latest == nil {
			fmt.Fprintf(&sb, "  • %s — (no results recorded yet)", e.ScheduleName)
			sb.WriteString("\n")
			continue
		}
		p := e.Latest.Payload
		line := fmt.Sprintf("  %s %s", statusIcon(p.Status), e.ScheduleName)
		text := strings.TrimSpace(p.Summary)
		if text == "" {
			text = strings.TrimSpace(p.Title)
		}
		if text != "" {
			line += " — " + text
		}
		line += " (" + relativeAge(e.Latest.PostedAt, now) + ")"
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatScheduleResultHistory renders one schedule's retained history,
// newest first. Only the most recent scheduleResultsHistoryLimit entries
// are shown; the full history stays on the CLI / PWA panel.
func formatScheduleResultHistory(name string, entries []scheduleResultEntry, now time.Time) string {
	sorted := append([]scheduleResultEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PostedAt.After(sorted[j].PostedAt) })
	total := len(sorted)
	if total > scheduleResultsHistoryLimit {
		sorted = sorted[:scheduleResultsHistoryLimit]
	}
	var sb strings.Builder
	if total > len(sorted) {
		fmt.Fprintf(&sb, "%q — latest %d of %d retained results (newest first):\n", name, len(sorted), total)
	} else {
		fmt.Fprintf(&sb, "%q — %d retained result%s (newest first):\n", name, total, pluralS(total))
	}
	for i, e := range sorted {
		p := e.Payload
		line := fmt.Sprintf("  %d. %s %s", i+1, statusIcon(p.Status), relativeAge(e.PostedAt, now))
		text := strings.TrimSpace(p.Summary)
		if text == "" {
			text = strings.TrimSpace(p.Title)
		}
		if text != "" {
			line += " — " + text
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// pluralS returns "s" when n != 1, "" otherwise.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
