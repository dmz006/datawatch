package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const mergePipelineViewBody = `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"}]},"source":"default","reason":"no file, no record","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record","default"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":null}`

func TestMergePipelineMCP_ToolsRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range MergePipelineToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestMergePipelineMCP_Capabilities(t *testing.T) {
	want := map[string]string{
		"autonomous_merge_pipeline_get":        federation.CapAutonomousRead,
		"autonomous_merge_pipeline_validate":   federation.CapAutonomousRead,
		"autonomous_merge_pipeline_set_record": federation.CapAutonomousWrite,
	}
	for tool, c := range want {
		if got, ok := federation.RequiredCapForMCPTool(tool); !ok || got != c {
			t.Errorf("%s: cap %q (known %v), want %q", tool, got, ok, c)
		}
	}
}

func TestMergePipelineMCP_ReadOnlyHints(t *testing.T) {
	s := &Server{}
	if h := s.toolAutonomousMergePipelineGet().Annotations.ReadOnlyHint; h == nil || !*h {
		t.Error("get must be read-only")
	}
	if h := s.toolAutonomousMergePipelineValidate().Annotations.ReadOnlyHint; h == nil || !*h {
		t.Error("validate must be read-only")
	}
	if h := s.toolAutonomousMergePipelineSetRecord().Annotations.ReadOnlyHint; h != nil && *h {
		t.Error("set_record writes and must not be marked read-only")
	}
}

func TestMergePipelineMCP_GetText(t *testing.T) {
	var path, query string
	first := true
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if first {
			path, query, first = r.URL.Path, r.URL.RawQuery, false
		}
		_, _ = w.Write([]byte(mergePipelineViewBody))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": " demo "}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if path != mergePipelinePath || query != "project=demo" {
		t.Errorf("proxied %s?%s", path, query)
	}
	text := automationText(t, res)
	for _, want := range []string{"Merge pipeline for demo", "built-in pipeline", "run tests", "make test"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q: %s", want, text)
		}
	}
	// the fixed body answers /api/config too, but carries no settings block,
	// so no hook detail may appear
	if strings.Contains(text, "Built-in targets:") || strings.Contains(text, "built-in registry_row") {
		t.Errorf("no hook detail without a settings block:\n%s", text)
	}
}

func TestMergePipelineMCP_GetJSONAndBadFormat(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mergePipelineViewBody))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"format": "json"}))
	if got := automationText(t, res); got != mergePipelineViewBody {
		t.Errorf("json format must pass the body untouched: %s", got)
	}
	for _, bad := range []any{"yaml", 3} {
		res, _ = s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"format": bad}))
		if res == nil || !res.IsError {
			t.Errorf("format %v must be refused", bad)
		}
	}
}

const mergePipelinePostShipConfigBody = `{"hostname":"h","autonomous":{"merge_pipeline":{"file_name":".datawatch/merge-pipeline.yaml","post_ship":{"enabled":true,"hooks":[{"name":"registry_row","builtin":"registry_row","enabled":true},{"name":"notify","command":"pw send","enabled":false}],"registry_file":".datawatch/registry.yaml","registry_row_pattern":"| {project} | {version} |","companion_repository":"acme/widgets","issue_title_template":"parity: {project} {version}","notify_template":"shipped {project} {version}"}}}}`

func TestMergePipelineMCP_GetSurfacesPostShip(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config" {
			_, _ = w.Write([]byte(mergePipelinePostShipConfigBody))
			return
		}
		_, _ = w.Write([]byte(mergePipelineViewBody))
	})
	res, err := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	for _, want := range []string{ // every hook, as defined, plus the built-in targets
		"Merge pipeline for demo",
		"Post-ship hooks: on",
		"1. registry_row: the built-in registry_row, would run",
		"2. notify: the command pw send, skipped (disabled)",
		"Built-in targets:",
		"registry file: .datawatch/registry.yaml",
		"registry row pattern: | {project} | {version} |",
		"companion repository: acme/widgets",
		"issue title template: parity: {project} {version}",
		"notify template: shipped {project} {version}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "Post-ship hooks:"); n != 1 {
		t.Errorf("the post-ship switch line is written %d times:\n%s", n, text)
	}

	// In json format the block stays under post_ship, beside the pipeline.
	res, err = s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"format": "json", "project": "demo"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(automationText(t, res)), &obj); err != nil {
		t.Fatal(err)
	}
	ps, ok := obj["post_ship"].(map[string]any)
	if !ok {
		t.Fatalf("the json reply lacks post_ship: %v", obj)
	}
	hooks, ok := ps["hooks"].([]any)
	if !ok || len(hooks) != 2 {
		t.Errorf("hooks: %v", ps["hooks"])
	}
	if ps["enabled"] != true || ps["companion_repository"] != "acme/widgets" || !strings.Contains(automationText(t, res), "make test") {
		t.Errorf("json values: %v", ps)
	}
}

func TestMergePipelineMCP_GetPostShipLookupFailsStillAnswers(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config" {
			http.Error(w, "settings gone", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(mergePipelineViewBody))
	})
	res, err := s.handleAutonomousMergePipelineGet(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("a failing settings lookup must not sink the read: err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "Merge pipeline for ") || strings.Contains(text, "Built-in targets:") {
		t.Errorf("the read must stand on its own:\n%s", text)
	}
}

func TestMergePipelineMCP_GetDaemonErrorAndBadBody(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request: project is empty", http.StatusBadRequest)
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(nil))
	if res == nil || !res.IsError || !strings.Contains(automationText(t, res), "project is empty") {
		t.Fatalf("daemon refusal must reach the caller: %+v", res)
	}
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`not json`)) })
	res, _ = s.handleAutonomousMergePipelineGet(context.Background(), callReq(nil))
	if res == nil || !res.IsError {
		t.Fatal("an unreadable body must be an error result")
	}
}

func TestMergePipelineMCP_GetProjectRequired(t *testing.T) {
	const body = `{"code":"project_required","error":"Choose a project with the project parameter, or set the merge base repository (the worktree_base_repo setting)."}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(nil))
	if res == nil || !res.IsError {
		t.Fatalf("res=%+v", res)
	}
	text := automationText(t, res)
	for _, want := range []string{
		body,
		"project_required",
		"a project is needed, pass the project argument or set the merge base repository",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}

	// A plain-text 400 keeps today's error text and gets no plain line.
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request: project is empty", http.StatusBadRequest)
	})
	res, _ = s.handleAutonomousMergePipelineGet(context.Background(), callReq(nil))
	if res == nil || !res.IsError {
		t.Fatal("a plain-text refusal must be an error result")
	}
	text = automationText(t, res)
	if !strings.Contains(text, "HTTP 400") || !strings.Contains(text, "project is empty") {
		t.Errorf("plain 400 must keep the old text: %s", text)
	}
	if strings.Contains(text, "a project is needed, pass the project argument") {
		t.Errorf("plain 400 must not get the plain line: %s", text)
	}

	// A 400 with a JSON body of another code keeps today's error text too.
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"not_found","error":"no such project"}`))
	})
	res, _ = s.handleAutonomousMergePipelineGet(context.Background(), callReq(nil))
	if res == nil || !res.IsError {
		t.Fatal("a structured refusal of another code must be an error result")
	}
	text = automationText(t, res)
	if !strings.Contains(text, "no such project") || strings.Contains(text, "a project is needed, pass the project argument") {
		t.Errorf("another code must keep the old text: %s", text)
	}
}

func TestMergePipelineMCP_SetRecordSendsPut(t *testing.T) {
	var method, path string
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"project":"demo","definition":{"steps":[{"name":"a","kind":"test","command":"make test"}]},"updated_at":"2026-11-01T10:00:00Z"}`))
	})
	res, _ := s.handleAutonomousMergePipelineSetRecord(context.Background(), callReq(map[string]any{
		"project": "demo", "yaml": "steps:\n  - name: a\n    kind: test\n    command: make test\n",
	}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if method != http.MethodPut || path != mergePipelineRecordPath || got["project"] != "demo" || got["yaml"] == nil {
		t.Errorf("sent %s %s %v", method, path, got)
	}
	if text := automationText(t, res); !strings.Contains(text, "Saved the merge pipeline record for demo") {
		t.Errorf("text: %s", text)
	}

	// A definition object is sent as an object.
	res, _ = s.handleAutonomousMergePipelineSetRecord(context.Background(), callReq(map[string]any{
		"definition": map[string]any{"steps": []any{map[string]any{"name": "a"}}},
	}))
	if res.IsError {
		t.Fatalf("definition: %s", automationText(t, res))
	}
	if _, ok := got["definition"].(map[string]any); !ok {
		t.Errorf("definition was not sent as an object: %v", got)
	}
}

func TestMergePipelineMCP_SetRecordBadInput(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	for name, args := range map[string]map[string]any{
		"none":              {"project": "demo"},
		"both":              {"yaml": "steps: []", "definition": map[string]any{"steps": []any{}}},
		"definition text":   {"definition": "steps"},
		"clear with yaml":   {"clear": true, "yaml": "steps: []"},
		"clear with object": {"clear": true, "definition": map[string]any{}},
		"bad format":        {"yaml": "steps: []", "format": "xml"},
	} {
		res, _ := s.handleAutonomousMergePipelineSetRecord(context.Background(), callReq(args))
		if res == nil || !res.IsError {
			t.Errorf("%s: must be refused", name)
		}
	}
	if called {
		t.Error("a bad request must not reach the daemon")
	}
}

func TestMergePipelineMCP_SetRecordDaemonRefusal(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request: invalid merge pipeline: step 1: no name", http.StatusBadRequest)
	})
	res, _ := s.handleAutonomousMergePipelineSetRecord(context.Background(), callReq(map[string]any{"yaml": "steps: [{}]"}))
	if res == nil || !res.IsError || !strings.Contains(automationText(t, res), "no name") {
		t.Fatalf("the daemon's problem list must reach the caller: %+v", res)
	}
}

func TestMergePipelineMCP_ClearSendsDelete(t *testing.T) {
	var method, path, query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"status":"deleted","project":"demo"}`))
	})
	res, _ := s.handleAutonomousMergePipelineSetRecord(context.Background(), callReq(map[string]any{"clear": true, "project": "demo"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if method != http.MethodDelete || path != mergePipelineRecordPath || query != "project=demo" {
		t.Errorf("sent %s %s?%s", method, path, query)
	}
	if text := automationText(t, res); !strings.Contains(text, "Removed the merge pipeline record for demo") {
		t.Errorf("text: %s", text)
	}
}

func TestMergePipelineMCP_ValidateModes(t *testing.T) {
	var got map[string]any
	var method, path string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"valid":false,"errors":["step 1: no name","step 2: unknown kind"],"source":"body"}`))
	})
	res, _ := s.handleAutonomousMergePipelineValidate(context.Background(), callReq(map[string]any{"yaml": "steps: [{}]"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if method != http.MethodPost || path != mergePipelineValidatePath {
		t.Errorf("sent %s %s", method, path)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "is not valid (2 problems)") || !strings.Contains(text, "- step 2: unknown kind") {
		t.Errorf("text: %s", text)
	}

	for in, want := range map[any]any{true: true, "true": true, "ci/pipeline.yaml": "ci/pipeline.yaml"} {
		_, _ = s.handleAutonomousMergePipelineValidate(context.Background(), callReq(map[string]any{"file": in}))
		if got["file"] != want {
			t.Errorf("file %v: sent %v, want %v", in, got["file"], want)
		}
	}
	_, _ = s.handleAutonomousMergePipelineValidate(context.Background(), callReq(map[string]any{"file": false, "yaml": "steps: []"}))
	if _, has := got["file"]; has {
		t.Errorf("file=false must not be sent: %v", got)
	}
}

func TestMergePipelineMCP_ValidateBadInput(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	for name, args := range map[string]map[string]any{
		"empty file":  {"file": "  "},
		"number file": {"file": 3},
		"both":        {"yaml": "steps: []", "definition": map[string]any{}},
		"bad format":  {"yaml": "steps: []", "format": "x"},
	} {
		res, _ := s.handleAutonomousMergePipelineValidate(context.Background(), callReq(args))
		if res == nil || !res.IsError {
			t.Errorf("%s: must be refused", name)
		}
	}
	if called {
		t.Error("a bad request must not reach the daemon")
	}
}

func TestMergePipelineMCP_ConfigArgs(t *testing.T) {
	got, err := mergePipelineConfigArgs(callReq(map[string]any{
		"merge_pipeline_file_name":      "ci/p.yaml",
		"merge_pipeline_source_order":   []any{"record", "default"},
		"merge_pipeline_conflict_model": "",
		"merge_pipeline_version_files":  []any{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"autonomous.merge_pipeline.file_name":      "ci/p.yaml",
		"autonomous.merge_pipeline.conflict_model": "",
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if l, ok := got["autonomous.merge_pipeline.version_files"].([]any); !ok || len(l) != 0 {
		t.Errorf("an explicit empty list must be sent as given: %v", got)
	}
	if len(got) != 4 {
		t.Errorf("unexpected entries: %v", got)
	}

	got, err = mergePipelineConfigArgs(callReq(map[string]any{"merge_pipeline_project": "demo", "merge_pipeline_conflict_policy": "stop_and_ask"}))
	if err != nil || got["autonomous.merge_pipeline.projects.demo.conflict_policy"] != "stop_and_ask" || len(got) != 1 {
		t.Errorf("project scoping: %v %v", got, err)
	}
	if got, err = mergePipelineConfigArgs(callReq(nil)); err != nil || len(got) != 0 {
		t.Errorf("no arguments must send nothing: %v %v", got, err)
	}
}

func TestMergePipelineMCP_ConfigArgsBad(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"project only":   {"merge_pipeline_project": "demo"},
		"empty project":  {"merge_pipeline_project": " ", "merge_pipeline_file_name": "x"},
		"number project": {"merge_pipeline_project": 4, "merge_pipeline_file_name": "x"},
	} {
		if _, err := mergePipelineConfigArgs(callReq(args)); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
}

func TestMergePipelineMCP_ConfigSetSendsKeys(t *testing.T) {
	var got map[string]any
	var method, path string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	res, _ := s.handleAutonomousConfigSet(context.Background(), mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{
		Arguments: map[string]any{"merge_pipeline_file_name": "ci/p.yaml"},
	}})
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if method != http.MethodPut || path != "/api/config" || got["autonomous.merge_pipeline.file_name"] != "ci/p.yaml" {
		t.Errorf("sent %s %s %v", method, path, got)
	}
	res, _ = s.handleAutonomousConfigSet(context.Background(), mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{
		Arguments: map[string]any{"merge_pipeline_project": "demo"},
	}})
	if res == nil || !res.IsError {
		t.Error("a project with no setting must be refused")
	}
}

func TestMergePipelineMCP_ConfigSetToolHasArguments(t *testing.T) {
	props := (&Server{}).toolAutonomousConfigSet().InputSchema.Properties
	for _, k := range []string{"merge_pipeline_project", "merge_pipeline_file_name", "merge_pipeline_source_order",
		"merge_pipeline_fallback_on_bad_file", "merge_pipeline_conflict_policy", "merge_pipeline_conflict_model", "merge_pipeline_version_files",
		"merge_pipeline_release_approval_patch", "merge_pipeline_release_approval_minor", "merge_pipeline_release_approval_major"} {
		if _, ok := props[k]; !ok {
			t.Errorf("autonomous_config_set lacks %s", k)
		}
	}
}

// mergePipelineViewReleaseBody is the view with the release approval mode in
// effect for each size, as the daemon reports it for a project.
const mergePipelineViewReleaseBody = `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"}]},"source":"default","reason":"no file, no record","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record","default"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":null,"release_approval_in_effect":{"patch":"auto","minor":"ask","major":"manual"}}`

// checkSentKeys fails the test for each want key whose sent value differs.
func checkSentKeys(t *testing.T, sent, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if sent[k] != v {
			t.Errorf("%s = %v, want %v (sent %v)", k, sent[k], v, sent)
		}
	}
}

func TestMergePipelineMCP_ReleaseApprovalValidSet(t *testing.T) {
	args := map[string]any{
		"merge_pipeline_release_approval_patch": "auto",
		"merge_pipeline_release_approval_minor": "ask",
		"merge_pipeline_release_approval_major": "manual",
	}
	want := map[string]any{
		"autonomous.merge_pipeline.release_approval.patch": "auto",
		"autonomous.merge_pipeline.release_approval.minor": "ask",
		"autonomous.merge_pipeline.release_approval.major": "manual",
	}
	got, err := mergePipelineConfigArgs(callReq(args))
	if err != nil {
		t.Fatal(err)
	}
	checkSentKeys(t, got, want)
	if len(got) != len(want) {
		t.Errorf("unexpected entries: %v", got)
	}

	// Through autonomous_config_set the three keys reach the one config PUT.
	var sent map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sent)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	res, _ := s.handleAutonomousConfigSet(context.Background(), mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: args}})
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	checkSentKeys(t, sent, want)
}

func TestMergePipelineMCP_ReleaseApprovalRefusesInvalid(t *testing.T) {
	cases := []struct {
		arg, key string
		value    any
	}{
		{"merge_pipeline_release_approval_minor", "autonomous.merge_pipeline.release_approval.minor", "sometimes"},
		{"merge_pipeline_release_approval_patch", "autonomous.merge_pipeline.release_approval.patch", 3},
		{"merge_pipeline_release_approval_major", "autonomous.merge_pipeline.release_approval.major", "Ask"},
	}
	for _, c := range cases {
		_, err := mergePipelineConfigArgs(callReq(map[string]any{c.arg: c.value}))
		if err == nil {
			t.Errorf("%s=%v must be refused", c.arg, c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.key) || !strings.Contains(err.Error(), "auto") {
			t.Errorf("refusal must name %s and the valid modes: %v", c.key, err)
		}
	}

	// Through autonomous_config_set a bad mode is refused before any request.
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	res, _ := s.handleAutonomousConfigSet(context.Background(), mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{
		Arguments: map[string]any{"merge_pipeline_release_approval_major": "yes"},
	}})
	if res == nil || !res.IsError {
		t.Fatal("a bad release approval mode must be an error result")
	}
	if text := automationText(t, res); !strings.Contains(text, "autonomous.merge_pipeline.release_approval.major") {
		t.Errorf("the refusal must name the key: %s", text)
	}
	if called {
		t.Error("a refused mode must not reach the daemon")
	}
}

func TestMergePipelineMCP_ReleaseApprovalProjectSet(t *testing.T) {
	args := map[string]any{"merge_pipeline_project": "demo", "merge_pipeline_release_approval_major": "manual"}
	got, err := mergePipelineConfigArgs(callReq(args))
	if err != nil {
		t.Fatal(err)
	}
	checkSentKeys(t, got, map[string]any{"autonomous.merge_pipeline.projects.demo.release_approval.major": "manual"})
	if len(got) != 1 {
		t.Errorf("a project set sends one key: %v", got)
	}

	// The project name is part of the refusal's key.
	_, err = mergePipelineConfigArgs(callReq(map[string]any{"merge_pipeline_project": "demo", "merge_pipeline_release_approval_major": "sometimes"}))
	if err == nil || !strings.Contains(err.Error(), "autonomous.merge_pipeline.projects.demo.release_approval.major") {
		t.Errorf("a bad per-project mode must name the project key: %v", err)
	}

	var sent map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sent)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	res, _ := s.handleAutonomousConfigSet(context.Background(), mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: args}})
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	checkSentKeys(t, sent, map[string]any{"autonomous.merge_pipeline.projects.demo.release_approval.major": "manual"})
	if _, global := sent["autonomous.merge_pipeline.release_approval.major"]; global {
		t.Errorf("a project set must not write the global mode: %v", sent)
	}
}

func TestMergePipelineMCP_ReleaseApprovalGetEachSize(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mergePipelineViewReleaseBody))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "Release approval: patch auto, minor ask, major manual") {
		t.Errorf("the text must show the mode in effect for each size:\n%s", text)
	}

	res, _ = s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo", "format": "json"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(automationText(t, res)), &obj); err != nil {
		t.Fatal(err)
	}
	rea, ok := obj["release_approval_in_effect"].(map[string]any)
	if !ok {
		t.Fatalf("the json reply lacks release_approval_in_effect: %v", obj)
	}
	for size, want := range map[string]string{"patch": "auto", "minor": "ask", "major": "manual"} {
		if rea[size] != want {
			t.Errorf("release_approval_in_effect.%s = %v, want %s", size, rea[size], want)
		}
	}
}

func TestMergePipelineMCP_ReleaseApprovalConfigSetRouting(t *testing.T) {
	var mu sync.Mutex
	stored := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// The daemon's check for the release approval keys: text, and one of
		// the three modes, with the key named in the refusal.
		for k, v := range body {
			if !strings.HasPrefix(k, "autonomous.merge_pipeline.release_approval.") {
				continue
			}
			mode, isStr := v.(string)
			if !isStr {
				http.Error(w, k+": not text", http.StatusBadRequest)
				return
			}
			if err := config.ValidateReleaseApprovalModeKey(k, mode); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		mu.Lock()
		for k, v := range body {
			stored[k] = v
		}
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	s := &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: true}}
	ctx := context.Background()
	set := func(key, value string) *mcpsdk.CallToolResult {
		t.Helper()
		res, err := s.handleConfigSet(ctx, mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{
			Arguments: map[string]any{"key": key, "value": value},
		}})
		if err != nil || res == nil {
			t.Fatalf("config_set %s=%s: err=%v res=%+v", key, value, err, res)
		}
		return res
	}

	const patch = "autonomous.merge_pipeline.release_approval.patch"
	if res := set(patch, "ask"); res.IsError {
		t.Fatalf("a valid mode must be accepted: %s", automationText(t, res))
	}
	mu.Lock()
	if stored[patch] != "ask" {
		t.Errorf("config_set stored %v for %s, want ask", stored[patch], patch)
	}
	mu.Unlock()

	// A bad mode is refused with the key named and nothing is stored.
	if text := automationText(t, set(patch, "bogus")); !strings.Contains(text, patch) {
		t.Errorf("a refused mode must name the key: %s", text)
	}
	mu.Lock()
	if stored[patch] != "ask" {
		t.Errorf("a refused mode must not change the stored value: %v", stored[patch])
	}
	mu.Unlock()
}

func TestMergePipelineMCP_ReleaseApprovalConfigSchema(t *testing.T) {
	props := (&Server{}).toolAutonomousConfigSet().InputSchema.Properties
	for size, def := range map[string]string{"patch": "auto", "minor": "auto", "major": "ask"} {
		prop, ok := props["merge_pipeline_release_approval_"+size].(map[string]any)
		if !ok {
			t.Fatalf("autonomous_config_set lacks merge_pipeline_release_approval_%s", size)
		}
		desc, _ := prop["description"].(string)
		for _, want := range []string{"autonomous.merge_pipeline.release_approval." + size, "auto", "ask", "manual", "Default " + def + "."} {
			if !strings.Contains(desc, want) {
				t.Errorf("%s description lacks %q: %s", size, want, desc)
			}
		}
	}
}

const releaseReadinessMCPBody = `{"size":"minor","project":"demo","readiness":{"size":"minor","ready":false},"bar":{"records":10,"conflicts":2,"pwa_changes":2}}`

func TestReleaseReadinessMCP_ToolRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	if !have["autonomous_release_readiness"] {
		t.Fatal("autonomous_release_readiness is not registered")
	}
	s := &Server{}
	tool := s.toolAutonomousReleaseReadiness()
	if h := tool.Annotations.ReadOnlyHint; h == nil || !*h {
		t.Error("autonomous_release_readiness must be read-only")
	}
	if got, ok := federation.RequiredCapForMCPTool("autonomous_release_readiness"); !ok || got != federation.CapAutonomousRead {
		t.Errorf("cap %q (known %v), want %q", got, ok, federation.CapAutonomousRead)
	}
}

func TestReleaseReadinessMCP_ValidSizeProxiesBody(t *testing.T) {
	var path, query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(releaseReadinessMCPBody))
	})
	res, _ := s.handleAutonomousReleaseReadiness(context.Background(), callReq(map[string]any{"size": " Minor ", "project": " demo "}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if path != releaseReadinessPath || query != "project=demo&size=minor" {
		t.Errorf("proxied %s?%s", path, query)
	}
	if got := automationText(t, res); got != releaseReadinessMCPBody {
		t.Errorf("body must pass through untouched: %s", got)
	}
}

func TestReleaseReadinessMCP_InvalidSizeRefused(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(releaseReadinessMCPBody))
	})
	for _, bad := range []any{"huge", "", nil, 3} {
		res, _ := s.handleAutonomousReleaseReadiness(context.Background(), callReq(map[string]any{"size": bad}))
		if res == nil || !res.IsError {
			t.Errorf("size %v must be refused", bad)
		}
	}
	if called {
		t.Error("an invalid size must not reach the daemon")
	}
}
