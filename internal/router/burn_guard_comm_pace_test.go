// Chat `configure usage.burn_guard.<pace key>=<v>` accepts the six pace_line
// values inside their ranges (routing by the full key, exactly like
// max_projection_gap_points) and refuses an out-of-range, fractional, or
// non-numeric value before configureFn is called, so nothing is written.

package router

import (
	"strings"
	"sync"
	"testing"
)

type paceConfigureCase struct {
	text  string
	key   string
	value string
	bad   bool
}

func paceConfigureCases() []paceConfigureCase {
	good := []struct {
		key   string
		value string
	}{
		{"mode", "off"}, {"mode", "shadow"}, {"mode", "on"}, {"mode", "ON"}, // matching is case-insensitive
		{"week_tolerance", "0"}, {"week_tolerance", "2"}, {"week_tolerance", "10"},
		{"points_to_week_factor", "1"}, {"points_to_week_factor", "1.3"}, {"points_to_week_factor", "10"},
		{"window_percent_per_point", "1"}, {"window_percent_per_point", "4"}, {"window_percent_per_point", "25"},
		{"window_stop_percent", "50"}, {"window_stop_percent", "85"}, {"window_stop_percent", "95"},
		{"reading_freshness_minutes", "1"}, {"reading_freshness_minutes", "60"}, {"reading_freshness_minutes", "1440"},
	}
	bad := []struct {
		key   string
		value string
	}{
		{"mode", "loud"},
		{"week_tolerance", "-1"}, {"week_tolerance", "11"}, {"week_tolerance", "2.5"},
		{"points_to_week_factor", "0.9"}, {"points_to_week_factor", "11"},
		{"window_percent_per_point", "0.5"}, {"window_percent_per_point", "26"},
		{"window_stop_percent", "49"}, {"window_stop_percent", "96"},
		{"reading_freshness_minutes", "0"}, {"reading_freshness_minutes", "1441"}, {"reading_freshness_minutes", "60.5"},
	}
	cases := make([]paceConfigureCase, 0, len(good)+len(bad))
	for _, g := range good {
		cases = append(cases, paceConfigureCase{text: "usage.burn_guard." + g.key + "=" + g.value, key: g.key, value: g.value, bad: false})
	}
	for _, b := range bad {
		cases = append(cases, paceConfigureCase{text: "usage.burn_guard." + b.key + "=" + b.value, key: b.key, value: b.value, bad: true})
	}
	return cases
}

func TestBurnGuardPaceConfigureAcceptsValidValues(t *testing.T) {
	var mu sync.Mutex
	var gotKey, gotValue string
	var sent []string
	wg := sync.WaitGroup{}
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		gotKey, gotValue = key, value
		mu.Unlock()
		return nil
	})
	for _, tc := range paceConfigureCases() {
		if tc.bad {
			continue
		}
		gotKey, gotValue, sent = "", "", nil
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: tc.text})
		wg.Wait()
		mu.Lock()
		if gotKey != "usage.burn_guard."+tc.key || gotValue != tc.value {
			t.Fatalf("configure %s: got key=%q value=%q", tc.text, gotKey, gotValue)
		}
		mu.Unlock()
	}
}

func TestBurnGuardPaceConfigureRefusesInvalidValues(t *testing.T) {
	for _, tc := range paceConfigureCases() {
		if !tc.bad {
			continue
		}
		var mu sync.Mutex
		calls := 0
		var sent []string
		wg := sync.WaitGroup{}
		capture := func(s string) {
			mu.Lock()
			sent = append(sent, s)
			mu.Unlock()
			wg.Done()
		}
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
		r.SetConfigureFunc(func(key, value string) error {
			mu.Lock()
			calls++
			mu.Unlock()
			return nil
		})
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: tc.text})
		wg.Wait()
		mu.Lock()
		if calls != 0 {
			t.Fatalf("configure %s: configureFn called %d times, want none", tc.text, calls)
		}
		if len(sent) == 0 || !strings.Contains(sent[0], "Refused") || !strings.Contains(sent[0], tc.key) {
			t.Fatalf("configure %s: capture = %v, want a refusal naming %q", tc.text, sent, tc.key)
		}
		mu.Unlock()
	}
}
