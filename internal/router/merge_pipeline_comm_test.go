package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const mergePipelineCommView = `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"}]},"source":"default","reason":"no file, no record","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record","default"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":null}`

type mergePipelineCommCall struct {
	method, path, query, body string
	calls                     int
}

// mergePipelineCommDaemon answers every request with status and body and
// records the last request, including its method.
func mergePipelineCommDaemon(t *testing.T, status int, body string) (*mergePipelineCommCall, int) {
	t.Helper()
	rec := &mergePipelineCommCall{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		rec.method, rec.path, rec.query = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		rec.body = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return rec, port
}

func mergePipelineSay(t *testing.T, port int, msg string) string {
	t.Helper()
	return strings.Join(setupPeerRouter(t, port).HandleTestMessage(msg), " ")
}

func TestMergePipelineComm_ShowRendersSharedText(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, mergePipelineCommView)
	got := mergePipelineSay(t, port, "autonomous merge-pipeline")
	if rec.method != http.MethodGet || rec.path != "/api/autonomous/merge-pipeline" || rec.query != "" {
		t.Errorf("sent %s %s?%s", rec.method, rec.path, rec.query)
	}
	for _, want := range []string{"Merge pipeline for demo", "Source: the built-in pipeline", "1. run tests [test] make test", "Merge conflict policy: stop_and_ask"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestMergePipelineComm_ShowVariantsSendProject(t *testing.T) {
	for msg, query := range map[string]string{
		"autonomous merge-pipeline show demo": "project=demo",
		"autonomous merge-pipeline demo":      "project=demo", // a bare word is the project
		"autonomous merge_pipeline show":      "",
	} {
		rec, port := mergePipelineCommDaemon(t, http.StatusOK, mergePipelineCommView)
		mergePipelineSay(t, port, msg)
		if rec.query != query || rec.path != "/api/autonomous/merge-pipeline" || rec.method != http.MethodGet {
			t.Errorf("%q: sent %s %s?%s, want query %q", msg, rec.method, rec.path, rec.query, query)
		}
	}
}

func TestMergePipelineComm_SetRecordSendsPipelineText(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, `{"project":"demo","definition":{"steps":[{"name":"a","kind":"test","command":"make test"}]},"updated_at":"2026-11-01T10:00:00Z"}`)
	got := mergePipelineSay(t, port, `autonomous merge-pipeline set-record project=demo {"steps":[{"name":"a","kind":"test","command":"make test"}]}`)
	if rec.method != http.MethodPut || rec.path != "/api/autonomous/merge-pipeline/record" {
		t.Errorf("sent %s %s", rec.method, rec.path)
	}
	if !strings.Contains(rec.body, `"project":"demo"`) || !strings.Contains(rec.body, `make test`) {
		t.Errorf("body: %s", rec.body)
	}
	if !strings.Contains(got, "Saved the merge pipeline record for demo (1 step).") {
		t.Errorf("reply: %q", got)
	}
}

func TestMergePipelineComm_ClearRecord(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, `{"status":"deleted","project":"demo"}`)
	got := mergePipelineSay(t, port, "autonomous merge-pipeline clear-record demo")
	if rec.method != http.MethodDelete || rec.path != "/api/autonomous/merge-pipeline/record" || rec.query != "project=demo" {
		t.Errorf("sent %s %s?%s", rec.method, rec.path, rec.query)
	}
	if !strings.Contains(got, "Removed the merge pipeline record for demo.") {
		t.Errorf("reply: %q", got)
	}
}

func TestMergePipelineComm_ValidateTextAndFile(t *testing.T) {
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, `{"valid":false,"errors":["step 1: no name"],"source":"body"}`)
	got := mergePipelineSay(t, port, "autonomous merge-pipeline validate steps: [{}]")
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/merge-pipeline/validate" || !strings.Contains(rec.body, `"yaml":"steps: [{}]"`) {
		t.Errorf("sent %s %s %s", rec.method, rec.path, rec.body)
	}
	if !strings.Contains(got, "is not valid (1 problem)") || !strings.Contains(got, "- step 1: no name") {
		t.Errorf("reply: %q", got)
	}

	rec, port = mergePipelineCommDaemon(t, http.StatusOK, `{"valid":true,"errors":[],"source":"file","file":".datawatch/merge-pipeline.yaml"}`)
	got = mergePipelineSay(t, port, "autonomous merge-pipeline validate file")
	if !strings.Contains(rec.body, `"file":true`) || !strings.Contains(got, "The repository pipeline file .datawatch/merge-pipeline.yaml is valid.") {
		t.Errorf("body=%s reply=%q", rec.body, got)
	}
	mergePipelineSay(t, port, "autonomous merge-pipeline validate project=demo file=ci/p.yaml")
	if !strings.Contains(rec.body, `"file":"ci/p.yaml"`) || !strings.Contains(rec.body, `"project":"demo"`) {
		t.Errorf("body=%s", rec.body)
	}
}

func TestMergePipelineComm_BadInputShowsUsageAndSendsNothing(t *testing.T) {
	for _, msg := range []string{
		"autonomous merge-pipeline set-record",
		"autonomous merge-pipeline set-record project=demo",
		"autonomous merge-pipeline set-record project= steps: []",
		"autonomous merge-pipeline validate",
		"autonomous merge-pipeline validate file=",
		"autonomous merge-pipeline show a b",
		"autonomous merge-pipeline clear-record a b",
		"autonomous merge-pipeline set-record " + strings.Repeat("x", mergePipelineCommMaxInput+1),
	} {
		rec, port := mergePipelineCommDaemon(t, http.StatusOK, `{}`)
		got := mergePipelineSay(t, port, msg)
		if rec.calls != 0 {
			t.Errorf("%.60q reached the daemon", msg)
		}
		if !strings.Contains(got, "autonomous merge-pipeline failed") {
			t.Errorf("%.60q: reply %.120q", msg, got)
		}
	}
}

func TestMergePipelineComm_DaemonRefusalAndBadBody(t *testing.T) {
	_, port := mergePipelineCommDaemon(t, http.StatusBadRequest, "bad request: invalid merge pipeline: step 1: no name")
	got := mergePipelineSay(t, port, "autonomous merge-pipeline set-record steps: [{}]")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "step 1: no name") {
		t.Errorf("reply: %q", got)
	}
	_, port = mergePipelineCommDaemon(t, http.StatusOK, "not json")
	if got := mergePipelineSay(t, port, "autonomous merge-pipeline"); !strings.Contains(got, "failed") {
		t.Errorf("an unreadable body must be reported: %q", got)
	}
}

func TestMergePipelineComm_LongReplyIsCut(t *testing.T) {
	text := strings.Repeat("a long line of text\n", 400)
	got := cutMergePipelineChat(text)
	if len(got) > mergePipelineCommMaxChars || !strings.Contains(got, "cut to fit one message") {
		t.Errorf("len=%d tail=%q", len(got), got[len(got)-80:])
	}
	if short := cutMergePipelineChat("short\n"); short != "short" {
		t.Errorf("short text must pass untouched: %q", short)
	}
}

func TestMergePipelineComm_VerbAndConfigureKeysListed(t *testing.T) {
	if v := MergePipelineCommVerbs(); len(v) != 1 || v[0] != "merge-pipeline" {
		t.Errorf("verbs: %v", v)
	}
	keys := MergePipelineConfigureKeys()
	if len(keys) != 20 {
		t.Errorf("keys: %v", keys)
	}
	for _, want := range []string{
		"autonomous.merge_pipeline.release_approval.patch", "autonomous.merge_pipeline.release_approval.minor",
		"autonomous.merge_pipeline.release_approval.major",
		"autonomous.merge_pipeline.post_ship.enabled", "autonomous.merge_pipeline.post_ship.hooks",
	} {
		found := false
		for _, k := range keys {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing key %q: %v", want, keys)
		}
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, "autonomous.merge_pipeline.") {
			t.Errorf("unexpected key %q", k)
		}
	}
}

// releaseApprovalConfigure sends one configure message through handleConfigure
// and returns the reply and the key=value pairs the daemon would be handed.
func releaseApprovalConfigure(t *testing.T, text string) (string, []string) {
	t.Helper()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		sent  string
		calls []string
	)
	// A release approval mode set from chat now reaches the daemon over the
	// REST config endpoint (so the change is recorded as a comm change); the
	// fake daemon records that request body as the key=value pair handed over.
	call, port := mergePipelineCommDaemon(t, http.StatusOK, `{"ok":true}`)
	r := &Router{hostname: "h", webPort: port, backend: &captureBackend{name: "test", capture: func(s string) {
		mu.Lock()
		sent = s
		mu.Unlock()
		wg.Done()
	}}}
	r.SetConfigureFunc(func(k, v string) error {
		calls = append(calls, k+"="+v)
		return nil
	})
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if call.calls > 0 {
		var body map[string]any
		if err := json.Unmarshal([]byte(call.body), &body); err == nil {
			for k, v := range body {
				calls = append(calls, fmt.Sprintf("%s=%v", k, v))
			}
		}
	}
	return sent, calls
}

// TestMergePipelineComm_ReleaseApprovalValidConfigure checks each release size
// takes each approval mode through configure and reaches the daemon unchanged.
func TestMergePipelineComm_ReleaseApprovalValidConfigure(t *testing.T) {
	for _, size := range []string{"patch", "minor", "major"} {
		for _, mode := range []string{"auto", "ask", "manual"} {
			key := "autonomous.merge_pipeline.release_approval." + size
			reply, calls := releaseApprovalConfigure(t, key+"="+mode)
			if len(calls) != 1 || calls[0] != key+"="+mode {
				t.Errorf("%s=%s: daemon handed %v", key, mode, calls)
			}
			if !strings.Contains(reply, "Set "+key+" = "+mode) {
				t.Errorf("%s=%s: reply %q", key, mode, reply)
			}
		}
	}
}

// TestMergePipelineComm_ReleaseApprovalRefusesInvalid checks a value that is
// not auto, ask or manual is refused in chat, naming the key, and never sent.
func TestMergePipelineComm_ReleaseApprovalRefusesInvalid(t *testing.T) {
	for _, text := range []string{
		"autonomous.merge_pipeline.release_approval.patch=fast",
		"autonomous.merge_pipeline.release_approval.minor=Auto",
		"autonomous.merge_pipeline.release_approval.major=yes",
		"autonomous.merge_pipeline.projects.demo.release_approval.major=never",
	} {
		reply, calls := releaseApprovalConfigure(t, text)
		if len(calls) != 0 {
			t.Errorf("%s reached the daemon: %v", text, calls)
		}
		key := text[:strings.Index(text, "=")]
		if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, key) ||
			!strings.Contains(reply, `use "auto", "ask" or "manual"`) {
			t.Errorf("%s: reply %q", text, reply)
		}
	}
}

// TestMergePipelineComm_ReleaseApprovalPerProjectConfigure checks the
// per-project form takes a valid mode for each size, including a project name
// that contains dots.
func TestMergePipelineComm_ReleaseApprovalPerProjectConfigure(t *testing.T) {
	for _, c := range []struct{ key, mode string }{
		{"autonomous.merge_pipeline.projects.demo.release_approval.patch", "manual"},
		{"autonomous.merge_pipeline.projects.demo.release_approval.minor", "ask"},
		{"autonomous.merge_pipeline.projects.demo.release_approval.major", "auto"},
		{"autonomous.merge_pipeline.projects.my.app.release_approval.minor", "ask"},
	} {
		reply, calls := releaseApprovalConfigure(t, c.key+"="+c.mode)
		if len(calls) != 1 || calls[0] != c.key+"="+c.mode {
			t.Errorf("%s: daemon handed %v", c.key, calls)
		}
		if !strings.Contains(reply, "Set "+c.key+" = "+c.mode) {
			t.Errorf("%s: reply %q", c.key, reply)
		}
	}
}

// TestMergePipelineComm_ReleaseApprovalShowReply checks the show reply carries
// the Release approval line with the modes in effect for the project.
func TestMergePipelineComm_ReleaseApprovalShowReply(t *testing.T) {
	body := strings.Replace(mergePipelineCommView, `"overrides":null}`,
		`"overrides":null,"release_approval_in_effect":{"patch":"auto","minor":"ask","major":"manual"}}`, 1)
	rec, port := mergePipelineCommDaemon(t, http.StatusOK, body)
	got := mergePipelineSay(t, port, "autonomous merge-pipeline demo")
	if rec.query != "project=demo" {
		t.Errorf("sent query %q", rec.query)
	}
	if !strings.Contains(got, "Release approval: patch auto, minor ask, major manual") {
		t.Errorf("reply lacks the release approval line: %q", got)
	}
}
