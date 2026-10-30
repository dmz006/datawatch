package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRegistryFile(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "plans", "README.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestOverlapSourcesRegistryRows(t *testing.T) {
	root := writeRegistryFile(t, `# Roadmap

| row | status |
|-----|--------|
| 12 | BL428 shared-resource coordination |
| 13 | BL428 lease/back-off, |
| 14
| x | not a number |
| 1b | not either |
| 12.5 | no |
| | blank cell |
| **5** | bold is not plain |

not a table row at all
`)
	cands, err := overlapRowCandidates(root)
	if err != nil {
		t.Fatalf("overlapRowCandidates: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("want 3 candidates (rows 12, 13, 14), got %d: %#v", len(cands), cands)
	}

	got := map[int]string{}
	for _, c := range cands {
		if c.Source != "registry_row" {
			t.Fatalf("wrong source kind: %#v", c)
		}
		got[c.Row] = c.Text
	}
	if got[12] != "BL428 shared-resource coordination" {
		t.Fatalf("row 12 text: %q", got[12])
	}
	// Short rows are tolerated: the second cell is empty.
	if got[13] != "BL428 lease/back-off," {
		t.Fatalf("row 13 text: %q", got[13])
	}
	if got[14] != "" {
		t.Fatalf("row 14 should have empty text, got %q", got[14])
	}
	for _, skipped := range []int{0, 5} {
		if _, ok := got[skipped]; ok {
			t.Fatalf("row %d must not be a candidate", skipped)
		}
	}
}

func TestOverlapSourcesRegistryRowsMissingFile(t *testing.T) {
	root := t.TempDir()
	if _, err := overlapRowCandidates(root); err == nil {
		t.Fatal("missing README must return an error")
	}
}

type fakeGitRunner struct {
	output map[string]string
	codes  map[string]int
	errs   map[string]error
	calls  [][]string
}

func (f *fakeGitRunner) run(args ...string) (string, int, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, args)
	if err, ok := f.errs[key]; ok {
		return "", -1, err
	}
	return f.output[key], f.codes[key], nil
}

func TestOverlapSourcesUnmergedBranches(t *testing.T) {
	f := &fakeGitRunner{
		output: map[string]string{
			"for-each-ref refs/heads/automaton/ --format=%(refname:short)": "automaton/gone\nautomaton/pending\n",
			"log --format=%s main..automaton/pending":                      "add overlap sources\nfix tests\n",
		},
		codes: map[string]int{
			"merge-base --is-ancestor automaton/gone main":    0, // on main: skip
			"merge-base --is-ancestor automaton/pending main": 1, // unmerged
		},
	}
	cands, err := overlapBranchCandidates(f)
	if err != nil {
		t.Fatalf("overlapBranchCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("want 1 candidate, got %d: %#v", len(cands), cands)
	}
	c := cands[0]
	if c.Source != "unmerged_branch" || c.Ref != "automaton/pending" {
		t.Fatalf("wrong candidate: %#v", c)
	}
	if !strings.Contains(c.Text, "automaton/pending") {
		t.Fatalf("text must carry the branch name: %q", c.Text)
	}
	if !strings.Contains(c.Text, "add overlap sources") || !strings.Contains(c.Text, "fix tests") {
		t.Fatalf("text must carry both subjects: %q", c.Text)
	}
	for _, call := range f.calls {
		joined := strings.Join(call, " ")
		if joined == "log --format=%s main..automaton/gone" {
			t.Fatal("subjects of an ancestor branch must not be fetched")
		}
	}
}

func TestOverlapSourcesGitError(t *testing.T) {
	f := &fakeGitRunner{
		errs: map[string]error{
			"for-each-ref refs/heads/automaton/ --format=%(refname:short)": errFakeGit,
		},
	}
	if _, err := overlapBranchCandidates(f); err == nil {
		t.Fatal("a git error must return an error")
	}

	f2 := &fakeGitRunner{
		output: map[string]string{
			"for-each-ref refs/heads/automaton/ --format=%(refname:short)": "automaton/b\n",
			"log --format=%s main..automaton/b":                            "subj\n",
		},
		codes: map[string]int{
			"merge-base --is-ancestor automaton/b main": 1,
		},
		errs: map[string]error{
			"log --format=%s main..automaton/b": errFakeGit,
		},
	}
	if _, err := overlapBranchCandidates(f2); err == nil {
		t.Fatal("a git error on log must return an error")
	}

	f3 := &fakeGitRunner{
		codes: map[string]int{"merge-base --is-ancestor automaton/b main": 128},
		output: map[string]string{
			"for-each-ref refs/heads/automaton/ --format=%(refname:short)": "automaton/b\n",
		},
	}
	if _, err := overlapBranchCandidates(f3); err == nil {
		t.Fatal("an unexpected merge-base exit code must return an error")
	}
}

func TestOverlapSourcesNoBranches(t *testing.T) {
	f := &fakeGitRunner{
		output: map[string]string{
			"for-each-ref refs/heads/automaton/ --format=%(refname:short)": "",
		},
	}
	cands, err := overlapBranchCandidates(f)
	if err != nil {
		t.Fatalf("no branches must not error: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("want no candidates, got %#v", cands)
	}
}

var errFakeGit = errSentinel("git failed")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
