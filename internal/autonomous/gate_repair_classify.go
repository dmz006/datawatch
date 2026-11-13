// Gate-repair failure classification: a pure, side-effect-free reader over a
// failed gate's output that names the failure shape and the repair that would
// fix it. The classifier is pure Go (no repo access, no shell): it decides
// only from the input carried in by the caller, which mirrors what
// runGateCheck records in GateResult (output text, exit code, the script's
// path and text, the task's changed files, and the story indices).
//
// The classes correspond to real failure modes seen in generated
// .datawatch/verify scripts and project checks:
//
//   - later_story_script — the gate script belongs to a later story whose
//     files do not exist yet, so the script is missing or empty (runGateCheck
//     reports it as "missing from the project's .datawatch/verify folder");
//   - docs_mirror_stale — the embedded docs mirror (internal/server/web/docs)
//     drifts from docs/, the failure shape scripts/check-docs-sync.sh prints
//     ("embedded PWA docs drift ... run 'make sync-docs' and re-stage");
//   - allowed_list_rejects — a scope check built on an allowed-files list
//     (see the cumulative allowed-file helper, docs/howto/
//     automaton-verification-scripts.md) rejects a file the task changed;
//   - tracker_id_css_hex — a tracker-ID leak check
//     (scripts/check-no-internal-refs.sh pattern \b(BL[0-9]+|F[0-9]+|B[0-9]+|
//     S[0-9]+)\b) matched a CSS hex color code, not a real tracker ID;
//   - pipe_sigpipe — the gate died of SIGPIPE (exit 141, e.g. "go test
//     ./... | grep -q" under pipefail) or a bash script was run under sh and
//     shows "Syntax error" / "not found" bashisms;
//   - restart_killed — the gate process was killed by a signal (GateResult
//     ExitCode < 0, gateSignalKilledReason) or the output carries a daemon
//     restart marker; it failed because it was interrupted, not because a
//     check failed;
//   - unknown — nothing above matched, or the evidence is ambiguous.
//
// ClassifyGateFailure orders the checks from the most specific (a story
// ordering fact, then distinctive output phrases) to the most generic (a
// signal kill). Any case where two shapes fit equally is left to the less
// specific check, and a gate with no recognizable shape at all is unknown.
package autonomous

import (
	"regexp"
	"strings"
)

// GateFailureClass names the shape of a failed gate's output.
type GateFailureClass string

// The known failure classes, in the order ClassifyGateFailure checks them.
// GateFailureUnknown is the catch-all for output that matches no shape, or
// whose evidence is ambiguous.
const (
	GateFailureLaterStoryScript   GateFailureClass = "later_story_script"
	GateFailureDocsMirrorStale    GateFailureClass = "docs_mirror_stale"
	GateFailureAllowedListRejects GateFailureClass = "allowed_list_rejects"
	GateFailureTrackerIDCSSHex    GateFailureClass = "tracker_id_css_hex"
	GateFailurePipeSigpipe        GateFailureClass = "pipe_sigpipe"
	GateFailureRestartKilled      GateFailureClass = "restart_killed"
	GateFailureUnknown            GateFailureClass = "unknown"
)

// GateFailureInput is everything ClassifyGateFailure may look at. All fields
// are ordinary values copied out of the gate result and the task record; the
// classifier performs no I/O.
type GateFailureInput struct {
	// Output is the failed gate's captured output text (GateResult.Output).
	Output string
	// ExitCode is the gate process exit code (GateResult.ExitCode); negative
	// when the process was killed by a signal.
	ExitCode int
	// SignalKilled reports whether the gate process was killed by a signal,
	// as runGateCheck records for ExitCode < 0 (gateSignalKilledReason).
	SignalKilled bool
	// ScriptText is the text of the gate script that ran (empty for a
	// command gate).
	ScriptText string
	// ScriptPath is the script's repo-relative path, e.g.
	// ".datawatch/verify/<task>.sh".
	ScriptPath string
	// ChangedFiles are the files the task changed (its generated files).
	ChangedFiles []string
	// InOwnVerifyFolder reports whether the script belongs to the automaton's
	// own .datawatch/verify folder (planner-written, per-story) rather than a
	// shared project script under scripts/.
	InOwnVerifyFolder bool
	// ScriptStoryIndex is the index of the story the script belongs to.
	ScriptStoryIndex int
	// GateStoryIndex is the index of the story the gate runs in.
	GateStoryIndex int
}

// ClassifyGateFailure returns the failure class of a failed gate's output.
// The checks run most-specific-first: a story-ordering fact wins over any
// output text, then distinctive output phrases (docs drift, allowed-list
// rejections, CSS hex colors), then process-level shapes (SIGPIPE, a signal
// kill). Output matching no shape, or matching several only weakly, is
// GateFailureUnknown.
func ClassifyGateFailure(in GateFailureInput) GateFailureClass {
	switch {
	case isLaterStoryScript(in):
		return GateFailureLaterStoryScript
	case isDocsMirrorStale(in.Output):
		return GateFailureDocsMirrorStale
	case isAllowedListRejects(in):
		return GateFailureAllowedListRejects
	case isTrackerIDCSSHex(in.Output):
		return GateFailureTrackerIDCSSHex
	case isPipeSigpipe(in):
		return GateFailurePipeSigpipe
	case isRestartKilled(in):
		return GateFailureRestartKilled
	}
	return GateFailureUnknown
}

// isLaterStoryScript reports whether the gate failed because its script
// belongs to a later story whose files have not landed yet: the script lives
// in the automaton's own verify folder, its story index is after the gate's
// story, and the output shows the script's own file is missing (or there is
// no output at all, as when a not-yet-written script cannot run). The
// story-ordering fact outranks the weaker "not found" output shape (a
// bashism), which a missing script can also produce.
func isLaterStoryScript(in GateFailureInput) bool {
	if !in.InOwnVerifyFolder {
		return false
	}
	if in.ScriptStoryIndex <= in.GateStoryIndex {
		return false
	}
	lower := strings.ToLower(in.Output)
	return strings.TrimSpace(in.Output) == "" ||
		strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "is missing from") ||
		strings.Contains(lower, "cannot access") ||
		strings.Contains(lower, "not found")
}

// isDocsMirrorStale reports whether the output is the embedded docs mirror
// drifting from docs/ — the failure shape scripts/check-docs-sync.sh prints
// ("ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and
// re-stage"), or any other check pairing docs-mirror wording with the
// sync-docs repair.
func isDocsMirrorStale(output string) bool {
	lower := strings.ToLower(output)
	repair := strings.Contains(lower, "sync-docs") || strings.Contains(lower, "check-docs-sync")
	mention := strings.Contains(lower, "docs drift") ||
		strings.Contains(lower, "docs mirror") ||
		strings.Contains(lower, "embedded pwa docs") ||
		strings.Contains(lower, "embedded docs")
	return repair && mention
}

// allowedFilesListRe matches a script that builds an allowed-files list, as
// the cumulative allowed-file helper habit prescribes (allowed_files=...,
// "allowed list", "allowed-files", ...).
var allowedFilesListRe = regexp.MustCompile(`(?i)allowed[-_ ]?(files?|list)`)

// allowedRejectWords are the rejection phrases a scope check prints when a
// changed file is not on the allowed list.
var allowedRejectWords = []string{
	"not in the allowed",
	"not in allowed",
	"not on the allowed",
	"outside the allowed",
	"not allowed",
	"out of scope",
	"unexpected file",
}

// isAllowedListRejects reports whether the gate is a scope check with an
// allowed-files list whose output rejects one of the files the task changed
// (a changelog, README, docs, locale or test file the task touched).
func isAllowedListRejects(in GateFailureInput) bool {
	if !allowedFilesListRe.MatchString(in.ScriptText) {
		return false
	}
	lower := strings.ToLower(in.Output)
	rejected := false
	for _, w := range allowedRejectWords {
		if strings.Contains(lower, w) {
			rejected = true
			break
		}
	}
	if !rejected {
		return false
	}
	for _, cf := range in.ChangedFiles {
		cf = strings.TrimSpace(cf)
		if cf != "" && strings.Contains(in.Output, cf) {
			return true
		}
	}
	return false
}

// trackerIDRe is the tracker-ID leak pattern the internal-ref checks use
// (scripts/check-no-internal-refs.sh): BL###, F##, B##, S##.
var trackerIDRe = regexp.MustCompile(`\b(BL[0-9]+|B[0-9]+|F[0-9]+|S[0-9]+)\b`)

// isTrackerIDCSSHex reports whether the output was flagged by a tracker-ID
// check whose reported matches are all CSS hex color codes (e.g. #B40044
// reading as "B40044") rather than real tracker IDs: every tracker-pattern
// match sits in a '#' hex token and the token is pure hex. If even one
// match looks like a genuine tracker ID, the class does not apply.
func isTrackerIDCSSHex(output string) bool {
	matches := trackerIDRe.FindAllStringIndex(output, -1)
	if len(matches) == 0 {
		return false
	}
	for _, m := range matches {
		start, end := m[0], m[1]
		// Widen to the full word the match sits in so partial matches (e.g.
		// the "B4" of "#B4F2C0") are judged with their neighbors.
		for start > 0 && isWordByte(output[start-1]) {
			start--
		}
		for end < len(output) && isWordByte(output[end]) {
			end++
		}
		if !isCSSHexToken(output, start, end) {
			return false
		}
	}
	return true
}

// isCSSHexToken reports whether s[start:end] is a CSS hex color code: it is
// directly preceded by '#' and consists only of hex digits (3-8 of them).
func isCSSHexToken(s string, start, end int) bool {
	if start == 0 || s[start-1] != '#' {
		return false
	}
	token := s[start:end]
	if len(token) < 3 || len(token) > 8 {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// goTestPipeGrepQRe matches a script that pipes go test output straight into
// "grep -q" — the pipe-fail shape that dies of SIGPIPE under pipefail once
// grep -q exits on the first match while go test is still writing.
var goTestPipeGrepQRe = regexp.MustCompile(`(?s)go\s+test\b[^|]*\|\s*grep\b[^\n]*-q\b`)

// shNotfoundRe matches the "word: not found" line a shell prints when a
// bashism runs under sh (e.g. "sh: 5: [[: not found").
var shNotfoundRe = regexp.MustCompile(`\b\S+?: not found\b`)

// isPipeSigpipe reports whether the gate died of a broken pipe: exit 141
// (128 + SIGPIPE), a script that pipes go test into grep -q, or a bash script
// run under sh showing "Syntax error" / "not found" bashisms.
func isPipeSigpipe(in GateFailureInput) bool {
	if in.ExitCode == 141 {
		return true
	}
	if goTestPipeGrepQRe.MatchString(in.ScriptText) {
		return true
	}
	lower := strings.ToLower(in.Output)
	if strings.Contains(lower, "syntax error") {
		return true
	}
	if strings.Contains(lower, "broken pipe") {
		return true
	}
	return shNotfoundRe.MatchString(in.Output)
}

// restartKilledMarkers are output lines that show the gate was interrupted
// by the daemon going away rather than by a failing check.
var restartKilledMarkers = []string{
	"daemon restart",
	"daemon shutting down",
	"shutting down",
	"sighup",
	"hangup",
	"signal: terminated",
}

// isRestartKilled reports whether the gate was killed by a signal (as
// runGateCheck records for a signal-killed process) or its output carries a
// daemon restart marker: the gate failed because it was interrupted, not
// because a check failed.
func isRestartKilled(in GateFailureInput) bool {
	if in.SignalKilled {
		return true
	}
	lower := strings.ToLower(in.Output)
	for _, m := range restartKilledMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// GateRepairDescriptions is the proposed repair per failure class: the
// one-paragraph action a repairer (or an operator) should take for that
// shape of failure.
var GateRepairDescriptions = map[GateFailureClass]string{
	GateFailureLaterStoryScript:   "The gate script belongs to a later story whose files do not exist yet. Do not create those files now; the gate will pass once that story lands. Re-run the gate after the later story completes.",
	GateFailureDocsMirrorStale:    "The embedded docs mirror (internal/server/web/docs) drifted from docs/. Run `make sync-docs` to rebuild the mirror and re-stage, then re-run the gate — no code change is needed.",
	GateFailureAllowedListRejects: "The scope check's allowed-files list rejects a file the task changed (a changelog, README, docs, locale or test file). Add that file to the cumulative allowed-files list in the gate script (or the helper it sources) so the scope check covers it, then re-run the gate.",
	GateFailureTrackerIDCSSHex:    "The tracker-ID check matched CSS hex color codes, not real tracker IDs. Tighten the check's pattern so it cannot match inside '#' hex tokens (for example, require the character before the ID to be non-hex), or exempt the color — no docs change is needed.",
	GateFailurePipeSigpipe:        "The gate died of a broken pipe (SIGPIPE, exit 141) or a bash script was run under sh. Stop piping go test output straight into grep -q (capture the output first, then grep it) and run bash scripts with bash, then re-run the gate.",
	GateFailureRestartKilled:      "The gate process was killed by a signal or a daemon restart before its checks finished; this is an interruption, not a failing check. Re-run the gate without the daemon restart interrupting it.",
	GateFailureUnknown:            "No known failure shape matched the gate output. Read the gate output, fix the underlying failure it reports, and re-run the gate.",
}

// Repair returns the proposed-repair description for this failure class.
// An unrecognized class falls back to the unknown-class repair.
func (c GateFailureClass) Repair() string {
	if r, ok := GateRepairDescriptions[c]; ok {
		return r
	}
	return GateRepairDescriptions[GateFailureUnknown]
}
