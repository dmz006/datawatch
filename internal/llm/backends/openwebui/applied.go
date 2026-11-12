// applied.go records which worker-profile fields the openwebui backends
// honour at launch, so the session start record can show the operator
// what was dropped.
package openwebui

import "github.com/dmz006/datawatch/internal/workerprofile"

// supportedFields lists the worker-profile fields these backends honour.
// OpenWebUI chat completions are sent to a model pinned at construction
// time, but the API call takes nothing for a profile's mcp, tools, skills,
// context, or effort.
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
// optional interface for the openwebui interactive backend.
func (b *InteractiveBackend) RecordApplied(p workerprofile.WorkerProfile) []workerprofile.FieldOutcome {
	return workerprofile.RecordApplied(p, supportedFields)
}
