// Chat `configure session.finalise_interval_minutes=<n>` routes the value to the
// config write by its full key, and the configure help and list replies name the
// key so the operator can find it.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestConfigureFinaliseIntervalWritesKey(t *testing.T) {
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

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "session.finalise_interval_minutes=15"})
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if gotKey != "session.finalise_interval_minutes" {
		t.Fatalf("key = %q", gotKey)
	}
	if gotValue != "15" {
		t.Fatalf("value = %q", gotValue)
	}
	if len(sent) < 1 || !strings.Contains(sent[0], "Set session.finalise_interval_minutes = 15") {
		t.Fatalf("reply = %v", sent)
	}
}

func TestConfigureHelpListsFinaliseInterval(t *testing.T) {
	for _, text := range []string{"help", "list"} {
		var mu sync.Mutex
		var sent []string
		wg := sync.WaitGroup{}
		capture := func(s string) {
			mu.Lock()
			sent = append(sent, s)
			mu.Unlock()
			wg.Done()
		}
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
		r.SetConfigureFunc(func(key, value string) error { return nil })

		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()

		mu.Lock()
		if len(sent) < 1 || !strings.Contains(sent[0], "session.finalise_interval_minutes") {
			t.Fatalf("configure %s reply does not name the key: %v", text, sent)
		}
		mu.Unlock()
	}
}
