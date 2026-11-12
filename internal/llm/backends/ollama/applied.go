// applied.go records which worker-profile fields the ollama backend
// honours at launch, so the session start record can show the operator
// what was dropped.
package ollama

import "github.com/dmz006/datawatch/internal/workerprofile"

// supportedFields lists the worker-profile fields this backend honours.
// Ollama runs a model pinned at construction time
// (`ollama run <model>`), but takes no flag or setter for a profile's
// mcp, tools, skills, context, or effort.
var supportedFields = map[string]bool{
	"model": true,
}

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface: one outcome per worker-profile field, recorded
// against this backend's supported set.
func (b *Backend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}
