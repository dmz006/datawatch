// v5.26.70 — Mempalace QW#1 port: room_detector_local.py.
//
// Heuristic classifier that auto-assigns wing/hall/room at memory
// save time when the caller hasn't supplied them. Keeps the same
// shape as the upstream Python version: keyword anchors → hall
// classification, project_dir basename → wing fallback, content
// keyword density → room.
//
// Operator-directed (mempalace audit, 2026-04-28). Pure Go port —
// no Python dependency. Hooked into Manager.SaveWithMeta when
// any of the three spatial fields are empty on the way in.

package memory

import (
	"path/filepath"
	"strings"
)

// classifyHall returns the most-appropriate hall for the given
// content. Mempalace uses 5 standard halls: facts, events,
// discoveries, preferences, advice. Ours mirrors them.
func classifyHall(content string) string {
	lower := strings.ToLower(content)

	// preferences — strong personal voice ("I prefer", "I like", "I want")
	prefAnchors := []string{
		"i prefer", "i like", "i want", "i don't like", "i don't want",
		"my favorite", "i always", "i never", "i usually",
	}
	for _, a := range prefAnchors {
		if strings.Contains(lower, a) {
			return "preferences"
		}
	}

	// advice — imperative voice ("you should", "always X", "never X")
	adviceAnchors := []string{
		"you should", "you must", "always ", "never ",
		"avoid ", "don't ", "remember to", "make sure", "be sure to",
	}
	for _, a := range adviceAnchors {
		if strings.Contains(lower, a) {
			return "advice"
		}
	}

	// events — time anchors
	eventAnchors := []string{
		"yesterday", "today", "tomorrow", "last week", "next week",
		"happened", "occurred", "scheduled", "meeting", "deadline",
	}
	for _, a := range eventAnchors {
		if strings.Contains(lower, a) {
			return "events"
		}
	}

	// discoveries — investigation language
	discoveryAnchors := []string{
		"discovered", "found that", "turns out", "realized",
		"learned that", "figured out", "the bug", "root cause",
		"investigation", "debug",
	}
	for _, a := range discoveryAnchors {
		if strings.Contains(lower, a) {
			return "discoveries"
		}
	}

	// Default — if it's stating something that "is" without a strong
	// signal pointing elsewhere, treat as facts.
	return "facts"
}

// deriveWing returns the project_dir basename (stripping any leading
// "~/" or trailing slash). When project_dir is empty, returns
// __global__ — same default as the namespace fallback.
func deriveWing(projectDir string) string {
	if projectDir == "" {
		return DefaultNamespace
	}
	clean := strings.TrimRight(projectDir, "/")
	clean = strings.TrimPrefix(clean, "~/")
	return filepath.Base(clean)
}

// classifyRoom picks a room (topic) from content keywords. Mempalace's
// room set is operator-extensible; ours covers the common datawatch
// topic axes. Empty room is fine — search filters by wing+hall first
// and treats empty room as "any room in the hall".
func classifyRoom(content string) string {
	lower := strings.ToLower(content)
	roomAnchors := map[string][]string{
		"auth":     {"auth", "login", "password", "token", "credential", "session"},
		"deploy":   {"deploy", "release", "ci/cd", "pipeline", "publish", "rollout"},
		"testing":  {"test", "spec", "mock", "fixture", "smoke", "regression"},
		"perf":     {"perf", "benchmark", "latency", "throughput", "memory leak", "cpu"},
		"db":       {"database", "sql", "postgres", "mysql", "sqlite", "migration", "query"},
		"ui":       {"ui", "frontend", "css", "html", "button", "modal", "render"},
		"api":      {"api", "endpoint", "rest", "graphql", "webhook"},
		"docs":     {"doc", "readme", "howto", "manual"},
		"security": {"security", "vulnerability", "cve", "exploit", "audit"},
	}
	for room, anchors := range roomAnchors {
		for _, a := range anchors {
			if strings.Contains(lower, a) {
				return room
			}
		}
	}
	return ""
}

// AutoTag fills in any empty wing/hall/room based on content +
// project_dir heuristics. Already-set fields are preserved
// (operator override always wins). Returns the resulting
// (wing, room, hall) triple.
func AutoTag(projectDir, content, wing, room, hall string) (string, string, string) {
	if wing == "" {
		wing = deriveWing(projectDir)
	}
	if hall == "" {
		hall = classifyHall(content)
	}
	if room == "" {
		room = classifyRoom(content)
	}
	return wing, room, hall
}
