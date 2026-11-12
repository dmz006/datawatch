package mergepipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRecordWriter struct {
	rows []ShippedRow
	err  error
	note string
}

func (f *fakeRecordWriter) UpsertShipped(_ context.Context, row ShippedRow) (string, error) {
	f.rows = append(f.rows, row)
	return f.note, f.err
}

const goldenShippedRegistry = `# Registry

| # | Item | Status | Depends on |
|---|------|--------|------------|
| 100 | Some other row | open | - |
| 101 | post-ship actions v9.9.9 | Shipped 9.9.9 | 100 |
`

func planRecordRun(t *testing.T, cfg PostShipSettings, w RecordWriter) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "REGISTRY.md")
	if err := os.WriteFile(path, []byte(testRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.RegistryFile = "REGISTRY.md"
	if cfg.RegistryRowPattern == "" {
		cfg.RegistryRowPattern = "post-ship actions v{version}"
	}
	r := &Runner{Dir: dir, Project: "acme"}
	if w != nil {
		r.PlanRecords = w
	}
	s := &runState{r: r, req: RunRequest{Title: "Post-ship actions", AutomatonID: "4bec0001"}}
	note, err := s.builtinPostShipHook(context.Background(), builtinRegistryRow, "9.9.9", nil, cfg)
	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return note, string(got), err
}

func TestPlanRecordWriteThroughOffKeepsReadmePath(t *testing.T) {
	w := &fakeRecordWriter{}
	note, got, err := planRecordRun(t, PostShipSettings{WriteThrough: false}, w)
	if err != nil || note != "updated the registry row" {
		t.Fatalf("note %q err %v", note, err)
	}
	if got != goldenShippedRegistry {
		t.Fatalf("registry file differs from the golden result:\n%s", got)
	}
	if len(w.rows) != 0 {
		t.Fatalf("the record writer must not be called with write_through off: %+v", w.rows)
	}
}

func TestPlanRecordWriteThroughOnLeavesReadmeByteIdentical(t *testing.T) {
	w := &fakeRecordWriter{note: "created the plan record x"}
	note, got, err := planRecordRun(t, PostShipSettings{WriteThrough: true}, w)
	if err != nil || note != "created the plan record x" {
		t.Fatalf("note %q err %v", note, err)
	}
	if got != testRegistry {
		t.Fatalf("the registry file must not change:\n%s", got)
	}
	if len(w.rows) != 1 {
		t.Fatalf("writer calls = %d, want 1", len(w.rows))
	}
	row := w.rows[0]
	if row.Project != "acme" || row.ShippedBy != "4bec0001" || row.ShippedVersion != "9.9.9" {
		t.Fatalf("row %+v", row)
	}
	if row.CreateTitle != "Post-ship actions" {
		t.Fatalf("create title %q, want the Automaton title", row.CreateTitle)
	}
	if len(row.MatchTitles) != 2 || row.MatchTitles[0] != "post-ship actions v9.9.9" {
		t.Fatalf("match titles %v", row.MatchTitles)
	}
}

func TestPlanRecordWriteThroughNoWriterFallsBackToReadme(t *testing.T) {
	note, got, err := planRecordRun(t, PostShipSettings{WriteThrough: true}, nil)
	if err != nil || note != "updated the registry row" {
		t.Fatalf("note %q err %v", note, err)
	}
	if got != goldenShippedRegistry {
		t.Fatalf("with no writer the old path must run:\n%s", got)
	}
}

func TestPlanRecordWriteThroughFailedWriteLeavesReadmeAlone(t *testing.T) {
	w := &fakeRecordWriter{err: errors.New("disk full")}
	_, got, err := planRecordRun(t, PostShipSettings{WriteThrough: true}, w)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v, want the write failure", err)
	}
	if got != testRegistry {
		t.Fatalf("a failed write must not fall back to editing the README:\n%s", got)
	}
}

func TestPlanRecordWriteThroughRedactsErrorText(t *testing.T) {
	w := &fakeRecordWriter{err: errors.New("store said secret=hunter2value and failed")}
	_, _, err := planRecordRun(t, PostShipSettings{WriteThrough: true}, w)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "hunter2value") {
		t.Fatalf("error leaks a credential: %v", err)
	}
}

func TestPlanRecordWriteThroughPatternLegacyNumber(t *testing.T) {
	cases := map[string]int{"| 396 | docs": 396, "row 12": 12, "post-ship actions": 0, "| 5": 0}
	for p, want := range cases {
		if got := patternLegacyNumber(p); got != want {
			t.Errorf("patternLegacyNumber(%q) = %d, want %d", p, got, want)
		}
	}
	if got := patternTitle("| 396 | Docs discoverability |"); got != "Docs discoverability" {
		t.Errorf("patternTitle = %q", got)
	}
	w := &fakeRecordWriter{}
	_, _, err := planRecordRun(t, PostShipSettings{WriteThrough: true, RegistryRowPattern: "| 101 | post-ship actions v{version}"}, w)
	if err != nil || len(w.rows) != 1 || w.rows[0].LegacyNumber != 101 {
		t.Fatalf("rows %+v err %v", w.rows, err)
	}
}
