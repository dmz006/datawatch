package inference

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Budget window modes reported by BudgetWindow.Mode.
const (
	BudgetModeRolling = "rolling"
	BudgetModeWeekly  = "weekly"
)

// maxBudgetHours is the longest rolling window a budget may use (30 days).
const maxBudgetHours = 720

// defaultBudgetTime is the anchor clock time used when Weekday is set and
// Time is empty.
const defaultBudgetTime = "00:00"

var budgetWeekdays = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

// BudgetWindow is a dollar ceiling over either a rolling trailing window or
// a weekly window that resets at a fixed weekday and clock time. It is pure
// data and window math; it holds no spend ledger and reads no clock.
type BudgetWindow struct {
	// CeilingUSD is the spend ceiling for one window; must be > 0.
	CeilingUSD float64 `yaml:"ceiling_usd" json:"ceiling_usd"`
	// Hours selects a rolling window of that many trailing hours (1..720).
	Hours int `yaml:"hours,omitempty" json:"hours,omitempty"`
	// Weekday selects a weekly window anchored on this day (monday..sunday,
	// case-insensitive). Mutually exclusive with Hours.
	Weekday string `yaml:"weekday,omitempty" json:"weekday,omitempty"`
	// Time is the 24h HH:MM anchor clock time of a weekly window; empty
	// means 00:00.
	Time string `yaml:"time,omitempty" json:"time,omitempty"`
	// Zone is the IANA zone the weekly anchor is read in; empty means UTC.
	Zone string `yaml:"zone,omitempty" json:"zone,omitempty"`
	// AllocationPct opts in to an estimated share-of-allocation figure. It
	// is informational and never blocks anything.
	AllocationPct bool `yaml:"allocation_pct,omitempty" json:"allocation_pct,omitempty"`
	// PeerWindow names federation peers explicitly opted in to counting
	// toward this ceiling; empty by default.
	PeerWindow []string `yaml:"peer_window,omitempty" json:"peer_window,omitempty"`
}

// Validate returns the first reason the budget is malformed, or nil.
func (b *BudgetWindow) Validate() error {
	if b.CeilingUSD <= 0 {
		return fmt.Errorf("budget: ceiling_usd %v must be > 0", b.CeilingUSD)
	}
	weekday := strings.TrimSpace(b.Weekday)
	switch {
	case b.Hours != 0 && weekday != "":
		return errors.New("budget: set either hours (rolling) or weekday (weekly), not both")
	case b.Hours == 0 && weekday == "":
		return errors.New("budget: one window style required: hours (rolling) or weekday (weekly)")
	}
	if b.Hours != 0 {
		if b.Hours < 1 || b.Hours > maxBudgetHours {
			return fmt.Errorf("budget: hours %d out of range (1..%d)", b.Hours, maxBudgetHours)
		}
		if strings.TrimSpace(b.Time) != "" {
			return errors.New("budget: time applies only to a weekly window")
		}
	} else {
		if _, ok := budgetWeekdays[strings.ToLower(weekday)]; !ok {
			return fmt.Errorf("budget: unknown weekday %q (allowed: monday..sunday)", b.Weekday)
		}
		if _, _, err := b.anchorClock(); err != nil {
			return err
		}
	}
	if b.Zone != "" {
		if _, err := time.LoadLocation(b.Zone); err != nil {
			return fmt.Errorf("budget: unknown zone %q: %w", b.Zone, err)
		}
	}
	seen := make(map[string]struct{}, len(b.PeerWindow))
	for _, p := range b.PeerWindow {
		if strings.TrimSpace(p) == "" {
			return errors.New("budget: peer_window entries must be non-empty")
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("budget: duplicate peer_window entry %q", p)
		}
		seen[p] = struct{}{}
	}
	return nil
}

// Mode returns "rolling" when the budget uses an hours window, else "weekly".
func (b *BudgetWindow) Mode() string {
	if b.Hours > 0 {
		return BudgetModeRolling
	}
	return BudgetModeWeekly
}

// anchorClock parses Time (default 00:00) into hour and minute.
func (b *BudgetWindow) anchorClock() (hour, minute int, err error) {
	s := strings.TrimSpace(b.Time)
	if s == "" {
		s = defaultBudgetTime
	}
	t, perr := time.Parse("15:04", s)
	if perr != nil {
		return 0, 0, fmt.Errorf("budget: time %q must be 24h HH:MM", b.Time)
	}
	return t.Hour(), t.Minute(), nil
}

// location resolves Zone; empty or unloadable falls back to UTC (Validate
// rejects an unloadable zone before a budget is stored).
func (b *BudgetWindow) location() *time.Location {
	if b.Zone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(b.Zone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// WindowAt returns the window containing now. A rolling window is
// [now-Hours, now]. A weekly window starts at the most recent anchor at or
// before now and ends at the next anchor, seven calendar days later computed
// in the zone so a daylight-saving change keeps the clock time. A now exactly
// on the anchor starts a new window.
func (b *BudgetWindow) WindowAt(now time.Time) (start, end time.Time) {
	if b.Mode() == BudgetModeRolling {
		return now.Add(-time.Duration(b.Hours) * time.Hour), now
	}
	loc := b.location()
	local := now.In(loc)
	hour, minute, _ := b.anchorClock()
	target := budgetWeekdays[strings.ToLower(strings.TrimSpace(b.Weekday))]

	back := (int(local.Weekday()) - int(target) + 7) % 7
	y, m, d := local.Date()
	start = time.Date(y, m, d-back, hour, minute, 0, 0, loc)
	if start.After(local) {
		start = time.Date(y, m, d-back-7, hour, minute, 0, 0, loc)
	}
	sy, sm, sd := start.Date()
	end = time.Date(sy, sm, sd+7, hour, minute, 0, 0, loc)
	return start, end
}
