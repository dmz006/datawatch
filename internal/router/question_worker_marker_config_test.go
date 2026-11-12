// The worker question marker switch (autonomous.questions.worker_marker;
// off when absent) is set from chat with configure, so both the usage
// (help) text and the key list must name it, and "configure <key>=<bool>"
// must reach the configureFn apply path with the key and value intact.

package router

import (
	"strings"
	"sync"
	"testing"
)

const questionWorkerMarkerKey = "autonomous.questions.worker_marker"

func TestQuestionWorkerMarkerConfig_ConfigureApplyPath(t *testing.T) {
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

	for _, val := range []string{"true", "false"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: questionWorkerMarkerKey + "=" + val})
		wg.Wait()
		mu.Lock()
		if gotKey != questionWorkerMarkerKey || gotValue != val {
			t.Fatalf("configureFn got key=%q value=%q (for value %q)", gotKey, gotValue, val)
		}
		last := sent[len(sent)-1]
		if !strings.Contains(last, "Set "+questionWorkerMarkerKey+" = "+val) {
			t.Fatalf("reply for %q lacks the apply confirmation: %q", val, last)
		}
		mu.Unlock()
	}
}

func TestQuestionWorkerMarkerConfig_MentionedInConfigureHelpAndList(t *testing.T) {
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
	for i, label := range []string{"help", "list"} {
		if !strings.Contains(sent[i], questionWorkerMarkerKey) {
			t.Errorf("reply %d (%q) lacks %s", i, label, questionWorkerMarkerKey)
		}
	}
}
