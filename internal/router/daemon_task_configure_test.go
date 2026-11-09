package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// The daemon-task keys are set from chat with configure. The router keeps no
// key list of its own: it hands key and value to the daemon, which validates
// them. These tests pin that the verb carries each key through intact, shows a
// refusal unchanged, and names all three keys in the usage text.

var daemonTaskConfigureKeys = []string{
	"autonomous.task_templates.mode.changelog",
	"autonomous.script_gate_timeout_seconds",
	"autonomous.project_gate_command",
}

func newDaemonTaskConfigureRouter(fn func(string, string) error) (*Router, *sync.WaitGroup, *[]string, *sync.Mutex) {
	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}
	sent := &[]string{}
	capture := func(s string) {
		mu.Lock()
		*sent = append(*sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(fn)
	return r, wg, sent, mu
}

func TestDaemonTaskConfigureSurface_KeyCarriedThrough(t *testing.T) {
	var gotKey, gotValue string
	r, wg, sent, mu := newDaemonTaskConfigureRouter(func(k, v string) error {
		gotKey, gotValue = k, v
		return nil
	})
	for _, c := range []struct{ key, val string }{
		{daemonTaskConfigureKeys[0], "shadow"},
		{daemonTaskConfigureKeys[1], "120"},
		{daemonTaskConfigureKeys[2], "make check"},
	} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: c.key + "=" + c.val})
		wg.Wait()
		mu.Lock()
		if gotKey != c.key || gotValue != c.val {
			t.Fatalf("got key=%q value=%q for %q=%q", gotKey, gotValue, c.key, c.val)
		}
		if last := (*sent)[len(*sent)-1]; !strings.Contains(last, "Set "+c.key+" = "+c.val) {
			t.Fatalf("no apply confirmation: %q", last)
		}
		mu.Unlock()
	}
}

func TestDaemonTaskConfigureSurface_RefusalShown(t *testing.T) {
	refusal := `autonomous.task_templates.mode.changelog: invalid value "bogus" (valid values: "off", "shadow", "on")`
	r, wg, sent, mu := newDaemonTaskConfigureRouter(func(string, string) error { return errors.New(refusal) })
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.task_templates.mode.changelog=bogus"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	last := (*sent)[len(*sent)-1]
	if !strings.Contains(last, "Configure failed") || !strings.Contains(last, refusal) {
		t.Fatalf("reply should carry the refusal unchanged, got %q", last)
	}
	if strings.Contains(last, "Set autonomous.task_templates") {
		t.Fatalf("refused value confirmed as set: %q", last)
	}
}

func TestDaemonTaskConfigureSurface_UsageNamesAllKeys(t *testing.T) {
	r, wg, sent, mu := newDaemonTaskConfigureRouter(func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	for i, s := range *sent {
		for _, want := range []string{
			"autonomous.task_templates.mode.<template id>",
			"autonomous.script_gate_timeout_seconds",
			"autonomous.project_gate_command",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("reply %d lacks %q", i, want)
			}
		}
	}
}
