// applied.go records which worker-profile fields the shell backend
// honours at launch, so the session start record can show the operator
// what was dropped.
package shell

import "github.com/dmz006/datawatch/internal/workerprofile"

// supportedFields lists the worker-profile fields this backend honours.
// The shell backend runs a plain shell (or script) with no LLM: it has no
// notion of a profile's mcp, tools, skills, context, model, or effort, so
// it honours none of them.
var supportedFields = map[string]bool{}

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface: one outcome per worker-profile field, recorded
// against this backend's supported set.
func (b *Backend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}
