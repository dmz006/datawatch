package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mergePipelineCLIView = `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"},{"name":"ship","kind":"ship","action":"push_release","on_failure":"stop"}]},"source":"record","reason":"the repository has no pipeline file","record_updated_at":"2026-11-01T10:00:00Z","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record","default"],"fallback_on_bad_file":"record","version_files":["cmd/main.go"]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","overrides":{"file_name":"ci/p.yaml"}}`

func writePipelineFile(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pipeline.yaml")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergePipelineCLI_ShowText(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineCLIView)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"show", "--project", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/merge-pipeline" || d.query != "project=demo" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	for _, w := range []string{
		"Merge pipeline for demo\n",
		"Source: the pipeline record kept by the daemon (set 2026-11-01T10:00:00Z)\n",
		"  1. run tests [test] make test\n",
		"  2. ship [ship] built-in push_release (on failure stop)\n",
		"Files that hold the version: cmd/main.go\n",
		"  file_name = ci/p.yaml\n",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func TestMergePipelineCLI_ShowJSONIsUntouched(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineCLIView)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"show", "--json"})
	if err != nil || out != mergePipelineCLIView {
		t.Errorf("err=%v out=%s", err, out)
	}
}

func TestMergePipelineCLI_ShowDaemonRefusalAndBadBody(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, "bad request: project is empty")
	_, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"show"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "project is empty") {
		t.Errorf("err=%v", err)
	}
	newLaneFillCLIDaemon(t, http.StatusOK, "not json")
	if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"show"}); err == nil {
		t.Error("an unreadable body must be an error")
	}
}

func TestMergePipelineCLI_SetRecord(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"project":"demo","definition":{"steps":[{"name":"a","kind":"test","command":"make test"}]},"updated_at":"2026-11-01T10:00:00Z"}`)
	file := writePipelineFile(t, "steps:\n  - name: a\n    kind: test\n    command: make test\n")
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"set-record", "--project", "demo", "--from-file", file})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/merge-pipeline/record" || !strings.Contains(d.body, `"project":"demo"`) || !strings.Contains(d.body, "make test") {
		t.Errorf("path=%s body=%s", d.path, d.body)
	}
	if !strings.Contains(out, "Saved the merge pipeline record for demo (1 step).") {
		t.Errorf("out: %s", out)
	}
}

func TestMergePipelineCLI_SetRecordFromStdin(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"project":"x","definition":{"steps":[]},"updated_at":"2026-11-01T10:00:00Z"}`)
	c := newAutonomousMergePipelineCmd()
	c.SetIn(strings.NewReader("steps: []\n"))
	if _, err := runLaneFillCmd(t, c, []string{"set-record", "--from-file", "-"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.body, `"yaml":"steps: []\n"`) {
		t.Errorf("body=%s", d.body)
	}
}

func TestMergePipelineCLI_SetRecordBadInput(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
	empty := writePipelineFile(t, "  \n")
	big := writePipelineFile(t, strings.Repeat("a", mergePipelineFileMax+1))
	for name, argv := range map[string][]string{
		"no source":    {"set-record"},
		"missing file": {"set-record", "--from-file", filepath.Join(t.TempDir(), "absent.yaml")},
		"empty file":   {"set-record", "--from-file", empty},
		"too large":    {"set-record", "--from-file", big},
	} {
		if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), argv); err == nil {
			t.Errorf("%s: must fail", name)
		}
	}
	if d.calls != 0 {
		t.Errorf("bad input reached the daemon %d times", d.calls)
	}
}

func TestMergePipelineCLI_SetRecordDaemonRefusal(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, "bad request: invalid merge pipeline: step 1: no name")
	file := writePipelineFile(t, "steps: [{}]")
	_, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"set-record", "--from-file", file})
	if err == nil || !strings.Contains(err.Error(), "step 1: no name") {
		t.Errorf("err=%v", err)
	}
}

func TestMergePipelineCLI_ClearRecord(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"status":"deleted","project":"demo"}`)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"clear-record", "--project", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/merge-pipeline/record" || d.query != "project=demo" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	if !strings.Contains(out, "Removed the merge pipeline record for demo.") {
		t.Errorf("out: %s", out)
	}
	newLaneFillCLIDaemon(t, http.StatusNotFound, "no merge pipeline record for project demo")
	if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"clear-record"}); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("a missing record must be reported: %v", err)
	}
}

func TestMergePipelineCLI_ValidateValidAndInvalid(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"valid":true,"errors":[],"definition":{"steps":[{"name":"a","kind":"test","command":"make test"}]},"source":"body"}`)
	file := writePipelineFile(t, "steps: []")
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"validate", "--from-file", file})
	if err != nil || !strings.Contains(out, "The pipeline you gave is valid.") {
		t.Fatalf("err=%v out=%s", err, out)
	}
	if d.path != "/api/autonomous/merge-pipeline/validate" {
		t.Errorf("path=%s", d.path)
	}

	newLaneFillCLIDaemon(t, http.StatusOK, `{"valid":false,"errors":["step 1: no name","step 2: unknown kind"],"source":"body"}`)
	out, err = runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"validate", "--from-file", file})
	if err == nil {
		t.Error("an invalid pipeline must give a failing exit status")
	}
	if !strings.Contains(out, "is not valid (2 problems)") || !strings.Contains(out, "  - step 2: unknown kind") {
		t.Errorf("out: %s", out)
	}
}

func TestMergePipelineCLI_ValidateRepoFile(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"valid":true,"errors":[],"source":"file","file":".datawatch/merge-pipeline.yaml"}`)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"validate", "--repo-file"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.body, `"file":true`) || !strings.Contains(out, "The repository pipeline file .datawatch/merge-pipeline.yaml is valid.") {
		t.Errorf("body=%s out=%s", d.body, out)
	}
	if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"validate", "--repo-file=ci/p.yaml", "--project", "demo"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.body, `"file":"ci/p.yaml"`) || !strings.Contains(d.body, `"project":"demo"`) {
		t.Errorf("body=%s", d.body)
	}
}

func TestMergePipelineCLI_ValidateBadInput(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
	file := writePipelineFile(t, "steps: []")
	for name, argv := range map[string][]string{
		"nothing to check": {"validate"},
		"both":             {"validate", "--from-file", file, "--repo-file"},
		"empty repo file":  {"validate", "--repo-file="},
	} {
		if _, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), argv); err == nil {
			t.Errorf("%s: must fail", name)
		}
	}
	if d.calls != 0 {
		t.Errorf("bad input reached the daemon %d times", d.calls)
	}
}

func TestMergePipelineCLI_RegisteredUnderAutonomous(t *testing.T) {
	found := false
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() == "merge-pipeline" {
			found = true
			var subs []string
			for _, s := range c.Commands() {
				subs = append(subs, s.Name())
			}
			for _, want := range []string{"show", "set-record", "clear-record", "validate"} {
				if !strings.Contains(strings.Join(subs, " "), want) {
					t.Errorf("merge-pipeline lacks %s: %v", want, subs)
				}
			}
		}
	}
	if !found {
		t.Error("merge-pipeline is not under autonomous")
	}
}

const mergePipelineCLIPostShipConfig = `{"hostname":"h","autonomous":{"merge_pipeline":{"file_name":".datawatch/merge-pipeline.yaml","post_ship":{"enabled":true,"hooks":[{"name":"registry_row","builtin":"registry_row","enabled":true},{"name":"notify","command":"pw send","enabled":false}],"registry_file":".datawatch/registry.yaml","registry_row_pattern":"| {project} | {version} |","companion_repository":"acme/widgets","issue_title_template":"parity: {project} {version}","notify_template":"shipped {project} {version}"}}}}`

func TestMergePipelineCLIPostShipShowText(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineCLIPostShipConfig)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"post-ship", "show"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/config" || d.query != "" || d.body != "" { // a read, with no body
		t.Errorf("path=%s query=%q body=%q", d.path, d.query, d.body)
	}
	for _, want := range []string{
		"Post-ship hooks: on\n",
		"1. registry_row: the built-in registry_row, would run\n",
		"2. notify: the command pw send, skipped (disabled)\n",
		"Built-in targets:\n",
		"registry file: .datawatch/registry.yaml\n",
		"registry row pattern: | {project} | {version} |\n",
		"companion repository: acme/widgets\n",
		"issue title template: parity: {project} {version}\n",
		"notify template: shipped {project} {version}\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMergePipelineCLIPostShipShowNoBlockAndJSON(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"hostname":"h"}`)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"post-ship", "show"})
	if err != nil || !strings.Contains(out, "the daemon does not report a post-ship block") {
		t.Errorf("err=%v out=%s", err, out)
	}
	newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineCLIPostShipConfig)
	out, err = runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"post-ship", "show", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var block map[string]any
	if err := json.Unmarshal([]byte(out), &block); err != nil || block["enabled"] != true || block["companion_repository"] != "acme/widgets" {
		t.Errorf("json block: %s (err=%v)", out, err)
	}
}

func TestMergePipelineCLIPostShipSetSendsOneKey(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"status":"ok"}`)
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"post-ship", "set", "registry_file", "sub/registry.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/config" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(d.body), &body); err != nil {
		t.Fatal(err)
	}
	if got, ok := body["autonomous.merge_pipeline.post_ship.registry_file"]; !ok || got != "sub/registry.yaml" || len(body) != 1 {
		t.Errorf("body=%v", body)
	}
	if !strings.Contains(out, "autonomous.merge_pipeline.post_ship.registry_file = sub/registry.yaml") {
		t.Errorf("out=%s", out)
	}
}

func TestMergePipelineCLIPostShipSetValidation(t *testing.T) {
	cmd := newAutonomousMergePipelineCmd()
	if _, err := runLaneFillCmd(t, cmd, []string{"post-ship", "set", "no_such_key", "x"}); err == nil || !strings.Contains(err.Error(), "unknown post-ship key") {
		t.Errorf("err=%v", err)
	}
	if _, err := runLaneFillCmd(t, cmd, []string{"post-ship", "set", "enabled", "maybe"}); err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Errorf("err=%v", err)
	}
	if _, err := runLaneFillCmd(t, cmd, []string{"post-ship", "set", "hooks", "not a list"}); err == nil || !strings.Contains(err.Error(), "JSON array") {
		t.Errorf("err=%v", err)
	}
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"status":"ok"}`)
	if _, err := runLaneFillCmd(t, cmd, []string{"post-ship", "set", "hooks", "[]"}); err != nil || !strings.Contains(d.body, `"autonomous.merge_pipeline.post_ship.hooks":"[]"`) {
		t.Errorf("an empty hooks array must pass: err=%v body=%q", err, d.body)
	}
}
