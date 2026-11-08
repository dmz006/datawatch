package main

import (
	"fmt"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// verdictForExemptNoChange handles a task that changed nothing by design: a
// note-style task carries a VerificationScriptReason (the B124 exemption) and
// has no VerificationScript. The caller only reaches it when the diff is empty
// and no files were added, so an empty diff is the expected outcome, not a
// failure. handled is true, with an OK verdict whose summary quotes the reason,
// only when reason is non-blank and script is blank. Otherwise handled is false
// and the caller falls through to its existing no-output failure.
func verdictForExemptNoChange(reason, script string) (autonomouspkg.VerificationResult, bool) {
	reason = strings.TrimSpace(reason)
	if reason == "" || strings.TrimSpace(script) != "" {
		return autonomouspkg.VerificationResult{}, false
	}
	return autonomouspkg.VerificationResult{
		OK:         true,
		Severity:   "low",
		Summary:    fmt.Sprintf("verifier: task changed nothing by design (no verification script; reason: %q)", reason),
		VerifiedAt: time.Now(),
	}, true
}
