// The three question notify toggles (autonomous.questions.notify_messaging,
// .notify_alert, .return_path; each defaults on when absent) are set from
// chat with configure, so both the usage (help) text and the key list must
// name them, and "configure <key>=<bool>" must reach the configureFn apply
// path with the key and value intact.

package router

import (
	"strings"
	"sync"
	"testing"
)

// questionNotifyToggles are the surface keys the comm configure verb must
// carry through to the daemon for the operator-question channel.
var questionNotifyToggles = []string{
	"autonomous.questions.notify_messaging",
	"autonomous.questions.notify_alert",
	"autonomous.questions.return_path",
}

func TestQuestionNotifyConfigSurface_ConfigureApplyPath(t *testing.T) {
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

	for _, key := range questionNotifyToggles {
		for _, val := range []string{"true", "false"} {
			wg.Add(1)
			r.handleConfigure(Command{Type: CmdConfigure, Text: key + "=" + val})
			wg.Wait()
			mu.Lock()
			if gotKey != key || gotValue != val {
				t.Fatalf("configureFn got key=%q value=%q (for %q=%q)", gotKey, gotValue, key, val)
			}
			last := sent[len(sent)-1]
			if !strings.Contains(last, "Set "+key+" = "+val) {
				t.Fatalf("reply for %q=%q lacks the apply confirmation: %q", key, val, last)
			}
			mu.Unlock()
		}
	}
}

func TestQuestionNotifyConfigSurface_MentionedInConfigureHelpAndList(t *testing.T) {
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
	labels := []string{"help", "list"}
	for i, s := range sent {
		if !strings.Contains(s, "autonomous.questions.enabled") {
			t.Errorf("reply %d (%q) lacks autonomous.questions.enabled", i, labels[i])
		}
		for _, key := range questionNotifyToggles {
			if !strings.Contains(s, key) {
				t.Errorf("reply %d (%q) lacks %s", i, labels[i], key)
			}
		}
	}
}
