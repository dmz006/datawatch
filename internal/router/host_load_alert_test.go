package router

import (
	"strings"
	"sync"
	"testing"
)

func TestHostLoadAlert_ConfigureHelpAndListMentionKeys(t *testing.T) {
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
		for _, key := range []string{"autonomous.host_load_alert.mode", "autonomous.host_load_alert.multiple", "autonomous.host_load_alert.minutes"} {
			if !strings.Contains(s, key) {
				t.Errorf("reply %d lacks %s", i, key)
			}
		}
	}
}

func TestHostLoadAlert_ConfigureForwardsKeyAndValue(t *testing.T) {
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
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.host_load_alert.mode=reap"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "autonomous.host_load_alert.mode" || gotValue != "reap" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Set autonomous.host_load_alert.mode = reap") {
		t.Fatalf("sent=%q", sent)
	}
}
