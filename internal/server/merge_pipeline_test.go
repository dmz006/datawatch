package server

// The merge pipeline routes (GET /api/autonomous/merge-pipeline, PUT and
// DELETE .../record, POST .../validate), their federation guard, and the
// autonomous.merge_pipeline block on GET and PUT /api/config.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const mpValidYAML = `steps:
  - name: unit tests
    kind: test
    command: "true"
`

const mpValidRecordJSON = `{"definition":{"steps":[{"name":"unit tests","kind":"test","command":"true"}]}}`

// newMergePipelineServer returns a server with an empty config, a project
// "demo" whose directory exists, and a private data directory (the record
// store lives under it).
func newMergePipelineServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.Projects = map[string]config.ProjectConfigEntry{"demo": {Dir: dir}}
	return &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml"), dataDir: cfg.DataDir}, dir
}

func mpDo(h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, rd))
	return rec
}

func mpGet(t *testing.T, s *Server, project string) mergePipelineView {
	t.Helper()
	rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project="+project, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var v mergePipelineView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	return v
}

func TestMergePipelineGetDefault(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	v := mpGet(t, s, "demo")
	if v.Source != "default" || v.Definition == nil || len(v.Definition.Steps) == 0 {
		t.Fatalf("want the built-in pipeline, got source=%q def=%v", v.Source, v.Definition)
	}
	if !strings.Contains(v.Reason, "built-in") {
		t.Errorf("reason should say the built-in pipeline is used: %q", v.Reason)
	}
	if v.Settings.FileName != config.MergePipelineDefaultFileName ||
		!reflect.DeepEqual(v.Settings.SourceOrder, []string{"file", "record"}) ||
		v.Settings.FallbackOnBadFile != "record" {
		t.Errorf("settings: %+v", v.Settings)
	}
	if v.ConflictPolicy != config.ConflictPolicyMechanicalAndModel || v.ConflictModel != config.MergePipelineDefaultConflictModel {
		t.Errorf("configured policy/model: %q %q", v.ConflictPolicy, v.ConflictModel)
	}
	// No model registry here, so the policy in effect degrades and says why.
	if v.ConflictPolicyInEffect != config.ConflictPolicyStopAndAsk || v.ConflictModelInEffect != "" || v.ConflictDegradedReason == "" {
		t.Errorf("in effect: %q %q %q", v.ConflictPolicyInEffect, v.ConflictModelInEffect, v.ConflictDegradedReason)
	}
	if v.Overrides != nil || v.FileError != "" {
		t.Errorf("overrides=%v file_error=%q", v.Overrides, v.FileError)
	}
}

func TestMergePipelineGetSourcesAndOverrides(t *testing.T) {
	s, dir := newMergePipelineServer(t)

	// A record is used when the repository has no file.
	if rec := mpDo(s.handleMergePipelineRecord, http.MethodPut, "/api/autonomous/merge-pipeline/record?project=demo", mpValidRecordJSON); rec.Code != http.StatusOK {
		t.Fatalf("PUT record: %d %s", rec.Code, rec.Body.String())
	}
	v := mpGet(t, s, "demo")
	if v.Source != "record" || v.RecordUpdatedAt == "" || v.Definition.Steps[0].Name != "unit tests" {
		t.Fatalf("record source: %+v", v)
	}

	// The file wins over the record.
	file := filepath.Join(dir, filepath.FromSlash(config.MergePipelineDefaultFileName))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(mpValidYAML, "unit tests", "from file", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	v = mpGet(t, s, "demo")
	if v.Source != "file" || v.Definition.Steps[0].Name != "from file" {
		t.Fatalf("file source: %+v", v)
	}

	// A bad file falls back to the record and reports the problem.
	if err := os.WriteFile(file, []byte("steps: [oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	v = mpGet(t, s, "demo")
	if v.Source != "record" || v.FileError == "" {
		t.Fatalf("bad file: want record with file_error, got %+v", v)
	}

	// With no substitute allowed the definition is null and the error shows.
	fb := "none"
	s.cfg.Autonomous.MergePipeline.Projects = map[string]config.MergePipelineOverride{
		"demo": {FallbackOnBadFile: fb, ConflictPolicy: config.ConflictPolicyMechanical, VersionFiles: []string{"a.go"}},
	}
	v = mpGet(t, s, "demo")
	if v.Definition != nil || v.FileError == "" || v.Source != "file" {
		t.Fatalf("no substitute: %+v", v)
	}
	if v.Overrides == nil || v.Overrides.FallbackOnBadFile != fb || v.ConflictPolicyInEffect != config.ConflictPolicyMechanical ||
		!reflect.DeepEqual(v.Settings.VersionFiles, []string{"a.go"}) {
		t.Fatalf("overrides not shown: %+v", v)
	}
}

func TestMergePipelineGetErrors(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	if rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("no project: want 400, got %d", rec.Code)
	}
	long := strings.Repeat("a", mergePipelineMaxNameBytes+1)
	if rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project="+long, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("long project: want 400, got %d", rec.Code)
	}
	if rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project=a%01b", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("control character: want 400, got %d", rec.Code)
	}
	rec := mpDo(s.handleMergePipeline, http.MethodPost, "/api/autonomous/merge-pipeline?project=demo", "")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET" {
		t.Errorf("POST: want 405 with Allow GET, got %d %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := mpDo((&Server{}).handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project=x", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no config: want 503, got %d", rec.Code)
	}
	// A data directory that cannot hold the record store is a server error.
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.dataDir = f
	if err := os.MkdirAll(filepath.Join(f, "merge-pipeline"), 0o755); err == nil {
		t.Skip("cannot make the data directory unusable here")
	}
	badDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(badDir, "merge-pipeline", "records.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.dataDir = badDir
	if rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project=demo", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("unreadable store: want 500, got %d", rec.Code)
	}
	if rec := mpDo(s.handleMergePipelineRecord, http.MethodDelete, "/api/autonomous/merge-pipeline/record?project=demo", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("DELETE unreadable store: want 500, got %d", rec.Code)
	}
	if rec := mpDo(s.handleMergePipelineRecord, http.MethodPut, "/api/autonomous/merge-pipeline/record?project=demo", mpValidRecordJSON); rec.Code != http.StatusInternalServerError {
		t.Errorf("PUT unreadable store: want 500, got %d", rec.Code)
	}
}

func TestMergePipelineGetProjectRequired(t *testing.T) {
	s, _ := newMergePipelineServer(t)

	// No project and no merge base repository: a JSON 400 with a code.
	rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no project: want 400, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body should be JSON: %v: %s", err, rec.Body.String())
	}
	if body["code"] != "project_required" || body["error"] == nil {
		t.Errorf("body: %v", body)
	}

	// The other 400 cases keep the plain text form, without a code.
	long := strings.Repeat("a", mergePipelineMaxNameBytes+1)
	for name, target := range map[string]string{
		"long name":         "/api/autonomous/merge-pipeline?project=" + long,
		"control char name": "/api/autonomous/merge-pipeline?project=a%01b",
	} {
		rec := mpDo(s.handleMergePipeline, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, rec.Code)
		}
		var plain map[string]any
		if json.Unmarshal(rec.Body.Bytes(), &plain) == nil {
			if _, has := plain["code"]; has {
				t.Errorf("%s: plain text 400 must not carry a code: %s", name, rec.Body.String())
			}
		}
	}

	// A project query value answers as before.
	if rec := mpDo(s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project=demo", ""); rec.Code != http.StatusOK {
		t.Errorf("project=demo: want 200, got %d %s", rec.Code, rec.Body.String())
	}

	// The merge base repository names the project when the query is empty.
	s2, _ := newMergePipelineServer(t)
	repo := t.TempDir()
	s2.autonomousMgr = &mergeRunSpy{repo: repo}
	rec = mpDo(s2.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("merge base repo: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var v mergePipelineView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if v.Project != filepath.Base(repo) {
		t.Errorf("project: want %q (base name of the merge base repo), got %q", filepath.Base(repo), v.Project)
	}
}

func TestMergePipelineRecordPutDelete(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	const url = "/api/autonomous/merge-pipeline/record"

	// The project may come in the body; YAML text is accepted too.
	rec := mpDo(s.handleMergePipelineRecord, http.MethodPut, url, `{"project":"demo","yaml":`+jsonString(mpValidYAML)+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"updated_at"`) {
		t.Fatalf("PUT yaml: %d %s", rec.Code, rec.Body.String())
	}
	if v := mpGet(t, s, "demo"); v.Source != "record" {
		t.Fatalf("source after PUT: %q", v.Source)
	}

	bad := map[string]string{
		"invalid json":      `{`,
		"no pipeline":       `{"project":"demo"}`,
		"both forms":        `{"project":"demo","yaml":` + jsonString(mpValidYAML) + `,"definition":{"steps":[]}}`,
		"empty definition":  `{"project":"demo","definition":{"steps":[]}}`,
		"bad kind":          `{"project":"demo","definition":{"steps":[{"name":"a","kind":"nope","command":"true"}]}}`,
		"bad yaml":          `{"project":"demo","yaml":"steps: [oops"}`,
		"no project":        mpValidRecordJSON,
		"control char name": `{"project":"a\u0001b","definition":{"steps":[{"name":"a","kind":"test","command":"true"}]}}`,
	}
	for name, body := range bad {
		if rec := mpDo(s.handleMergePipelineRecord, http.MethodPut, url, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// Nothing bad was stored over the good record.
	if v := mpGet(t, s, "demo"); v.Definition.Steps[0].Name != "unit tests" {
		t.Fatalf("record changed by a refused PUT: %+v", v.Definition)
	}

	if rec := mpDo(s.handleMergePipelineRecord, http.MethodDelete, url, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE without project: want 400, got %d", rec.Code)
	}
	if rec := mpDo(s.handleMergePipelineRecord, http.MethodDelete, url+"?project=demo", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body.String())
	}
	if rec := mpDo(s.handleMergePipelineRecord, http.MethodDelete, url+"?project=demo", ""); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE twice: want 404, got %d", rec.Code)
	}
	if v := mpGet(t, s, "demo"); v.Source != "default" {
		t.Errorf("source after DELETE: %q", v.Source)
	}
	rec = mpDo(s.handleMergePipelineRecord, http.MethodGet, url, "")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "DELETE, PUT" {
		t.Errorf("GET: want 405 with Allow DELETE, PUT, got %d %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := mpDo((&Server{}).handleMergePipelineRecord, http.MethodPut, url, mpValidRecordJSON); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no config: want 503, got %d", rec.Code)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestMergePipelineValidate(t *testing.T) {
	s, dir := newMergePipelineServer(t)
	const url = "/api/autonomous/merge-pipeline/validate"
	type result struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
		Source string   `json:"source"`
		File   string   `json:"file"`
	}
	post := func(body string, want int) result {
		t.Helper()
		rec := mpDo(s.handleMergePipelineValidate, http.MethodPost, url, body)
		if rec.Code != want {
			t.Fatalf("POST %s: want %d, got %d: %s", body, want, rec.Code, rec.Body.String())
		}
		var r result
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		return r
	}

	if r := post(mpValidRecordJSON, 200); !r.Valid || r.Source != "body" || len(r.Errors) != 0 {
		t.Errorf("valid definition: %+v", r)
	}
	if r := post(`{"yaml":`+jsonString(mpValidYAML)+`}`, 200); !r.Valid {
		t.Errorf("valid yaml: %+v", r)
	}
	r := post(`{"definition":{"steps":[{"name":"a","kind":"nope"},{"name":"a","kind":"test","command":"true"}]}}`, 200)
	if r.Valid || len(r.Errors) < 2 {
		t.Errorf("every problem should be listed: %+v", r)
	}
	if r := post(`{"yaml":"steps: [oops"}`, 200); r.Valid || len(r.Errors) != 1 {
		t.Errorf("bad yaml: %+v", r)
	}
	post(`{`, 400)
	post(`{}`, 400)
	post(`{"file":true,"yaml":"x"}`, 400)

	// Nothing was saved by any of it.
	if v := mpGet(t, s, "demo"); v.Source != "default" {
		t.Errorf("validate must not save: source %q", v.Source)
	}

	// A repository file: missing, valid, malformed, named explicitly.
	if r := post(`{"project":"demo","file":true}`, 200); r.Valid || r.Source != "file" || r.File != config.MergePipelineDefaultFileName || len(r.Errors) != 1 {
		t.Errorf("missing file: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.yaml"), []byte(mpValidYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := post(`{"project":"demo","file":"p.yaml"}`, 200); !r.Valid || r.File != "p.yaml" {
		t.Errorf("named file: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.yaml"), []byte("steps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := post(`{"project":"demo","file":"p.yaml"}`, 200); r.Valid || len(r.Errors) == 0 {
		t.Errorf("malformed file: %+v", r)
	}
	post(`{"project":"demo","file":"../x.yaml"}`, 400)
	post(`{"project":"demo","file":"/etc/passwd"}`, 400)
	post(`{"project":"demo","file":""}`, 400)
	post(`{"project":"demo","file":5}`, 400)
	post(`{"project":"nobody","file":true}`, 404)
	post(`{"file":true}`, 400) // no project and no merge base repo to default to

	rec := mpDo(s.handleMergePipelineValidate, http.MethodGet, url, "")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Errorf("GET: want 405 with Allow POST, got %d %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := mpDo((&Server{}).handleMergePipelineValidate, http.MethodPost, url, mpValidRecordJSON); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no config: want 503, got %d", rec.Code)
	}
}

func TestMergePipelineFederationGuard(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	cases := []struct {
		name    string
		h       http.HandlerFunc
		method  string
		target  string
		body    string
		need    string
		allowed int
	}{
		{"get", s.handleMergePipeline, http.MethodGet, "/api/autonomous/merge-pipeline?project=demo", "", federation.CapAutonomousRead, 200},
		{"validate", s.handleMergePipelineValidate, http.MethodPost, "/api/autonomous/merge-pipeline/validate", mpValidRecordJSON, federation.CapAutonomousRead, 200},
		{"put", s.handleMergePipelineRecord, http.MethodPut, "/api/autonomous/merge-pipeline/record?project=demo", mpValidRecordJSON, federation.CapAutonomousWrite, 200},
		{"delete", s.handleMergePipelineRecord, http.MethodDelete, "/api/autonomous/merge-pipeline/record?project=demo", "", federation.CapAutonomousWrite, 200},
	}
	for _, c := range cases {
		call := func(caps ...string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
			req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, caps))
			rec := httptest.NewRecorder()
			c.h(rec, req)
			return rec
		}
		if rec := call(federation.CapHealthRead); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), c.need) {
			t.Errorf("%s without %s: want 403 naming it, got %d %q", c.name, c.need, rec.Code, rec.Body.String())
		}
		if c.need == federation.CapAutonomousWrite {
			// Read access alone never writes.
			if rec := call(federation.CapAutonomousRead); rec.Code != http.StatusForbidden {
				t.Errorf("%s with read only: want 403, got %d", c.name, rec.Code)
			}
		}
		if rec := call(c.need); rec.Code != c.allowed {
			t.Errorf("%s with %s: want %d, got %d %s", c.name, c.need, c.allowed, rec.Code, rec.Body.String())
		}
	}
	// The MCP tool map classifies the read.
	if got, ok := federation.RequiredCapForMCPTool("autonomous_merge_pipeline_get"); !ok || got != federation.CapAutonomousRead {
		t.Errorf("MCP tool cap: %q %v", got, ok)
	}
}

// ---- config ----------------------------------------------------------------

func putMergePipelineConfig(s *Server, patch map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rec
}

func mergePipelineBlock(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["merge_pipeline"]
	if !ok {
		t.Fatal("GET /api/config autonomous lacks merge_pipeline")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestMergePipelineConfigDefaultsAndRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	m := mergePipelineBlock(t, s)
	if string(m["file_name"]) != `".datawatch/merge-pipeline.yaml"` || string(m["source_order"]) != `["file","record"]` ||
		string(m["fallback_on_bad_file"]) != `"record"` || string(m["conflict_policy"]) != `"mechanical_and_model"` ||
		string(m["conflict_model"]) != `"`+config.MergePipelineDefaultConflictModel+`"` ||
		string(m["version_files"]) != `[]` || string(m["projects"]) != `{}` {
		t.Fatalf("defaults: %v", m)
	}

	rec := putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.file_name":                            "ci/merge.yaml",
		"autonomous.merge_pipeline.source_order":                         []any{"record", "file"},
		"autonomous.merge_pipeline.fallback_on_bad_file":                 "default",
		"autonomous.merge_pipeline.conflict_policy":                      "mechanical",
		"autonomous.merge_pipeline.conflict_model":                       "my-model",
		"autonomous.merge_pipeline.version_files":                        "a.go, b.go",
		"autonomous.merge_pipeline.projects.demo.conflict_policy":        "stop_and_ask",
		"autonomous.merge_pipeline.projects.demo.conflict_model":         "",
		"autonomous.merge_pipeline.projects.my.app.version_files":        []any{"v.go"},
		"autonomous.merge_pipeline.projects.my.app.fallback_on_bad_file": "none",
		"autonomous.merge_pipeline.projects.other.source_order":          "file",
		"autonomous.merge_pipeline.projects.other.file_name":             "x.yaml",
		"autonomous.merge_pipeline.projects.other.version_files":         nil,
		"autonomous.merge_pipeline.projects.gone":                        nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	check := func(c config.MergePipelineConfig, where string) {
		t.Helper()
		model := ""
		want := config.MergePipelineConfig{
			FileName: "ci/merge.yaml", SourceOrder: []string{"record", "file"}, FallbackOnBadFile: "default",
			ConflictPolicy: "mechanical", VersionFiles: []string{"a.go", "b.go"},
			Projects: map[string]config.MergePipelineOverride{
				"demo":   {ConflictPolicy: "stop_and_ask", ConflictModel: &model},
				"my.app": {VersionFiles: []string{"v.go"}, FallbackOnBadFile: "none"},
				"other":  {SourceOrder: []string{"file"}, FileName: "x.yaml"},
			},
		}
		m := "my-model"
		want.ConflictModel = &m
		if !reflect.DeepEqual(c, want) {
			t.Fatalf("%s:\n got %+v\nwant %+v", where, c, want)
		}
	}
	check(s.cfg.Autonomous.MergePipeline, "live config")
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load saved: %v", err)
	}
	check(saved.Autonomous.MergePipeline, "saved file")
	m2 := mergePipelineBlock(t, s)
	if string(m2["conflict_model"]) != `"my-model"` || string(m2["file_name"]) != `"ci/merge.yaml"` {
		t.Errorf("GET after PUT: %v", m2)
	}

	// null returns a key to its default; removing a project drops its overrides.
	rec = putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.conflict_model":                       nil,
		"autonomous.merge_pipeline.file_name":                            nil,
		"autonomous.merge_pipeline.projects.demo":                        nil,
		"autonomous.merge_pipeline.projects.my.app.fallback_on_bad_file": nil,
		"autonomous.merge_pipeline.projects.my.app.version_files":        nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT null: %d %s", rec.Code, rec.Body.String())
	}
	got := s.cfg.Autonomous.MergePipeline
	if got.ConflictModel != nil || got.FileName != "" {
		t.Errorf("null did not clear: %+v", got)
	}
	if _, ok := got.Projects["demo"]; ok {
		t.Errorf("project override not removed")
	}
	if _, ok := got.Projects["my.app"]; ok {
		t.Errorf("an override with every key cleared should disappear")
	}
	if _, ok := got.Projects["other"]; !ok {
		t.Errorf("an unrelated project must stay")
	}
	// The effective conflict model is the default again.
	if string(mergePipelineBlock(t, s)["conflict_model"]) != `"`+config.MergePipelineDefaultConflictModel+`"` {
		t.Errorf("default model not back")
	}
}

func TestMergePipelineConfigRefusesBadValues(t *testing.T) {
	bad := map[string]struct {
		patch map[string]any
		want  string
	}{
		"file name absolute":   {map[string]any{"autonomous.merge_pipeline.file_name": "/etc/x"}, "file_name"},
		"file name escapes":    {map[string]any{"autonomous.merge_pipeline.file_name": "../x"}, "file_name"},
		"file name not text":   {map[string]any{"autonomous.merge_pipeline.file_name": 5}, "file_name"},
		"source unknown":       {map[string]any{"autonomous.merge_pipeline.source_order": []any{"file", "cloud"}}, "source_order[1]"},
		"source twice":         {map[string]any{"autonomous.merge_pipeline.source_order": "file,file"}, "listed twice"},
		"source not text":      {map[string]any{"autonomous.merge_pipeline.source_order": []any{1}}, "source_order[0]"},
		"source wrong type":    {map[string]any{"autonomous.merge_pipeline.source_order": 3}, "source_order"},
		"too many files":       {map[string]any{"autonomous.merge_pipeline.version_files": strings.Repeat("a,", 150) + "a"}, "too many"},
		"version file empty":   {map[string]any{"autonomous.merge_pipeline.version_files": "a,,b"}, "version_files"},
		"version file escapes": {map[string]any{"autonomous.merge_pipeline.version_files": []any{"../v.go"}}, "version_files"},
		"fallback unknown":     {map[string]any{"autonomous.merge_pipeline.fallback_on_bad_file": "maybe"}, "fallback_on_bad_file"},
		"policy unknown":       {map[string]any{"autonomous.merge_pipeline.conflict_policy": "guess"}, "conflict_policy"},
		"model not text":       {map[string]any{"autonomous.merge_pipeline.conflict_model": 7}, "conflict_model"},
		"model with space":     {map[string]any{"autonomous.merge_pipeline.conflict_model": "two words"}, "conflict_model"},
		"unknown key":          {map[string]any{"autonomous.merge_pipeline.bogus": "x"}, "unknown key"},
		"project bad policy":   {map[string]any{"autonomous.merge_pipeline.projects.demo.conflict_policy": "guess"}, "projects.demo.conflict_policy"},
		"project whole object": {map[string]any{"autonomous.merge_pipeline.projects.demo": "x"}, "only null"},
		"project name empty":   {map[string]any{"autonomous.merge_pipeline.projects..file_name": "x"}, "project name is empty"},
		"project name missing": {map[string]any{"autonomous.merge_pipeline.projects.": nil}, "project name is empty"},
	}
	for name, c := range bad {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		before := s.cfg.Autonomous.MergePipeline.Clone()
		// A good key in the same PUT must not be applied when another is bad.
		patch := map[string]any{"autonomous.merge_pipeline.conflict_policy": "mechanical"}
		for k, v := range c.patch {
			patch[k] = v
		}
		rec := putMergePipelineConfig(s, patch)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: want 400 naming %q, got %d %q", name, c.want, rec.Code, rec.Body.String())
		}
		if !reflect.DeepEqual(s.cfg.Autonomous.MergePipeline.Clone(), before) {
			t.Errorf("%s: a refused PUT changed the config", name)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Errorf("%s: a refused PUT wrote the config file", name)
		}
	}
}

func TestMergePipelineConfigKeyHelpers(t *testing.T) {
	if known, err := applyMergePipelineKey(&config.MergePipelineConfig{}, "autonomous.other", 1); known || err != nil {
		t.Errorf("a key outside the block: known=%v err=%v", known, err)
	}
	if err := validateMergePipelinePatch(map[string]any{"autonomous.infra_retry.enabled": true}); err != nil {
		t.Errorf("no merge pipeline key must not be checked: %v", err)
	}
	cfg := config.DefaultConfig()
	if applyMergePipelinePatch(cfg, "autonomous.other", 1) {
		t.Errorf("a key outside the block reported as known")
	}
	if !applyMergePipelinePatch(cfg, "autonomous.merge_pipeline.conflict_policy", "mechanical") || cfg.Autonomous.MergePipeline.ConflictPolicy != "mechanical" {
		t.Errorf("apply did not store the value")
	}
	// An explicit empty list stays empty rather than unset.
	if err := func() error {
		_, e := applyMergePipelineKey(&cfg.Autonomous.MergePipeline, "autonomous.merge_pipeline.version_files", "")
		return e
	}(); err != nil || cfg.Autonomous.MergePipeline.VersionFiles == nil {
		t.Errorf("empty list: err=%v files=%v", err, cfg.Autonomous.MergePipeline.VersionFiles)
	}
}

// TestMergePipelineReleaseApprovalStoresGlobal checks that a valid mode for
// each release size is stored in the global block through PUT /api/config.
func TestMergePipelineReleaseApprovalStoresGlobal(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	wireReleaseModeRecords(t, s)
	patch := map[string]any{
		"autonomous.merge_pipeline.release_approval.patch": "manual",
		"autonomous.merge_pipeline.release_approval.minor": "ask",
		"autonomous.merge_pipeline.release_approval.major": "auto",
	}
	if rec := putMergePipelineConfig(s, patch); rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ra := s.cfg.Autonomous.MergePipeline.ReleaseApproval
	if ra.Patch != "manual" || ra.Minor != "ask" || ra.Major != "auto" {
		t.Errorf("global release_approval = %+v, want manual/ask/auto", ra)
	}
	if got := s.cfg.Autonomous.MergePipeline.EffectiveReleaseApproval("", config.ReleaseSizeMinor); got != "ask" {
		t.Errorf("effective minor = %q, want ask", got)
	}
}

// TestMergePipelineReleaseApprovalStoresProject checks a per-project value is
// stored and that it wins over the global value for that project.
func TestMergePipelineReleaseApprovalStoresProject(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	wireReleaseModeRecords(t, s)
	patch := map[string]any{"autonomous.merge_pipeline.projects.demo.release_approval.minor": "manual"}
	if rec := putMergePipelineConfig(s, patch); rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	o, ok := s.cfg.Autonomous.MergePipeline.Projects["demo"]
	if !ok || o.ReleaseApproval.Minor != "manual" {
		t.Fatalf("project override = %+v (present %v), want minor manual", o, ok)
	}
	if got := s.cfg.Autonomous.MergePipeline.EffectiveReleaseApproval("demo", config.ReleaseSizeMinor); got != "manual" {
		t.Errorf("effective demo minor = %q, want manual", got)
	}
}

// TestMergePipelineReleaseApprovalRefusesBadValue checks that an invalid mode
// is refused on PUT /api/config at global and project level, the error names
// the key and the valid modes, and nothing is saved.
func TestMergePipelineReleaseApprovalRefusesBadValue(t *testing.T) {
	cases := map[string]string{
		"global":  "autonomous.merge_pipeline.release_approval.major",
		"project": "autonomous.merge_pipeline.projects.demo.release_approval.major",
	}
	for name, key := range cases {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		before := s.cfg.Autonomous.MergePipeline.Clone()
		patch := map[string]any{
			"autonomous.merge_pipeline.conflict_policy": "mechanical",
			key: "sometimes",
		}
		rec := putMergePipelineConfig(s, patch)
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d: %s", name, rec.Code, body)
		}
		for _, want := range []string{key, "auto", "ask", "manual"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: error %q does not contain %q", name, body, want)
			}
		}
		if !reflect.DeepEqual(s.cfg.Autonomous.MergePipeline.Clone(), before) {
			t.Errorf("%s: a refused PUT changed the config", name)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Errorf("%s: a refused PUT wrote the config file", name)
		}
	}
}

// TestMergePipelineReleaseApprovalClearKey checks that an empty string clears
// a global size back to inheriting, and that a project whose only value is a
// release approval mode is kept, then dropped once that mode is cleared.
func TestMergePipelineReleaseApprovalClearKey(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	wireReleaseModeRecords(t, s)
	if rec := putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.release_approval.patch":               "ask",
		"autonomous.merge_pipeline.projects.demo.release_approval.patch": "manual",
	}); rec.Code != http.StatusOK {
		t.Fatalf("set: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok := s.cfg.Autonomous.MergePipeline.Projects["demo"]; !ok {
		t.Fatal("a project override holding only release_approval was dropped")
	}
	if rec := putMergePipelineConfig(s, map[string]any{
		"autonomous.merge_pipeline.release_approval.patch":               "",
		"autonomous.merge_pipeline.projects.demo.release_approval.patch": "",
	}); rec.Code != http.StatusOK {
		t.Fatalf("clear: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := s.cfg.Autonomous.MergePipeline.ReleaseApproval.Patch; got != "" {
		t.Errorf("global patch after clear = %q, want empty", got)
	}
	if _, ok := s.cfg.Autonomous.MergePipeline.Projects["demo"]; ok {
		t.Error("an emptied project override was kept after its release_approval was cleared")
	}
}

// mpReleaseApprovalGet reads the release_approval_in_effect of GET
// /api/autonomous/merge-pipeline for project demo.
func mpReleaseApprovalGet(t *testing.T, s *Server) [3]string {
	t.Helper()
	v := mpGet(t, s, "demo")
	return [3]string{v.ReleaseApprovalInEffect.Patch, v.ReleaseApprovalInEffect.Minor, v.ReleaseApprovalInEffect.Major}
}

// TestMergePipelineReleaseApprovalInEffectDefault checks that with nothing set
// the view shows the built-in mode for each size (patch auto, minor auto,
// major ask), so the line is never empty.
func TestMergePipelineReleaseApprovalInEffectDefault(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	want := [3]string{"auto", "auto", "ask"}
	if got := mpReleaseApprovalGet(t, s); got != want {
		t.Fatalf("default release_approval_in_effect = %v, want %v", got, want)
	}
}

// TestMergePipelineReleaseApprovalInEffectGlobal checks that a global value
// replaces the default for each size it sets, and unset sizes keep theirs.
func TestMergePipelineReleaseApprovalInEffectGlobal(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	s.cfg.Autonomous.MergePipeline.ReleaseApproval = config.ReleaseApprovalModes{Patch: "manual", Major: "auto"}
	want := [3]string{"manual", "auto", "auto"}
	if got := mpReleaseApprovalGet(t, s); got != want {
		t.Fatalf("global release_approval_in_effect = %v, want %v", got, want)
	}
}

// TestMergePipelineReleaseApprovalInEffectOverride checks that the project's
// override wins over the global value, and the global value still applies to
// the sizes the override leaves unset.
func TestMergePipelineReleaseApprovalInEffectOverride(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	s.cfg.Autonomous.MergePipeline.ReleaseApproval = config.ReleaseApprovalModes{Patch: "manual", Minor: "ask", Major: "auto"}
	s.cfg.Autonomous.MergePipeline.Projects = map[string]config.MergePipelineOverride{
		"demo": {ReleaseApproval: config.ReleaseApprovalModes{Minor: "manual"}},
	}
	want := [3]string{"manual", "manual", "auto"}
	if got := mpReleaseApprovalGet(t, s); got != want {
		t.Fatalf("override release_approval_in_effect = %v, want %v", got, want)
	}
}

// TestMergePipelineReleaseApprovalSurfaceDefault checks the GET /api/config
// surface resolves an unset release_approval to its defaults.
func TestMergePipelineReleaseApprovalSurfaceDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	m := mergePipelineConfigSurface(cfg.Autonomous.MergePipeline)
	got, ok := m["release_approval"].(config.ReleaseApprovalModes)
	if !ok {
		t.Fatalf("release_approval is %T, want config.ReleaseApprovalModes", m["release_approval"])
	}
	if want := (config.ReleaseApprovalModes{Patch: "auto", Minor: "auto", Major: "ask"}); got != want {
		t.Fatalf("default surface release_approval = %+v, want %+v", got, want)
	}
}

// TestMergePipelineReleaseApprovalSurfaceGlobalAndOverride checks the surface
// shows the resolved global values, and keeps the project override visible
// under projects as set.
func TestMergePipelineReleaseApprovalSurfaceGlobalAndOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Autonomous.MergePipeline.ReleaseApproval = config.ReleaseApprovalModes{Patch: "manual"}
	cfg.Autonomous.MergePipeline.Projects = map[string]config.MergePipelineOverride{
		"demo": {ReleaseApproval: config.ReleaseApprovalModes{Major: "manual"}},
	}
	m := mergePipelineConfigSurface(cfg.Autonomous.MergePipeline)
	got := m["release_approval"].(config.ReleaseApprovalModes)
	if want := (config.ReleaseApprovalModes{Patch: "manual", Minor: "auto", Major: "ask"}); got != want {
		t.Fatalf("global surface release_approval = %+v, want %+v", got, want)
	}
	projects, ok := m["projects"].(map[string]config.MergePipelineOverride)
	if !ok {
		t.Fatalf("projects is %T", m["projects"])
	}
	if o := projects["demo"]; o.ReleaseApproval.Major != "manual" || o.ReleaseApproval.Patch != "" {
		t.Fatalf("project override not kept as set: %+v", o.ReleaseApproval)
	}
	// The override's own resolution is visible through the effective function.
	if eff := cfg.Autonomous.MergePipeline.EffectiveReleaseApprovalModes("demo"); eff != (config.ReleaseApprovalModes{Patch: "manual", Minor: "auto", Major: "manual"}) {
		t.Fatalf("effective for demo = %+v", eff)
	}
}
