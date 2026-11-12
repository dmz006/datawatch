package usagecalc

import (
	"strings"
	"testing"
)

func TestStartupAdviceForPicksLargestAverage(t *testing.T) {
	got := StartupAdviceFor([]StartupProfile{
		{Profile: "digest", Attempts: 2, AvgTokens: 800, HasAverage: true},
		{Profile: "full", Attempts: 5, AvgTokens: 4200.6, HasAverage: true},
	})
	if got == nil {
		t.Fatal("nil advice, want the full profile")
	}
	if got.Profile != "full" || got.Attempts != 5 {
		t.Errorf("got %+v, want full over 5 attempts", got)
	}
	if !strings.Contains(got.Line, "4201 tokens") {
		t.Errorf("line %q does not state the rounded average", got.Line)
	}
	if !strings.Contains(got.Line, "digest instead of full") {
		t.Errorf("line %q does not name the profile change", got.Line)
	}
}

func TestStartupAdviceForSkipsRowsWithoutData(t *testing.T) {
	cases := map[string][]StartupProfile{
		"nil":         nil,
		"no average":  {{Profile: "full", Attempts: 3}},
		"no attempts": {{Profile: "full", Attempts: 0, AvgTokens: 9, HasAverage: true}},
		"unnamed row": {{Profile: " ", Attempts: 1, AvgTokens: 9, HasAverage: true}},
	}
	for name, rows := range cases {
		if got := StartupAdviceFor(rows); got != nil {
			t.Errorf("%s: got %+v, want nil", name, got)
		}
	}
}

func TestServiceStartupLineOnlyWithSource(t *testing.T) {
	f := newServiceFixture(t, Settings{}, nil)
	f.seedReading(t)
	if res := f.advice(Options{}); res.Startup != nil {
		t.Fatalf("no source wired: Startup = %+v, want nil", res.Startup)
	}
	svc := NewService(f.series, f.store, func() Settings { return Settings{} }, nil,
		WithServiceNow(f.clock.Now),
		WithStartupSource(func() []StartupProfile {
			return []StartupProfile{{Profile: "full", Attempts: 1, AvgTokens: 700, HasAverage: true}}
		}))
	res, err := svc.Advice(t.Context(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Startup == nil || res.Startup.Profile != "full" {
		t.Fatalf("Startup = %+v, want full", res.Startup)
	}
	if !strings.Contains(res.Text.CLI, "start-up: profile full") {
		t.Errorf("CLI text lacks the start-up line:\n%s", res.Text.CLI)
	}
}
