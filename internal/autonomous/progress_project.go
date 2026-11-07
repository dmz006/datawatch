package autonomous

import "strings"

// ProjectKeyNone is the key counted for an Automaton that carries no project.
const ProjectKeyNone = "(none)"

// ProjectKey returns the project key of an Automaton record. The explicit
// project profile wins; the project directory is the fallback; a record with
// neither (or a nil record) returns ProjectKeyNone.
func ProjectKey(p *PRD) string {
	if p == nil {
		return ProjectKeyNone
	}
	if k := strings.TrimSpace(p.ProjectProfile); k != "" {
		return k
	}
	if k := strings.TrimSpace(p.ProjectDir); k != "" {
		return k
	}
	return ProjectKeyNone
}
