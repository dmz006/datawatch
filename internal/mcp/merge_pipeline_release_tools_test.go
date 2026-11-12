package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The release settings of the merge pipeline (release size by kind, major
// trigger, release cadence, version scheme) on the MCP surfaces: the
// autonomous_config_set arguments, the autonomous_merge_pipeline_get text and
// json, and the daemon's refusal of a value that is not available yet.

// mergePipelineReleaseViewBody is the GET /api/autonomous/merge-pipeline body
// for project demo with the release settings in effect (the daemon's
// effective values, as its view reports them).
const mergePipelineReleaseViewBody = `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"}]},"source":"default","reason":"no file, no record","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record","default"],"fallback_on_bad_file":"record","version_files":[],"release_size_map":{"bug":"patch","feature":"minor","security":"major"},"major_trigger":"milestone","release_cadence":"per_merge","version_scheme":"semver"},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":null}`

func TestMergePipelineReleaseMCP_ConfigSetSchemaHasArgs(t *testing.T) {
	s := &Server{}
	props := s.toolAutonomousConfigSet().InputSchema.Properties
	for _, name := range []string{
		"merge_pipeline_project",
		"merge_pipeline_release_size_map",
		"merge_pipeline_major_trigger",
		"merge_pipeline_release_cadence",
		"merge_pipeline_version_scheme",
	} {
		if _, ok := props[name]; !ok {
			t.Errorf("autonomous_config_set lacks argument %s", name)
		}
	}
}

func TestMergePipelineReleaseMCP_ConfigArgsMapToKeys(t *testing.T) {
	got, err := mergePipelineConfigArgs(callReq(map[string]any{
		"merge_pipeline_release_size_map": "feature=minor,bug=patch",
		"merge_pipeline_major_trigger":    "milestone",
		"merge_pipeline_release_cadence":  "per_merge",
		"merge_pipeline_version_scheme":   "semver",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"autonomous.merge_pipeline.release_size_map": "feature=minor,bug=patch",
		"autonomous.merge_pipeline.major_trigger":    "milestone",
		"autonomous.merge_pipeline.release_cadence":  "per_merge",
		"autonomous.merge_pipeline.version_scheme":   "semver",
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
}

func TestMergePipelineReleaseMCP_ConfigArgsWithProject(t *testing.T) {
	got, err := mergePipelineConfigArgs(callReq(map[string]any{
		"merge_pipeline_project":          " demo ",
		"merge_pipeline_release_size_map": map[string]any{"bug": "minor"},
		"merge_pipeline_major_trigger":    "never",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got["autonomous.merge_pipeline.projects.demo.major_trigger"] != "never" {
		t.Errorf("project major_trigger missing: %v", got)
	}
	if m, ok := got["autonomous.merge_pipeline.projects.demo.release_size_map"].(map[string]any); !ok || m["bug"] != "minor" {
		t.Errorf("project release_size_map must be sent as given: %v", got)
	}
	if _, has := got["autonomous.merge_pipeline.major_trigger"]; has {
		t.Errorf("a project-scoped value must not reach the global key: %v", got)
	}
}

func TestMergePipelineReleaseMCP_ConfigSetSendsDottedKeys(t *testing.T) {
	var path, method string
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"merge_pipeline_project":          "demo",
		"merge_pipeline_release_size_map": "bug=patch",
		"merge_pipeline_major_trigger":    "milestone",
		"merge_pipeline_release_cadence":  "per_merge",
		"merge_pipeline_version_scheme":   "semver",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPut || path != "/api/config" {
		t.Errorf("sent %s %s, want PUT /api/config", method, path)
	}
	for k, want := range map[string]any{
		"autonomous.merge_pipeline.projects.demo.release_size_map": "bug=patch",
		"autonomous.merge_pipeline.projects.demo.major_trigger":    "milestone",
		"autonomous.merge_pipeline.projects.demo.release_cadence":  "per_merge",
		"autonomous.merge_pipeline.projects.demo.version_scheme":   "semver",
	} {
		if got[k] != want {
			t.Errorf("body[%s] = %v, want %v (body %v)", k, got[k], want, got)
		}
	}
}

// The daemon refuses a cadence or scheme that is not available yet. The
// refusal must reach the caller as a tool error with the daemon's message,
// not as a protocol error.
func TestMergePipelineReleaseMCP_CadenceAndSchemeRefusalIsToolError(t *testing.T) {
	const cadenceRefusal = `autonomous.merge_pipeline.release_cadence: "batch" is invalid; use "per_merge"`
	const schemeRefusal = `autonomous.merge_pipeline.version_scheme: "calver" is not available yet; use "semver"`
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"cadence batch", map[string]any{"merge_pipeline_release_cadence": "batch"}, cadenceRefusal},
		{"scheme calver", map[string]any{"merge_pipeline_version_scheme": "calver"}, schemeRefusal},
		{"cadence with project", map[string]any{"merge_pipeline_project": "demo", "merge_pipeline_release_cadence": "batch"}, cadenceRefusal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				// the daemon's refusal of the whole config write
				http.Error(w, `bad request: `+tc.want, http.StatusBadRequest)
			})
			res, err := s.handleAutonomousConfigSet(context.Background(), callReq(tc.args))
			if err != nil {
				t.Fatalf("the refusal must be a tool result, not a protocol error: %v", err)
			}
			if res == nil || !res.IsError {
				t.Fatalf("the refusal must be a tool error: %+v", res)
			}
			text := automationText(t, res)
			if !strings.Contains(text, tc.want) {
				t.Errorf("the daemon's message must reach the caller, got: %s", text)
			}
			if !strings.HasPrefix(text, "autonomous_config_set:") {
				t.Errorf("the tool error must name the tool: %s", text)
			}
		})
	}
}

// A refusal of a config write that carries no merge pipeline setting keeps its
// existing reply (a protocol error), so other settings are unchanged.
func TestMergePipelineReleaseMCP_OtherConfigRefusalUnchanged(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `bad request: something else`, http.StatusBadRequest)
	})
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"inherited_tags_cache_seconds": 30,
	}))
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("the refusal must still surface: res=%+v", res)
	}
}

func TestMergePipelineReleaseMCP_GetTextShowsEffectiveValues(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mergePipelineReleaseViewBody))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	text := automationText(t, res)
	for _, want := range []string{
		"Release size by kind: bug=patch, feature=minor, security=major\n",
		"Major release trigger: milestone\n",
		"Release cadence: per_merge\n",
		"Version scheme: semver\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestMergePipelineReleaseMCP_GetTextEmptyValuesReadAsNone(t *testing.T) {
	const body = `{"project":"demo","source":"default","settings":{"file_name":"p.yaml","source_order":["default"],"fallback_on_bad_file":"record","version_files":[],"release_size_map":{},"major_trigger":"","release_cadence":"","version_scheme":""},"conflict_policy":"stop_and_ask"}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	text := automationText(t, res)
	for _, want := range []string{
		"Release size by kind: (none)\n",
		"Major release trigger: (none)\n",
		"Release cadence: (none)\n",
		"Version scheme: (none)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestMergePipelineReleaseMCP_GetJSONKeepsReleaseSettings(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mergePipelineReleaseViewBody))
	})
	res, _ := s.handleAutonomousMergePipelineGet(context.Background(), callReq(map[string]any{"project": "demo", "format": "json"}))
	if res == nil || res.IsError {
		t.Fatalf("res=%+v", res)
	}
	var obj struct {
		Settings struct {
			ReleaseSizeMap map[string]string `json:"release_size_map"`
			MajorTrigger   string            `json:"major_trigger"`
			ReleaseCadence string            `json:"release_cadence"`
			VersionScheme  string            `json:"version_scheme"`
		} `json:"settings"`
	}
	if err := json.Unmarshal([]byte(automationText(t, res)), &obj); err != nil {
		t.Fatal(err)
	}
	st := obj.Settings
	if st.ReleaseSizeMap["security"] != "major" || st.MajorTrigger != "milestone" ||
		st.ReleaseCadence != "per_merge" || st.VersionScheme != "semver" {
		t.Errorf("json must pass the release settings through: %+v", st)
	}
}
