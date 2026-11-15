package usagecalc

import (
	"testing"
	"time"
)

func TestReadingsForEntry(t *testing.T) {
	at := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	rs := []Reading{{ID: "a", At: at}, {ID: "b", At: at, LLM: "first"}, {ID: "c", At: at, LLM: "second"}}
	ids := func(in []Reading) string {
		s := ""
		for _, r := range in {
			s += r.ID
		}
		return s
	}
	if got := ids(ReadingsForEntry(rs, "first", true)); got != "ab" {
		t.Fatalf("first entry = %q, want ab (unnamed readings are the first entry's)", got)
	}
	if got := ids(ReadingsForEntry(rs, "second", false)); got != "c" {
		t.Fatalf("second entry = %q, want c", got)
	}
	SetDefaultReadingEntry("first")
	defer SetDefaultReadingEntry("")
	if got := ids(DefaultEntryReadings(rs)); got != "ab" {
		t.Fatalf("default entry readings = %q", got)
	}
}
