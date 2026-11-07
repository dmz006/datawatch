package router

import (
	"strings"
	"sync"
	"testing"
)

func TestParsePlatformContextConfigure(t *testing.T) {
	cases := []struct {
		text, key, value string
		ok, err          bool
	}{
		{"platform_context enabled on", "platform_context.enabled", "true", true, false},
		{"platform_context enabled OFF", "platform_context.enabled", "false", true, false},
		{"platform_context note Use the staging cluster", "platform_context.note", "Use the staging cluster", true, false},
		{"platform_context note", "platform_context.note", "", true, false},
		{"platform_context enabled maybe", "", "", true, true},
		{"platform_context", "", "", true, true},
		{"platform_context bogus x", "", "", true, true},
		{"platform_context.enabled=false", "", "", false, false},
		{"session.max_sessions=3", "", "", false, false},
	}
	for _, c := range cases {
		got, ok := parsePlatformContextConfigure(c.text)
		if ok != c.ok || got.Key != c.key || got.Value != c.value || (got.Err != "") != c.err {
			t.Errorf("%q: got %+v ok=%v", c.text, got, ok)
		}
	}
	long := "platform_context note " + strings.Repeat("x", 2001)
	if got, _ := parsePlatformContextConfigure(long); got.Err == "" {
		t.Error("over-long note not refused")
	}
}

func TestHandleConfigurePlatformContextApplies(t *testing.T) {
	var mu sync.Mutex
	var keys, vals, sent []string
	wg := sync.WaitGroup{}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}}}
	r.SetConfigureFunc(func(k, v string) error {
		mu.Lock()
		keys, vals = append(keys, k), append(vals, v)
		mu.Unlock()
		return nil
	})
	for _, text := range []string{"platform_context enabled off", "platform_context note be brief", "platform_context enabled bad"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] != "platform_context.enabled" || vals[0] != "false" ||
		keys[1] != "platform_context.note" || vals[1] != "be brief" {
		t.Fatalf("applied keys=%v vals=%v", keys, vals)
	}
	if !strings.Contains(sent[0], "Platform context is now off") ||
		!strings.Contains(sent[1], "note set to: be brief") ||
		!strings.Contains(sent[2], "Usage: configure platform_context") {
		t.Fatalf("replies = %v", sent)
	}
}
