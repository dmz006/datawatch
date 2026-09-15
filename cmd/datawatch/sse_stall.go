package main

import "strings"

// autonomousSSEStallPatterns lists pane output fragments that indicate
// opencode has lost its Ollama SSE connection and will not self-terminate.
// Scanned every ~30s inside the autonomousVerify wait loop and every 60s by
// the automata-watchdog goroutine (cmd/datawatch/main.go); on match the
// session is killed so the executor's retry path fires.
//
// B89 (2026-09-15): a qwen3.8:27b task stalled ~2h undetected because
// opencode printed "SSE read timed out" — a phrasing not covered by the
// then-current list ("SSE Timeout", "SSE error", ...). This is the third
// time a new provider-error phrasing has gone undetected (B83 fixed
// verifying/running_tests blindness, B85 added "provider response
// headers"); an exact-case substring list keeps going blind to new
// wording. matchSSEStallPattern below now compares case-insensitively so
// at least casing drift can't cause a repeat, and this file's test locks
// the exact real-world strings observed so far as a regression guard.
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

// matchSSEStallPattern reports whether scrollback contains any known SSE
// stall fragment, case-insensitively, and returns the matched pattern (in
// its canonical casing from autonomousSSEStallPatterns) for logging.
func matchSSEStallPattern(scrollback string) (string, bool) {
	lower := strings.ToLower(scrollback)
	for _, pat := range autonomousSSEStallPatterns {
		if strings.Contains(lower, strings.ToLower(pat)) {
			return pat, true
		}
	}
	return "", false
}
