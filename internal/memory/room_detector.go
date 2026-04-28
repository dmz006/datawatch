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

// deriveFloor returns the project's parent-directory basename when
// project_dir is nested two levels deep (e.g. /home/user/work/foo →
// "work"). When the parent matches a known org marker (workspace,
// projects, src, work, code) the grandparent is used. Empty when
// the structure is shallower than that. (v5.26.72)
func deriveFloor(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	clean := strings.TrimRight(projectDir, "/")
	clean = strings.TrimPrefix(clean, "~/")
	parent := filepath.Base(filepath.Dir(clean))
	if parent == "" || parent == "." || parent == "/" {
		return ""
	}
	// Skip generic container dirs — climb one more level.
	skip := map[string]bool{
		"workspace": true, "projects": true, "src": true,
		"work": true, "code": true, "repos": true, "git": true,
	}
	if skip[parent] {
		grand := filepath.Base(filepath.Dir(filepath.Dir(clean)))
		if grand != "" && grand != "." && grand != "/" && !skip[grand] {
			return grand
		}
		return ""
	}
	return parent
}

// classifyShelf carves a sub-room out of a room when the content
// names a known sub-axis. Returns "" when no sub-axis matches —
// callers shouldn't auto-set shelf when the room is itself empty.
// (v5.26.72)
func classifyShelf(content, room string) string {
	if room == "" {
		return ""
	}
	lower := strings.ToLower(content)
	shelves := map[string]map[string][]string{
		"auth": {
			"oauth":    {"oauth", "openid", "oidc"},
			"sessions": {"session cookie", "session id", "session token"},
			"2fa":      {"2fa", "totp", "mfa", "two-factor"},
			"sso":      {"sso", "saml", "single sign"},
		},
		"db": {
			"migration": {"migration", "schema_version", "alter table"},
			"query":     {"select ", "join ", "where ", "subquery"},
			"index":     {"index", "btree", "vacuum", "explain"},
		},
		"deploy": {
			"k8s":     {"kubernetes", "k8s", "kubectl", "helm"},
			"docker":  {"docker", "container", "image"},
			"release": {"release tag", "rollout", "rollback"},
		},
		"testing": {
			"unit":        {"unit test", "go test"},
			"integration": {"integration test", "e2e"},
			"smoke":       {"smoke", "release-smoke"},
		},
	}
	if topics, ok := shelves[room]; ok {
		for shelf, anchors := range topics {
			for _, a := range anchors {
				if strings.Contains(lower, a) {
					return shelf
				}
			}
		}
	}
	return ""
}

// deriveBox bundles by author/source. When source names a known
// agent/operator pattern, returns it as the box value. Otherwise
// falls back to "operator" for manual entries, "" for everything
// else (the sweeper + UI both treat empty box as "all authors").
// (v5.26.72)
func deriveBox(source string) string {
	if source == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(source, "agent:"):
		return source // already prefixed; keep verbatim
	case strings.HasPrefix(source, "channel:"):
		return source
	case source == "operator", source == "manual":
		return "operator"
	case source == "session", strings.HasPrefix(source, "session:"):
		return "session"
	default:
		return source
	}
}

// AutoTagFull (v5.26.72) extends AutoTag with the full mempalace
// spatial axes: floor + shelf + box. Existing AutoTag callers are
// untouched; new code paths can switch to this one when they want
// the complete 6-dim classification.
func AutoTagFull(projectDir, content, source, wing, room, hall, floor, shelf, box string) (
	string, string, string, string, string, string,
) {
	wing, room, hall = AutoTag(projectDir, content, wing, room, hall)
	if floor == "" {
		floor = deriveFloor(projectDir)
	}
	if shelf == "" {
		shelf = classifyShelf(content, room)
	}
	if box == "" {
		box = deriveBox(source)
	}
	return wing, room, hall, floor, shelf, box
}
