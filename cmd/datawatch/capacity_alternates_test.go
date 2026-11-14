package main

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

// fakeAlternateCaps is a LaneCapSource over a fixed list.
type fakeAlternateCaps []autonomous.LaneCapEntry

func (f fakeAlternateCaps) LaneCapEntries() []autonomous.LaneCapEntry { return f }

// alternatesFixture is a Claude entry, a GPU entry and two more entries, with
// the lane fill backends set to claude-code and pi-prompt.
type alternatesFixture struct {
	entries  []inference.LLM
	caps     map[string]autonomous.LaneCapEntry
	inflight map[string]int
	laneFill autonomous.LaneFillConfig
}

func newAlternatesFixture() *alternatesFixture {
	return &alternatesFixture{
		entries: []inference.LLM{
			{Name: "claude-code", Kind: inference.KindClaudeCode, Model: "sonnet"},
			{Name: "pi-prompt", Kind: "pi-prompt", Model: "qwen3.8:27b"},
			{Name: "gemini", Kind: "gemini", Model: "flash"},
			{Name: "aider", Kind: "aider", Model: "gpt"},
		},
		caps: map[string]autonomous.LaneCapEntry{
			"claude-code": {Backend: "claude-code", Enabled: true, Cap: 3},
			"pi-prompt":   {Backend: "pi-prompt", Enabled: true, Cap: 2},
			"gemini":      {Backend: "gemini", Enabled: true, Cap: 0},
			"aider":       {Backend: "aider", Enabled: true, Cap: 1},
		},
		inflight: map[string]int{},
		laneFill: autonomous.LaneFillConfig{ClaudeBackend: "claude-code", GPUBackend: "pi-prompt"},
	}
}

func (f *alternatesFixture) mark(name string) {
	for i := range f.entries {
		if f.entries[i].Name == name {
			f.entries[i].FallthroughTarget = true
		}
	}
}

func (f *alternatesFixture) resolver() autonomous.CapacityAlternatesFn {
	return capacityAlternatesFor(capacityAlternatesSource{
		LaneFill: func() autonomous.LaneFillConfig { return f.laneFill },
		Entries:  func() []inference.LLM { return append([]inference.LLM(nil), f.entries...) },
		Caps: func() autonomous.LaneCapSource {
			var list fakeAlternateCaps
			for _, e := range f.entries {
				if c, ok := f.caps[e.Name]; ok {
					list = append(list, c)
				}
			}
			return list
		}(),
		Inflight: func(backend string) int { return f.inflight[backend] },
	})
}

func alt(backend, model string) autonomous.CapacityAlternate {
	return autonomous.CapacityAlternate{Backend: backend, Model: model}
}

func TestCapacityAlternates_LaneFillPairBothDirections(t *testing.T) {
	f := newAlternatesFixture()
	got := f.resolver()("claude-code", "opus")
	if want := []autonomous.CapacityAlternate{alt("pi-prompt", "qwen3.8:27b")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude request: got %v, want %v", got, want)
	}
	got = f.resolver()("pi-prompt", "qwen3.8:27b")
	if want := []autonomous.CapacityAlternate{alt("claude-code", "sonnet")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gpu request: got %v, want %v", got, want)
	}
}

func TestCapacityAlternates_GPUModelFromLaneFillConfig(t *testing.T) {
	f := newAlternatesFixture()
	f.laneFill.GPUModel = "qwen3.8:27b"
	f.entries[1].Models = []inference.EnabledModel{{Node: "gpu1", Model: "qwen3.8:27b"}, {Node: "gpu1", Model: "other"}}
	got := f.resolver()("claude-code", "sonnet")
	if want := []autonomous.CapacityAlternate{alt("pi-prompt", "qwen3.8:27b")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCapacityAlternates_PreferenceOrder_LaneFillThenMarked(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("gemini")
	f.mark("aider")
	got := f.resolver()("claude-code", "sonnet")
	want := []autonomous.CapacityAlternate{
		alt("pi-prompt", "qwen3.8:27b"), // lane fill pair first
		alt("aider", "gpt"),             // then marked entries in name order
		alt("gemini", "flash"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCapacityAlternates_RequestedPairSkipped(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("claude-code")
	f.mark("gemini")
	got := f.resolver()("gemini", "flash")
	for _, a := range got {
		if a.Backend == "gemini" && a.Model == "flash" {
			t.Fatalf("requested pair listed as an alternate: %v", got)
		}
	}
	// A task on a marked claude-code entry never lists claude-code on its own
	// requested model either.
	for _, a := range f.resolver()("claude-code", "sonnet") {
		if a.Backend == "claude-code" && a.Model == "sonnet" {
			t.Fatalf("requested pair listed: %v", f.resolver()("claude-code", "sonnet"))
		}
	}
}

func TestCapacityAlternates_NoLaneFillPairForThirdBackend(t *testing.T) {
	f := newAlternatesFixture()
	if got := f.resolver()("gemini", "flash"); len(got) != 0 {
		t.Fatalf("a task on neither lane backend, with nothing marked, got %v", got)
	}
}

func TestCapacityAlternates_DisabledBackendSkipped(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("gemini")
	c := f.caps["pi-prompt"]
	c.Enabled = false
	f.caps["pi-prompt"] = c
	g := f.caps["gemini"]
	g.Enabled = false
	f.caps["gemini"] = g
	f.mark("aider")
	got := f.resolver()("claude-code", "sonnet")
	if want := []autonomous.CapacityAlternate{alt("aider", "gpt")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCapacityAlternates_OverCapBackendSkipped(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("aider")
	f.inflight["pi-prompt"] = 2 // at its cap of 2
	f.inflight["aider"] = 1     // at its cap of 1
	if got := f.resolver()("claude-code", "sonnet"); len(got) != 0 {
		t.Fatalf("over-cap backends listed: %v", got)
	}
	f.inflight["pi-prompt"] = 1 // room again
	got := f.resolver()("claude-code", "sonnet")
	if want := []autonomous.CapacityAlternate{alt("pi-prompt", "qwen3.8:27b")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCapacityAlternates_UnlimitedCapNeverOver(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("gemini")
	f.inflight["gemini"] = 50 // cap 0 = unlimited
	got := f.resolver()("claude-code", "sonnet")
	if !reflect.DeepEqual(got[len(got)-1], alt("gemini", "flash")) {
		t.Fatalf("unlimited entry dropped: %v", got)
	}
}

func TestCapacityAlternates_ModelNotServedSkipped(t *testing.T) {
	f := newAlternatesFixture()
	// The GPU lanes are configured for a model the GPU entry does not list.
	f.laneFill.GPUModel = "missing-model"
	f.entries[1].Models = []inference.EnabledModel{{Node: "gpu1", Model: "qwen3.8:27b"}}
	if got := f.resolver()("claude-code", "sonnet"); len(got) != 0 {
		t.Fatalf("alternate on a model the entry does not serve listed: %v", got)
	}
}

func TestCapacityAlternates_MarkedEntryUsesRequestedModelWhenServed(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("gemini")
	f.entries[2].Models = []inference.EnabledModel{{Model: "flash"}, {Model: "pro"}}
	got := f.resolver()("claude-code", "pro")
	if want := alt("gemini", "pro"); got[len(got)-1] != want {
		t.Fatalf("got %v, want last %v", got, want)
	}
	got = f.resolver()("claude-code", "unlisted")
	if want := alt("gemini", "flash"); got[len(got)-1] != want {
		t.Fatalf("an unserved requested model falls back to the entry default: got %v, want last %v", got, want)
	}
}

func TestCapacityAlternates_MarkedEntryIncluded_UnmarkedExcluded(t *testing.T) {
	f := newAlternatesFixture()
	f.mark("gemini")
	got := f.resolver()("claude-code", "sonnet")
	var sawGemini, sawAider bool
	for _, a := range got {
		sawGemini = sawGemini || a.Backend == "gemini"
		sawAider = sawAider || a.Backend == "aider"
	}
	if !sawGemini {
		t.Fatalf("marked entry missing: %v", got)
	}
	if sawAider {
		t.Fatalf("unmarked entry listed: %v", got)
	}
}

func TestCapacityAlternates_NoEntriesNoAlternates(t *testing.T) {
	fn := capacityAlternatesFor(capacityAlternatesSource{})
	if got := fn("claude-code", "sonnet"); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestCapacityAlternates_LiveMarkApplies(t *testing.T) {
	f := newAlternatesFixture()
	fn := f.resolver()
	if got := fn("gemini", "flash"); len(got) != 0 {
		t.Fatalf("before the mark: %v", got)
	}
	f.mark("aider")
	if got := fn("gemini", "flash"); len(got) != 1 || got[0].Backend != "aider" {
		t.Fatalf("after the mark: %v", got)
	}
}
