package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const modeTestCustomID = "custom_note"

var modeTestBuiltInIDs = []string{
	TemplateChangelogEntry,
	TemplateParityIssueDraft,
	TemplateDocsSet,
	TemplateBoundedLoadStep,
	TemplateFinalGate,
	TemplateVersionBumpLeftToMerge,
}

// modeTestConfig builds a config where every listed id has the given mode.
// daemon is only valid for daemon-capable ids; callers respect that.
func modeTestConfig(modes map[string]string) *config.Config {
	cfg := &config.Config{}
	cfg.Autonomous.TaskTemplates.Enabled = true
	cfg.Autonomous.TaskTemplates.Mode = modes
	cfg.Autonomous.TaskTemplates.Templates = []config.TaskTemplateDef{{
		ID:    modeTestCustomID,
		Title: "Custom step for {{feature}}",
		Spec:  "Do the custom step for '{{feature}}'.",
		Kind:  "docs",
		Files: "docs/custom.md",
	}}
	return cfg
}

func modeTestFind(stories []Story, id string) *Task {
	for _, s := range stories {
		for i := range s.Tasks {
			if s.Tasks[i].ID == templateTaskID(s.ID, id) {
				return &s.Tasks[i]
			}
		}
	}
	return nil
}

func modeTestDaemonCapable(id string) bool {
	for _, d := range config.DaemonCapableTaskTemplateIDs() {
		if d == id {
			return true
		}
	}
	return false
}

func TestApplyTaskTemplatesModeEachMode(t *testing.T) {
	ids := append(append([]string(nil), modeTestBuiltInIDs...), modeTestCustomID)
	for _, id := range ids {
		for _, mode := range []string{"llm", "daemon", "off"} {
			if mode == "daemon" && !modeTestDaemonCapable(id) {
				continue
			}
			t.Run(id+"/"+mode, func(t *testing.T) {
				cfg := modeTestConfig(map[string]string{id: mode})
				stories := []Story{testTaskTemplateStory(t)}
				out, _ := ApplyTaskTemplates(stories, "feature", "", cfg)
				task := modeTestFind(out, id)
				switch mode {
				case "off":
					if task != nil {
						t.Fatalf("mode off still created task %s", task.ID)
					}
				case "llm":
					if task == nil {
						t.Fatal("mode llm created no task")
					}
					if task.DaemonRun {
						t.Error("mode llm set DaemonRun")
					}
				case "daemon":
					if task == nil {
						t.Fatal("mode daemon created no task")
					}
					if !task.DaemonRun {
						t.Error("mode daemon did not set DaemonRun")
					}
					if task.ReadOnly {
						t.Error("daemon task is read-only")
					}
				}
				if task != nil {
					if task.Status != TaskQueued {
						t.Errorf("status = %q, want queued", task.Status)
					}
					if !strings.Contains(task.Spec, TaskTemplateIDMarker(id)) {
						t.Errorf("spec %q lacks marker", task.Spec)
					}
				}
			})
		}
	}
}

func TestApplyTaskTemplatesModeDaemonKeepsLLMShape(t *testing.T) {
	for _, id := range config.DaemonCapableTaskTemplateIDs() {
		llm, _ := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "feature", "", modeTestConfig(map[string]string{id: "llm"}))
		dae, _ := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "feature", "", modeTestConfig(map[string]string{id: "daemon"}))
		a, b := modeTestFind(llm, id), modeTestFind(dae, id)
		if a == nil || b == nil {
			t.Fatalf("%s: missing task (llm=%v daemon=%v)", id, a, b)
		}
		if a.ID != b.ID || a.Title != b.Title || a.Spec != b.Spec ||
			strings.Join(a.FilesPlanned, ",") != strings.Join(b.FilesPlanned, ",") ||
			strings.Join(a.DependsOn, ",") != strings.Join(b.DependsOn, ",") {
			t.Errorf("%s: daemon task differs from llm task beyond DaemonRun:\nllm=%+v\ndaemon=%+v", id, a, b)
		}
	}
}

func TestApplyTaskTemplatesModeIdempotent(t *testing.T) {
	modes := map[string]string{
		TemplateChangelogEntry:   "daemon",
		TemplateParityIssueDraft: "off",
		TemplateDocsSet:          "llm",
		modeTestCustomID:         "off",
	}
	cfg := modeTestConfig(modes)
	first, applied1 := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "feature", "", cfg)
	if len(applied1) == 0 {
		t.Fatal("first run applied nothing")
	}
	second, applied2 := ApplyTaskTemplates(first, "feature", "", cfg)
	if len(applied2) != 0 {
		t.Errorf("second run applied %v, want none", applied2)
	}
	if len(second[0].Tasks) != len(first[0].Tasks) {
		t.Errorf("task count changed %d -> %d", len(first[0].Tasks), len(second[0].Tasks))
	}
	for _, id := range []string{TemplateParityIssueDraft, modeTestCustomID} {
		if modeTestFind(second, id) != nil {
			t.Errorf("off template %s was added", id)
		}
	}
	if tk := modeTestFind(second, TemplateChangelogEntry); tk == nil || !tk.DaemonRun {
		t.Error("daemon task lost or lost DaemonRun")
	}
}

func TestApplyTaskTemplatesModeEnabledOverrideWins(t *testing.T) {
	cfg := modeTestConfig(map[string]string{TemplateChangelogEntry: "daemon"})
	const dir = "project/a"
	off := false
	cfg.Autonomous.TaskTemplates.Projects = map[string]config.TaskTemplateProjectOverride{dir: {Enabled: &off}}
	out, applied := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "feature", dir, cfg)
	if len(applied) != 0 || modeTestFind(out, TemplateChangelogEntry) != nil {
		t.Errorf("disabled project still got templates: %v", applied)
	}
	// Another project keeps the global setting.
	out, _ = ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "feature", "project/b", cfg)
	if tk := modeTestFind(out, TemplateChangelogEntry); tk == nil || !tk.DaemonRun {
		t.Error("other project lost its daemon task")
	}
	// Enabled override beats a globally disabled config.
	on := true
	cfg.Autonomous.TaskTemplates.Enabled = false
	cfg.Autonomous.TaskTemplates.Projects = map[string]config.TaskTemplateProjectOverride{dir: {Enabled: &on}}
	out, _ = ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "feature", dir, cfg)
	if tk := modeTestFind(out, TemplateChangelogEntry); tk == nil || !tk.DaemonRun {
		t.Error("enabled override did not win over a disabled global")
	}
}
