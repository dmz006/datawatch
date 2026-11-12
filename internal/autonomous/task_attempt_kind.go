package autonomous

import (
	"path"
	"strings"
)

// Attempt kinds a task can be classified as, used to pick how much a task
// attempt should cost.
const (
	attemptKindImplementation = "implementation"
	attemptKindDocs           = "docs"
	attemptKindNote           = "note"
	attemptKindFinalGate      = "final_gate"
)

// taskAttemptKind classifies a task by what its attempt produces. Rules, in
// order: a final_gate task is final_gate; a read-only task or one with no
// listed files is a note; a task whose every listed file is documentation is
// docs; anything else is implementation.
func taskAttemptKind(t *Task) string {
	if t == nil {
		return attemptKindNote
	}
	if t.Kind == TaskKindFinalGate {
		return attemptKindFinalGate
	}
	if t.ReadOnly || len(t.FilesPlanned) == 0 {
		return attemptKindNote
	}
	for _, f := range t.FilesPlanned {
		if !isDocumentationFile(f) {
			return attemptKindImplementation
		}
	}
	return attemptKindDocs
}

// isDocumentationFile reports whether p has a .md extension or sits under a
// docs directory.
func isDocumentationFile(p string) bool {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if strings.EqualFold(path.Ext(p), ".md") {
		return true
	}
	dir := path.Dir(p)
	for _, seg := range strings.Split(dir, "/") {
		if strings.EqualFold(seg, "docs") {
			return true
		}
	}
	return false
}
