package mergeshadow

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guards the documents against the gate vocabulary drifting from the code:
// no document may present `load_sensitive` as a gate status, because no gate
// records it. A failing test gate that passes on rerun is `intermittent`.

const (
	docsRoot         = "../../docs"
	howtoPath        = docsRoot + "/howto/merge-shadow.md"
	parityDraftPath  = docsRoot + "/plans/parity-issues/2026-11-07-merge-shadow-check-vocabulary.md"
	noSuchStatusText = "no `load_sensitive` gate status"
)

// liveGateStatuses are the statuses a plan's gate can carry.
var liveGateStatuses = []GateStatus{GatePass, GateFail, GateTimeout, GateSkipped, GateIntermittent}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- fixed paths under docs
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// oneLine joins wrapped lines so a sentence can be matched across line breaks.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestNoDocPresentsLoadSensitiveAsAGateStatus lets a document name
// load_sensitive only in a paragraph that also says no such status exists.
// Historical plans record what was true when written and are left alone.
func TestNoDocPresentsLoadSensitiveAsAGateStatus(t *testing.T) {
	err := filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "historical-plans" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		for _, para := range strings.Split(readDoc(t, path), "\n\n") {
			if strings.Contains(para, "load_sensitive") && !strings.Contains(strings.ToLower(oneLine(para)), "no `load_sensitive`") {
				t.Errorf("%s presents load_sensitive as a status: %.160q", path, oneLine(para))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestParityDraftListsTheLiveGateStatuses checks the status set the client
// parity draft contracts for, and that the replay files it names exist.
func TestParityDraftListsTheLiveGateStatuses(t *testing.T) {
	draft := oneLine(readDoc(t, parityDraftPath))
	quoted := make([]string, 0, len(liveGateStatuses))
	for _, s := range liveGateStatuses {
		quoted = append(quoted, "`"+string(s)+"`")
	}
	want := "gate statuses " + strings.Join(quoted, ", ")
	if !strings.Contains(draft, want) {
		t.Errorf("draft does not list %q", want)
	}
	for _, m := range regexp.MustCompile("`(replay_[a-z_]+_test\\.go)`").FindAllStringSubmatch(draft, -1) {
		if _, err := os.Stat(m[1]); err != nil {
			t.Errorf("draft names the replay test %s, which is not in the package", m[1])
		}
	}
	for _, m := range regexp.MustCompile("`(testdata/mergeshadow/[^`]+)`").FindAllStringSubmatch(draft, -1) {
		if _, err := os.Stat(filepath.Join("../..", m[1])); err != nil {
			t.Errorf("draft names the fixture %s, which is not in the tree", m[1])
		}
	}
}
