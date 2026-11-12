// question_context_ref.go — the computed context_ref on question reads.
//
// GET /api/questions/{id} and the rows of GET /api/questions carry a
// context_ref computed from the stored context links at read time; it is
// never written to the stored record.
package server

import (
	"io/fs"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// questionNameResolver is the optional part of the Automaton manager that
// names Automata and tasks by id. autonomous.API implements it; a manager
// without it leaves the id as the title.
type questionNameResolver interface {
	AutomatonName(id string) (string, bool)
	TaskName(id string) (string, bool)
}

// questionDocsFileExists is the docs tree check context_ref uses; a variable
// so a test can stand in for the embedded tree, which is mirrored from docs/
// at build time.
var questionDocsFileExists = servedDocsFile

// servedDocsFile reports whether a repository-relative path under docs/ is a
// file in the embedded docs tree the documents viewer serves.
func servedDocsFile(repoRel string) bool {
	if !strings.HasPrefix(repoRel, "docs/") {
		return false
	}
	info, err := fs.Stat(webFS, "web/"+repoRel)
	return err == nil && info.Mode().IsRegular()
}

// questionContextRefLookup builds the lookup for context_ref from what the
// server has wired.
func (s *Server) questionContextRefLookup() autonomous.ContextRefLookup {
	lk := autonomous.ContextRefLookup{DocExists: questionDocsFileExists}
	if s.autonomousMgr != nil {
		if r, ok := s.autonomousMgr.(questionNameResolver); ok {
			lk.AutomatonName = r.AutomatonName
			lk.TaskName = r.TaskName
		}
	}
	return lk
}

// questionWithContextRef is a stored question with its computed context_ref
// beside it. The embedded question's fields are flattened into the object.
type questionWithContextRef struct {
	autonomous.Question
	ContextRef autonomous.ContextRef `json:"context_ref"`
}

func (s *Server) withContextRef(q autonomous.Question, lk autonomous.ContextRefLookup) questionWithContextRef {
	return questionWithContextRef{Question: q, ContextRef: autonomous.QuestionContextRef(q, lk)}
}
