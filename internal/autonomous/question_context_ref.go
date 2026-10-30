// question_context_ref.go — the computed context reference of a question.
//
// A stored Question carries its context as bare links (ProposalPath, PRDID,
// TaskID, RegistryRowKey). QuestionContextRef turns those links into one
// ContextRef value a surface can show and follow: a kind, a title, the id,
// the repository-relative document path and, for a proposal inside the served
// docs tree, the in-app documents viewer link. It is computed on every read
// and never stored on the record.
package autonomous

import (
	"net/url"
	"path"
	"strings"
)

// ContextRef kinds.
const (
	ContextRefProposal    = "proposal"
	ContextRefAutomaton   = "automaton"
	ContextRefTask        = "task"
	ContextRefRegistryRow = "registry_row"
	ContextRefNone        = "none"
)

// contextRefDocsRoot is the repository directory the in-app documents viewer
// serves; contextRefViewerBase is the viewer page the PWA links to, which
// takes "#docs/<path under docs>" in its hash.
const (
	contextRefDocsRoot   = "docs/"
	contextRefViewerBase = "/diagrams.html#docs/"
)

// ContextRef is the computed view of a question's context links.
type ContextRef struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	ID        string `json:"id"`
	DocPath   string `json:"doc_path"`
	ViewerURL string `json:"viewer_url"`
}

// ContextRefLookup resolves what the stored links alone cannot tell. Every
// field is optional: a nil AutomatonName or TaskName means the name is not
// resolvable (the id is the title), and a nil DocExists means the docs tree
// is not checked for the file (the path rules still apply).
type ContextRefLookup struct {
	// AutomatonName returns the stored name of an Automaton by id.
	AutomatonName func(id string) (string, bool)
	// TaskName returns the stored name of a task by id.
	TaskName func(id string) (string, bool)
	// DocExists reports whether a repository-relative path (always under
	// docs/) is a file in the served docs tree.
	DocExists func(repoRelPath string) bool
}

// QuestionContextRef computes the context reference of q. A context kind
// whose link is missing, and the none context, give the none kind with every
// other field empty.
func QuestionContextRef(q Question, lk ContextRefLookup) ContextRef {
	switch q.Context {
	case QuestionContextProposal:
		if q.ProposalPath == "" {
			break
		}
		ref := ContextRef{Kind: ContextRefProposal, Title: path.Base(q.ProposalPath)}
		if checkProposalPath(q.ProposalPath) != nil {
			// Not repository-relative: never echoed back as a path.
			ref.Title = ""
			return ref
		}
		ref.DocPath = q.ProposalPath
		ref.ViewerURL = proposalViewerURL(q.ProposalPath, lk.DocExists)
		return ref
	case QuestionContextPrd:
		if q.PRDID == "" {
			break
		}
		return ContextRef{
			Kind:  ContextRefAutomaton,
			Title: resolvedName(q.PRDID, lk.AutomatonName),
			ID:    q.PRDID,
		}
	case QuestionContextTask:
		if q.TaskID == "" {
			break
		}
		return ContextRef{
			Kind:  ContextRefTask,
			Title: resolvedName(q.TaskID, lk.TaskName),
			ID:    q.TaskID,
		}
	case QuestionContextRegistryRow:
		if q.RegistryRowKey == "" {
			break
		}
		return ContextRef{
			Kind:  ContextRefRegistryRow,
			Title: q.RegistryRowKey,
			ID:    q.RegistryRowKey,
		}
	}
	return ContextRef{Kind: ContextRefNone}
}

// resolvedName is the stored name when the lookup finds a non-blank one, and
// the id otherwise.
func resolvedName(id string, lookup func(string) (string, bool)) string {
	if lookup != nil {
		if name, ok := lookup(id); ok && strings.TrimSpace(name) != "" {
			return name
		}
	}
	return id
}

// proposalViewerURL is the documents viewer link for a repository-relative
// proposal path, or "" when the path is not a markdown file inside the served
// docs tree. The path must already be clean (no '.', '..', empty or
// backslash segments and no control characters) so the link can never point
// outside docs/.
func proposalViewerURL(p string, exists func(string) bool) string {
	if !strings.HasPrefix(p, contextRefDocsRoot) || !strings.HasSuffix(p, ".md") {
		return ""
	}
	if path.Clean(p) != p || strings.ContainsAny(p, "\\\x00") {
		return ""
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	rest := strings.TrimPrefix(p, contextRefDocsRoot)
	segs := strings.Split(rest, "/")
	for i, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
		segs[i] = url.PathEscape(seg)
	}
	if exists != nil && !exists(p) {
		return ""
	}
	return contextRefViewerBase + strings.Join(segs, "/")
}
