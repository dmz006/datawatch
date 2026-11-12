package main

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// The SSE stall detector exists for one narrow failure mode: an
// opencode-family session loses its Ollama (or other provider) SSE
// connection and prints a short provider-error line, then sits silently
// forever. It scans the pane scrollback every ~30s inside the
// autonomousVerify wait loop and every 60s by the automata-watchdog
// goroutine (cmd/datawatch/main.go); on match the session is killed so the
// executor's retry path fires.
//
// It is deliberately gated on the backend family (sseStallAppliesTo) and on
// a line BEING the error (sseCandidateLines + sseLineMatches): other
// families (claude-code, pi-prompt, openwebui, ollama) already have their
// own silence/activity/idle detectors, and a worker on any backend can
// legitimately SHOW these phrases in output it wrote, printed, or asserted
// on — the old whole-scrollback substring search
// (matchSSEStallPattern, replaced by the scan below) ended such sessions
// and retried their tasks until they failed.
//
// B89 (2026-09-15): a qwen3.8:27b task stalled ~2h undetected because
// opencode printed "SSE read timed out" — a phrasing not covered by the
// then-current list ("SSE Timeout", "SSE error", ...). This is the third
// time a new provider-error phrasing has gone undetected (B83 fixed
// verifying/running_tests blindness, B85 added "provider response
// headers"); an exact-case substring list keeps going blind to new
// wording. The line matcher below compares case-insensitively so at least
// casing drift can't cause a repeat, and this file's test locks the exact
// real-world strings observed so far as a regression guard.
var autonomousSSEStallPatterns = []string{
	"SSE Timeout",
	"SSE error",
	"SSE connection",
	"SSE read timed out",
	"connection refused",
	"dial tcp: lookup",
	"context deadline exceeded",
	"failed to connect to ollama",
	"no such host",
	"cannot be parsed as a URL",
	"provider response headers",
}

// sseStallFamilies is the closed set of backend families the detector may
// act on: the three registered opencode-family names (main.go registers
// the "opencode-acp" and "opencode-prompt" inference backends, and
// "opencode" is used for direct panes). The decision is always made from
// the session's BackendFamily, never from what the pane shows: claude-code,
// pi-prompt, openwebui, ollama, goose, aider and the rest keep their own
// stall detectors untouched and this one must not fire for them.
var sseStallFamilies = map[string]bool{
	"opencode":        true,
	"opencode-acp":    true,
	"opencode-prompt": true,
}

// sseStallAppliesTo reports whether the SSE stall detector may act on a
// session whose backend family is backendFamily. An empty or unknown
// family returns false.
func sseStallAppliesTo(backendFamily string) bool {
	return sseStallFamilies[backendFamily]
}

// Scan sources name the call site; they are recorded on the match and in
// the end reason so the journal and alert text say where it came from.
const (
	sseStallSourceWaitLoop = "verifier wait loop"
	sseStallSourceWatchdog = "automata watchdog"
)

// sseStallMatch is what a scan found: the pattern (canonical casing from
// autonomousSSEStallPatterns), the cleaned scrollback line that matched,
// the backend family the gate passed on, and the call-site source.
type sseStallMatch struct {
	Pattern string
	Line    string
	Backend string
	Source  string
}

// sseMaxLineLen bounds how long a line may be to count as a provider
// error. The real provider errors opencode prints are short one-liners; a
// long line is prose, log noise, or code that merely contains a phrase.
const sseMaxLineLen = 200

// ANSI escape cleanup: CSI sequences, OSC strings, and F0/SOS-style
// two-byte escapes as they appear in tmux pane captures.
var (
	sseAnsiCsi = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	sseAnsiOsc = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)?")
	sseAnsiF0  = regexp.MustCompile("\x1b[@-Z\\-_]")
)

// sseCodeShape rejects lines that show the phrase is source code, a test
// expectation, or quoted content rather than an opencode-emitted error:
// asserts, expected/want phrasing, t.Fatalf, Errorf, "err =", and call
// syntax "()".
var sseCodeShapeRe = regexp.MustCompile(`(?i)(assert\w*|expected|\bwant\b|t\.fatal\w*|err\s*=|\(\))`)

// sseLineNumber and sseFileLineCol reject tool-output shapes: a leading
// source line number, or a path.ext:line:col prefix such as
// "server.go:88:17: connection refused".
var (
	sseLineNumberRe = regexp.MustCompile(`^\d+[\t:]`)
	sseFileLineRe   = regexp.MustCompile(`^\S+\.\w{1,8}:\d+:\d+[,:]`)
)

// sseCleanLine strips ANSI escapes, TUI box-drawing characters (the
// "┃ ┃ ┃" frame characters opencode wraps its status text in — that is how
// the B89 "SSE read timed out" line arrived) and surrounding whitespace,
// leaving the bare text of the line.
func sseCleanLine(raw string) string {
	s := sseAnsiCsi.ReplaceAllString(raw, "")
	s = sseAnsiOsc.ReplaceAllString(s, "")
	s = sseAnsiF0.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0x2500 && r <= 0x259F: // box drawing + block elements
		case r >= 0x2800 && r <= 0x28FF: // braille patterns
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), " \t\r\n\u00A0\u200B\uFEFF")
}

// sseIsCandidateLine reports whether a cleaned line could be a provider
// error as opencode prints it. It rejects empty lines, lines longer than
// sseMaxLineLen, and lines that are clearly other content: tool-result or
// output markers (the ⎿ tool-result prefix, and prompt/diff markers $, >,
// +, -), quoted content or code punctuation (quotes or backticks, or the
// sseCodeShape code/test phrasing), leading source line numbers, and file
// paths with :line:col suffixes.
func sseIsCandidateLine(line string) bool {
	if line == "" {
		return false
	}
	if utf8.RuneCountInString(line) > sseMaxLineLen {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(line); r == '⎿' || r == '$' || r == '>' || r == '+' || r == '-' {
		return false
	}
	if strings.ContainsAny(line, "\"'`") {
		return false
	}
	if sseCodeShapeRe.MatchString(line) {
		return false
	}
	if sseLineNumberRe.MatchString(line) {
		return false
	}
	if sseFileLineRe.MatchString(line) {
		return false
	}
	return true
}

// sseCandidateLines walks the scrollback line by line, tracks ``` fenced
// blocks while walking, and returns the cleaned lines that could be
// provider errors, in scrollback order.
func sseCandidateLines(scrollback string) []string {
	rawLines := strings.Split(scrollback, "\n")
	candidates := make([]string, 0, len(rawLines))
	inFence := false
	for _, raw := range rawLines {
		cleaned := sseCleanLine(raw)
		if strings.HasPrefix(strings.ToLower(cleaned), "```") {
			// A fence line opens/closes a code block; the markers
			// themselves are never errors and the block content is
			// code, which is rejected below.
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if !sseIsCandidateLine(cleaned) {
			continue
		}
		candidates = append(candidates, cleaned)
	}
	return candidates
}

// sseErrPrefixes are the short provider-error prefixes opencode prints
// before a raw error phrase ("error: connection refused",
// "AI_API...Error: no such host"). Compared case-insensitively. The
// third literal is built by concatenation because of a rendering quirk in
// this session's file editing that kept corrupting it when typed directly.
var sseErrPrefixes = []string{"", "error:", strings.ToLower("AI_API" + "CallError:")}

// sseLineMatches reports whether a cleaned line IS a known error phrase:
// the line starts with the phrase (case-insensitively), optionally after
// a short provider-error prefix and a " " or ":"-style separator. A mere
// mid-line mention does not match — that is the whole point of replacing
// the old whole-scrollback substring search.
func sseLineMatches(line, pattern string) bool {
	lower := strings.ToLower(line)
	phrase := strings.ToLower(pattern)
	for _, pre := range sseErrPrefixes {
		if strings.HasPrefix(lower, pre+phrase) {
			return true
		}
		if strings.HasPrefix(lower, pre+" "+phrase) {
			return true
		}
	}
	return false
}

// sseFreshMaxSessions bounds the per-session scan history; when the cap is
// reached the oldest session is dropped, so the map cannot grow without
// bound.
const sseFreshMaxSessions = 512

// sseFreshState keeps, per session full id, the multiset of candidate
// lines seen in the previous scan. A candidate line counts as evidence
// only while it is NEW relative to the previous scan (its count in this
// scan exceeds its count in the previous scan), so a long-lived pane that
// merely CONTAINS an old error string does not keep tripping the detector
// when that line was already there, and the state is updated on every
// scan, matched or not. The first scan of a session treats everything as
// new. One package-level instance is shared by both call sites so the
// wait loop and the watchdog agree on what is fresh.
type sseFreshState struct {
	mu sync.Mutex
	// seen maps session full id -> cleaned candidate line -> its count in
	// the most recent scan of that session.
	seen map[string]map[string]int
	// order is the insertion order of seen (oldest first) for eviction.
	order []string
}

func newSSEFreshTracker() *sseFreshState {
	return &sseFreshState{seen: make(map[string]map[string]int)}
}

// forget drops a session's history, e.g. when the wait loop or the
// automata watchdog sees the session finished.
func (f *sseFreshState) forget(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, known := f.seen[sessionID]; !known {
		return
	}
	delete(f.seen, sessionID)
	kept := f.order[:0]
	for _, id := range f.order {
		if id != sessionID {
			kept = append(kept, id)
		}
	}
	f.order = kept
}

// scanRecords returns the candidate lines that are new relative to
// sessionID's previous scan (a missing previous scan is the first scan, so
// everything is new), then records this scan's multiset for the next one.
func (f *sseFreshState) scanRecords(sessionID string, lines []string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.seen[sessionID]
	if prev == nil {
		// New session: leave room by dropping the oldest entries, then
		// remember insertion order for eviction.
		for len(f.seen) >= sseFreshMaxSessions && len(f.order) > 0 {
			oldest := f.order[0]
			f.order = f.order[1:]
			if _, still := f.seen[oldest]; still {
				delete(f.seen, oldest)
				break
			}
		}
		f.order = append(f.order, sessionID)
	}
	cur := make(map[string]int, len(lines))
	for _, l := range lines {
		cur[l]++
	}
	f.seen[sessionID] = cur
	fresh := make([]string, 0, len(lines))
	seen := make(map[string]bool)
	for _, l := range lines {
		// First scan: prev is nil (all zero counts), so every distinct
		// fresh line is reported.
		if cur[l] > prev[l] && !seen[l] {
			seen[l] = true
			fresh = append(fresh, l)
		}
	}
	return fresh
}

// sseFreshTracker is the one shared scan-history instance both call sites
// scan through; tests may reset it by assigning a fresh instance.
var sseFreshTracker = newSSEFreshTracker()

// sseStallScan is the single shared entry point called by the verifier
// wait loop and the automata watchdog. It returns false immediately (and
// records nothing) when the backend family is not in sseStallFamilies;
// otherwise it runs the line matcher over the scrollback under the
// fresh-text rule and reports the first line that IS a known provider
// error, with the pattern, line, backend and source.
func sseStallScan(sessionID, backendFamily, scrollback string, source string) (sseStallMatch, bool) {
	if !sseStallAppliesTo(backendFamily) {
		return sseStallMatch{}, false
	}
	candidates := sseCandidateLines(scrollback)
	for _, line := range sseFreshTracker.scanRecords(sessionID, candidates) {
		for _, pat := range autonomousSSEStallPatterns {
			if sseLineMatches(line, pat) {
				return sseStallMatch{
					Pattern: pat,
					Line:    line,
					Backend: backendFamily,
					Source:  source,
				}, true
			}
		}
	}
	return sseStallMatch{}, false
}
