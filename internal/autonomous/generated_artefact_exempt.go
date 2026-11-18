// Generated artefacts that never hold a finished Automaton.
//
// The daemon writes a few files into an Automaton's work tree on its own:
// the final gate report, the hash file kept beside it, the parity issue
// draft and the drafts folder. They are produced, never hand-written, so
// when they are untracked they must not count as uncommitted work that
// holds a merged Automaton's work tree. They are deleted with the work tree.
//
// The exemption is narrow. Only an untracked ("??") record whose path is
// one of those artefacts is exempt; a modified or staged record, and any
// other untracked file, still holds the work tree. Fails closed: a quoted
// or unparseable path stays in the dirt.
package autonomous

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/genartifacts"
)

const (
	// generatedHashSuffix is appended to a gate report path to name its
	// hash file.
	generatedHashSuffix = ".sha256"
	// generatedDraftsDir is the drafts folder.
	generatedDraftsDir = "docs/plans/drafts"
	// generatedParityDir and generatedParitySuffix name the parity issue
	// draft the parity-issue template writes.
	generatedParityDir    = "docs/plans/parity-issues"
	generatedParitySuffix = "-parity-draft.md"
)

// isGeneratedArtefact reports whether p (slash-separated, relative to the
// work tree) is a generated artefact. extraReports names gate report paths
// other than the default one (a task may name its own).
func isGeneratedArtefact(p string, extraReports []string) bool {
	if p == "" || strings.HasPrefix(p, "\"") {
		return false
	}
	clean := path.Clean(filepath.ToSlash(p))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return false
	}
	reports := append([]string{defaultGateReportPath}, extraReports...)
	for _, r := range reports {
		r = path.Clean(filepath.ToSlash(r))
		if r == "." || r == "" {
			continue
		}
		if clean == r || clean == r+generatedHashSuffix {
			return true
		}
	}
	// The generator's own paths: a gate report, parity draft or draft named
	// by the generator, and the hash sidecar it keeps beside each of them.
	// A parity folder file named only by the Automaton id cannot be told from
	// a hand-written one by name, so it is not exempt here.
	if path.Dir(clean) != generatedParityDir && genartifacts.MatchesTrackedPath(clean) {
		return true
	}
	if base := path.Base(clean); strings.HasPrefix(base, ".") && strings.HasSuffix(base, ".genhash") && len(base) > len(".genhash")+1 {
		target := path.Join(path.Dir(clean), strings.TrimSuffix(base[1:], ".genhash"))
		if isGeneratedArtefact(target, extraReports) {
			return true
		}
	}
	if clean == generatedDraftsDir || strings.HasPrefix(clean, generatedDraftsDir+"/") {
		return true
	}
	if path.Dir(clean) == generatedParityDir && strings.HasSuffix(clean, generatedParitySuffix) {
		return true
	}
	return false
}

// SplitGeneratedArtefacts splits `git status --porcelain
// --untracked-files=all` output into the generated artefacts (untracked
// records only; returned as paths) and the remaining records, which are
// what still counts as uncommitted work.
func SplitGeneratedArtefacts(porcelain string, extraReports []string) (generated []string, remaining []string) {
	for _, line := range strings.Split(porcelain, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) >= 4 && line[2] == ' ' && line[:2] == "??" && isGeneratedArtefact(line[3:], extraReports) {
			generated = append(generated, path.Clean(line[3:]))
			continue
		}
		remaining = append(remaining, line)
	}
	return generated, remaining
}

// RemoveGeneratedArtefacts deletes the listed artefact files from the work
// tree so that `git worktree remove` (never forced) is not refused for
// them. It removes files only, never a folder with its content, and skips
// any path that resolves outside the work tree or is a symlink. Empty
// folders left behind are removed. It returns the paths it deleted.
func RemoveGeneratedArtefacts(worktree string, paths []string) []string {
	var removed []string
	if worktree == "" {
		return nil
	}
	root := filepath.Clean(worktree)
	dirs := map[string]struct{}{}
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if !strings.HasPrefix(full, root+string(filepath.Separator)) {
			continue
		}
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(full); err != nil {
			continue
		}
		removed = append(removed, p)
		for d := filepath.Dir(full); strings.HasPrefix(d, root+string(filepath.Separator)); d = filepath.Dir(d) {
			dirs[d] = struct{}{}
		}
	}
	// Deepest first; os.Remove only removes empty folders.
	for len(dirs) > 0 {
		deepest := ""
		for d := range dirs {
			if len(d) > len(deepest) {
				deepest = d
			}
		}
		delete(dirs, deepest)
		_ = os.Remove(deepest)
	}
	return removed
}
