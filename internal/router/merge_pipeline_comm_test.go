package router

import (
	"io"
	"net/http"
	"strings"
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
	if len(keys) != 17 {
		t.Errorf("keys: %v", keys)
	}
	for _, want := range []string{"autonomous.merge_pipeline.post_ship.enabled", "autonomous.merge_pipeline.post_ship.hooks"} {
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
