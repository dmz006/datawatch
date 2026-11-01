package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

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
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
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
		"merge_pipeline_fallback_on_bad_file", "merge_pipeline_conflict_policy", "merge_pipeline_conflict_model", "merge_pipeline_version_files"} {
		if _, ok := props[k]; !ok {
			t.Errorf("autonomous_config_set lacks %s", k)
		}
	}
}
