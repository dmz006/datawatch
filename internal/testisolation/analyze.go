// Package testisolation is the guard behind scripts/check-test-isolation.sh: it
// fails a test run in which a test reads or writes outside its own temporary
// area and the repository.
//
// Each test binary runs in its own sandbox. There the machine temp folder and
// the user's home are empty private folders holding planted decoys, which stand
// for other people's files. TMPDIR names a stand-in shared temp folder. Two
// checks run:
//
//   - the timestamps of the decoys and of the three roots (machine temp,
//     shared temp, home), compared after the binary ends: listing a root, or
//     reading, changing or removing a decoy, by the test binary or by any
//     process it started. This needs a file system that records access times
//     (the guard checks that first and fails otherwise);
//   - the Go test log of the binary (what it opened, stat'ed and changed into):
//     fixed paths in the machine temp folder or the home that are outside the
//     allowed folders.
//
// A passing run is not proof that no test reaches outside: what the guard does
// not see is listed in docs/testing.md.
package testisolation

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DecoyPrefix is part of the name of every decoy that no code has a reason to
// know about. Some decoys instead carry the names the daemon's own clean-up
// passes look for; Layout.Decoys lists every one.
const DecoyPrefix = "zz-guard-"

// Layout names the folders of one guarded run.
type Layout struct {
	TempRoot string   // the machine temp folder of the sandbox
	Shared   string   // stand-in shared temp folder, given to the tests as TMPDIR
	Home     string   // stand-in for the user's real home
	Allowed  []string // folders the tests may use: repository, Go toolchain and caches, command folders
	Decoys   []string // planted paths; any access is a violation
}

// Violation is one distinct access outside the test's own area.
type Violation struct {
	What  string // "open", "stat", "chdir" (test log), or a finding on the file system
	Path  string
	Count int // test log entries; 0 for a file system finding
	// After is the name of the last test whose temp folder appeared in the
	// test log before the first occurrence: a hint to which test to look at.
	After string
}

func (v Violation) String() string {
	s := v.What + " " + v.Path
	if v.Count > 0 {
		s += fmt.Sprintf(" (x%d)", v.Count)
	}
	if v.After != "" {
		s += ", first seen after the temp folder of " + v.After
	}
	return s
}

// lookupNames are the files tools look for in every parent folder of where they
// run. Asking whether one exists (stat only) in a parent of an allowed folder
// is how a repository or project root is found; opening one is not allowed.
var lookupNames = map[string]bool{
	".git": true, ".datawatch": true, "go.mod": true, "go.work": true,
	"CLAUDE.md": true, "CLAUDE.local.md": true, "AGENTS.md": true, "AGENT.md": true,
}

// testDirRe matches the folder t.TempDir makes: the test name and a number.
var testDirRe = regexp.MustCompile(`/((?:Test|Fuzz)[^/]*?)\d{5,}(?:/|$)`)

func within(p, dir string) bool {
	return dir != "" && (p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/"))
}

func (l Layout) decoy(p string) bool {
	for _, d := range l.Decoys {
		if within(p, d) {
			return true
		}
	}
	return false
}

// ancestor reports whether p is a parent folder of the shared temp folder or of
// an allowed folder.
func (l Layout) ancestor(p string) bool {
	if l.Shared != p && within(l.Shared, p) {
		return true
	}
	for _, a := range l.Allowed {
		if a != p && within(a, p) {
			return true
		}
	}
	return false
}

// outside reports whether one test log entry reaches outside the test's own
// area. Paths that are neither in the machine temp folder nor in the home are
// system or made-up paths: the sandbox mounts them read-only.
func (l Layout) outside(op, p string) bool {
	if l.decoy(p) {
		return true
	}
	if within(p, l.Shared) {
		return false
	}
	if !within(p, l.TempRoot) && !within(p, l.Home) {
		return false
	}
	for _, a := range l.Allowed {
		if within(p, a) {
			return false
		}
	}
	// Asking about a parent folder, or opening one to remove something inside
	// it, reads no one else's data. Listing a root is caught by its access
	// time.
	if l.ancestor(p) {
		return false
	}
	if op == "stat" && lookupNames[filepath.Base(p)] && l.ancestor(filepath.Dir(p)) {
		return false
	}
	return true
}

// AnalyzeLog reads a Go test log and returns the entries outside the test's own
// area, one per operation and path, in order of first appearance. cwd is the
// folder the test binary started in; relative entries are resolved against it
// and against later chdir entries.
func (l Layout) AnalyzeLog(r io.Reader, cwd string) ([]Violation, error) {
	var out []Violation
	index := map[string]int{}
	lastTest := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		op, p, ok := strings.Cut(sc.Text(), " ")
		if !ok || (op != "open" && op != "stat" && op != "chdir") || p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		p = filepath.Clean(p)
		if op == "chdir" {
			cwd = p
		}
		if !l.outside(op, p) {
			if m := testDirRe.FindStringSubmatch(p); m != nil {
				lastTest = m[1]
			}
			continue
		}
		key := op + " " + p
		if i, seen := index[key]; seen {
			out[i].Count++
			continue
		}
		index[key] = len(out)
		out = append(out, Violation{What: op, Path: p, Count: 1, After: lastTest})
	}
	return out, sc.Err()
}

// Report renders the violations of one test binary, at most max lines of
// detail.
func Report(name string, vs []Violation, max int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "test-isolation: FAIL %s: %d accesses outside the test's own folders and the repository\n", name, len(vs))
	sorted := append([]Violation(nil), vs...)
	// File system findings and decoys first: they are the plainest proof.
	sort.SliceStable(sorted, func(i, j int) bool { return rank(sorted[i]) < rank(sorted[j]) })
	for i, v := range sorted {
		if i == max {
			fmt.Fprintf(&b, "  ... and %d more\n", len(sorted)-max)
			break
		}
		fmt.Fprintf(&b, "  %s\n", v)
	}
	return b.String()
}

func rank(v Violation) int {
	switch {
	case v.Count == 0:
		return 0
	case strings.Contains(v.Path, DecoyPrefix):
		return 1
	}
	return 2
}
