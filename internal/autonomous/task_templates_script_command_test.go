package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// A template's verification_script must be one repo-relative script
// file. A command string (an operator writing the build and test
// commands straight into the field) is rejected, and the error names
// the template id.
func TestTemplateVerificationScriptCommandStringRejected(t *testing.T) {
	const id = "gate-build-and-test"
	bad := config.TaskTemplateDef{
		ID:                 id,
		Title:              "Gate",
		VerificationScript: "go build ./... && go test ./...",
	}
	err := validateTemplateVerificationScript(bad)
	if err == nil {
		t.Fatal("command string verification_script was accepted; want rejection")
	}
	if !strings.Contains(err.Error(), id) {
		t.Errorf("error %q does not name template id %q", err, id)
	}
	if !strings.Contains(err.Error(), "repo-relative script file") {
		t.Errorf("error %q does not say a verification script must be a repo-relative script file", err)
	}

	// Every shape that is not one repo-relative path is a command string.
	for _, v := range []string{
		"go build ./... && go test ./...",
		"make lint || true",
		"go test ./...; echo done",
		"cat plan.md | sh",
		"echo ok > out.txt",
		"sh < input.sh",
		"$HOME/verify.sh",
		"`which verify`",
		"scripts/verify.sh --all",
		"scripts/a\tb.sh",
		"/usr/local/bin/verify.sh",
		"../outside/verify.sh",
		"scripts/../../verify.sh",
		".",
	} {
		def := config.TaskTemplateDef{ID: id, VerificationScript: v}
		if err := validateTemplateVerificationScript(def); err == nil {
			t.Errorf("verification_script %q accepted; want rejection", v)
		}
	}

	// A rejected template is never instantiated: applying the set to a
	// story adds no task for it.
	story := testTaskTemplateStory(t)
	_, applied := applyTemplatesToStory(story, "feature", []config.TaskTemplateDef{bad}, nil)
	if len(applied) != 0 {
		t.Errorf("applied = %v, want none for a rejected template", applied)
	}

	// The operator write path refuses the set with the same error.
	if err := validateTaskTemplateVerificationScripts([]config.TaskTemplateDef{bad}); err == nil {
		t.Error("validateTaskTemplateVerificationScripts accepted a command string; want rejection")
	}
}

// The operator write path (SetDecomposeLintConfig, the PUT behind the
// autonomous config) refuses a task_templates block carrying a command
// string, names the template id, and leaves the live config untouched.
func TestTemplateVerificationScriptCommandStringRefusedOnWrite(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := &API{M: mgr}
	before := len(mgr.cfg.TaskTemplates.Templates)

	bad := map[string]any{
		"task_templates": map[string]any{
			"enabled": true,
			"templates": []any{
				map[string]any{"id": "gate-build-and-test", "verification_script": "go build ./... && go test ./..."},
			},
		},
	}
	err = api.SetDecomposeLintConfig(bad)
	if err == nil {
		t.Fatal("SetDecomposeLintConfig accepted a command-string verification_script")
	}
	if !strings.Contains(err.Error(), "gate-build-and-test") {
		t.Errorf("error %q does not name the template id", err)
	}
	if got := len(mgr.cfg.TaskTemplates.Templates); got != before {
		t.Errorf("live templates = %d after refused write, want %d", got, before)
	}

	good := map[string]any{
		"task_templates": map[string]any{
			"enabled": true,
			"templates": []any{
				map[string]any{"id": "gate", "verification_script": "scripts/verify.sh"},
			},
		},
	}
	if err := api.SetDecomposeLintConfig(good); err != nil {
		t.Fatalf("SetDecomposeLintConfig refused a repo-relative script: %v", err)
	}
	if len(mgr.cfg.TaskTemplates.Templates) != 1 {
		t.Errorf("live templates = %d after accepted write, want 1", len(mgr.cfg.TaskTemplates.Templates))
	}
}

// A repo-relative script path is accepted, an empty value means the
// template carries no script, and every built-in template passes.
func TestTemplateVerificationScriptPathAccepted(t *testing.T) {
	for _, v := range []string{
		"",
		"scripts/verify.sh",
		"./scripts/verify.sh",
		".datawatch/verify/t1.sh",
		"verify.sh",
	} {
		def := config.TaskTemplateDef{ID: "ok", VerificationScript: v}
		if err := validateTemplateVerificationScript(def); err != nil {
			t.Errorf("verification_script %q rejected: %v", v, err)
		}
		if verificationScriptLooksLikeCommand(v) {
			t.Errorf("verificationScriptLooksLikeCommand(%q) = true, want false", v)
		}
	}

	if err := validateTaskTemplateVerificationScripts(BuiltInTaskTemplates()); err != nil {
		t.Errorf("built-in templates rejected: %v", err)
	}
	if err := validateTaskTemplateVerificationScripts(EffectiveTaskTemplates(nil)); err != nil {
		t.Errorf("effective default templates rejected: %v", err)
	}

	// A valid script path is copied onto the instantiated task.
	story := testTaskTemplateStory(t)
	good := config.TaskTemplateDef{ID: "gate", Title: "Gate", VerificationScript: "scripts/verify.sh"}
	out, applied := applyTemplatesToStory(story, "feature", []config.TaskTemplateDef{good}, nil)
	if len(applied) != 1 {
		t.Fatalf("applied = %v, want one task for a valid script path", applied)
	}
	var found bool
	for _, tk := range out.Tasks {
		if tk.VerificationScript == "scripts/verify.sh" {
			found = true
		}
	}
	if !found {
		t.Error("instantiated task does not carry the repo-relative verification script")
	}
}
