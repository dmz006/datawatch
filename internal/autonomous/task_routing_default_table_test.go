package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// Every built-in planner template must map to a known task type, and the
// shipped default routing profile must have a row for it and validate.
func TestDefaultRoutingTableCoversEveryPlannerTemplate(t *testing.T) {
	def := config.DefaultTaskRouting()
	prof := def.Profiles[config.TaskRoutingDefaultProfile]
	for _, tpl := range BuiltInTaskTemplates() {
		tt, ok := templateTaskTypes[tpl.ID]
		if !ok {
			t.Errorf("template %q has no task type in templateTaskTypes", tpl.ID)
			continue
		}
		if !config.IsTaskRoutingTaskType(tt) {
			t.Errorf("template %q maps to unknown task type %q", tpl.ID, tt)
		}
		if _, ok := prof[tt]; !ok {
			t.Errorf("default routing profile has no row for %q (template %q)", tt, tpl.ID)
		}
	}
	if errs := config.ValidateTaskRouting(def, config.TaskRoutingChecks{}); len(errs) != 0 {
		t.Errorf("shipped default table does not validate: %v", errs)
	}
}
