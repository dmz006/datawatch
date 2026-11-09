// Worker session profile configure keys — the autonomous.worker_profiles,
// autonomous.worker_profile_defaults and autonomous.worker_profile settings
// are settable from chat with `configure <key>=<value>`. handleConfigure
// passes the pair to the daemon, which validates it with the same rules the
// PUT /api/config path runs (validateWorkerProfilePatch in internal/server):
// an invalid value is refused with its message and nothing is written. This
// file owns what the router knows about these keys: the closed set, the help
// text spliced into `configure help` / `configure list`, and the per-key help
// a bare `configure <key>` returns.
package router

import "strings"

// workerProfileHowtoPath is the how-to every worker-profile help line points at.
const workerProfileHowtoPath = "docs/howto/worker-session-profiles.md"

// workerProfileConfigureEntry is one settable worker-profile key and the
// help text that documents it.
type workerProfileConfigureEntry struct {
	Key  string // full dot-path key as the daemon stores it
	Help string // one-line help shown in `configure help`, `configure list` and for a bare key
}

// workerProfileConfigureEntries — the settable worker-profile keys, in help
// order.
var workerProfileConfigureEntries = []workerProfileConfigureEntry{
	{
		"autonomous.worker_profiles",
		"JSON object of user profiles keyed by name (name, mcp, tools, skills, context, model, effort); built-in names are reserved; see " + workerProfileHowtoPath,
	},
	{
		"autonomous.worker_profile_defaults",
		"JSON object {\"role\": {...}, \"kind\": {...}} mapping a role or kind to a profile name; see " + workerProfileHowtoPath,
	},
	{
		"autonomous.worker_profile",
		"global profile name applied to every worker session (empty clears it); see " + workerProfileHowtoPath,
	},
}

// WorkerProfileConfigureHelpLines renders the registry as the help lines
// handleConfigure splices into the `configure help` and `configure list`
// text; each line carries the two-space indent the other key lines have.
func WorkerProfileConfigureHelpLines() []string {
	lines := make([]string, 0, len(workerProfileConfigureEntries))
	for _, e := range workerProfileConfigureEntries {
		lines = append(lines, "  "+e.Key+" ("+e.Help+")")
	}
	return lines
}

// WorkerProfileConfigureKeyHelp reports the help text for one worker-profile
// key (exact match).
func WorkerProfileConfigureKeyHelp(key string) (string, bool) {
	for _, e := range workerProfileConfigureEntries {
		if e.Key == key {
			return e.Help, true
		}
	}
	return "", false
}

// WorkerProfileConfigureIsKnown reports whether key is one of the settable
// worker-profile keys.
func WorkerProfileConfigureIsKnown(key string) bool {
	_, ok := WorkerProfileConfigureKeyHelp(strings.TrimSpace(key))
	return ok
}
