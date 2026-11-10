// Chat `configure usage.burn_guard.max_projection_gap_points=<n>` accepts 0 and 25,
// routes the value to the config set path by its full key, and refuses an invalid
// value (negative, NaN, over the maximum) before configureFn is called, so nothing
// is written.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestBurnGuardProjectionGapConfigureAcceptsZeroAndDefault(t *testing.T) {
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

	for _, val := range []string{"0", "25"} {
		gotKey, gotValue = "", ""
		sent = nil
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.max_projection_gap_points=" + val})
		wg.Wait()

		mu.Lock()
		if gotKey != "usage.burn_guard.max_projection_gap_points" {
			t.Fatalf("configure %s: key = %q", val, gotKey)
		}
		if gotValue != val {
			t.Fatalf("configure %s: value = %q", val, gotValue)
		}
		if len(sent) < 1 || sent[0] != "[h] Set usage.burn_guard.max_projection_gap_points = "+val+". Restart may be required." {
			t.Fatalf("capture = %v", sent)
		}
		mu.Unlock()
	}
}

func TestBurnGuardProjectionGapConfigureRefusesInvalid(t *testing.T) {
	var mu sync.Mutex
	var calls int
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

	for _, bad := range []string{"-1", "NaN", "101", "many"} {
		sent = nil
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.max_projection_gap_points=" + bad})
		wg.Wait()

		mu.Lock()
		if len(sent) < 1 || !strings.HasPrefix(sent[0], "[h] Refused: ") || !strings.Contains(sent[0], "max_projection_gap_points") {
			t.Fatalf("configure %s: capture = %v, want a refusal naming the key", bad, sent)
		}
		if calls != 0 {
			t.Fatalf("configure %s reached configureFn (%d call(s))", bad, calls)
		}
		mu.Unlock()
	}
}
