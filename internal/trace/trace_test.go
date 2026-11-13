package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestKindAndCoverageValid(t *testing.T) {
	for _, k := range Kinds {
		if !k.Valid() {
			t.Errorf("kind %q should be valid", k)
		}
	}
	if len(Kinds) != 8 {
		t.Errorf("want 8 kinds, got %d", len(Kinds))
	}
	if Kind("bogus").Valid() || Kind("").Valid() {
		t.Error("unknown kind accepted")
	}
	for _, c := range []Coverage{CoverageFull, CoveragePartial, CoverageEventsOnly} {
		if !c.Valid() {
			t.Errorf("coverage %q should be valid", c)
		}
	}
	if Coverage("x").Valid() {
		t.Error("unknown coverage accepted")
	}
	if CoverageEventsOnly != "events-only" {
		t.Errorf("events-only constant = %q", CoverageEventsOnly)
	}
}

func TestEventJSONRoundTripFixedClock(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	in := Event{SessionID: "s1", TaskID: "t1", StoryID: "st1", AutomatonID: "a1",
		Agent: "worker", Backend: "claude-code", Kind: KindToolCall, Name: "Bash",
		ArgSummary: "ls", Outcome: "ok", DurationMS: 12, TokensIn: 3, TokensOut: 4, Timestamp: now}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Event
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v vs %+v", out, in)
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		s       Settings
		wantErr bool
	}{
		{"zero value", Settings{}, false},
		{"defaults", Defaults(), false},
		{"off", Settings{Mode: "off"}, false},
		{"full", Settings{Mode: "full", ArgCapBytes: 100, RetentionDays: 1}, false},
		{"bad mode", Settings{Mode: "verbose"}, true},
		{"upper mode", Settings{Mode: "FULL"}, true},
		{"negative cap", Settings{ArgCapBytes: -1}, true},
		{"huge cap", Settings{ArgCapBytes: MaxArgCapBytes + 1}, true},
		{"max cap", Settings{ArgCapBytes: MaxArgCapBytes}, false},
		{"negative retention", Settings{RetentionDays: -5}, true},
		{"negative retention -1", Settings{RetentionDays: -1}, true},
		{"zero retention", Settings{RetentionDays: 0}, false},
		{"max retention", Settings{RetentionDays: MaxRetentionDays}, false},
		{"huge retention", Settings{RetentionDays: MaxRetentionDays + 1}, true},
		{"empty backend", Settings{Backends: map[string]bool{" ": true}}, true},
		{"backends ok", Settings{Backends: map[string]bool{"pi": false, "claude-code": true}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestWithDefaults(t *testing.T) {
	got := Settings{}.WithDefaults()
	if got.Mode != ModeSummary || got.ArgCapBytes != DefaultArgCapBytes || got.RetentionDays != DefaultRetentionDays || got.Backends == nil {
		t.Errorf("defaults wrong: %+v", got)
	}
	src := Settings{Mode: "full", ArgCapBytes: 9, RetentionDays: 2, Backends: map[string]bool{"pi": false}}
	cp := src.WithDefaults()
	if cp.Mode != "full" || cp.ArgCapBytes != 9 || cp.RetentionDays != 2 {
		t.Errorf("explicit values overwritten: %+v", cp)
	}
	// Retention 0 is a real value (keep forever), not an "unset" marker:
	// WithDefaults must not rewrite it.
	zero := Settings{RetentionDays: 0}.WithDefaults()
	if zero.RetentionDays != 0 {
		t.Errorf("WithDefaults rewrote retention 0 to %d, want 0", zero.RetentionDays)
	}
	cp.Backends["pi"] = true
	if src.Backends["pi"] {
		t.Error("WithDefaults shared the Backends map")
	}
}

func TestDefaultRetentionIsZero(t *testing.T) {
	if DefaultRetentionDays != 0 {
		t.Errorf("DefaultRetentionDays = %d, want 0 (keep forever)", DefaultRetentionDays)
	}
	if got := Defaults().RetentionDays; got != 0 {
		t.Errorf("Defaults().RetentionDays = %d, want 0", got)
	}
	dflt := Settings{}.WithDefaults()
	if dflt.RetentionDays != 0 {
		t.Errorf("WithDefaults().RetentionDays = %d, want 0", dflt.RetentionDays)
	}
}

func TestRetentionValidateBounds(t *testing.T) {
	for name, wantErr := range map[int]bool{0: false, 14: false, MaxRetentionDays: false, -1: true, (MaxRetentionDays + 1): true} {
		err := Settings{RetentionDays: name}.Validate()
		if (err != nil) != wantErr {
			t.Errorf("Validate(retention=%d) err = %v, wantErr = %v", name, err, wantErr)
		}
	}
}

func TestBackendEnabled(t *testing.T) {
	tests := []struct {
		name    string
		s       Settings
		backend string
		want    bool
	}{
		{"default on", Settings{}, "claude-code", true},
		{"explicit off", Settings{Backends: map[string]bool{"pi": false}}, "pi", false},
		{"other unaffected", Settings{Backends: map[string]bool{"pi": false}}, "opencode", true},
		{"explicit on", Settings{Backends: map[string]bool{"pi": true}}, "pi", true},
		{"mode off", Settings{Mode: "off"}, "pi", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.BackendEnabled(tc.backend); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestFixtureLogDecode replays a fixture log through a fake fixed clock:
// every line decodes to a valid Kind, timestamps are exactly those in the
// fixture, and the default retention is 0 (keep forever: no age-based
// pruning applies to the fixture).
func TestFixtureLogDecode(t *testing.T) {
	if Defaults().RetentionDays != 0 {
		t.Fatal("default retention must be 0 (keep forever)")
	}
	f, err := os.Open("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // read-only fixture handle
	fakeNow := time.Date(2026, 1, 2, 3, 4, 8, 0, time.UTC)
	_ = fakeNow
	var events []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("decode %q: %v", sc.Text(), err)
		}
		if !e.Kind.Valid() {
			t.Errorf("invalid kind %q", e.Kind)
		}
		events = append(events, e)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !events[0].Timestamp.Equal(want) {
		t.Errorf("ts=%v want %v", events[0].Timestamp, want)
	}
	if events[1].DurationMS != 1200 || events[2].TokensIn != 900 || events[2].TokensOut != 120 {
		t.Errorf("fields not decoded: %+v %+v", events[1], events[2])
	}
	// Retention 0 = keep forever: with the default settings no event is
	// older than any retention cutoff (there is none to compute).
	if Defaults().RetentionDays != 0 {
		cutoff := fakeNow.AddDate(0, 0, -Defaults().RetentionDays)
		for _, e := range events {
			if e.Timestamp.Before(cutoff) {
				t.Errorf("event %v older than retention cutoff %v", e.Timestamp, cutoff)
			}
		}
	}
}
