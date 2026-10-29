package usagecalc

import (
	"fmt"
	"time"
)

// daysPerWeek is the number of calendar days between two resets.
const daysPerWeek = 7

// ResetSpec is the weekly reset moment: a weekday, a local clock time and an IANA time zone
// name, for example Sunday 08:00 in America/New_York. The zone's own rules decide the
// instant, so a reset stays at the same local clock time across a daylight saving change.
// A fixed UTC offset is never stored or used.
type ResetSpec struct {
	Weekday time.Weekday `json:"weekday"`
	Clock   string       `json:"clock"` // HH:MM, 24-hour
	Zone    string       `json:"zone"`  // IANA name such as America/New_York

	// ServerZone marks that Zone is the daemon's local zone: the operator
	// left the zone unset and the reset follows the machine's clock, so the
	// advice and the text forms render the zone with the "(server zone)"
	// marker. FellBack marks that even the server's zone could not be
	// resolved and Zone fell back to UTC; the advice carries a note telling
	// the operator to set the zone explicitly then.
	ServerZone bool
	FellBack   bool
}

// resetAnchor is a ResetSpec resolved against one moment: the calendar date of the most
// recent reset at or before that moment. at(k) is the reset clock time k days later.
type resetAnchor struct {
	loc       *time.Location
	hour, min int
	year      int
	month     time.Month
	day       int
	last      time.Time
}

// at returns the reset clock time on the calendar day dayOffset days after the anchor
// date. time.Date applies the zone's rules, so the result follows daylight saving.
func (a resetAnchor) at(dayOffset int) time.Time {
	return time.Date(a.year, a.month, a.day+dayOffset, a.hour, a.min, 0, 0, a.loc)
}

// Validate reports why the spec cannot be used, or nil when it can.
func (r ResetSpec) Validate() error {
	_, _, _, err := r.parse()
	return err
}

// Location returns the spec's time zone.
func (r ResetSpec) Location() (*time.Location, error) {
	loc, _, _, err := r.parse()
	return loc, err
}

// LastReset returns the most recent reset at or before at.
func (r ResetSpec) LastReset(at time.Time) (time.Time, error) {
	a, err := r.anchor(at)
	if err != nil {
		return time.Time{}, err
	}
	return a.last, nil
}

// NextReset returns the first reset strictly after at.
func (r ResetSpec) NextReset(at time.Time) (time.Time, error) {
	a, err := r.anchor(at)
	if err != nil {
		return time.Time{}, err
	}
	return a.at(daysPerWeek), nil
}

func (r ResetSpec) parse() (*time.Location, int, int, error) {
	if r.Weekday < time.Sunday || r.Weekday > time.Saturday {
		return nil, 0, 0, fmt.Errorf("reset weekday %d is not between 0 (Sunday) and 6 (Saturday)", int(r.Weekday))
	}
	hour, min, err := parseClock(r.Clock)
	if err != nil {
		return nil, 0, 0, err
	}
	// "" and "Local" would silently resolve to UTC or the host zone, so they are rejected.
	if r.Zone == "" || r.Zone == "Local" {
		return nil, 0, 0, fmt.Errorf("reset zone %q is not an IANA time zone name", r.Zone)
	}
	loc, err := time.LoadLocation(r.Zone)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("reset zone %q is not a known time zone: %w", r.Zone, err)
	}
	return loc, hour, min, nil
}

// parseClock reads a strict HH:MM 24-hour clock time.
func parseClock(clock string) (int, int, error) {
	bad := fmt.Errorf("reset clock %q is not HH:MM between 00:00 and 23:59", clock)
	if len(clock) != 5 || clock[2] != ':' {
		return 0, 0, bad
	}
	digits := clock[:2] + clock[3:]
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, 0, bad
		}
	}
	hour := int(clock[0]-'0')*10 + int(clock[1]-'0')
	min := int(clock[3]-'0')*10 + int(clock[4]-'0')
	if hour > 23 || min > 59 {
		return 0, 0, bad
	}
	return hour, min, nil
}

func (r ResetSpec) anchor(at time.Time) (resetAnchor, error) {
	loc, hour, min, err := r.parse()
	if err != nil {
		return resetAnchor{}, err
	}
	local := at.In(loc)
	year, month, day := local.Date()
	back := (int(local.Weekday()) - int(r.Weekday) + daysPerWeek) % daysPerWeek
	candidate := time.Date(year, month, day-back, hour, min, 0, 0, loc)
	if candidate.After(at) {
		back += daysPerWeek
		candidate = time.Date(year, month, day-back, hour, min, 0, 0, loc)
	}
	return resetAnchor{
		loc: loc, hour: hour, min: min,
		year: year, month: month, day: day - back,
		last: candidate,
	}, nil
}
