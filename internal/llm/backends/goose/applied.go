// applied.go records which worker-profile fields the goose backends
// honour at launch, so the session start record can show the operator
// what was dropped.
package goose

import "github.com/dmz006/datawatch/internal/workerprofile"

// supportedFields lists the worker-profile fields these backends honour.
// Goose accepts a model pin (the manager's duck-typed SetModel), but no
// flag or setter for a profile's mcp, tools, skills, context, or effort.
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

// RecordApplied implements the session manager's duck-typed
// `interface{ RecordApplied(workerprofile.WorkerProfile) []workerprofile.FieldOutcome }`
// optional interface for the one-shot goose-prompt backend.
func (b *PromptBackend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}
