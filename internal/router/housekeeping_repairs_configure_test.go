package router

import (
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestHousekeepingRepairs_ConfigurePassesKeysAndShowsRefusal checks the comm
// configure verb passes each autonomous.housekeeping_repairs key to the
// configure func unchanged and shows the shared validator's refusal text.
func TestHousekeepingRepairs_ConfigurePassesKeysAndShowsRefusal(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	var gotKey, gotValue string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		return config.ValidateHousekeepingRepairsSetting(key, value)
	})

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.housekeeping_repairs.lane_fill=on"})
	wg.Wait()
	if gotKey != "autonomous.housekeeping_repairs.lane_fill" || gotValue != "on" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}

	for _, tc := range []struct{ key, value string }{
		{"autonomous.housekeeping_repairs.task_chain", "sometimes"},
		{"autonomous.housekeeping_repairs.grace_seconds", "-5"},
	} {
		want := config.ValidateHousekeepingRepairsSetting(tc.key, tc.value).Error()
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: tc.key + "=" + tc.value})
		wg.Wait()
		mu.Lock()
		last := sent[len(sent)-1]
		mu.Unlock()
		if !strings.Contains(last, "Configure failed: "+want) {
			t.Fatalf("comm reply = %q, want it to carry %q", last, want)
		}
	}
}
