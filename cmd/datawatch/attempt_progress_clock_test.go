package main

import (
	"testing"
	"time"
)

func TestAttemptProgressClockDecide(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	min := time.Minute

	// baseline is the seed observation at the attempt start.
	baseline := attemptObservation{
		Now:             start,
		LogSize:         100,
		WorkFingerprint: "fp-1",
		HeadSHA:         "head-1",
		ChildCPUTicks:   10,
	}
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }
	// atTime returns a copy of baseline stamped at t with all other fields
	// unchanged (i.e. no progress signal moved since baseline).
	atTime := func(t time.Time) attemptObservation {
		c := baseline
		c.Now = t
		return c
	}
	// with returns a copy of o with logSize overridden.
	withLog := func(o attemptObservation, size int64) attemptObservation {
		c := o
		c.LogSize = size
		return c
	}
	withFingerprint := func(o attemptObservation, fp string) attemptObservation {
		c := o
		c.WorkFingerprint = fp
		return c
	}
	withHead := func(o attemptObservation, sha string) attemptObservation {
		c := o
		c.HeadSHA = sha
		return c
	}
	withCPU := func(o attemptObservation, ticks uint64) attemptObservation {
		c := o
		c.ChildCPUTicks = ticks
		return c
	}

	noStop := struct {
		stop   bool
		reason string
	}{false, ""}

	cases := []struct {
		name       string
		obs        []attemptObservation
		maxAttempt time.Duration
		noProgress time.Duration
		want       []struct {
			stop   bool
			reason string
		}
	}{
		{
			name:       "attempt over the max stops",
			obs:        []attemptObservation{baseline, atTime(at(61))},
			maxAttempt: 60 * time.Second,
			noProgress: 0,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				{true, "attempt exceeded the maximum attempt time of 60 seconds (ran 61 seconds)"},
			},
		},
		{
			name:       "no progress for the limit stops with exact evidence text",
			obs:        []attemptObservation{baseline, atTime(at(31))},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				{true, "no progress for 31 seconds: no output, no file changes, no commits"},
			},
		},
		{
			name:       "log growth resets the clock past the old deadline",
			obs:        []attemptObservation{baseline, withLog(atTime(at(10)), 200), withLog(atTime(at(35)), 200)},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{noStop, noStop, noStop},
		},
		{
			name:       "work fingerprint change resets the clock past the old deadline",
			obs:        []attemptObservation{baseline, withFingerprint(atTime(at(10)), "fp-2"), withFingerprint(atTime(at(35)), "fp-2")},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{noStop, noStop, noStop},
		},
		{
			name:       "new commit resets the clock past the old deadline",
			obs:        []attemptObservation{baseline, withHead(atTime(at(10)), "head-2"), withHead(atTime(at(35)), "head-2")},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{noStop, noStop, noStop},
		},
		{
			name:       "child CPU increase resets the clock past the old deadline",
			obs:        []attemptObservation{baseline, withCPU(atTime(at(10)), 20), withCPU(atTime(at(35)), 20)},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{noStop, noStop, noStop},
		},
		{
			name:       "log size shrink does not reset the clock",
			obs:        []attemptObservation{baseline, withLog(atTime(at(40)), 50)},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				{true, "no progress for 40 seconds: no output, no file changes, no commits"},
			},
		},
		{
			name:       "unchanged child CPU does not reset the clock",
			obs:        []attemptObservation{baseline, atTime(at(40))},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				{true, "no progress for 40 seconds: no output, no file changes, no commits"},
			},
		},
		{
			name:       "both limits 0 never stop even after a very long time",
			obs:        []attemptObservation{baseline, atTime(at(7200)), atTime(start.Add(400 * time.Hour))},
			maxAttempt: 0,
			noProgress: 0,
			want:       []struct {
				stop   bool
				reason string
			}{noStop, noStop, noStop},
		},
		{
			name:       "max set, no progress off: only the max limit fires",
			obs:        []attemptObservation{baseline, atTime(at(40)), atTime(at(61))},
			maxAttempt: 1 * min,
			noProgress: 0,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				noStop,
				{true, "attempt exceeded the maximum attempt time of 60 seconds (ran 61 seconds)"},
			},
		},
		{
			name:       "no progress set, max off: only the no-progress limit fires",
			obs:        []attemptObservation{baseline, withLog(atTime(at(10)), 150), withLog(atTime(at(70)), 150)},
			maxAttempt: 0,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				noStop,
				{true, "no progress for 60 seconds: no output, no file changes, no commits"},
			},
		},
		{
			name:       "max attempt wins when both limits apply",
			obs:        []attemptObservation{baseline, atTime(at(65))},
			maxAttempt: 60 * time.Second,
			noProgress: 30 * time.Second,
			want: []struct {
				stop   bool
				reason string
			}{
				noStop,
				{true, "attempt exceeded the maximum attempt time of 60 seconds (ran 65 seconds)"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newAttemptProgressClock(tc.obs[0].Now, tc.obs[0])
			for i, obs := range tc.obs {
				if i > 0 {
					clock.observe(obs)
				}
				stop, reason := clock.decide(obs, tc.maxAttempt, tc.noProgress)
				if stop != tc.want[i].stop || reason != tc.want[i].reason {
					t.Fatalf("observation %d: got (%v, %q), want (%v, %q)",
						i, stop, reason, tc.want[i].stop, tc.want[i].reason)
				}
			}
		})
	}
}
