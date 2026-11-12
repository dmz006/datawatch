// TestUsageCalcStartupLine: the advice's start-up line names the worker profile with the
// largest average start-up token size, states that average and names the profile change
// that would reduce it. It is advice only: a fake config writer records every write, and
// none must happen. With no start-up data the line is absent.

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// fakeConfigWriter stands in for the config writer the advice must never reach. It records
// every write it is asked for; the read-only test asserts that none happened.
type fakeConfigWriter struct {
	calls []string
}

func (w *fakeConfigWriter) Set(key string, value any) error {
	w.calls = append(w.calls, key)
	return nil
}

// startupAdviceFor builds an advice service over the test server's series, wired to the
// given fake start-up rows and the fake writer's settings, and returns one advice.
func startupAdviceFor(t *testing.T, s *Server, rows []usagecalc.StartupProfile, w *fakeConfigWriter) *usagecalc.AdviceResult {
	t.Helper()
	series, store, err := s.usageSeriesAndStore()
	if err != nil {
		t.Fatalf("usage series: %v", err)
	}
	settings := func() usagecalc.Settings {
		_ = w // settings are read, never written
		return usagecalc.Settings{}
	}
	svc := usagecalc.NewService(series, store, settings, nil,
		usagecalc.WithStartupSource(func() []usagecalc.StartupProfile { return rows }))
	postUsagePercent(t, s, 42)
	res, err := svc.Advice(context.Background(), usagecalc.Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	return res
}

// squashSpace joins the words of s with single spaces.
func squashSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestUsageCalcStartupLineNamesLargestProfile(t *testing.T) {
	s := newUsageCalcServer(t)
	w := &fakeConfigWriter{}
	rows := []usagecalc.StartupProfile{
		{Profile: "digest", Attempts: 4, AvgTokens: 1200, HasAverage: true},
		{Profile: "full", Attempts: 6, AvgTokens: 9800.4, HasAverage: true},
		{Profile: "review", Attempts: 2, AvgTokens: 3100, HasAverage: true},
	}
	res := startupAdviceFor(t, s, rows, w)

	if res.Startup == nil {
		t.Fatal("Startup = nil, want the start-up line")
	}
	if res.Startup.Profile != "full" {
		t.Errorf("profile = %q, want full (largest average)", res.Startup.Profile)
	}
	if res.Startup.AvgTokens != 9800.4 || res.Startup.Attempts != 6 {
		t.Errorf("avg/attempts = %v/%d, want 9800.4/6", res.Startup.AvgTokens, res.Startup.Attempts)
	}
	line := res.Startup.Line
	for _, want := range []string{"profile full", "9800 tokens", "over 6 attempts", "digest instead of full"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q missing %q", line, want)
		}
	}
	// The text forms word-wrap at their widths, so compare the words, not the breaks.
	if !strings.Contains(squashSpace(res.Text.CLI), squashSpace(line)) {
		t.Errorf("CLI text does not carry the start-up line:\n%s", res.Text.CLI)
	}
	if !strings.Contains(squashSpace(res.Text.Comm), squashSpace(line)) {
		t.Errorf("comm text does not carry the start-up line:\n%s", res.Text.Comm)
	}
	if len(w.calls) != 0 {
		t.Errorf("config writer recorded %v, want no writes", w.calls)
	}
}

func TestUsageCalcStartupLineNoData(t *testing.T) {
	s := newUsageCalcServer(t)
	w := &fakeConfigWriter{}
	// Attempts exist but none carries a start-up size: no average, so no line.
	rows := []usagecalc.StartupProfile{
		{Profile: "full", Attempts: 3, HasAverage: false},
	}
	res := startupAdviceFor(t, s, rows, w)
	if res.Startup != nil {
		t.Errorf("Startup = %+v, want nil with no start-up data", res.Startup)
	}
	if strings.Contains(res.Text.CLI, "start-up:") || strings.Contains(res.Text.Comm, "start-up:") {
		t.Errorf("text carries a start-up line with no data:\n%s", res.Text.CLI)
	}
	if len(w.calls) != 0 {
		t.Errorf("config writer recorded %v, want no writes", w.calls)
	}
}

func TestUsageCalcStartupLineReadOnly(t *testing.T) {
	s := newUsageCalcServer(t)
	w := &fakeConfigWriter{}
	rows := []usagecalc.StartupProfile{{Profile: "full", Attempts: 1, AvgTokens: 500, HasAverage: true}}
	startupAdviceFor(t, s, rows, w)
	// A second call reads the same figures and still writes nothing.
	startupAdviceFor(t, s, rows, w)
	if len(w.calls) != 0 {
		t.Errorf("config writer recorded %v, want no writes", w.calls)
	}
}
