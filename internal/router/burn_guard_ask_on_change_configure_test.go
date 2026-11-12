// Chat `configure usage.burn_guard.ask_on_change=<bool>` accepts the key and the
// true/false value, routes it to the config set path by its full key, and the
// apply confirmation reports the key the operator typed.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestBurnGuardAskOnChangeConfigureAcceptsTrueAndFalse(t *testing.T) {
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

	for _, val := range []string{"true", "false"} {
		gotKey, gotValue = "", ""
		sent = nil
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: "usage.burn_guard.ask_on_change=" + val})
		wg.Wait()

		mu.Lock()
		if gotKey != "usage.burn_guard.ask_on_change" {
			t.Fatalf("configure %s: key = %q", val, gotKey)
		}
		if gotValue != val {
			t.Fatalf("configure %s: value = %q", val, gotValue)
		}
		if len(sent) < 1 || sent[0] != "[h] Set usage.burn_guard.ask_on_change = "+val+". Restart may be required." {
			t.Fatalf("capture = %v", sent)
		}
		mu.Unlock()
	}

	// The chat configure help names the key with its behavior description.
	mu.Lock()
	sent = nil
	mu.Unlock()
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "help"})
	wg.Wait()
	mu.Lock()
	def := "when false (default) the guard sends one alert per change and no question; when true it keeps at most one open question"
	if len(sent) < 1 || !strings.Contains(sent[0], "usage.burn_guard.ask_on_change") || !strings.Contains(sent[0], def) {
		t.Fatalf("help capture does not describe ask_on_change:\n%s", sent)
	}
	mu.Unlock()
}
