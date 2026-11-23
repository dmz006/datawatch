package packs

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// fakeTrace is an in-memory TraceReader.
type fakeTrace struct {
	events []trace.Event
	err    error
}

func (f fakeTrace) ReadAll() ([]trace.Event, error) { return f.events, f.err }

var usageT0 = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func TestUsageByPackGroupsRunsCostAndLastUsed(t *testing.T) {
	events := []trace.Event{
		// Pack "alpha" 1.0.0, run s1: two events, one run.
		{PackName: "alpha", PackVersion: "1.0.0", SessionID: "s1", Cost: 0.5, Timestamp: usageT0},
		{PackName: "alpha", PackVersion: "1.0.0", SessionID: "s1", Cost: 0.25, Timestamp: usageT0.Add(time.Hour)},
		// Same pack, run s2 with a task ID only.
		{PackName: "alpha", PackVersion: "1.0.0", TaskID: "t9", Cost: 1, Timestamp: usageT0.Add(2 * time.Hour)},
		// Same pack name, different version: its own row.
		{PackName: "alpha", PackVersion: "2.0.0", AutomatonID: "a1", Cost: 2, Timestamp: usageT0.Add(3 * time.Hour)},
		// Pack "beta" with no version, run identified by automaton ID.
		{PackName: "beta", AutomatonID: "a2", Cost: 0.1, Timestamp: usageT0.Add(30 * time.Minute)},
	}
	got, err := UsageByPack(fakeTrace{events: events})
	if err != nil {
		t.Fatalf("UsageByPack: %v", err)
	}
	want := []PackUsage{
		{Name: "alpha", Version: "1.0.0", Runs: 2, Cost: 1.75, LastUsed: usageT0.Add(2 * time.Hour)},
		{Name: "alpha", Version: "2.0.0", Runs: 1, Cost: 2, LastUsed: usageT0.Add(3 * time.Hour)},
		{Name: "beta", Version: "", Runs: 1, Cost: 0.1, LastUsed: usageT0.Add(30 * time.Minute)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UsageByPack =\n%+v\nwant\n%+v", got, want)
	}
}

func TestUsageByPackSkipsUnattributedEvents(t *testing.T) {
	events := []trace.Event{
		{PackName: "   ", PackVersion: "1.0.0", SessionID: "s1", Cost: 9, Timestamp: usageT0},
		{SessionID: "s2", Cost: 4, Timestamp: usageT0},
	}
	got, err := UsageByPack(fakeTrace{events: events})
	if err != nil {
		t.Fatalf("UsageByPack: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no rows for unattributed events, got %+v", got)
	}
}

func TestUsageByPackTrimsNameAndVersion(t *testing.T) {
	events := []trace.Event{
		{PackName: " gamma ", PackVersion: " 3.1 ", SessionID: "s1", Timestamp: usageT0},
		{PackName: "gamma", PackVersion: "3.1", SessionID: "s1", Timestamp: usageT0},
	}
	got, err := UsageByPack(fakeTrace{events: events})
	if err != nil {
		t.Fatalf("UsageByPack: %v", err)
	}
	if len(got) != 1 || got[0].Name != "gamma" || got[0].Version != "3.1" || got[0].Runs != 1 {
		t.Fatalf("trimmed grouping wrong: %+v", got)
	}
}

func TestUsageByPackRunWithNoIDsAddsCostOnly(t *testing.T) {
	events := []trace.Event{
		{PackName: "delta", PackVersion: "1", Cost: 0.3, Timestamp: usageT0},
	}
	got, err := UsageByPack(fakeTrace{events: events})
	if err != nil {
		t.Fatalf("UsageByPack: %v", err)
	}
	if len(got) != 1 || got[0].Runs != 0 || got[0].Cost != 0.3 {
		t.Fatalf("expected 0 runs and cost 0.3, got %+v", got)
	}
}

func TestUsageByPackEmptyStore(t *testing.T) {
	got, err := UsageByPack(fakeTrace{})
	if err != nil {
		t.Fatalf("UsageByPack: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no rows, got %+v", got)
	}
}

func TestUsageByPackPropagatesReadError(t *testing.T) {
	boom := errors.New("disk gone")
	if _, err := UsageByPack(fakeTrace{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("expected read error to propagate, got %v", err)
	}
}

func TestUsageByPackReadsRealTraceStore(t *testing.T) {
	st, err := trace.NewStore(t.TempDir(), trace.Settings{}, func() time.Time { return usageT0 })
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := st.Append(
		trace.Event{SessionID: "s1", Kind: trace.KindModelRequest, PackName: "epsilon", PackVersion: "0.2.0", Cost: 0.75, Timestamp: usageT0},
		trace.Event{SessionID: "s1", Kind: trace.KindToolCall, Name: "Bash", PackName: "epsilon", PackVersion: "0.2.0", Cost: 0.25, Timestamp: usageT0.Add(time.Minute)},
		trace.Event{SessionID: "s2", Kind: trace.KindToolCall, Name: "Bash", Timestamp: usageT0},
	); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := UsageByPack(st)
	if err != nil {
		t.Fatalf("UsageByPack: %v", err)
	}
	want := []PackUsage{{Name: "epsilon", Version: "0.2.0", Runs: 1, Cost: 1, LastUsed: usageT0.Add(time.Minute)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UsageByPack over store =\n%+v\nwant\n%+v", got, want)
	}
}
