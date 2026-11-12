package router

import (
	"strings"
	"sync"
	"testing"
)

// TestPeerSilenceConfigure: the configure command lists the peer silence key in
// its help and forwards it unchanged to the config set path.
func TestPeerSilenceConfigure(t *testing.T) {
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
	r.handleConfigure(Command{Type: CmdConfigure, Text: "help"})
	wg.Wait()
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "observer.peers.silent_alert_minutes=9"})
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(sent[0], "observer.peers.silent_alert_minutes") {
		t.Errorf("help lacks the key: %q", sent[0])
	}
	if gotKey != "observer.peers.silent_alert_minutes" || gotValue != "9" {
		t.Fatalf("configureFn got %q=%q", gotKey, gotValue)
	}
	if !strings.Contains(sent[1], "Set observer.peers.silent_alert_minutes = 9") {
		t.Errorf("reply = %q", sent[1])
	}
}
