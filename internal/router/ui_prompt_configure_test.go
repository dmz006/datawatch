// The four UI prompt handling keys must be settable from chat with
// `configure`, so both the help text and the list text name each of them,
// and configure passes a key through to the daemon.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestUIPromptSettings_HelpAndListMentionKeys(t *testing.T) {
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
	r.SetConfigureFunc(func(string, string) error { return nil })

	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent=%q", sent)
	}
	for i, s := range sent {
		for _, key := range []string{
			"session.dismiss_ui_prompts",
			"session.ui_prompt_dismiss_delay_seconds",
			"session.ui_prompt_clear_bound_seconds",
			"session.ui_prompt_max_attempts",
		} {
			if !strings.Contains(s, key) {
				t.Errorf("reply %d lacks %s: %q", i, key, s)
			}
		}
	}
}

func TestUIPromptSettings_ConfigureAcceptsKeys(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	muGot := sync.Mutex{}
	got := map[string]string{}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		muGot.Lock()
		got[key] = value
		muGot.Unlock()
		return nil
	})

	for _, kv := range []string{
		"session.dismiss_ui_prompts=false",
		"session.ui_prompt_dismiss_delay_seconds=0",
		"session.ui_prompt_clear_bound_seconds=45",
		"session.ui_prompt_max_attempts=3",
	} {
		parts := strings.SplitN(kv, "=", 2)
		key, value := parts[0], parts[1]
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: kv})
		wg.Wait()
		muGot.Lock()
		if g := got[key]; g != value {
			muGot.Unlock()
			t.Fatalf("configure %s: got %q, want %q", kv, g, value)
		}
		muGot.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 4 {
		t.Fatalf("sent=%q", sent)
	}
	for i, s := range sent {
		if !strings.Contains(s, "Set session.") {
			t.Errorf("reply %d does not confirm the set: %q", i, s)
		}
	}
}
