package main

import (
	"fmt"
	"time"
)

// attemptObservation is a single observation of an attempt's progress
// signals. All fields are plain values so tests can drive the clock with a
// fake Now without any files, tmux session or git repository.
type attemptObservation struct {
	// Now is the observation wall-clock time, supplied by the caller
	// (never time.Now() inside the clock).
	Now time.Time
	// LogSize is the current size of the attempt's output log in bytes.
	LogSize int64
	// WorkFingerprint is a fingerprint of the work directory contents.
	WorkFingerprint string
	// HeadSHA is the current HEAD commit sha ("" when unavailable).
	HeadSHA string
	// ChildCPUTicks is the child process's cumulative CPU ticks
	// (0 when the signal is unavailable).
	ChildCPUTicks uint64
}

// attemptProgressClock decides when an attempt should be stopped. It is
// driven entirely by the Now values of the observations fed to it, so a
// fake clock is sufficient for testing.
type attemptProgressClock struct {
	AttemptStart   time.Time
	LastProgressAt time.Time
	last           attemptObservation
}

// newAttemptProgressClock seeds the clock with the first observation.
// LastProgressAt starts at the attempt start, so idle time from the start
// counts against the no-progress limit.
func newAttemptProgressClock(start time.Time, first attemptObservation) *attemptProgressClock {
	return &attemptProgressClock{
		AttemptStart:   start,
		LastProgressAt: start,
		last:           first,
	}
}

// observe records obs and, if anything counts as progress since the
// previous observation, moves LastProgressAt to obs.Now. Progress is:
// the log grew (increase only — shrinking is not progress), the work
// fingerprint changed, the head sha changed (a new commit), or the child's
// CPU ticks increased. A child that is merely alive but burning no CPU is
// not progress.
func (c *attemptProgressClock) observe(obs attemptObservation) {
	if obs.LogSize > c.last.LogSize ||
		obs.WorkFingerprint != c.last.WorkFingerprint ||
		obs.HeadSHA != c.last.HeadSHA ||
		obs.ChildCPUTicks > c.last.ChildCPUTicks {
		c.LastProgressAt = obs.Now
	}
	c.last = obs
}

// decide reports whether the attempt should be stopped given the latest
// observation obs. A limit of <= 0 switches that check off entirely.
// The max-attempt check wins when both would fire. Everything is computed
// from obs.Now — never from time.Now() — so tests drive a fake clock.
func (c *attemptProgressClock) decide(obs attemptObservation, maxAttempt, noProgress time.Duration) (stop bool, reason string) {
	c.observe(obs)

	if maxAttempt > 0 {
		elapsed := obs.Now.Sub(c.AttemptStart)
		if elapsed > maxAttempt {
			return true, fmt.Sprintf(
				"attempt exceeded the maximum attempt time of %d seconds (ran %d seconds)",
				int64(maxAttempt.Seconds()), int64(elapsed.Seconds()))
		}
	}

	if noProgress > 0 {
		idle := obs.Now.Sub(c.LastProgressAt)
		if idle > noProgress {
			return true, fmt.Sprintf(
				"no progress for %d seconds: no output, no file changes, no commits",
				int64(idle.Seconds()))
		}
	}

	return false, ""
}
