// Unlanded work counts: a pure summary of Automaton branches per landing state
// and per age band, plus the oldest pending branch. It reads only the entries
// it is given and the clock it is handed, so it never touches git.

package autonomous

import (
	"strconv"
	"time"
)

// Landing states counted by CountUnlandedBranches.
const (
	// LandingLanded: every commit is already on the default branch.
	LandingLanded = "landed"
	// LandingPending: commits are not on the default branch but later work
	// replaced them; the branch waits for cleanup.
	LandingPending = "pending"
	// LandingLive: the branch has no settled recommendation yet.
	LandingLive = "live"
	// LandingUnlanded: commits are not on the default branch and need a
	// decision.
	LandingUnlanded = "unlanded"
)

// Age bands counted by CountUnlandedBranches.
const (
	AgeUnderDay  = "under_one_day"
	AgeUnderWeek = "under_one_week"
	AgeOlder     = "older"
)

// UnlandedOldestPending names the oldest pending Automaton branch.
type UnlandedOldestPending struct {
	Branch string        `json:"branch"`
	Title  string        `json:"title"`
	Age    time.Duration `json:"age_ns"`
	// AgeText is the age in plain words, such as "3d 4h".
	AgeText string `json:"age"`
}

// UnlandedCounts is the counts section of the unlanded-work read.
type UnlandedCounts struct {
	Total         int                    `json:"total"`
	ByState       map[string]int         `json:"by_state"`
	ByAge         map[string]int         `json:"by_age"`
	OldestPending *UnlandedOldestPending `json:"oldest_pending,omitempty"`
}

// LandingStateOf maps a branch recommendation to its landing state.
func LandingStateOf(recommendation string) string {
	switch recommendation {
	case UnlandedLanded:
		return LandingLanded
	case UnlandedSuperseded:
		return LandingPending
	case UnlandedNeedsDecision:
		return LandingUnlanded
	default:
		return LandingLive
	}
}

// AgeBandOf places an age in a band. Exactly one day falls in under one week;
// exactly one week falls in older. A negative age (clock skew) counts as
// under one day.
func AgeBandOf(age time.Duration) string {
	switch {
	case age < 24*time.Hour:
		return AgeUnderDay
	case age < 7*24*time.Hour:
		return AgeUnderWeek
	default:
		return AgeOlder
	}
}

// CountUnlandedBranches groups branches into a count per landing state and per
// age band, using each branch's last-commit time against now. A branch with no
// last-commit time is counted as older. Every state and band key is present,
// zero when empty.
func CountUnlandedBranches(branches []UnlandedBranch, now time.Time) UnlandedCounts {
	c := UnlandedCounts{
		ByState: map[string]int{LandingLanded: 0, LandingPending: 0, LandingLive: 0, LandingUnlanded: 0},
		ByAge:   map[string]int{AgeUnderDay: 0, AgeUnderWeek: 0, AgeOlder: 0},
	}
	for _, b := range branches {
		c.Total++
		state := LandingStateOf(b.Recommendation)
		c.ByState[state]++
		band := AgeOlder
		var age time.Duration
		if !b.LastActivity.IsZero() {
			age = now.Sub(b.LastActivity)
			band = AgeBandOf(age)
		}
		c.ByAge[band]++
		if state != LandingPending || b.LastActivity.IsZero() {
			continue
		}
		if c.OldestPending == nil || age > c.OldestPending.Age {
			title := b.Title
			if title == "" {
				title = b.AutomatonID
			}
			c.OldestPending = &UnlandedOldestPending{Branch: b.Branch, Title: title, Age: age, AgeText: formatUnlandedAge(age)}
		}
	}
	return c
}

func formatUnlandedAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	if days > 0 {
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h"
	}
	mins := int((d % time.Hour) / time.Minute)
	return strconv.Itoa(hours) + "h " + strconv.Itoa(mins) + "m"
}
