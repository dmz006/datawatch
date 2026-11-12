package router

// The autonomous.merge_shadow keys (enabled, gate_timeout, plan_keep) are set
// from chat with configure: configure forwards key=value to the daemon's
// config PATCH API, so the operator also needs the keys visible in the
// configure help. Both the acceptance path and the help text are covered
// here, following the other autonomous-*.configure tests.

import (
	"strings"
	"sync"
	"testing"
)

func TestMergeShadowConfigure_AcceptsEachKey(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	cfg := map[string]string{}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		cfg[key] = value
		return nil
	})

	pairs := []struct{ text, key, value string }{
		{"autonomous.merge_shadow.enabled=true", "autonomous.merge_shadow.enabled", "true"},
		{"autonomous.merge_shadow.gate_timeout=15m", "autonomous.merge_shadow.gate_timeout", "15m"},
		{"autonomous.merge_shadow.plan_keep=100", "autonomous.merge_shadow.plan_keep", "100"},
	}
	for _, p := range pairs {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: p.text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if got := cfg["autonomous.merge_shadow.enabled"]; got != "true" {
		t.Errorf("configureFn got enabled=%q, want true (cfg=%v)", got, cfg)
	}
	if got := cfg["autonomous.merge_shadow.gate_timeout"]; got != "15m" {
		t.Errorf("configureFn got gate_timeout=%q, want 15m (cfg=%v)", got, cfg)
	}
	if got := cfg["autonomous.merge_shadow.plan_keep"]; got != "100" {
		t.Errorf("configureFn got plan_keep=%q, want 100 (cfg=%v)", got, cfg)
	}
	if len(sent) != 3 {
		t.Fatalf("sent=%d messages: %q", len(sent), sent)
	}
	if !strings.Contains(sent[0], "Set autonomous.merge_shadow.enabled = true") {
		t.Errorf("reply: %q", sent[0])
	}
	if !strings.Contains(sent[1], "Set autonomous.merge_shadow.gate_timeout = 15m") {
		t.Errorf("reply: %q", sent[1])
	}
	if !strings.Contains(sent[2], "Set autonomous.merge_shadow.plan_keep = 100") {
		t.Errorf("reply: %q", sent[2])
	}
}

func TestMergeShadowConfigure_HelpAndListMentionKeys(t *testing.T) {
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
		t.Fatalf("sent=%d messages: %q", len(sent), sent)
	}
	for i, s := range sent {
		for _, key := range []string{
			"autonomous.merge_shadow.enabled",
			"autonomous.merge_shadow.gate_timeout",
			"autonomous.merge_shadow.plan_keep",
		} {
			if !strings.Contains(s, key) {
				t.Errorf("reply %d (%q) lacks %s", i, []string{"help", "list"}[i], key)
			}
		}
	}
}
