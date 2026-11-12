// applied.go records which worker-profile fields the opencode backends
// honour at launch, so the session start record can show the operator
// what was dropped.
package opencode

import "github.com/dmz006/datawatch/internal/workerprofile"

// supportedFields lists the worker-profile fields these backends honour.
// Opencode is started as `opencode run <task>` (or its ACP/one-shot
// variants): none of them take a flag or setter for a profile's mcp,
// tools, skills, context, model, or effort — model selection stays in
// opencode's own config — so they honour none of them.
var supportedFields = map[string]bool{}

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface: one outcome per worker-profile field, recorded
// against this backend's supported set.
func (b *Backend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface for the one-shot opencode-prompt backend.
func (b *PromptBackend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface for the opencode-acp backend.
func (b *ACPBackend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}
