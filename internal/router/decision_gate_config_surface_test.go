// The decision gate keys (autonomous.questions.decision_gate and
// autonomous.questions.decision_gate_mode) are set from chat with configure.
// The router holds no key list of its own: it hands the key and value to the
// daemon's PUT /api/config, which validates the mode (shadow or on) and
// answers 400 for anything else. So the tests pin that the verb carries both
// keys through intact, shows the daemon's refusal text to the operator, and
// names both keys in its usage and key list.

package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestDecisionGateConfigSurface_ConfigureSetsKeys(t *testing.T) {
	wg := sync.WaitGroup{}
	var mu sync.Mutex
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
		mu.Lock()
		gotKey, gotValue = key, value
		mu.Unlock()
		return nil
	})

	for _, c := range []struct{ key, val string }{
		{"autonomous.questions.decision_gate_mode", "on"},
		{"autonomous.questions.decision_gate_mode", "shadow"},
		{"autonomous.questions.decision_gate", "false"},
		{"autonomous.questions.decision_gate", "true"},
	} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: c.key + "=" + c.val})
		wg.Wait()
		mu.Lock()
		if gotKey != c.key || gotValue != c.val {
			t.Fatalf("configureFn got key=%q value=%q (for %q=%q)", gotKey, gotValue, c.key, c.val)
		}
		if last := sent[len(sent)-1]; !strings.Contains(last, "Set "+c.key+" = "+c.val) {
			t.Fatalf("reply for %q=%q lacks the apply confirmation: %q", c.key, c.val, last)
		}
		mu.Unlock()
	}
}

// A bad mode is refused by the daemon; the operator must see that refusal and
// no "Set" confirmation.
func TestDecisionGateConfigSurface_ConfigureShowsBadModeRefusal(t *testing.T) {
	wg := sync.WaitGroup{}
	var mu sync.Mutex
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	refusal := `autonomous.questions.decision_gate_mode: invalid value "bogus" (valid values: "shadow", "on", or empty for "shadow")`
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(string, string) error { return errors.New(refusal) })

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.questions.decision_gate_mode=bogus"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	last := sent[len(sent)-1]
	if !strings.Contains(last, "Configure failed") || !strings.Contains(last, refusal) {
		t.Fatalf("reply should carry the daemon's refusal, got %q", last)
	}
	if strings.Contains(last, "Set autonomous.questions.decision_gate_mode") {
		t.Fatalf("a refused value must not be confirmed as set: %q", last)
	}
}

func TestDecisionGateConfigSurface_MentionedInConfigureHelpAndList(t *testing.T) {
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
		for _, want := range []string{
			"autonomous.questions.decision_gate (",
			"autonomous.questions.decision_gate_mode (shadow|on",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("reply %d lacks %q", i, want)
			}
		}
	}
}
