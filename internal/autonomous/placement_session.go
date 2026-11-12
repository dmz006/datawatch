package autonomous

import (
	"path/filepath"

	"github.com/dmz006/datawatch/internal/session"
)

// SessionPlacement maps a session record onto the Placement the placement
// tags are derived from. It is a pure function: it reads s and the arguments
// and nothing else.
//
//   - Node is s.ComputeNodeRef; when that is empty it is localNode, the node
//     name the capacity ledger records for local work (the caller passes it).
//   - Peer is never set here: the peer comes from the placement record only.
//   - Model is not an axis and is never set.
//   - Lane is laneForBackend(s.BackendFamily).
//   - Project is filepath.Base(s.ProjectDir) when ProjectDir is set.
//   - Automaton is s.PRDID; Automation is automationOf(s.PRDID) when the
//     Automaton is set and automationOf is non-nil.
//
// A field whose source is empty stays empty, so it produces no tag. Nothing
// is invented: a value that the record does not hold is left unset.
func SessionPlacement(s *session.Session, localNode string, automationOf func(prdID string) string) Placement {
	if s == nil {
		return Placement{}
	}
	p := Placement{
		Node:      s.ComputeNodeRef,
		Backend:   s.BackendFamily,
		Lane:      laneForBackend(s.BackendFamily),
		Role:      s.LaunchRole,
		Kind:      s.LaunchKind,
		Automaton: s.PRDID,
		Stage:     s.LaunchStage,
	}
	if p.Node == "" {
		p.Node = localNode
	}
	if s.ProjectDir != "" {
		p.Project = filepath.Base(s.ProjectDir)
	}
	if p.Automaton != "" && automationOf != nil {
		p.Automation = automationOf(p.Automaton)
	}
	return p
}
