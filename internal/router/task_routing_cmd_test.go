package router

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

type taskRoutingCommCall struct{ method, path, body string }

// taskRoutingCommFake answers by "METHOD path" and records every call.
func taskRoutingCommFake(t *testing.T, replies map[string]string) (*[]taskRoutingCommCall, int) {
	t.Helper()
	var mu sync.Mutex
	calls := &[]taskRoutingCommCall{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		*calls = append(*calls, taskRoutingCommCall{r.Method, r.URL.Path, string(raw)})
		mu.Unlock()
		rep, ok := replies[r.Method+" "+r.URL.Path]
		if !ok {
			http.Error(w, "no fake reply", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(rep))
	})
	return calls, port
}

func TestTaskRoutingCommShowAndAlias(t *testing.T) {
	if !slices.Contains(TaskRoutingCommVerbs(), "task-routing") {
		t.Fatalf("TaskRoutingCommVerbs() = %v", TaskRoutingCommVerbs())
	}
	if c := Parse("autonomous task-routing show"); c.Type != CmdAutonomous {
		t.Errorf("Parse type = %v", c.Type)
	}
	calls, port := taskRoutingCommFake(t, map[string]string{"GET /api/autonomous/task-routing": `{"enabled":true,"active_profile":"default","profiles":{"default":{` +
		`"docs_set":{"worker_backend":"pi-prompt","worker_model":"qwen","verification_backend":"claude-code","verification_model":"sonnet","effort":"low"}}},"validation":{"valid":true}}`})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous task-routing show"), "\n")
	for _, want := range []string{"Task routing: enabled=true profile=default (valid)", "docs_set | pi-prompt/qwen | claude-code/sonnet | low"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	_ = r.HandleTestMessage("autonomous task_routing show")
	if len(*calls) != 2 || (*calls)[1].path != "/api/autonomous/task-routing" {
		t.Errorf("calls = %+v", *calls)
	}
}

func TestTaskRoutingCommShowIsCappedForChat(t *testing.T) {
	var rows []string
	for i := 0; i < 30; i++ {
		rows = append(rows, `"t`+string(rune('a'+i%26))+string(rune('a'+i/26))+`":{"worker_backend":"b"}`)
	}
	_, port := taskRoutingCommFake(t, map[string]string{"GET /api/autonomous/task-routing": `{"enabled":true,"active_profile":"p","profiles":{"p":{` +
		strings.Join(rows, ",") + `}},"validation":{"valid":true}}`})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous task-routing show"), "\n")
	if !strings.Contains(got, "... and 15 more") {
		t.Errorf("long tables must be capped: %q", got)
	}
}

func TestTaskRoutingCommProfileListAndUse(t *testing.T) {
	calls, port := taskRoutingCommFake(t, map[string]string{
		"GET /api/autonomous/task-routing/profiles":       `{"active_profile":"default","profiles":["cheap","default"]}`,
		"PUT /api/autonomous/task-routing/active-profile": `{"active_profile":"cheap"}`,
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous task-routing profile list"), "\n")
	if !strings.Contains(got, "* default") || !strings.Contains(got, "  cheap") {
		t.Errorf("list reply: %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous task-routing profile use cheap"), "\n")
	if !strings.Contains(got, "Active profile is now cheap") {
		t.Errorf("use reply: %q", got)
	}
	last := (*calls)[len(*calls)-1]
	if last.method != "PUT" || last.body != `{"name":"cheap"}` {
		t.Errorf("use sent %+v", last)
	}
}

func TestTaskRoutingCommUseFailureIsReported(t *testing.T) {
	_, port := taskRoutingCommFake(t, map[string]string{})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous task-routing profile use nope"), "\n")
	if !strings.Contains(got, "autonomous task-routing failed") || strings.Contains(got, "Active profile is now") {
		t.Errorf("a refused switch must not claim success: %q", got)
	}
}

func TestTaskRoutingCommAutomatonAndDryRun(t *testing.T) {
	calls, port := taskRoutingCommFake(t, map[string]string{
		"GET /api/autonomous/prds/a1/task-routing": `{"automaton_id":"a1","tasks":[` +
			`{"task_id":"t1","task_type":"docs_set","classified_by":"default","row":"docs_set","applied":true,"effective":{"backend":"pi-prompt","model":"qwen","source":"task"}}]}`,
		"POST /api/autonomous/prds/a1/task-routing/dry-run": `{"automaton_id":"a1","entries":[` +
			`{"task_id":"t1","task_type":"docs_set","classified_by":"default","profile":"cheap","row":"docs_set","would_apply":true,"result":{"worker_backend":"pi-prompt","worker_model":"qwen"}}]}`,
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous task-routing automaton a1"), "\n")
	for _, want := range []string{"Task routing for Automaton a1", "t1 | docs_set | docs_set | pi-prompt/qwen | task | routed"} {
		if !strings.Contains(got, want) {
			t.Errorf("automaton reply lacks %q: %q", want, got)
		}
	}
	got = strings.Join(r.HandleTestMessage("autonomous task-routing dry-run a1 cheap"), "\n")
	if !strings.Contains(got, "Dry run for Automaton a1 (nothing written)") || !strings.Contains(got, "pi-prompt/qwen") {
		t.Errorf("dry-run reply: %q", got)
	}
	if last := (*calls)[len(*calls)-1]; last.body != `{"profile":"cheap"}` {
		t.Errorf("dry-run body = %q", last.body)
	}
}

func TestTaskRoutingCommBadUsageAndHelp(t *testing.T) {
	calls, port := taskRoutingCommFake(t, map[string]string{})
	r := setupPeerRouter(t, port)
	for _, msg := range []string{"autonomous task-routing", "autonomous task-routing bogus",
		"autonomous task-routing automaton", "autonomous task-routing profile", "autonomous task-routing profile use"} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(got, "usage: autonomous task-routing show") {
			t.Errorf("%q: %q", msg, got)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("bad usage must not reach the daemon: %+v", *calls)
	}
	help := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	if !strings.Contains(help, "task-routing [show|profile list|profile use <name>|automaton <id>|dry-run <id> [profile]]") {
		t.Errorf("chat help does not list task-routing: %q", help)
	}
}

// The comm configure verb carries the three task routing keys to the daemon
// unchanged and shows the daemon's reply, including a skipped key.

func taskRoutingConfigureReply(t *testing.T, fn func(key, value string) error, text string) string {
	t.Helper()
	var wg sync.WaitGroup
	var got string
	wg.Add(1)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) {
		got = s
		wg.Done()
	}}}
	r.SetConfigureFunc(fn)
	r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	wg.Wait()
	return got
}

func TestTaskRoutingConfigureVerbWritesAndReadsBack(t *testing.T) {
	block := `{"enabled":true,"active_profile":"lean","profiles":{"lean":{"docs_set":{"worker_backend":"pi-prompt"}}}}`
	store := map[string]string{}
	set := func(k, v string) error {
		if k == "autonomous.task_routing.active_profile" && v == "ghost" {
			return errors.New(k + " not stored (the active profile must name an existing profile)")
		}
		store[k] = v
		return nil
	}
	for _, c := range []struct{ key, value string }{
		{"autonomous.task_routing.enabled", "true"},
		{"autonomous.task_routing.active_profile", "lean"},
		{"autonomous.task_routing", block},
	} {
		reply := taskRoutingConfigureReply(t, set, c.key+"="+c.value)
		if !strings.Contains(reply, "Set "+c.key) {
			t.Errorf("%s: reply %q does not confirm the write", c.key, reply)
		}
		// The key and the whole value (a JSON block holds '=' free text and
		// commas) reach the daemon unchanged.
		if store[c.key] != c.value {
			t.Errorf("%s read back %q, want %q", c.key, store[c.key], c.value)
		}
	}
	// A skipped key is a failure reply, and nothing is stored.
	reply := taskRoutingConfigureReply(t, set, "autonomous.task_routing.active_profile=ghost")
	if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, "not stored") {
		t.Errorf("a skipped profile must be reported as a failure, got %q", reply)
	}
	if store["autonomous.task_routing.active_profile"] != "lean" {
		t.Errorf("a skipped profile was stored: %q", store["autonomous.task_routing.active_profile"])
	}
}
