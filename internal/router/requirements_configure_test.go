// Comm-channel configure path for the min-Ollama overrides: the operator
// types `configure autonomous.requirements.min_ollama.<capability>=<version>`
// (or a whole-block `configure autonomous.requirements=<object>`) and the
// key/value are forwarded verbatim to the daemon, which validates against
// the one shared rule set before applying the patch.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestCommConfigure_RequirementsMinOllamaForwardedToDaemon(t *testing.T) {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
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

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.requirements.min_ollama.classification_verdict=0.9.1"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "autonomous.requirements.min_ollama.classification_verdict" {
		t.Fatalf("key = %q, want autonomous.requirements.min_ollama.classification_verdict", gotKey)
	}
	if gotValue != "0.9.1" {
		t.Fatalf("value = %q, want 0.9.1", gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "autonomous.requirements.min_ollama.classification_verdict") {
		t.Fatalf("sent = %q, want confirmation naming the key", sent)
	}
}

func TestCommConfigureHelp_MentionsRequirementsMinOllama(t *testing.T) {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error { return nil })

	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent = %d messages, want 2", len(sent))
	}
	for i, s := range sent {
		if !strings.Contains(s, "autonomous.requirements.min_ollama") {
			t.Errorf("reply %d lacks autonomous.requirements.min_ollama", i)
		}
	}
}
