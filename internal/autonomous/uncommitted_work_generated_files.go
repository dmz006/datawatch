package autonomous

import (
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// dropUneditedGeneratedFiles serves only the finish-time check and never the
// per-task check. It returns a new slice, in the input order, without the
// entries that are the daemon's own unedited generated documents: an entry
// whose Path is still exactly what the daemon last generated for it
// (genartifacts.IsUneditedGenerated), and an entry that is the sidecar of one
// of those unedited documents (genartifacts.SidecarRelPath of the document),
// so the sidecar goes with its document. A sidecar whose document is not in
// the list, or whose document has been edited, is kept. The input slice is not
// modified.
func dropUneditedGeneratedFiles(root string, files []UncommittedFile) []UncommittedFile {
	sidecarOfUnedited := make(map[string]bool, len(files))
	for _, g := range files {
		if genartifacts.IsUneditedGenerated(root, g.Path) {
			sidecarOfUnedited[genartifacts.SidecarRelPath(g.Path)] = true
		}
	}

	var out []UncommittedFile
	for _, f := range files {
		if genartifacts.IsUneditedGenerated(root, f.Path) {
			continue
		}
		if sidecarOfUnedited[f.Path] {
			continue
		}
		out = append(out, f)
	}
	return out
}
