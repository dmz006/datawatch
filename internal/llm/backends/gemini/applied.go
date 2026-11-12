// applied.go records which worker-profile fields the gemini backend
// honours at launch, so the session start record can show the operator
// what was dropped.
package gemini

import "github.com/dmz006/datawatch/internal/workerprofile"

// supportedFields lists the worker-profile fields this backend honours.
// Gemini is started as `gemini -p <task>`: it takes no flag or setter for
// a profile's mcp, tools, skills, context, model, or effort, so it honours
// none of them.
var supportedFields = map[string]bool{}

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface: one outcome per worker-profile field, recorded
// against this backend's supported set.
func (b *Backend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}
