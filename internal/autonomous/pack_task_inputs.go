// pack_task_inputs.go — records a pack task's resolved inputs on the task.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/packs"
)

// RecordPackTaskInputs resolves the pack's inputs for a task (Automaton, then
// project, then global, then the declared default), redacts secret inputs, and
// stores the redacted values on t.PackInputs. The stored map is a private copy,
// so a later configuration change cannot alter it: a task keeps the values it
// was created with. A resolve error leaves the task unchanged and is returned.
func RecordPackTaskInputs(t *Task, pack *packs.Pack, automaton, project, global map[string]string) error {
	if t == nil || pack == nil {
		return nil
	}
	resolved, err := packs.ResolveInputs(pack, automaton, project, global)
	if err != nil {
		return err
	}
	redacted := packs.RedactInputs(resolved, pack.Inputs)
	stored := make(map[string]string, len(redacted))
	for k, v := range redacted {
		stored[k] = v
	}
	t.PackInputs = stored
	return nil
}
