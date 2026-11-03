package router

import (
	"strings"
	"sync"
	"testing"
)

func TestHandleConfigure_CapacityFallthrough(t *testing.T) {
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

	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.capacity_fallthrough=true"})
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 3 {
		t.Fatalf("sent=%q", sent)
	}
	for i := 0; i < 2; i++ {
		if !strings.Contains(sent[i], "autonomous.capacity_fallthrough") {
			t.Errorf("reply %d lacks autonomous.capacity_fallthrough: %q", i, sent[i])
		}
	}
	if gotKey != "autonomous.capacity_fallthrough" || gotValue != "true" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
	if !strings.Contains(sent[2], "Set autonomous.capacity_fallthrough = true") {
		t.Fatalf("sent=%q", sent[2])
	}
}
