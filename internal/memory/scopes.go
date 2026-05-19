// v7.0.0 S5 — scope-hierarchy memory model.
//
// Operator-decided 2026-05-08 (BL295 ASK Q17):
//   Layered: persona-global → persona-in-project → project-shared → session-local
//   Recalls walk top-down. Writes go to the most-specific layer
//   (session-local) unless explicitly promoted.
//
// Operator-decided 2026-05-08 (BL295 ASK Q16):
//   Borrow = read-only references via memory_recall(namespace=...)
//   Seed   = explicit operator-curated copy (memory_seed --from --to --filter)
//
// Operator-decided 2026-05-08 (BL295 ASK Q29):
//   Promote preserves breadcrumb metadata
//   {session, persona, run, promoted_at, promoted_by}.
//
// Naming distinction: this is NOT the v6.x Layers wake-up stack
// (L0 identity → L3 deep search by token-budget). That layer concept
// stays. This S5 work is a SCOPE hierarchy (ownership / visibility),
// orthogonal to wake-up size.
//
// Storage: layers project onto the existing Backend's
// (projectDir, role, sessionID) tuple by convention:
//
//   ScopeSessionLocal      → (projectDir,    role,                  sessionID)
//   ScopeProjectShared     → (projectDir,    role,                  ""       )
//   ScopePersonaInProject  → (projectDir,    "persona/"+personaName, ""      )
//   ScopePersonaGlobal     → (""+,           "persona/"+personaName, ""      )
//
// (The +"" projectDir for PersonaGlobal is a sentinel — backends key
// rows on projectDir, so an empty value is fine for "no project".)
//
// Backend changes are non-breaking — this layer is pure convention
// over the existing Backend interface.

package memory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Scope enumerates the 4 ownership/visibility layers per BL295 Q17.
type Scope string

const (
	ScopePersonaGlobal    Scope = "persona-global"     // per persona, ALL projects
	ScopePersonaInProject Scope = "persona-in-project" // per persona, current project
	ScopeProjectShared    Scope = "project-shared"     // current datawatch episodic memory (cross-persona)
	ScopeSessionLocal     Scope = "session-local"      // per council run / session
	ScopeDiscussion       Scope = "discussion"         // per-discussion federated shared memory (BL332)
)

// AllScopesTopDown is the recall walk order — most-general to most-
// specific. Recall returns merged results carrying which-scope each
// memory came from.
var AllScopesTopDown = []Scope{
	ScopePersonaGlobal,
	ScopePersonaInProject,
	ScopeProjectShared,
	ScopeSessionLocal,
	ScopeDiscussion,
}

// ScopeRef ties a Scope to its keying triple (projectDir, role,
// sessionID). Helper for resolving a Scope into the Backend's
// existing API surface.
type ScopeRef struct {
	Scope     Scope
	Persona   string // empty for project-shared / session-local
	Project   string // empty for persona-global
	SessionID string // empty for everything except session-local
}

// Resolve returns the (projectDir, role, sessionID) tuple the Backend
// expects for this scope/persona/project/session combination.
func (sr ScopeRef) Resolve() (projectDir, role, sessionID string) {
	switch sr.Scope {
	case ScopePersonaGlobal:
		return "", "persona/" + sr.Persona, ""
	case ScopePersonaInProject:
		return sr.Project, "persona/" + sr.Persona, ""
	case ScopeProjectShared:
		return sr.Project, "", ""
	case ScopeSessionLocal:
		return sr.Project, "", sr.SessionID
	case ScopeDiscussion:
		// discussionID is passed in the SessionID field by convention.
		// An empty SessionID means no discussion is scoped — callers must
		// supply the discussion ID.
		return "", "discussion/" + sr.SessionID, ""
	}
	return sr.Project, "", ""
}

// ScopedMemory wraps a Memory with the Scope it was found in. Used
// in merged recall results so the caller can show "from X layer".
type ScopedMemory struct {
	Memory
	Scope Scope `json:"scope"`
}

// ScopedRecall walks the layers top-down and returns merged results.
// nil layers = AllScopesTopDown. topK is per-layer; final list is
// concatenated in walk order (most-general to most-specific).
//
// Backend errors on individual layers are logged but don't fail the
// walk — recall is best-effort across layers (a missing project
// shouldn't suppress persona-global hits).
func ScopedRecall(b Backend, queryVec []float32, persona, project, sessionID string, layers []Scope, topK int) ([]ScopedMemory, error) {
	if b == nil {
		return nil, errors.New("memory backend nil")
	}
	if topK <= 0 {
		topK = 10
	}
	if len(layers) == 0 {
		layers = AllScopesTopDown
	}
	out := []ScopedMemory{}
	for _, sc := range layers {
		ref := ScopeRef{Scope: sc, Persona: persona, Project: project, SessionID: sessionID}
		// Only walk persona-* layers when persona is set.
		if (sc == ScopePersonaGlobal || sc == ScopePersonaInProject) && persona == "" {
			continue
		}
		// Only walk session-local when sessionID is set.
		if sc == ScopeSessionLocal && sessionID == "" {
			continue
		}
		// Only walk discussion when sessionID (discussion ID) is set.
		if sc == ScopeDiscussion && sessionID == "" {
			continue
		}
		dir, _, _ := ref.Resolve()
		var hits []Memory
		var err error
		if len(queryVec) == 0 {
			// No query vector — fall back to most-recent. Useful for
			// "what's in this scope" introspection + tests.
			hits, err = b.ListRecent(dir, topK)
		} else {
			hits, err = b.Search(dir, queryVec, topK)
		}
		if err != nil {
			continue // best-effort
		}
		for i := range hits {
			out = append(out, ScopedMemory{Memory: hits[i], Scope: sc})
		}
	}
	return out, nil
}

// SeedFilter narrows which memories Seed copies. Empty filter copies
// every entry from the source. Today supports role-prefix and a
// simple substring match on Content.
type SeedFilter struct {
	RolePrefix       string // e.g. "persona/security-skeptic" → only that persona's entries
	ContentSubstring string // case-insensitive substring on Memory.Content
	Since            time.Time // entries newer than this only
}

// Seed copies entries from a source ScopeRef into a target ScopeRef.
// Operator-driven (BL295 Q16) — the result is target's own
// permanent memory; no ongoing link to source.
//
// Returns the number of entries copied + the first error if Save
// fails partway through. Partial copies are NOT rolled back.
func Seed(b Backend, from, to ScopeRef, filter SeedFilter, n int) (int, error) {
	if b == nil {
		return 0, errors.New("memory backend nil")
	}
	if n <= 0 {
		n = 1000
	}
	srcDir, srcRole, _ := from.Resolve()
	dstDir, _, dstSession := to.Resolve()
	var src []Memory
	var err error
	if filter.RolePrefix != "" || srcRole != "" {
		role := filter.RolePrefix
		if role == "" {
			role = srcRole
		}
		src, err = b.ListByRole(srcDir, role, n)
	} else {
		src, err = b.ListRecent(srcDir, n)
	}
	if err != nil {
		return 0, fmt.Errorf("seed: list source: %w", err)
	}
	count := 0
	for _, m := range src {
		if filter.ContentSubstring != "" && !strings.Contains(strings.ToLower(m.Content), strings.ToLower(filter.ContentSubstring)) {
			continue
		}
		if !filter.Since.IsZero() && m.CreatedAt.Before(filter.Since) {
			continue
		}
		// Annotate the copied entry's content with a borrowed-from
		// breadcrumb so the operator can trace provenance.
		annotated := m.Content + "\n\n_(seeded from " + string(from.Scope) + ":" + from.SessionID + " at " + time.Now().UTC().Format(time.RFC3339) + ")_"
		// Embedding is recomputed on the destination side via the
		// retriever — pass nil so the backend re-embeds from content.
		_, serr := b.Save(dstDir, annotated, m.Summary, "seeded", dstSession, nil)
		if serr != nil {
			return count, fmt.Errorf("seed: save target: %w", serr)
		}
		count++
	}
	return count, nil
}

// Breadcrumb is the metadata trail preserved on Promote per BL295 Q29.
type Breadcrumb struct {
	Session     string    `json:"session,omitempty"`
	Persona     string    `json:"persona,omitempty"`
	Run         string    `json:"run,omitempty"`
	PromotedAt  time.Time `json:"promoted_at"`
	PromotedBy  string    `json:"promoted_by"` // "operator" | "<persona-name>"
	FromScope   Scope     `json:"from_scope"`
	ToScope     Scope     `json:"to_scope"`
}

// Promote moves a memory from one scope to another (typically
// session-local → project-shared, or persona-in-project → persona-
// global). Preserves breadcrumb metadata in the new entry's content
// suffix so the operator can trace provenance.
//
// The original memory is NOT deleted — promotion is additive (a copy
// at the new scope). Caller can Delete the original separately.
//
// Returns the new memory id + the breadcrumb that was attached.
func Promote(b Backend, memID int64, from, to ScopeRef, breadcrumb Breadcrumb) (int64, *Breadcrumb, error) {
	if b == nil {
		return 0, nil, errors.New("memory backend nil")
	}
	srcDir, _, _ := from.Resolve()
	dstDir, _, dstSession := to.Resolve()
	// Find the source memory by id (best-effort; backends differ in
	// how Get-by-id works, so we rely on ListRecent + filter).
	rows, err := b.ListRecent(srcDir, 1000)
	if err != nil {
		return 0, nil, fmt.Errorf("promote: list source: %w", err)
	}
	var src *Memory
	for i := range rows {
		if rows[i].ID == memID {
			src = &rows[i]
			break
		}
	}
	if src == nil {
		return 0, nil, fmt.Errorf("promote: memory %d not found in source scope %s", memID, from.Scope)
	}
	breadcrumb.PromotedAt = time.Now().UTC()
	breadcrumb.FromScope = from.Scope
	breadcrumb.ToScope = to.Scope
	if breadcrumb.PromotedBy == "" {
		breadcrumb.PromotedBy = "operator"
	}
	annotated := src.Content + "\n\n_(promoted " + string(from.Scope) + " → " + string(to.Scope) + " at " + breadcrumb.PromotedAt.Format(time.RFC3339) + " by " + breadcrumb.PromotedBy + ")_"
	// Embedding is recomputed on the destination side via the
	// retriever — pass nil so the backend re-embeds from content.
	id, serr := b.Save(dstDir, annotated, src.Summary, "promoted", dstSession, nil)
	if serr != nil {
		return 0, nil, fmt.Errorf("promote: save target: %w", serr)
	}
	return id, &breadcrumb, nil
}

// BorrowReadOnly lets an operator query another session/scope's
// memory as read-only context per BL295 Q16. This is just a thin
// alias around Search with the source scope's projectDir — the
// caller doesn't merge results into their own scope. Useful for
// "marketing session wants to peek at web-project session's
// learnings without copying them in."
func BorrowReadOnly(b Backend, from ScopeRef, queryVec []float32, topK int) ([]Memory, error) {
	if b == nil {
		return nil, errors.New("memory backend nil")
	}
	if topK <= 0 {
		topK = 10
	}
	dir, _, _ := from.Resolve()
	return b.Search(dir, queryVec, topK)
}
