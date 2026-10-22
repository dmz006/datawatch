package session

import (
	"strings"
	"time"
	"unicode"
)

// taskDeliveredTime returns when sess's task was delivered: the in-memory
// taskDeliveredAt value if present, else the persisted Session.TaskDeliveredAt
// (which survives a daemon restart, unlike the in-memory map).
func (m *Manager) taskDeliveredTime(sess *Session) (time.Time, bool) {
	if sess == nil {
		return time.Time{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.taskDeliveredAt[sess.FullID]; ok {
		return t, true
	}
	if cur, ok := m.store.Get(sess.FullID); ok && cur.TaskDeliveredAt != nil {
		return *cur.TaskDeliveredAt, true
	}
	if sess.TaskDeliveredAt != nil {
		return *sess.TaskDeliveredAt, true
	}
	return time.Time{}, false
}

// compactEchoRune reports whether r is dropped when comparing pane text with
// task text: whitespace plus TUI box-drawing and block characters.
func compactEchoRune(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x2500 && r <= 0x259F)
}

func compactEchoText(s string) string {
	return strings.Map(func(r rune) rune {
		if compactEchoRune(r) {
			return -1
		}
		return r
	}, StripANSI(s))
}

// minEchoMatchLen is the shortest compacted task line trusted as an echo
// locator, so a generic short line cannot be mistaken for the echo.
const minEchoMatchLen = 8

// lastTaskEchoIndex returns the index in lines of the last line belonging to
// the echoed task text, or -1 when the task cannot be located. Matching
// ignores whitespace, TUI wrapping and box characters, so a task line wrapped
// over several pane lines is still found. The task's last locatable
// non-empty line decides the position.
func lastTaskEchoIndex(lines []string, task string) int {
	if strings.TrimSpace(task) == "" {
		return -1
	}
	var pane strings.Builder
	var ends []int // ends[i] = offset in pane just past line i's compacted text
	for _, l := range lines {
		pane.WriteString(compactEchoText(l))
		ends = append(ends, pane.Len())
	}
	paneStr := pane.String()
	taskLines := strings.Split(task, "\n")
	for i := len(taskLines) - 1; i >= 0; i-- {
		tl := compactEchoText(taskLines[i])
		if len(tl) < minEchoMatchLen {
			continue
		}
		at := strings.LastIndex(paneStr, tl)
		if at < 0 {
			continue
		}
		last := at + len(tl) // offset past the final matched byte
		for li, e := range ends {
			if e >= last {
				return li
			}
		}
	}
	return -1
}

// completionSummary returns the text after the first occurrence of pat in l.
func completionSummary(l, pat string) string {
	i := strings.Index(l, pat)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(StripANSI(l[i+len(pat):]))
}

// isRealCompletionLine reports whether l is a genuine completion line for pat:
// it matches the marker, is not an instruction placeholder, and carries a
// non-empty summary that is not an angle-bracket placeholder.
func isRealCompletionLine(l, pat string) bool {
	if !matchesCompletionPattern(l, pat) || isCompletionPlaceholder(l) {
		return false
	}
	s := completionSummary(l, pat)
	return s != "" && !strings.HasPrefix(s, "<")
}

// echoGate decides whether a completion line at index i of a scanned capture
// may count. echoEnd is lastTaskEchoIndex for that capture (-1 if unknown).
// A line at or before the echoed task text never counts.
func echoGate(i, echoEnd int) bool {
	return echoEnd < 0 || i > echoEnd
}
