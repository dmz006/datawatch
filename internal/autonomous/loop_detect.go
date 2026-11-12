package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/pilog"
)

// announcementPatterns are lowercase, case-insensitive phrases that announce
// an action the worker is about to take. A turn whose text contains one of
// them but carries no tool call is "announcing without acting".
var announcementPatterns = []string{
	"now i will",
	"let me create",
	"let me write",
	"i will now",
	"next, i'll",
}

// LoopKind names what a LoopFinding detected.
type LoopKind string

const (
	// KindAnnounce: consecutive turns announce an action but never call a tool.
	KindAnnounce LoopKind = "announce"
	// KindRepeat: consecutive turns repeat the identical tool call.
	KindRepeat LoopKind = "repeat"
)

// LoopActionTextLen caps the announced-action phrase carried in LoopFinding.
const LoopActionTextLen = 100

// LoopFinding describes a detected worker loop and how to break it.
type LoopFinding struct {
	// Kind is KindAnnounce or KindRepeat.
	Kind LoopKind
	// Action names the stuck action: for announcements the phrase the
	// worker kept announcing, for repeats the repeated tool name.
	Action string
	// Hint tells the worker what to do immediately to escape the loop.
	Hint string
}

// DetectLoop is a pure function over recent assistant turns. It reports a
// finding when the last n turns (n <= 0 disables detection) either all
// announce an action with zero tool calls (KindAnnounce) or all repeat the
// identical tool call, same name and same arguments (KindRepeat). With fewer
// than n turns available, no finding is reported.
func DetectLoop(turns []pilog.Turn, n int) (LoopFinding, bool) {
	if n <= 0 || len(turns) < n {
		return LoopFinding{}, false
	}
	last := pilog.Recent(turns, n)

	if allAnnouncingOnly(last) {
		return LoopFinding{
			Kind:   KindAnnounce,
			Action: clampedAnnouncement(last[len(last)-1].Text),
			Hint:   "Stop announcing. Call the write tool immediately and actually create or update the file now.",
		}, true
	}
	if name, ok := allRepeating(last); ok {
		return LoopFinding{
			Kind:   KindRepeat,
			Action: name,
			Hint:   "You are repeating the same tool call with identical arguments. Stop retrying and call the write tool immediately with the file contents you intend to produce.",
		}, true
	}
	return LoopFinding{}, false
}

// allAnnouncingOnly reports whether every turn announces an action in its
// text and carries no tool calls.
func allAnnouncingOnly(turns []pilog.Turn) bool {
	for _, t := range turns {
		if len(t.ToolCalls) != 0 || !announcesAction(t.Text) {
			return false
		}
	}
	return true
}

// announcesAction reports whether text contains an announcement pattern.
func announcesAction(text string) bool {
	t := strings.ToLower(text)
	for _, p := range announcementPatterns {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

// allRepeating reports whether every turn's final tool call is the identical
// call (same name and same arguments); it returns the repeated tool name.
func allRepeating(turns []pilog.Turn) (string, bool) {
	base := turns[0]
	if len(base.ToolCalls) == 0 || base.ToolCalls[len(base.ToolCalls)-1].Name == "" {
		return "", false
	}
	ident := base.ToolCalls[len(base.ToolCalls)-1]
	for _, t := range turns[1:] {
		if len(t.ToolCalls) == 0 {
			return "", false
		}
		lt := t.ToolCalls[len(t.ToolCalls)-1]
		if lt.Name != ident.Name || lt.Args != ident.Args {
			return "", false
		}
	}
	return ident.Name, true
}

func clampedAnnouncement(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= LoopActionTextLen {
		return s
	}
	return s[:LoopActionTextLen] + "…"
}
