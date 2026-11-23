package costreport

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/trace"
)

func TestPackBreakdownEmpty(t *testing.T) {
	if rows := PackBreakdown(nil); len(rows) != 0 {
		t.Fatalf("want no rows for no events, got %+v", rows)
	}
}

func TestPackBreakdownGroupsByNameAndVersion(t *testing.T) {
	events := []trace.Event{
		{PackName: "cost-pack", PackVersion: "1.0.0", TokensIn: 100, TokensOut: 200, Cost: 0.5},
		{PackName: "cost-pack", PackVersion: "1.0.0", TokensIn: 10, TokensOut: 20, Cost: 0.25},
		{PackName: "cost-pack", PackVersion: "1.1.0", TokensIn: 1, TokensOut: 2, Cost: 0.125},
		{PackName: "alpha", PackVersion: "2.0.0", TokensIn: 7, TokensOut: 8, Cost: 1},
	}
	got := PackBreakdown(events)
	want := []PackRow{
		{Name: "alpha", Version: "2.0.0", Events: 1, TokensIn: 7, TokensOut: 8, Cost: 1},
		{Name: "cost-pack", Version: "1.0.0", Events: 2, TokensIn: 110, TokensOut: 220, Cost: 0.75},
		{Name: "cost-pack", Version: "1.1.0", Events: 1, TokensIn: 1, TokensOut: 2, Cost: 0.125},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows mismatch\nwant %+v\ngot  %+v", want, got)
	}
}

func TestPackBreakdownUnattributedGoesLast(t *testing.T) {
	events := []trace.Event{
		{TokensIn: 5, TokensOut: 6, Cost: 0.1},                             // no pack at all
		{PackName: "   ", PackVersion: "9.9.9", TokensIn: 1, TokensOut: 1}, // blank name, version ignored
		{PackName: "zeta", PackVersion: "0.1.0", TokensIn: 3, TokensOut: 4},
	}
	got := PackBreakdown(events)
	want := []PackRow{
		{Name: "zeta", Version: "0.1.0", Events: 1, TokensIn: 3, TokensOut: 4},
		{Name: PackUnattributedName, Version: "", Events: 2, TokensIn: 6, TokensOut: 7, Cost: 0.1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows mismatch\nwant %+v\ngot  %+v", want, got)
	}
}

func TestPackBreakdownUnattributedOnly(t *testing.T) {
	got := PackBreakdown([]trace.Event{{Kind: trace.KindModelRequest, TokensIn: 2}})
	if len(got) != 1 || got[0].Name != PackUnattributedName || got[0].Events != 1 || got[0].TokensIn != 2 {
		t.Fatalf("want one unattributed row, got %+v", got)
	}
}

func TestPackBreakdownTrimsNameAndVersion(t *testing.T) {
	got := PackBreakdown([]trace.Event{
		{PackName: " cost-pack ", PackVersion: " 1.0.0 "},
		{PackName: "cost-pack", PackVersion: "1.0.0"},
	})
	if len(got) != 1 || got[0].Name != "cost-pack" || got[0].Version != "1.0.0" || got[0].Events != 2 {
		t.Fatalf("want trimmed keys to merge into one row, got %+v", got)
	}
}

func TestPackBreakdownDeterministicOrder(t *testing.T) {
	events := []trace.Event{
		{PackName: "b", PackVersion: "1"},
		{PackName: "a", PackVersion: "2"},
		{PackName: "a", PackVersion: "1"},
		{},
	}
	first := PackBreakdown(events)
	for i := 0; i < 20; i++ {
		if again := PackBreakdown(events); !reflect.DeepEqual(first, again) {
			t.Fatalf("order changed between calls:\n%+v\n%+v", first, again)
		}
	}
	names := make([]string, 0, len(first))
	for _, r := range first {
		names = append(names, r.Name+"@"+r.Version)
	}
	want := []string{"a@1", "a@2", "b@1", PackUnattributedName + "@"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("order mismatch: want %v, got %v", want, names)
	}
}
