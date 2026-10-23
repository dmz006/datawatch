//go:build !linux

package shutdowncause

import "os"

// CandidatesFor has no /proc on non-Linux platforms: no candidates.
func CandidatesFor(daemonPID int) []Candidate { return nil }

// capturePlatform is the non-Linux fallback. It cannot read siginfo or
// /proc, so it records only the signal name and reports Source=unknown.
func capturePlatform(sig os.Signal) Cause {
	var c Cause
	c.Source = SourceUnknown
	if sig != nil {
		c.Signal = sig.String()
	}
	return c
}
