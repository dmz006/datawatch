// The merge pipeline record store must live under the server's configured
// data directory (Server.statePath), never under a home directory derived
// from the process environment.

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// statePathTree lists every entry below root, relative to it, sorted.
func statePathTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(strings.TrimPrefix(p, root)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestStatePathMergeRecordsUnderDataDir pins the merge record path to the
// configured data directory: a Server with a temp data directory and a decoy
// HOME must write the record file under the data directory, with the decoy
// HOME tree left exactly as it was.
func TestStatePathMergeRecordsUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	decoyHome := t.TempDir()
	t.Setenv("HOME", decoyHome)
	if err := os.WriteFile(filepath.Join(decoyHome, "preexisting"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := statePathTree(t, decoyHome)

	repo := t.TempDir()
	spy := &mergeRunSpy{repo: repo}
	cfg := config.DefaultConfig()
	cfg.DataDir = dataDir
	cfg.Projects = map[string]config.ProjectConfigEntry{"demo": {Dir: t.TempDir()}}
	s := &Server{
		cfg:           cfg,
		cfgPath:       filepath.Join(t.TempDir(), "config.yaml"),
		dataDir:       dataDir,
		autonomousMgr: spy,
	}

	// Write a pipeline record through the merge record route, whose store is
	// built from the same Server-scoped source as the run route.
	rec := mpDo(s.handleMergePipelineRecord, http.MethodPut, "/api/autonomous/merge-pipeline/record?project=demo", mpValidRecordJSON)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT record = %d: %s", rec.Code, rec.Body)
	}
	want := filepath.Join(dataDir, "merge-pipeline", "records.json")
	if st, err := os.Stat(want); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("record file under the data directory: stat %q = %v, %v", want, st, err)
	}

	// The run route's record store (newMergeRunner over s.statePath) must
	// read the very same file.
	runner := newMergeRunner(spy, s.mergePipelineConfig(), repo, "demo", s.statePath(), mergepipeline.ExecShipRunner{})
	if runner.Resolver.Records == nil {
		t.Fatal("run route built no record store")
	}
	got, err := runner.Resolver.Records.Get("demo")
	if err != nil {
		t.Fatalf("run store Get(demo) = %v", err)
	}
	if got.Definition.Steps[0].Name != "unit tests" {
		t.Fatalf("record from the data directory: %+v", got.Definition)
	}

	// The decoy HOME must be untouched: no .datawatch, no record file, no
	// entry added or removed.
	if after := statePathTree(t, decoyHome); !reflect.DeepEqual(before, after) {
		t.Fatalf("decoy HOME tree changed: before=%v after=%v", before, after)
	}
}
