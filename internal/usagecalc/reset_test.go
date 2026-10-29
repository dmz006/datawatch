package usagecalc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestResetServerZoneAdvice pins the server-zone markers: when the operator
// left the zone unset, the advice reports the effective zone with
// server_zone true, carries no fallen-back note, and both text forms show
// the "(server zone)" marker at the reset time.
func TestResetServerZoneAdvice(t *testing.T) {
	r := sundayEight()
	r.ServerZone = true
	in := Input{Readings: []Reading{percentReading(at(t, 2026, 10, 23, 22, 8), 72)}, Reset: r, Lanes: 1}
	a := Advise(in)
	if a.Needs != "" {
		t.Fatalf("Needs = %q, want none", a.Needs)
	}
	if a.Zone != "America/New_York" {
		t.Errorf("zone = %q, want America/New_York", a.Zone)
	}
	if !a.ServerZone {
		t.Errorf("server_zone = false, want true")
	}
	var m map[string]interface{}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["zone"] != "America/New_York" {
		t.Errorf("json zone = %v, want America/New_York", m["zone"])
	}
	if m["server_zone"] != true {
		t.Errorf("json server_zone = %v, want true", m["server_zone"])
	}
	for name, form := range map[string]string{"cli": FormatCLI(AdviceResult{Advice: a}), "comm": FormatComm(AdviceResult{Advice: a})} {
		want := "(server zone)"
		if name == "cli" {
			want = "Sun 08:00 EDT (server zone)" // no wrap on the CLI line
		}
		if !strings.Contains(form, want) {
			t.Errorf("%s form lacks the server-zone marker:\n%s", name, form)
		}
	}
	for _, n := range a.Advice.Notes {
		if strings.Contains(n, "fell back to UTC") {
			t.Errorf("unexpected fallback note: %q", n)
		}
	}
}

// TestResetZoneFellBackNote pins the last-resort path: the daemon's local
// zone could not be resolved, the reset ran in UTC and the advice names the
// key to set, on every line of every rendering.
func TestResetZoneFellBackNote(t *testing.T) {
	r := ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "UTC", ServerZone: true, FellBack: true}
	in := Input{Readings: []Reading{percentReading(time.Date(2026, 10, 23, 22, 8, 0, 0, time.UTC), 72)}, Reset: r, Lanes: 1}
	a := Advise(in)
	var found bool
	for _, n := range a.Advice.Notes {
		if strings.Contains(n, "fell back to UTC") && strings.Contains(n, "usage.calculator.reset_zone") {
			found = true
		}
	}
	if !found {
		t.Errorf("no fallback note among %v", a.Advice.Notes)
	}
	if a.Zone != "UTC" || !a.ServerZone {
		t.Errorf("zone/server_zone = %q/%v, want UTC/true", a.Zone, a.ServerZone)
	}
	b, _ := json.Marshal(a) //nolint:errcheck
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["zone"] != "UTC" || m["server_zone"] != true {
		t.Errorf("json zone/server_zone = %v/%v, want UTC/true", m["zone"], m["server_zone"])
	}
	for _, form := range []string{FormatCLI(AdviceResult{Advice: a}), FormatComm(AdviceResult{Advice: a})} {
		if !strings.Contains(form, "fell back to UTC") {
			t.Errorf("form lacks the fallback note:\n%s", form)
		}
	}
}

func TestResetSpecValidate(t *testing.T) {
	ok := []ResetSpec{
		{Weekday: time.Sunday, Clock: "08:00", Zone: "America/New_York"},
		{Weekday: time.Saturday, Clock: "23:59", Zone: "UTC"},
		{Weekday: time.Wednesday, Clock: "00:00", Zone: "Asia/Kolkata"},
	}
	for _, r := range ok {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := map[string]ResetSpec{
		"weekday low":      {Weekday: time.Weekday(-1), Clock: "08:00", Zone: "UTC"},
		"weekday high":     {Weekday: time.Weekday(7), Clock: "08:00", Zone: "UTC"},
		"clock empty":      {Weekday: time.Sunday, Clock: "", Zone: "UTC"},
		"clock no colon":   {Weekday: time.Sunday, Clock: "0800", Zone: "UTC"},
		"clock short":      {Weekday: time.Sunday, Clock: "8:00", Zone: "UTC"},
		"clock long":       {Weekday: time.Sunday, Clock: "08:000", Zone: "UTC"},
		"clock letters":    {Weekday: time.Sunday, Clock: "ab:cd", Zone: "UTC"},
		"clock plus sign":  {Weekday: time.Sunday, Clock: "+8:00", Zone: "UTC"},
		"clock hour 24":    {Weekday: time.Sunday, Clock: "24:00", Zone: "UTC"},
		"clock minute 60":  {Weekday: time.Sunday, Clock: "08:60", Zone: "UTC"},
		"zone empty":       {Weekday: time.Sunday, Clock: "08:00", Zone: ""},
		"zone local":       {Weekday: time.Sunday, Clock: "08:00", Zone: "Local"},
		"zone unknown":     {Weekday: time.Sunday, Clock: "08:00", Zone: "Mars/Olympus_Mons"},
		"zone fixed style": {Weekday: time.Sunday, Clock: "08:00", Zone: "UTC-4 hours"},
	}
	for name, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: %+v validated", name, r)
		}
		if _, err := r.LastReset(time.Now()); err == nil {
			t.Errorf("%s: LastReset accepted the spec", name)
		}
		if _, err := r.NextReset(time.Now()); err == nil {
			t.Errorf("%s: NextReset accepted the spec", name)
		}
		if _, err := r.Location(); err == nil {
			t.Errorf("%s: Location accepted the spec", name)
		}
	}
	if err := bad["clock hour 24"].Validate(); !strings.Contains(err.Error(), "24:00") {
		t.Errorf("error does not name the clock: %v", err)
	}
}

func TestResetLastAndNextReset(t *testing.T) {
	r := sundayEight()
	cases := []struct {
		name       string
		when       time.Time
		last, next time.Time
	}{
		{"mid week", at(t, 2026, 10, 23, 22, 8), at(t, 2026, 10, 18, 8, 0), at(t, 2026, 10, 25, 8, 0)},
		{"just before the reset", at(t, 2026, 10, 25, 7, 59), at(t, 2026, 10, 18, 8, 0), at(t, 2026, 10, 25, 8, 0)},
		{"at the reset", at(t, 2026, 10, 25, 8, 0), at(t, 2026, 10, 25, 8, 0), at(t, 2026, 11, 1, 8, 0)},
		{"reset day before the clock", at(t, 2026, 10, 25, 3, 0), at(t, 2026, 10, 18, 8, 0), at(t, 2026, 10, 25, 8, 0)},
		{"reset day after the clock", at(t, 2026, 10, 25, 9, 0), at(t, 2026, 10, 25, 8, 0), at(t, 2026, 11, 1, 8, 0)},
		{"given in another zone", at(t, 2026, 10, 23, 22, 8).UTC(), at(t, 2026, 10, 18, 8, 0), at(t, 2026, 10, 25, 8, 0)},
	}
	for _, c := range cases {
		last, err := r.LastReset(c.when)
		if err != nil || !last.Equal(c.last) {
			t.Errorf("%s: LastReset = %v (%v), want %v", c.name, last, err, c.last)
		}
		next, err := r.NextReset(c.when)
		if err != nil || !next.Equal(c.next) {
			t.Errorf("%s: NextReset = %v (%v), want %v", c.name, next, err, c.next)
		}
	}
	loc, err := r.Location()
	if err != nil || loc.String() != "America/New_York" {
		t.Errorf("Location = %v, %v", loc, err)
	}
}

func TestResetDaylightSavingChange(t *testing.T) {
	r := sundayEight()
	loc := newYork(t)

	// The week ending Sunday 2026-11-01 gains an hour: 169 hours.
	fallStart := at(t, 2026, 10, 25, 8, 0)
	fallEnd, err := r.NextReset(at(t, 2026, 10, 30, 12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := fallEnd.Sub(fallStart); got != 169*time.Hour {
		t.Errorf("fall-back week = %v, want 169h", got)
	}
	// A spring-forward week loses an hour: 167 hours (the clocks change on 2026-03-08).
	springStart := at(t, 2026, 3, 1, 8, 0)
	springEnd, _ := r.NextReset(at(t, 2026, 3, 4, 12, 0))
	if got := springEnd.Sub(springStart); got != 167*time.Hour {
		t.Errorf("spring-forward week = %v, want 167h", got)
	}
	// The reset stays Sunday 08:00 local on both sides of the change, and the offset moves.
	for _, reset := range []time.Time{fallStart, fallEnd, springStart, springEnd} {
		if l := reset.In(loc); l.Weekday() != time.Sunday || l.Hour() != 8 || l.Minute() != 0 {
			t.Errorf("reset %v is not Sunday 08:00 local", l)
		}
	}
	if _, off := fallStart.In(loc).Zone(); off != -4*3600 {
		t.Errorf("offset before the change = %d", off)
	}
	if _, off := fallEnd.In(loc).Zone(); off != -5*3600 {
		t.Errorf("offset after the change = %d", off)
	}

	// The pace line reaches 100 at the next reset on a 169-hour and a 167-hour week.
	for _, week := range []struct {
		start time.Time
		hours float64
	}{{fallStart, 169}, {springStart, 167}} {
		readAt := week.start.Add(time.Hour * 100)
		a := Advise(Input{Readings: []Reading{percentReading(readAt, 50)}, Reset: r, Lanes: 1})
		nextReset, _ := r.NextReset(readAt)
		hoursToEnd := nextReset.Sub(readAt).Hours()
		near(t, "pace line mid week", a.PaceLine, 100.0/week.hours*100, 1e-9)
		near(t, "allowance per 24h", a.AllowancePer24h, 100/(week.hours/24), 1e-9)
		// One more reading at the very reset moment: the pace line is 100 there.
		end := Advise(Input{Readings: []Reading{percentReading(readAt, 50), percentReading(nextReset.Add(-time.Nanosecond), 60)}, Reset: r, Lanes: 1})
		near(t, "pace line at the reset", end.PaceLine, 100, 1e-6)
		if hoursToEnd != week.hours-100 {
			t.Errorf("hours to the reset = %v, want %v", hoursToEnd, week.hours-100)
		}
	}

	// A week without a change keeps the plain 100/7 allowance.
	plain := Advise(workedExample(t))
	near(t, "plain allowance", plain.AllowancePer24h, 100.0/7, 1e-9)

	// The run-out time is correct across the change: a reading late on Saturday 2026-10-31
	// at 20:00 EDT burning 60 points a day runs out 8 real hours later, which is 03:00 EST.
	in := Input{
		Readings: []Reading{
			percentReading(at(t, 2026, 10, 31, 12, 0), 60),
			percentReading(at(t, 2026, 10, 31, 20, 0), 80),
		},
		Reset: r, Lanes: 3, RecentLanes: intp(3),
	}
	a := Advise(in)
	if a.RunOutAtRecent == nil {
		t.Fatal("no run out across the change")
	}
	if got := a.RunOutAtRecent.Sub(in.Readings[1].At); got != 8*time.Hour {
		t.Errorf("run out is %v after the reading, want 8h of real time", got)
	}
	if got := a.RunOutAtRecent.In(loc).Format("Mon 15:04 MST"); got != "Sun 03:00 EST" {
		t.Errorf("run out = %s, want Sun 03:00 EST", got)
	}
	// The reset itself is 13 real hours after the reading (the extra hour is counted).
	near(t, "days left", a.DaysLeft, 13.0/24, 1e-9)
	near(t, "hours early", a.HoursEarlyRecent, 5, 1e-9)
	if a.NextResetAt == nil || a.NextResetAt.In(loc).Format("Mon 15:04 MST") != "Sun 08:00 EST" {
		t.Errorf("next reset = %v", a.NextResetAt)
	}
	if a.LastResetAt == nil || a.LastResetAt.In(loc).Format("Mon 15:04 MST") != "Sun 08:00 EDT" {
		t.Errorf("last reset = %v", a.LastResetAt)
	}
}

func TestResetOtherZones(t *testing.T) {
	// A zone without daylight saving and a zone east of UTC.
	for _, zone := range []string{"UTC", "Asia/Tokyo", "Europe/London"} {
		r := ResetSpec{Weekday: time.Friday, Clock: "17:30", Zone: zone}
		loc, _ := r.Location()
		when := time.Date(2026, 6, 10, 12, 0, 0, 0, loc) // a Wednesday
		last, _ := r.LastReset(when)
		next, _ := r.NextReset(when)
		if l := last.In(loc); l.Weekday() != time.Friday || l.Hour() != 17 || l.Minute() != 30 || !l.Before(when) {
			t.Errorf("%s: last reset %v", zone, l)
		}
		if !next.After(when) || next.Sub(last) != 7*24*time.Hour {
			t.Errorf("%s: next reset %v", zone, next)
		}
	}
}
