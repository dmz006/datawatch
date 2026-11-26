// Cleanup of datawatch-specific scratch files written into a project
// directory during an autonomous run (decompose/verify outputs). Called at
// the end of a PRD's run; unlike CleanupArtifacts (third-party backend
// files), it never deletes anything that git tracks, never touches
// anything outside the project directory, and degrades safely — reporting
// unremovable candidates as leftovers — when git is unavailable or the
// project directory is not in a repository.
package tooling

import (
	"bufio"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// isRunArtifactName reports whether a file basename matches one of
// datawatch's known run-scratch naming patterns:
//
//	.decompose-output-*.json      (decompose session output)
//	.verify-output-*.json         (per-task verification output)
//	decompose-output.json         (decompose output, no session suffix)
//	decompose-output.json.failed  (decompose output, failure marker)
func isRunArtifactName(name string) bool {
	switch {
	case strings.HasPrefix(name, ".decompose-output-") && strings.HasSuffix(name, ".json"):
		return true
	case strings.HasPrefix(name, ".verify-output-") && strings.HasSuffix(name, ".json"):
		return true
	}
	return name == "decompose-output.json" || name == "decompose-output.json.failed"
}

type fileTrackingState int

const (
	fileUntracked fileTrackingState = iota
	fileTracked
	fileTrackingUnknown
)

// projectTracks asks the project folder's own repository whether it tracks rel.
// It can track a file under a folder that later became a repository of its own,
// which that inner repository knows nothing about.
func projectTracks(gitBin, projectDir, rel string) bool {
	return exec.Command(gitBin, "-C", projectDir, "ls-files", "--error-unmatch", "--", rel).Run() == nil
}

// gitTracking asks the repository that holds the file whether it tracks it.
// git runs in the file's own folder: a project folder can hold other
// repositories (clones, linked work trees, submodules), and the project's own
// repository knows nothing about what those track. Only git's "no such tracked
// file" answer (exit status 1) means untracked; any other failure is unknown.
// A file is deleted only when projectTracks says no and this says untracked.
func gitTracking(gitBin, abs string) fileTrackingState {
	err := exec.Command(gitBin, "-C", filepath.Dir(abs),
		"--literal-pathspecs", "ls-files", "--error-unmatch", "--", filepath.Base(abs)).Run()
	if err == nil {
		return fileTracked
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return fileUntracked
	}
	return fileTrackingUnknown
}

// CleanupRunArtifacts removes known harness scratch files at or under
// projectDir, restricted to files confirmed untracked via
// "git ls-files --error-unmatch". Tracked matches of the scratch patterns
// are never deleted and are surfaced as leftovers. Any other untracked
// file found by "git status --porcelain --ignored" is also reported as a
// leftover (left in place). When git is unavailable or projectDir is not
// inside a work tree, nothing is deleted — the scratch candidates are
// returned as leftovers instead.
//
// Returns (removed, leftover), both as projectDir-relative paths in
// stable order.
func CleanupRunArtifacts(projectDir string) (removed []string, leftover []string) {
	gitBin, gitLookupErr := exec.LookPath("git")
	gitOK := false
	if gitLookupErr == nil {
		out, perr := exec.Command(gitBin, "-C", projectDir,
			"rev-parse", "--is-inside-work-tree").Output()
		gitOK = perr == nil && strings.TrimSpace(string(out)) == "true"
	}
	if !gitOK {
		log.Printf("[tooling] run-artifact cleanup: %s: git unavailable or not in a work tree; reporting candidates without deleting", projectDir)
	}

	// Candidate scratch files at or under projectDir (never outside).
	var candidates []string
	_ = filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			if path != projectDir && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if isRunArtifactName(d.Name()) {
			rel, rerr := filepath.Rel(projectDir, path)
			if rerr != nil {
				return nil
			}
			candidates = append(candidates, rel)
		}
		return nil
	})

	seen := map[string]bool{}
	add := func(target *[]string) func(p string) bool {
		return func(p string) bool {
			if p == "" || seen[p] {
				return false
			}
			seen[p] = true
			*target = append(*target, p)
			return true
		}
	}
	addLeftover := add(&leftover)
	addRemoved := add(&removed)

	if !gitOK {
		for _, rel := range candidates {
			addLeftover(rel)
		}
		sort.Strings(removed)
		sort.Strings(leftover)
		return removed, leftover
	}

	// Known-pattern files: delete only when confirmed untracked.
	for _, rel := range candidates {
		abs := filepath.Join(projectDir, rel)
		state := fileTracked
		if !projectTracks(gitBin, projectDir, rel) {
			state = gitTracking(gitBin, abs)
		}
		switch state {
		case fileTracked:
			// Tracked — must never be deleted.
			log.Printf("[tooling] run-artifact cleanup: %s: tracked file matches scratch pattern, kept (%s)", projectDir, rel)
			addLeftover(rel)
			continue
		case fileTrackingUnknown:
			log.Printf("[tooling] run-artifact cleanup: %s: git could not say whether this file is tracked, kept (%s)", projectDir, rel)
			addLeftover(rel)
			continue
		}
		if err := os.Remove(abs); err != nil {
			log.Printf("[tooling] run-artifact cleanup: remove %s: %v", abs, err)
			addLeftover(rel)
		} else {
			addRemoved(rel)
		}
	}

	// Unexpected untracked files: report, leave in place.
	if out, oerr := exec.Command(gitBin, "-C", projectDir,
		"-c", "core.quotepath=false", "status", "--porcelain", "--ignored").Output(); oerr == nil {
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			line := sc.Text()
			if len(line) < 4 || line[0] != '?' || line[1] != '?' {
				continue
			}
			rel := strings.TrimSpace(line[3:])
			if rel == "" || strings.HasSuffix(rel, "/") {
				continue
			}
			if isRunArtifactName(filepath.Base(rel)) {
				continue // already handled above
			}
			log.Printf("[tooling] run-artifact cleanup: %s: unexpected untracked file left in place (%s)", projectDir, rel)
			addLeftover(rel)
		}
	}

	sort.Strings(removed)
	sort.Strings(leftover)
	return removed, leftover
}
