// standard_task_lists_golden_test.go — pins the standard task list that every
// Automaton kind starts from to a byte-for-byte golden file.
//
// The standard task list an Automaton of any kind is offered is the built-in
// task-template pack: BuiltInTaskTemplates (task_templates_defaults.go). This
// test serializes that pack, for every Automaton kind in the autonomous kind
// list (progress_kind.go), to canonical JSON and compares it byte for byte with
// testdata/standard_task_lists.golden.json.
//
// The golden file is written once from the unchanged code with the update flag
// this test defines (run: go test ./internal/autonomous -run
// TestStandardTaskListsGolden -update-standard-task-lists-golden). After that,
// a bare run must pass against the committed file; a byte difference means the
// pack or the kind list changed and the golden needs a deliberate review.
package autonomous

import (
	"bytes"
	"flag"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// standardTaskListGoldenPath is the committed golden file this test pins the
// built-in task list against.
const standardTaskListGoldenPath = "testdata/standard_task_lists.golden.json"

// standardTaskListGoldenUpdate is the update flag: when set, the test
// regenerates the golden file from the current code and skips the comparison.
var standardTaskListGoldenUpdate = flag.Bool(
	"update-standard-task-lists-golden",
	false,
	"regenerate "+standardTaskListGoldenPath+" from BuiltInTaskTemplates and skip the byte-for-byte comparison")

// standardTaskListKinds is the autonomous kind list: every kind an Automaton
// can carry (see progress_kind.go).
func standardTaskListKinds() []string {
	return []string{PRDKindFeature, PRDKindBug, PRDKindOther, PRDKindMaintenance}
}

// standardTaskListsPerKind is the map the golden file pins: for every Automaton
// kind, the standard task list is the built-in task-template pack.
func standardTaskListsPerKind() map[string][]config.TaskTemplateDef {
	pack := BuiltInTaskTemplates()
	m := make(map[string][]config.TaskTemplateDef, len(standardTaskListKinds()))
	for _, kind := range standardTaskListKinds() {
		m[kind] = pack
	}
	return m
}

// canonicalTaskListJSON is the deterministic serialization the golden file
// stores: the per-kind map indented two spaces, map keys sorted (Go's JSON
// encoder sorts map keys), struct fields in declaration order, and a single
// trailing newline. Both the update path and the comparison path use this, so a
// regenerated file and a bare run agree byte for byte.
func canonicalTaskListJSON(v map[string][]config.TaskTemplateDef) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

func TestStandardTaskListsGolden(t *testing.T) {
	got := canonicalTaskListJSON(standardTaskListsPerKind())

	if *standardTaskListGoldenUpdate {
		if err := os.MkdirAll(filepath.Dir(standardTaskListGoldenPath), 0o755); err != nil {
			t.Fatalf("make golden dir: %v", err)
		}
		if err := os.WriteFile(standardTaskListGoldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Skipf("wrote %s from the unchanged code; re-run without the update flag to verify", standardTaskListGoldenPath)
	}

	want, err := os.ReadFile(standardTaskListGoldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("standard task lists differ from the golden file.\n"+
			"got (%d bytes):\n%s\n"+
			"golden (%d bytes):\n%s", len(got), got, len(want), want)
	}
}
