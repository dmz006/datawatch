package main

import (
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// verdictForAlreadyLanded handles a verified no-output case: on a retry the
// per-attempt diff window can be empty by construction when an earlier
// attempt of the same task already committed the real work. Instead of
// failing with "no changes detected", re-run the task's own deterministic
// verification script; if it passes, the work clearly landed already, so
// accept the task as verified. Returns (result, true) only when the script
// was present and exited 0; (zero, false) otherwise so the caller falls
// through to the existing no-output failure.
func verdictForAlreadyLanded(scriptRel, projectDir string) (autonomouspkg.VerificationResult, bool) {
	if strings.TrimSpace(scriptRel) == "" {
		return autonomouspkg.VerificationResult{}, false
	}
	_, ok, err := autonomouspkg.RunVerificationScript(scriptRel, projectDir)
	if !ok || err != nil {
		return autonomouspkg.VerificationResult{}, false
	}
	return autonomouspkg.VerificationResult{
		OK:         true,
		Severity:   "low",
		Summary:    "verifier: no new changes; work already landed in an earlier attempt and its verification script passed",
		VerifiedAt: time.Now(),
	}, true
}
