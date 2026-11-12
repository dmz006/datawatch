// The parallel task settings (autonomous.parallel.worktree_per_task,
// merge_conflict_policy and merge_conflict_model) are set from chat with
// configure, and the per-task merge state shows in the autonomous get reply.

package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

var parallelKeys = []string{
	"autonomous.parallel.worktree_per_task",
	"autonomous.parallel.merge_conflict_policy",
	"autonomous.parallel.merge_conflict_model",
	"autonomous.parallel.merge_retry_seconds",
}

func TestParallelConfigSurface_ConfigureCarriesKeysAndRefusal(t *testing.T) {
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
	var failWith error
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		gotKey, gotValue = key, value
		err := failWith
		mu.Unlock()
		return err
	})
	for _, c := range []struct{ key, val string }{
		{parallelKeys[0], "false"},
		{parallelKeys[1], "triage"},
		{parallelKeys[2], "qwen3.8:27b"},
		{parallelKeys[3], "120"},
	} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: c.key + "=" + c.val})
		wg.Wait()
		mu.Lock()
		if gotKey != c.key || gotValue != c.val {
			t.Fatalf("configureFn got %q=%q, want %q=%q", gotKey, gotValue, c.key, c.val)
		}
		if last := sent[len(sent)-1]; !strings.Contains(last, "Set "+c.key+" = "+c.val) {
			t.Fatalf("reply lacks the confirmation: %q", last)
		}
		mu.Unlock()
	}
	mu.Lock()
	failWith = errors.New("invalid value \"bogus\" (valid values: additive_then_model, block, triage)")
	mu.Unlock()
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: parallelKeys[1] + "=bogus"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	last := sent[len(sent)-1]
	if !strings.Contains(last, "valid values") || strings.Contains(last, "Set "+parallelKeys[1]) {
		t.Fatalf("refusal not shown cleanly: %q", last)
	}
}

func TestParallelConfigSurface_HelpNamesKeys(t *testing.T) {
	for _, arg := range []string{"help", "list"} {
		wg := sync.WaitGroup{}
		var sent string
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) { sent = s; wg.Done() }}}
		r.SetConfigureFunc(func(string, string) error { return nil })
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: arg})
		wg.Wait()
		for _, k := range parallelKeys {
			if !strings.Contains(sent, k) {
				t.Errorf("configure %s does not name %s", arg, k)
			}
		}
	}
}

func TestTaskMergeStatusLines(t *testing.T) {
	body := `{"stories":[{"tasks":[{"id":"t1","merge_state":"merged","merge_commit":"0123456789"},{"id":"t2","merge_state":"conflict"},{"id":"t3"}]}]}`
	got := strings.Join(taskMergeStatusLines(body), "|")
	if got != "merge: t1 merged 0123456|merge: t2 conflict" {
		t.Fatalf("lines = %q", got)
	}
	if l := taskMergeStatusLines(`{"tasks":[{"id":"a","merge_state":"pending"}]}`); len(l) != 1 || l[0] != "merge: a pending" {
		t.Fatalf("flat view lines = %q", l)
	}
	if taskMergeStatusLines(`{"stories":[{"tasks":[{"id":"x"}]}]}`) != nil || taskMergeStatusLines("junk") != nil {
		t.Fatal("expected no lines")
	}
}

func TestParallelConfigSurface_MergeRetrySecondsRefusal(t *testing.T) {
	wg := sync.WaitGroup{}
	var sent string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) { sent = s; wg.Done() }}}
	r.SetConfigureFunc(func(string, string) error {
		return errors.New("autonomous.parallel.merge_retry_seconds: -5 must be a whole number >= 0")
	})
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: parallelKeys[3] + "=-5"})
	wg.Wait()
	if !strings.Contains(sent, ">= 0") || strings.Contains(sent, "Set "+parallelKeys[3]) {
		t.Fatalf("refusal not shown cleanly: %q", sent)
	}
}
