package router

// The post-ship hook settings in the chat channel: configure takes the seven
// global keys (post_ship.hooks as its JSON text), the help and list texts
// name them and say they have no per-project form, and
// "autonomous merge-pipeline" shows whether the block is on, how many hooks
// are defined, and which would run.

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestMergePipelinePostShipComm_ConfigureSendsKeyAndJSONText(t *testing.T) {
	var wg sync.WaitGroup
	captured := func(s string) { wg.Done() }
	var got map[string]string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: captured}}
	got = map[string]string{}
	r.SetConfigureFunc(func(k, v string) error { got[k] = v; return nil })

	hooksText := `[{"name":"registry row","builtin":"registry_row","enabled":true},{"name":"ping","command":"curl -s http://localhost/ping"}]`
	for _, kv := range []string{
		"autonomous.merge_pipeline.post_ship.enabled=true",
		"autonomous.merge_pipeline.post_ship.registry_file=docs/registry.md",
		"autonomous.merge_pipeline.post_ship.registry_row_pattern=| {project} | shipped |",
		"autonomous.merge_pipeline.post_ship.companion_repository=dmz006/companion",
		"autonomous.merge_pipeline.post_ship.issue_title_template=Parity: {project} {version}",
		"autonomous.merge_pipeline.post_ship.notify_template=Shipped {version} of {project}.",
		"autonomous.merge_pipeline.post_ship.hooks=" + hooksText,
	} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: kv})
		wg.Wait()
	}
	if got["autonomous.merge_pipeline.post_ship.enabled"] != "true" ||
		got["autonomous.merge_pipeline.post_ship.registry_file"] != "docs/registry.md" ||
		got["autonomous.merge_pipeline.post_ship.registry_row_pattern"] != "| {project} | shipped |" ||
		got["autonomous.merge_pipeline.post_ship.companion_repository"] != "dmz006/companion" ||
		got["autonomous.merge_pipeline.post_ship.issue_title_template"] != "Parity: {project} {version}" ||
		got["autonomous.merge_pipeline.post_ship.notify_template"] != "Shipped {version} of {project}." {
		t.Fatalf("configure: %+v", got)
	}
	if got["autonomous.merge_pipeline.post_ship.hooks"] != hooksText {
		t.Fatalf("hooks value: %q", got["autonomous.merge_pipeline.post_ship.hooks"])
	}
}

// The help and list texts must name the post-ship keys and say the block is
// global, because the per-project form is refused.
func TestMergePipelinePostShipComm_HelpAndListMentionKeys(t *testing.T) {
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
		for _, want := range []string{
			"autonomous.merge_pipeline.post_ship.enabled",
			"autonomous.merge_pipeline.post_ship.registry_file",
			"autonomous.merge_pipeline.post_ship.registry_row_pattern",
			"autonomous.merge_pipeline.post_ship.companion_repository",
			"autonomous.merge_pipeline.post_ship.issue_title_template",
			"autonomous.merge_pipeline.post_ship.notify_template",
			"autonomous.merge_pipeline.post_ship.hooks",
			"global for all projects",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("reply %d lacks %q", i, want)
			}
		}
	}
}

func TestMergePipelinePostShipComm_ShowStates(t *testing.T) {
	for name, c := range map[string]struct {
		body string
		want string
	}{
		"off, none defined": {
			`"post_ship":{"enabled":false,"hook_count":0,"enabled_hooks":null}`,
			"Post-ship hooks: off (none defined)",
		},
		"off, defined": {
			`"post_ship":{"enabled":false,"hook_count":2,"enabled_hooks":["row","parity"]}`,
			"Post-ship hooks: off (2 defined)",
		},
		"on, some run": {
			`"post_ship":{"enabled":true,"hook_count":3,"enabled_hooks":["registry row","parity"]}`,
			"Post-ship hooks: on, 2 of 3 defined would run: registry row, parity",
		},
		"on, none run": {
			`"post_ship":{"enabled":true,"hook_count":1,"enabled_hooks":[]}`,
			"Post-ship hooks: on, 1 defined, none to run",
		},
	} {
		rec, port := mergePipelineCommDaemon(t, http.StatusOK,
			`{"project":"demo","definition":{"steps":[{"name":"a","kind":"test","command":"true"}]},"source":"default","reason":"built-in","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]}`+","+c.body+`}`)
		if rec == nil {
			t.Fatal("daemon")
		}
		got := mergePipelineSay(t, port, "autonomous merge-pipeline")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: reply lacks %q:\n%s", name, c.want, got)
		}
	}
}
