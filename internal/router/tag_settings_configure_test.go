package router

import (
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestTagSettings_ConfigureShowsTheDaemonRefusal checks that a refused tag
// setting reaches the comm channel with the shared validator's text, the same
// text REST returns.
func TestTagSettings_ConfigureShowsTheDaemonRefusal(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		// The daemon's configure func returns the REST refusal text verbatim;
		// the validator produces that text.
		return config.ValidateTagSetting(key, value)
	})

	cases := []struct{ key, value string }{
		{"autonomous.tags.attention_statuses", "bogus"},
		{"autonomous.tags.default_match_mode", "sometimes"},
	}
	for _, tc := range cases {
		want := config.ValidateTagSetting(tc.key, tc.value).Error()
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

// TestTagSettings_ConfigureAcceptsEveryTagKey checks the comm parser passes
// each tag key through to the configure func unchanged.
func TestTagSettings_ConfigureAcceptsEveryTagKey(t *testing.T) {
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
		return nil
	})

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.tags.registry_sync_seconds=45"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "autonomous.tags.registry_sync_seconds" || gotValue != "45" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
}
