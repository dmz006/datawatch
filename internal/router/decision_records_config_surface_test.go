// The decision record keys (autonomous.decisions.*) are set from chat with
// configure. The router holds no key list of its own: it hands the key and
// value to the daemon's PUT /api/config, which validates and answers 400 for a
// bad value. So the tests pin that the verb carries every key through intact,
// shows the daemon's refusal text to the operator, and names every key in its
// usage and key list.

package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

var decisionRecordConfigKeys = []string{
	"autonomous.decisions.enabled",
	"autonomous.decisions.folder",
	"autonomous.decisions.folder_glob",
	"autonomous.decisions.front_matter_delimiter",
	"autonomous.decisions.front_matter_separator",
	"autonomous.decisions.front_matter_field_separator",
	"autonomous.decisions.front_matter_keys",
	"autonomous.decisions.folder_withdraw_on_delete",
	"autonomous.decisions.clarity_mode",
	"autonomous.decisions.clarity_min_text_chars",
	"autonomous.decisions.clarity_require_tradeoffs",
	"autonomous.decisions.clarity_require_label",
	"autonomous.decisions.clarity_tracker_id_pattern",
	"autonomous.decisions.doc_root",
	"autonomous.decisions.doc_status_mode",
	"autonomous.decisions.doc_status_pattern",
	"autonomous.decisions.doc_status_template",
	"autonomous.decisions.follow_ups",
}

func TestDecisionRecordConfigSurface_Configure(t *testing.T) {
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

	for _, c := range []struct{ key, val string }{
		{"autonomous.decisions.enabled", "false"},
		{"autonomous.decisions.folder", "inbox/decisions"},
		{"autonomous.decisions.clarity_mode", "refuse"},
		{"autonomous.decisions.clarity_min_text_chars", "0"},
		{"autonomous.decisions.doc_status_mode", "on"},
		{"autonomous.decisions.doc_status_pattern", "^Status:.*$"},
		{"autonomous.decisions.front_matter_delimiter", "+++"},
		{"autonomous.decisions.follow_ups", `[{"name":"build","kind":"start_template","target":"t1"}]`},
	} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: c.key + "=" + c.val})
		wg.Wait()
		mu.Lock()
		if gotKey != c.key || gotValue != c.val {
			t.Fatalf("configureFn got key=%q value=%q (for %q=%q)", gotKey, gotValue, c.key, c.val)
		}
		if last := sent[len(sent)-1]; !strings.Contains(last, "Set "+c.key+" = "+c.val) {
			t.Fatalf("reply for %q=%q lacks the apply confirmation: %q", c.key, c.val, last)
		}
		mu.Unlock()
	}
}

// A bad value is refused by the daemon; the operator must see that refusal and
// no "Set" confirmation.
func TestDecisionRecordConfigSurface_ConfigureShowsRefusal(t *testing.T) {
	wg := sync.WaitGroup{}
	var mu sync.Mutex
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	refusal := `autonomous.decisions.clarity_mode: invalid value "bogus" (valid values: "off", "warn", "refuse", or empty for "warn")`
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(string, string) error { return errors.New(refusal) })

	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.decisions.clarity_mode=bogus"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	last := sent[len(sent)-1]
	if !strings.Contains(last, "Configure failed") || !strings.Contains(last, refusal) {
		t.Fatalf("reply should carry the daemon's refusal, got %q", last)
	}
	if strings.Contains(last, "Set autonomous.decisions.clarity_mode") {
		t.Fatalf("a refused value must not be confirmed as set: %q", last)
	}
}

func TestDecisionRecordConfigSurface_MentionedInConfigureHelpAndList(t *testing.T) {
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
		for _, key := range decisionRecordConfigKeys {
			if !strings.Contains(s, key+" (") && !strings.Contains(s, key+",") {
				t.Errorf("reply %d lacks the key %q", i, key)
			}
		}
		for _, want := range []string{"clarity_mode (off|warn|refuse", "doc_status_mode (off|report|on", "start_template or open_automaton"} {
			if !strings.Contains(s, want) {
				t.Errorf("reply %d lacks %q", i, want)
			}
		}
	}
}
