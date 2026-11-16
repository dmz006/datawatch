package server

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// entryAdmission is a fake admission seam with one cap, route and hold per backend, and
// a model-limited hold.
type entryAdmission struct {
	mu     sync.Mutex
	caps   map[string]int
	routes map[string]string
	held   map[string]bool
	models map[string][]string
	// holdRung is the rung name the hold call recorded per backend, so a test can
	// assert the guard passed the rung in force (entry-generic, not assumed).
	holdRung map[string]string
}

func newEntryAdmission() *entryAdmission {
	return &entryAdmission{caps: map[string]int{}, routes: map[string]string{}, held: map[string]bool{}, models: map[string][]string{}, holdRung: map[string]string{}}
}
func (a *entryAdmission) InflightCap(b string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v, ok := a.caps[b]; ok {
		return v
	}
	return 3
}
func (a *entryAdmission) SetInflight(b string, n int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.caps[b] = n
	return nil
}
func (a *entryAdmission) RouteBackend(from, to string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if on {
		a.routes[from] = to
	} else {
		delete(a.routes, from)
	}
	return nil
}
func (a *entryAdmission) HoldNewWork(b, rung string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held[b] = on
	if on && rung != "" {
		a.holdRung[b] = rung
	} else if !on {
		delete(a.holdRung, b)
	}
	delete(a.models, b)
	return nil
}
func (a *entryAdmission) HoldModels(b string, models []string, rung string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held[b] = on
	if on && rung != "" {
		a.holdRung[b] = rung
	} else if !on {
		delete(a.holdRung, b)
	}
	if on {
		a.models[b] = append([]string(nil), models...)
	} else {
		delete(a.models, b)
	}
	return nil
}

// The entry names below are arbitrary on purpose: the guard must not care what backend
// an entry is.
const (
	entryFlatOver  = "paid-alpha"
	entryFlatUnder = "paid-beta"
	entryNoSource  = "free-gamma"
	entryMetered   = "meter-delta"
	entryMetered2  = "meter-epsilon"
)

var entriesNow = time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC) // Friday; reset Saturday 09:00

func entriesRig(t *testing.T, entries []config.BurnGuardEntryConfig, readings []usagecalc.Reading,
	rates map[string]burnguard.ModelRate, tokens map[string][]burnguard.Bucket) (*BurnGuard, *entryAdmission) {
	t.Helper()
	cfg := config.DefaultBurnGuard()
	cfg.Entries = entries
	cfg.MaxProjectionGapPoints = 0
	cfg.RefitOnReading = false
	adm := newEntryAdmission()
	in := BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return cfg },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			return readings, 0, usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}, nil
		},
		Tokens: func(_ context.Context, entry string, _ bool, _ time.Time) ([]burnguard.Bucket, time.Time, error) {
			return tokens[entry], entriesNow, nil
		},
		Rate: func(e config.BurnGuardEntryConfig) (burnguard.ModelRate, bool) {
			r, ok := rates[e.LLM+"/"+e.Model]
			return r, ok
		},
		Now: func() time.Time { return entriesNow },
	}
	g, err := NewBurnGuard(in, adm, &failingSessions{}, &fakeNotifier{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g, adm
}

func pctReading(entry string, ago time.Duration, v float64) usagecalc.Reading {
	return usagecalc.Reading{LLM: entry, At: entriesNow.Add(-ago), Percent: &v}
}

func spendReading(entry string, ago time.Duration, v float64) usagecalc.Reading {
	return usagecalc.Reading{LLM: entry, At: entriesNow.Add(-ago), SpendUSD: &v}
}

func entryStatus(t *testing.T, g *BurnGuard, name string) BurnGuardEntryStatus {
	t.Helper()
	for _, e := range g.Status().Entries {
		if e.LLM == name {
			return e
		}
	}
	t.Fatalf("no status for entry %q in %+v", name, g.Status().Entries)
	return BurnGuardEntryStatus{}
}

// outputTokens is one bucket of output tokens an hour before now.
func outputTokens(n int64) []burnguard.Bucket {
	return []burnguard.Bucket{{Hour: entriesNow.Add(-time.Hour), Worker: burnguard.Tokens{Output: n}}}
}

// TestBurnGuardHoldsOneEntryWhileTheOtherRuns: two paid entries with different models and
// rates, each on its own readings. The one over its pace line is held (on its held
// models); the one under it keeps running, and an entry with no usage source is never
// held even though its readings are far over.
func TestBurnGuardHoldsOneEntryWhileTheOtherRuns(t *testing.T) {
	hold := []string{burnguard.RungHoldClaude}
	entries := []config.BurnGuardEntryConfig{
		{LLM: entryFlatOver, UsageSource: "readings", Plan: "flat", Model: "model-x", Ladder: hold, HeldModels: []string{"model-x"}},
		{LLM: entryFlatUnder, UsageSource: "readings", Plan: "flat", Model: "model-y", Ladder: hold},
		{LLM: entryNoSource, UsageSource: "none", Plan: "flat", Ladder: hold},
	}
	readings := []usagecalc.Reading{
		pctReading(entryFlatOver, 30*time.Hour, 80), pctReading(entryFlatOver, 2*time.Hour, 97),
		pctReading(entryFlatUnder, 30*time.Hour, 20), pctReading(entryFlatUnder, 2*time.Hour, 22),
		pctReading(entryNoSource, 30*time.Hour, 90), pctReading(entryNoSource, 2*time.Hour, 99),
	}
	rates := map[string]burnguard.ModelRate{
		entryFlatOver + "/model-x":  {OutPerK: 0.015},
		entryFlatUnder + "/model-y": {OutPerK: 0.075},
	}
	g, adm := entriesRig(t, entries, readings, rates, nil)
	g.Tick(context.Background())

	if !adm.held[entryFlatOver] {
		t.Fatalf("%s is over its line and was not held: %+v", entryFlatOver, entryStatus(t, g, entryFlatOver))
	}
	if got := adm.models[entryFlatOver]; len(got) != 1 || got[0] != "model-x" {
		t.Fatalf("%s hold models = %v, want [model-x]", entryFlatOver, got)
	}
	if adm.held[entryFlatUnder] {
		t.Fatalf("%s is under its line and was held: %+v", entryFlatUnder, entryStatus(t, g, entryFlatUnder))
	}
	if adm.held[entryNoSource] {
		t.Fatalf("%s has no usage source and was held", entryNoSource)
	}
	// The legacy first-version rung name (hold_claude) in the entry ladders above
	// is accepted and reported under its entry-generic name (hold_entry).
	if st := entryStatus(t, g, entryNoSource); st.Enabled || st.State != "off" || st.HasUsageSource {
		t.Fatalf("no-source entry status = %+v, want off", st)
	}
	if st := entryStatus(t, g, entryFlatOver); st.State != "guarding" || st.Rung != burnguard.RungHoldEntry {
		t.Fatalf("over entry status = %+v", st)
	}
	if st := entryStatus(t, g, entryFlatUnder); st.State != "idle" || st.Level != 0 {
		t.Fatalf("under entry status = %+v", st)
	}
	// The top-level status repeats the first entry's.
	if s := g.Status(); s.State != "guarding" || s.Rung != burnguard.RungHoldEntry {
		t.Fatalf("top-level status = %+v", s)
	}
	// No backend name is hard-coded anywhere: nothing was done to any other backend.
	for b := range adm.held {
		if b != entryFlatOver && b != entryFlatUnder && b != entryNoSource {
			t.Fatalf("hold reached unexpected backend %q", b)
		}
	}
}

// TestBurnGuardEntryWithoutSourceNeverHeldAndReleases: an entry that loses its usage
// source releases whatever it had in force and is never held again.
func TestBurnGuardEntryWithoutSourceNeverHeldAndReleases(t *testing.T) {
	hold := []string{burnguard.RungHoldClaude}
	entries := []config.BurnGuardEntryConfig{
		{LLM: entryFlatOver, UsageSource: "readings", Plan: "flat", Ladder: hold},
	}
	readings := []usagecalc.Reading{pctReading(entryFlatOver, 30*time.Hour, 80), pctReading(entryFlatOver, 2*time.Hour, 97)}
	g, adm := entriesRig(t, entries, readings, nil, nil)
	g.Tick(context.Background())
	if !adm.held[entryFlatOver] {
		t.Fatal("entry with a source and over the line was not held")
	}
	entries[0].UsageSource = ""
	for i := 0; i < 3; i++ {
		g.Tick(context.Background())
		if adm.held[entryFlatOver] {
			t.Fatalf("tick %d: entry without a usage source is held", i)
		}
	}
}

// TestBurnGuardMeteredEntryProjectsFromSpendAtItsModelRate: two metered entries on the
// same spend readings and the same tokens, but different model rates. Each projects
// from its spend in dollars, its tokens priced at its own model's rate; the dearer one
// runs over its dollar target and is held, the cheaper one is not.
func TestBurnGuardMeteredEntryProjectsFromSpendAtItsModelRate(t *testing.T) {
	hold := []string{burnguard.RungHoldClaude}
	entries := []config.BurnGuardEntryConfig{
		{LLM: entryMetered, UsageSource: "readings", Plan: "metered", PeriodTarget: 200, Model: "cheap", Ladder: hold},
		{LLM: entryMetered2, UsageSource: "readings", Plan: "metered", PeriodTarget: 200, Model: "dear", Ladder: hold},
	}
	readings := []usagecalc.Reading{
		spendReading(entryMetered, 50*time.Hour, 100), spendReading(entryMetered, 2*time.Hour, 120),
		spendReading(entryMetered2, 50*time.Hour, 100), spendReading(entryMetered2, 2*time.Hour, 120),
	}
	rates := map[string]burnguard.ModelRate{
		entryMetered + "/cheap": {OutPerK: 0.001}, // 1M output tokens = $1
		entryMetered2 + "/dear": {OutPerK: 0.02},  // 1M output tokens = $20
	}
	tokens := map[string][]burnguard.Bucket{entryMetered: outputTokens(1_000_000), entryMetered2: outputTokens(1_000_000)}
	g, adm := entriesRig(t, entries, readings, rates, tokens)
	g.Tick(context.Background())

	cheap := entryStatus(t, g, entryMetered).LastProjection
	dear := entryStatus(t, g, entryMetered2).LastProjection
	if cheap == nil || dear == nil || cheap.Needs != "" || dear.Needs != "" {
		t.Fatalf("projections: %+v / %+v", cheap, dear)
	}
	if cheap.PercentAtReading != 120 || cheap.Ceiling != 200 {
		t.Fatalf("metered base/ceiling = %v/%v, want the $120 spend and the $200 target", cheap.PercentAtReading, cheap.Ceiling)
	}
	if math.Abs(cheap.PointsSinceReading-1) > 1e-9 || math.Abs(dear.PointsSinceReading-20) > 1e-9 {
		t.Fatalf("tokens since the reading priced at %v and %v dollars, want 1 and 20 (each model's own rate)", cheap.PointsSinceReading, dear.PointsSinceReading)
	}
	// 13 hours to the reset: cheap burns $1/3h, dear $20/3h.
	if cheap.Over || adm.held[entryMetered] {
		t.Fatalf("cheap entry projected %v against %v and held=%v", cheap.Projected, cheap.Target, adm.held[entryMetered])
	}
	if !dear.Over || !adm.held[entryMetered2] {
		t.Fatalf("dear entry projected %v against %v and held=%v", dear.Projected, dear.Target, adm.held[entryMetered2])
	}
}

// TestBurnGuardUnnamedReadingsBelongToTheFirstEntry: today's single reading series (no
// entry named) keeps working as the first entry's; a second entry ignores it.
func TestBurnGuardUnnamedReadingsBelongToTheFirstEntry(t *testing.T) {
	hold := []string{burnguard.RungHoldClaude}
	entries := []config.BurnGuardEntryConfig{
		{LLM: entryFlatOver, UsageSource: "readings", Plan: "flat", Ladder: hold},
		{LLM: entryFlatUnder, UsageSource: "readings", Plan: "flat", Ladder: hold},
	}
	readings := []usagecalc.Reading{pctReading("", 30*time.Hour, 80), pctReading("", 2*time.Hour, 97)}
	g, adm := entriesRig(t, entries, readings, nil, nil)
	g.Tick(context.Background())
	if !adm.held[entryFlatOver] {
		t.Fatal("the first entry did not read the unnamed readings")
	}
	if adm.held[entryFlatUnder] {
		t.Fatal("the second entry read readings that name no entry")
	}
	if st := entryStatus(t, g, entryFlatUnder); st.State != "waiting" {
		t.Fatalf("second entry with no readings of its own = %+v, want waiting", st)
	}
}

// TestBurnGuardDefaultEntryFromLegacyConfig: with no entries configured the guard builds
// one entry from configuration defaults, and the runner code itself names no backend.
func TestBurnGuardDefaultEntryFromLegacyConfig(t *testing.T) {
	es := config.DefaultBurnGuard().EffectiveEntries()
	if len(es) != 1 || es[0].LLM != config.BurnGuardLegacyEntryDefault || !es[0].HasUsageSource() || es[0].IsMetered() {
		t.Fatalf("default entries = %+v", es)
	}
	for _, f := range []string{"burn_guard.go", "../burnguard/ladder.go", "../burnguard/project.go", "../burnguard/entry.go"} {
		src := readSource(t, f)
		for _, name := range []string{`"claude-code"`, `"pi-prompt"`} {
			if strings.Contains(src, name) {
				t.Errorf("%s hard-codes the backend %s", f, name)
			}
		}
	}
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	b, err := readFileForTest(path)
	if err != nil {
		t.Fatal(fmt.Errorf("read %s: %w", path, err))
	}
	return string(b)
}

func readFileForTest(path string) ([]byte, error) { return os.ReadFile(path) }
