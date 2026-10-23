package router

import (
	"strings"
	"sync"
	"testing"
)

func TestHandleConfigure_HelpAndListMentionMaxConcurrentTasks(t *testing.T) {
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
		if !strings.Contains(s, "autonomous.max_concurrent_tasks") {
			t.Errorf("reply %d lacks autonomous.max_concurrent_tasks: %q", i, s)
		}
	}
}

func TestHandleConfigure_HelpAndListMentionMaxEnginesPerNode(t *testing.T) {
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
		if !strings.Contains(s, "autonomous.max_engines_per_node") {
			t.Errorf("reply %d lacks autonomous.max_engines_per_node: %q", i, s)
		}
	}
}

// B165 — the boot-resume age limit (default 3600; 0 = never resume,
// -1 = no age limit) must be listed on the same surfaces as the
// concurrency keys so callers know it exists.
func TestHandleConfigure_HelpAndListMentionResumeMaxAge(t *testing.T) {
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
		if !strings.Contains(s, "autonomous.resume_max_age_seconds") {
			t.Errorf("reply %d lacks autonomous.resume_max_age_seconds: %q", i, s)
		}
	}
}

func TestHandleConfigure_AcceptsMaxEnginesPerNodeNumeric(t *testing.T) {
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
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.max_engines_per_node=2"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "autonomous.max_engines_per_node" || gotValue != "2" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Set autonomous.max_engines_per_node = 2") {
		t.Fatalf("sent=%q", sent)
	}
}

func TestHandleConfigure_HelpAndListMentionGateDisagreement(t *testing.T) {
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
		if !strings.Contains(s, "autonomous.gate_disagreement") {
			t.Errorf("reply %d lacks autonomous.gate_disagreement: %q", i, s)
		}
	}
}

func TestHandleConfigure_AcceptsGateDisagreement(t *testing.T) {
	var wg sync.WaitGroup
	capture := func(s string) { wg.Done() }
	var gotKey, gotValue string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	})

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.gate_disagreement=block"})
	wg.Wait()
	if gotKey != "autonomous.gate_disagreement" || gotValue != "block" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
}
