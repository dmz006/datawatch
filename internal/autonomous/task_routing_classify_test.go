package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestTaskTypeClassifyByTemplateKind(t *testing.T) {
	cases := []struct {
		task Task
		want string
	}{
		{Task{Kind: TaskKindSmoke, Title: "Write docs"}, config.TaskTypeFinalGate},
		{Task{Kind: TaskKindFinalGate}, config.TaskTypeFinalGate},
		{Task{Title: "Changelog", Spec: "x (" + TaskTemplateMarkerPrefix + TemplateChangelogEntry + ")"}, config.TaskTypeChangelogEntry},
		{Task{Title: "Parity", Spec: "x (" + TaskTemplateMarkerPrefix + TemplateParityIssueDraft + ")"}, config.TaskTypeParityIssueDraft},
		{Task{Title: "Version", Spec: "x (" + TaskTemplateMarkerPrefix + TemplateVersionBumpLeftToMerge + ")"}, config.TaskTypeVersionNote},
		{Task{Title: "Step", Spec: "x (" + TaskTemplateMarkerPrefix + TemplateBoundedLoadStep + ")"}, config.TaskTypeBoundedLoadStep},
		{Task{Title: "Docs", Spec: "x (" + TaskTemplateMarkerPrefix + TemplateDocsSet + ")"}, config.TaskTypeDocsSet},
		{Task{Title: "Gate", Spec: "x (" + TaskTemplateMarkerPrefix + TemplateFinalGate + ")"}, config.TaskTypeFinalGate},
	}
	for _, c := range cases {
		got, by := ClassifyTask(&c.task, nil)
		if got != c.want || by != TaskClassifiedByTemplateKind {
			t.Errorf("%q/%q: got %s by %s, want %s by template_kind", c.task.Title, c.task.Kind, got, by, c.want)
		}
	}
}

func TestTaskTypeClassifyByTitleSpecEveryFreeType(t *testing.T) {
	cases := []struct {
		title, spec, want string
	}{
		{"Write the how-to and definitions", "", config.TaskTypeDocsSet},
		{"Add the locale strings", "locales/en.json keys", config.TaskTypeLocaleStrings},
		{"Add unit tests for the router", "", config.TaskTypeTests},
		{"Run gofmt over the package", "", config.TaskTypeFormattingCleanup},
		{"Rename the helper", "", config.TaskTypeFormattingCleanup},
		{"Add the Sessions tab to app.js", "", config.TaskTypeUICode},
		{"Update style.css for the header", "", config.TaskTypeUICode},
		{"Add the REST endpoint", "", config.TaskTypeMCPAPISurface},
		{"Expose it in internal/mcp", "", config.TaskTypeMCPAPISurface},
		{"Resolve the merge conflict in manager.go", "", config.TaskTypeMergeConflictRepair},
		{"Fix the review findings", "", config.TaskTypeReviewRepair},
		{"Fix the rules review block", "", config.TaskTypeReviewRepair},
		{"Run the soak step", "", config.TaskTypeBoundedLoadStep},
		{"Implement the routing engine", "no keywords here", config.TaskTypeDaemonBackendCode},
	}
	for _, c := range cases {
		got, by := ClassifyTask(&Task{Title: c.title, Spec: c.spec}, nil)
		if got != c.want {
			t.Errorf("%q: got %s want %s", c.title, got, c.want)
		}
		wantBy := TaskClassifiedByTitleSpec
		if c.want == config.TaskTypeDaemonBackendCode {
			wantBy = TaskClassifiedByDefault
		}
		if by != wantBy {
			t.Errorf("%q: classified_by %s want %s", c.title, by, wantBy)
		}
	}
}

func TestTaskTypeClassifySpecKeywordsWhenTitleSilent(t *testing.T) {
	got, by := ClassifyTask(&Task{Title: "Step 3", Spec: "Resolve the merge conflict markers left in the file"}, nil)
	if got != config.TaskTypeMergeConflictRepair || by != TaskClassifiedByTitleSpec {
		t.Errorf("got %s by %s", got, by)
	}
}

func TestTaskTypeCoverageOfAll14Types(t *testing.T) {
	seen := map[string]bool{}
	add := func(tt string) { seen[tt] = true }
	for _, r := range taskTypeRules {
		add(r.taskType)
	}
	for _, tt := range templateTaskTypes {
		add(tt)
	}
	add(config.TaskTypeDaemonBackendCode)
	for _, tt := range config.TaskRoutingTaskTypes() {
		if !seen[tt] {
			t.Errorf("task type %s is not reachable by classification", tt)
		}
	}
}

func TestTaskTypeTieResolvesToCodeForInternalGoFiles(t *testing.T) {
	task := &Task{Title: "Add unit tests and docs", Spec: "edit internal/autonomous/manager.go and add tests"}
	if got, _ := ClassifyTask(task, nil); got != config.TaskTypeDaemonBackendCode {
		t.Errorf("mechanical type for a task that edits internal Go: %s", got)
	}
	api := &Task{Title: "Update the docs", FilesPlanned: []string{"internal/server/api.go"}}
	if got, _ := ClassifyTask(api, nil); got != config.TaskTypeMCPAPISurface {
		t.Errorf("server file tie: %s", got)
	}
	// Test files alone do not turn tests into code.
	tests := &Task{Title: "Add unit tests", Spec: "edit internal/autonomous/manager_test.go"}
	if got, _ := ClassifyTask(tests, nil); got != config.TaskTypeTests {
		t.Errorf("test-file-only task: %s", got)
	}
}

func TestTaskTypeRuleOrderIsFixed(t *testing.T) {
	var order []string
	for _, r := range taskTypeRules {
		order = append(order, r.taskType)
	}
	want := []string{
		config.TaskTypeMergeConflictRepair, config.TaskTypeReviewRepair, config.TaskTypeBoundedLoadStep,
		config.TaskTypeLocaleStrings, config.TaskTypeFormattingCleanup, config.TaskTypeMCPAPISurface,
		config.TaskTypeUICode, config.TaskTypeDocsSet, config.TaskTypeTests,
	}
	if len(order) != len(want) {
		t.Fatalf("order %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("rule %d = %s, want %s", i, order[i], want[i])
		}
	}
}
