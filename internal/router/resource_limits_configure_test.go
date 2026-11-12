package router

import (
	"strings"
	"sync"
	"testing"
)

var resourceLimitsKeys = []string{
	"resource_limits.memory_share_percent",
	"resource_limits.memory_floor_mb",
	"resource_limits.memory_cap_mb",
}

// configure passes each memory ceiling key and its number to the daemon, and
// help and list name all three keys.
func TestConfigure_ResourceLimitsKeys(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}}}
	var gotKey, gotValue string
	r.SetConfigureFunc(func(key, value string) error { gotKey, gotValue = key, value; return nil })
	for i, key := range resourceLimitsKeys {
		val := []string{"40", "1024", "8192"}[i]
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: key + "=" + val})
		wg.Wait()
		if gotKey != key || gotValue != val {
			t.Fatalf("configureFn got %q=%q, want %q=%q", gotKey, gotValue, key, val)
		}
		mu.Lock()
		if last := sent[len(sent)-1]; !strings.Contains(last, "Set "+key+" = "+val) {
			t.Fatalf("reply lacks confirmation: %q", last)
		}
		mu.Unlock()
	}
	for _, arg := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: arg})
		wg.Wait()
		mu.Lock()
		last := sent[len(sent)-1]
		mu.Unlock()
		for _, key := range resourceLimitsKeys {
			if !strings.Contains(last, key) {
				t.Errorf("configure %s output lacks %s", arg, key)
			}
		}
	}
}
