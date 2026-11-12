// The autonomous.questions.enabled switch (gate for the /api/questions
// endpoints, default off) is set from chat with configure, so both the
// usage (help) text and the key list must name it, and
// "configure autonomous.questions.enabled=<bool>" must reach the
// configureFn apply path with the key and value intact.

package router

import (
	"strings"
	"sync"
	"testing"
)

func TestQuestionsConfig_ConfigureApplyPath(t *testing.T) {
	wg := sync.WaitGroup{}
	var gotKey, gotValue string
	var mu sync.Mutex
	var sent []string
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

	for _, text := range []string{"autonomous.questions.enabled=true", "autonomous.questions.enabled=false"} {
		wantValue := text[len("autonomous.questions.enabled="):]
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
		mu.Lock()
		if gotKey != "autonomous.questions.enabled" || gotValue != wantValue {
			t.Fatalf("configureFn got key=%q value=%q (for %q)", gotKey, gotValue, text)
		}
		mu.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent=%q", sent)
	}
	if !strings.Contains(sent[0], "Set autonomous.questions.enabled = true") {
		t.Fatalf("reply 0 lacks the apply confirmation: %q", sent[0])
	}
}

func TestQuestionsConfig_MentionedInConfigureHelpAndList(t *testing.T) {
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
		if !strings.Contains(s, "autonomous.questions.enabled") {
			t.Errorf("reply %d lacks autonomous.questions.enabled: %q", i, s)
		}
	}
}
