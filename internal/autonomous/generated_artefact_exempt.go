// Generated documents that never hold a finished Automaton's work tree.
//
// The daemon writes a few documents into an Automaton's work tree on its own
// (the gate report, the parity issue draft and the drafts) and keeps a hash
// file beside each. Left untracked and unedited they are the daemon's own
// records, not uncommitted work, so they do not hold a merged work tree and
// are deleted with it.
//
// A file name is never enough. The exemption uses the same proof as the
// finish-time uncommitted-work check (dropUneditedGeneratedFiles): the
// generated marker, no edited line, and a body that still matches the recorded
// hash. A hand-written file, an edited document, a hash file without its
// document, a link and every tracked change still hold the work tree.
package autonomous

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/genartifacts"
)

// artefactGitFn runs one git command in a work tree.
type artefactGitFn func(ctx context.Context, args ...string) (string, error)

// splitGeneratedArtefactDirt takes the `git status --porcelain` output of the
// work tree at root and returns it without the daemon's own unedited generated
// documents, which it returns separately as work-tree-relative paths.
//
// git reports a folder that is wholly new as one line. Such a line is listed
// file by file only when a generated document could be inside it, and stays as
// it was when none is, or when the listing fails.
func splitGeneratedArtefactDirt(ctx context.Context, root, porcelain string, run artefactGitFn) (rest string, generated []string) {
	lines := strings.Split(porcelain, "\n")
	// listed holds, per line index, the untracked files that line stands for.
	listed := map[int][]string{}
	var untracked []UncommittedFile
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "?? ") {
			continue
		}
		p := line[3:]
		files := []string{p}
		if strings.HasSuffix(p, "/") {
			if !mayHoldGeneratedArtefact(p) {
				continue
			}
			files = listUntrackedFiles(ctx, p, run)
		}
		listed[i] = files
		for _, f := range files {
			if isRegularFile(root, f) {
				untracked = append(untracked, UncommittedFile{Path: f, Status: "??"})
			}
		}
	}
	exempt := uneditedArtefactSet(root, untracked)
	if len(exempt) == 0 {
		return porcelain, nil
	}
	var kept []string
	for i, line := range lines {
		files := listed[i]
		var other []string
		for _, f := range files {
			if exempt[f] {
				generated = append(generated, f)
			} else {
				other = append(other, f)
			}
		}
		if len(other) == len(files) {
			kept = append(kept, line) // nothing on this line is exempt
			continue
		}
		for _, f := range other {
			kept = append(kept, "?? "+f)
		}
	}
	rest = strings.Join(kept, "\n")
	if strings.TrimSpace(rest) == "" {
		rest = ""
	}
	return rest, generated
}

// uneditedArtefactSet returns the paths among files that are an unedited
// generated document, or the hash file of one that is in the list, and are
// regular files (never links).
func uneditedArtefactSet(root string, files []UncommittedFile) map[string]bool {
	kept := map[string]bool{}
	for _, f := range dropUneditedGeneratedFiles(root, files) {
		kept[f.Path] = true
	}
	out := map[string]bool{}
	for _, f := range files {
		if !kept[f.Path] && isRegularFile(root, f.Path) {
			out[f.Path] = true
		}
	}
	return out
}

// mayHoldGeneratedArtefact reports whether the untracked folder dir
// (work-tree-relative, with a trailing slash) is, or contains, a folder the
// generator writes to.
func mayHoldGeneratedArtefact(dir string) bool {
	for _, tracked := range genartifacts.DefaultPaths() {
		if strings.HasPrefix(path.Dir(tracked)+"/", dir) {
			return true
		}
	}
	return false
}

// listUntrackedFiles lists the untracked, not ignored files under dir. When
// they cannot be listed it returns dir itself, which is never exempt.
func listUntrackedFiles(ctx context.Context, dir string, run artefactGitFn) []string {
	out, err := run(ctx, "ls-files", "--others", "--exclude-standard", "-z", "--", dir)
	if err != nil {
		return []string{dir}
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if strings.ContainsAny(f, "\r\n") {
			return []string{dir} // not expressible as a status line
		}
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return []string{dir}
	}
	return files
}

// isRegularFile reports whether root/rel is a regular file, not following a
// final link.
func isRegularFile(root, rel string) bool {
	st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && st.Mode().IsRegular()
}

// removeGeneratedArtefacts deletes the exempt files before the work tree is
// removed, so the removal itself needs no force. Every file is verified again
// first: if any is no longer an unedited generated document, nothing is
// deleted and the error names it.
func removeGeneratedArtefacts(root string, rels []string) error {
	files := make([]UncommittedFile, 0, len(rels))
	for _, r := range rels {
		files = append(files, UncommittedFile{Path: r, Status: "??"})
	}
	exempt := uneditedArtefactSet(root, files)
	for _, r := range rels {
		if !exempt[r] {
			return fmt.Errorf("%s changed after it was checked; nothing was deleted", r)
		}
	}
	for _, r := range rels {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(r))); err != nil {
			return fmt.Errorf("delete generated document %s: %w", r, err)
		}
	}
	return nil
}
