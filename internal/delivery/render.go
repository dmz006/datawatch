// The report as plain text, for the CLI and the chat channels.

package delivery

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// RenderReport is r as plain text with at most maxItems entry lines in each
// group; 0 means no limit. It prints IDs, kinds, states, reason codes, counts
// and times only: it never reads a field that could carry other text, such as
// who acted or a send result.
func RenderReport(r Report, maxItems int) string {
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}

	result := r.Result
	if result == "" {
		result = "unknown"
	}
	head := "Delivery check: " + renderText(result)
	if r.ReportOnly {
		head += " (report only: nothing is blocked)"
	}
	line("%s", head)
	if r.At.IsZero() {
		line("The check has not run yet.")
	} else {
		line("At %s, pass %d", renderTime(r.At), r.Pass)
	}
	if len(r.Red) > 0 {
		line("Why: %s", renderList(r.Red))
	}
	channels := "none"
	if len(r.Channels) > 0 {
		channels = renderList(r.Channels)
	}
	line("Message channels: %s", channels)
	if r.Heartbeat != "" {
		line("Heartbeat: %s", renderText(r.Heartbeat))
	}
	if r.StartMark != nil && !r.StartMark.IsZero() {
		line("Start mark: %s", renderTime(*r.StartMark))
	}

	renderGroup(&b, "New", r.Items, maxItems)
	renderGroup(&b, "Backlog", r.Backlog, maxItems)
	renderGroup(&b, "Accepted", r.Accepted, maxItems)

	if len(r.Guarded) > 0 {
		line("Held back by the mass-expiry guard (%d):", len(r.Guarded))
		shown := renderLimit(len(r.Guarded), maxItems)
		for _, key := range r.Guarded[:shown] {
			line("  %s", renderText(key))
		}
		if rest := len(r.Guarded) - shown; rest > 0 {
			line("  and %d more", rest)
		}
	}
	if r.Unrecorded > 0 {
		line("Not recorded (more new entries than one check saves): %d", r.Unrecorded)
	}
	var unread []string
	for _, s := range r.Sources {
		if !s.OK {
			unread = append(unread, s.Name)
		}
	}
	if len(unread) > 0 {
		line("Sources not read: %s", renderList(unread))
	}
	if len(r.ByRule) > 0 {
		line("By rule: %s", renderCounts(r.ByRule))
	}
	if len(r.ByKind) > 0 {
		line("By kind: %s", renderCounts(r.ByKind))
	}
	if len(r.Uncovered) > 0 {
		line("Not covered by this check: %s", renderList(r.Uncovered))
	}
	if r.NonOperatorActions > 0 {
		line("Acknowledged, accepted or dropped by a session or peer: %d", r.NonOperatorActions)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderGroup writes one group of entries, one line each.
func renderGroup(b *strings.Builder, title string, entries []Entry, maxItems int) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "%s (%d):\n", title, len(entries))
	shown := renderLimit(len(entries), maxItems)
	for _, e := range entries[:shown] {
		fmt.Fprintf(b, "  %s\n", renderEntryLine(e))
	}
	if rest := len(entries) - shown; rest > 0 {
		fmt.Fprintf(b, "  and %d more\n", rest)
	}
}

func renderEntryLine(e Entry) string {
	key := e.Key
	if key == "" {
		key = string(e.Kind) + ":" + e.ID + ":" + e.State
	}
	parts := []string{renderText(key)}
	if why := strings.TrimSpace(renderText(e.Invariant) + " " + renderText(e.Reason)); why != "" {
		parts = append(parts, why)
	}
	if e.Count > 1 {
		parts = append(parts, fmt.Sprintf("count %d", e.Count))
	}
	if !e.Since.IsZero() {
		parts = append(parts, "since "+renderTime(e.Since))
	}
	if !e.ReviewBy.IsZero() {
		parts = append(parts, "review by "+renderTime(e.ReviewBy))
	}
	if e.WaitsOn == WaitsOnOperator {
		parts = append(parts, "waiting on you")
	}
	if e.Accepted != nil {
		parts = append(parts, "accepted "+renderText(e.Accepted.Reason)+" until "+renderTime(e.Accepted.Until))
	}
	if e.SeenAt != nil {
		parts = append(parts, "seen")
	}
	return strings.Join(parts, " | ")
}

func renderLimit(n, maxItems int) int {
	if maxItems > 0 && n > maxItems {
		return maxItems
	}
	return n
}

// renderText keeps a value on its own line: control characters become spaces.
func renderText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
}

func renderList(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = renderText(v)
	}
	return strings.Join(out, ", ")
}

func renderCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("%s=%d", renderText(k), m[k])
	}
	return strings.Join(out, ", ")
}

func renderTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
