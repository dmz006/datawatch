package changerecord

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeDiff struct {
	files []FileChange
	diffs map[string]FileDiffData
	err   error
}

func (f fakeDiff) ChangedFiles() ([]FileChange, error) { return f.files, f.err }
func (f fakeDiff) FileDiff(p string) (FileDiffData, error) {
	d, ok := f.diffs[p]
	if !ok {
		return FileDiffData{}, errors.New("no diff for " + p)
	}
	return d, nil
}

func addDiff(lines ...string) string {
	return "--- a/x\n+++ b/x\n@@ -1 +1 @@\n context\n+" + strings.Join(lines, "\n+") + "\n"
}

func TestBuildFromDiffAdditions(t *testing.T) {
	fd := fakeDiff{
		files: []FileChange{
			{"internal/server/web/locales/en.json", StatusModified},
			{"internal/config/config.go", StatusModified},
			{"internal/server/api.go", StatusModified},
			{"internal/widget/widget_test.go", StatusAdded},
			{"internal/widget/widget.go", StatusAdded},
			{"docs/config-reference.yaml", StatusModified},
			{"docs/api/openapi.yaml", StatusModified},
		},
		diffs: map[string]FileDiffData{
			"internal/server/web/locales/en.json": {
				Unified: addDiff(`"a.new": "One"`),
				Before:  `{"a":{"old":"Old"}}`,
				After:   `{"a":{"old":"Old","new":"One"},"b":"Two"}`,
			},
			"internal/config/config.go": {Unified: addDiff("	Widget bool `yaml:\"widget_enabled,omitempty\" json:\"widget_enabled\"`")},
			"internal/server/api.go":    {Unified: addDiff(`	mux.HandleFunc("/api/widgets", s.handleWidgets)`)},
			"internal/widget/widget_test.go": {Unified: addDiff(
				"func TestWidgetWorks(t *testing.T) {", "func helper() {}")},
			"internal/widget/widget.go":  {Unified: addDiff("package widget")},
			"docs/config-reference.yaml": {Unified: addDiff("  widget_enabled: true")},
			"docs/api/openapi.yaml":      {Unified: addDiff("  /api/widgets:")},
		},
	}
	cmds := []CommandRun{{Command: "go test ./...", ExitCode: 0, Pass: 5, Fail: 0}}
	rec, err := BuildFromDiff(fd, cmds)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.SettingsAdded, []string{"widget_enabled"}) {
		t.Errorf("settings %v", rec.SettingsAdded)
	}
	if !reflect.DeepEqual(rec.RoutesAdded, []string{"/api/widgets"}) {
		t.Errorf("routes %v", rec.RoutesAdded)
	}
	wantLoc := []LocaleKey{{"a.new", "One"}, {"b", "Two"}}
	if !reflect.DeepEqual(rec.LocaleKeysAdded, wantLoc) {
		t.Errorf("locale %v", rec.LocaleKeysAdded)
	}
	if !reflect.DeepEqual(rec.TestsAdded, []string{"TestWidgetWorks"}) {
		t.Errorf("tests %v", rec.TestsAdded)
	}
	wantPk := []string{"internal/config", "internal/server", "internal/widget"}
	if !reflect.DeepEqual(rec.Packages, wantPk) {
		t.Errorf("packages %v", rec.Packages)
	}
	if len(rec.Files) != 7 || rec.Files[0].Path != "docs/api/openapi.yaml" {
		t.Errorf("files %v", rec.Files)
	}
	if !reflect.DeepEqual(rec.Commands, cmds) || rec.Timestamp.Location() != time.UTC || rec.Timestamp.IsZero() {
		t.Errorf("commands/timestamp %v %v", rec.Commands, rec.Timestamp)
	}
}

func TestBuildFromDiffOnlyDeletions(t *testing.T) {
	fd := fakeDiff{
		files: []FileChange{{"internal/config/config.go", StatusModified}, {"internal/old/old_test.go", StatusDeleted}},
		diffs: map[string]FileDiffData{
			"internal/config/config.go": {Unified: "--- a/x\n+++ b/x\n@@ -1 +0,0 @@\n-	Old bool `yaml:\"old_key\"`\n"},
			"internal/old/old_test.go":  {Unified: "--- a/x\n+++ /dev/null\n@@ -1 +0,0 @@\n-func TestOld(t *testing.T) {\n"},
		},
	}
	rec, err := BuildFromDiff(fd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.SettingsAdded)+len(rec.RoutesAdded)+len(rec.LocaleKeysAdded)+len(rec.TestsAdded) != 0 {
		t.Errorf("deletions must add nothing: %+v", rec)
	}
	if len(rec.Files) != 2 || rec.Files[1].Status != StatusDeleted {
		t.Errorf("files %v", rec.Files)
	}
}

func TestBuildFromDiffErrors(t *testing.T) {
	if _, err := BuildFromDiff(nil, nil); err == nil {
		t.Error("nil source must fail")
	}
	if _, err := BuildFromDiff(fakeDiff{err: errors.New("boom")}, nil); err == nil {
		t.Error("list error must surface")
	}
	if _, err := BuildFromDiff(fakeDiff{files: []FileChange{{"a.go", StatusAdded}}}, nil); err == nil {
		t.Error("diff error must surface")
	}
	bad := fakeDiff{
		files: []FileChange{{"internal/server/web/locales/en.json", StatusModified}},
		diffs: map[string]FileDiffData{"internal/server/web/locales/en.json": {After: "{not json"}},
	}
	if _, err := BuildFromDiff(bad, nil); err == nil {
		t.Error("bad locale json must fail")
	}
}

func TestMerge(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &ChangeRecord{AutomatonID: "auto", Timestamp: t1,
		Files:    []FileChange{{"a.go", StatusAdded}, {"b.go", StatusModified}},
		Packages: []string{"."}, SettingsAdded: []string{"x"}, TestsAdded: []string{"TestA"},
		LocaleKeysAdded: []LocaleKey{{"k", "v"}}, Commands: []CommandRun{{Command: "c1"}}}
	b := &ChangeRecord{Timestamp: t1.Add(time.Hour),
		Files:    []FileChange{{"b.go", StatusDeleted}, {"c.go", StatusAdded}},
		Packages: []string{".", "pkg"}, SettingsAdded: []string{"x", "y"}, RoutesAdded: []string{"/r"},
		LocaleKeysAdded: []LocaleKey{{"k", "v"}}, Commands: []CommandRun{{Command: "c2"}}}
	m := Merge(a, nil, b)
	if m.AutomatonID != "auto" || m.TaskID != "" || !m.Timestamp.Equal(t1.Add(time.Hour)) {
		t.Errorf("ids/time %+v", m)
	}
	if !reflect.DeepEqual(m.Files, []FileChange{{"a.go", StatusAdded}, {"b.go", StatusDeleted}, {"c.go", StatusAdded}}) {
		t.Errorf("files %v", m.Files)
	}
	if !reflect.DeepEqual(m.Packages, []string{".", "pkg"}) || !reflect.DeepEqual(m.SettingsAdded, []string{"x", "y"}) ||
		!reflect.DeepEqual(m.RoutesAdded, []string{"/r"}) || !reflect.DeepEqual(m.TestsAdded, []string{"TestA"}) ||
		len(m.LocaleKeysAdded) != 1 {
		t.Errorf("lists %+v", m)
	}
	if len(m.Commands) != 2 || m.Commands[0].Command != "c1" {
		t.Errorf("commands %v", m.Commands)
	}
	empty := Merge()
	if empty.Files == nil || empty.Commands == nil {
		t.Error("empty merge must have non-nil slices")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec := &ChangeRecord{TaskID: "t", AutomatonID: "auto", Timestamp: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
		Files: []FileChange{{"a.go", StatusAdded}}, Commands: []CommandRun{{"go test", 0, 3, 0}}}
	if err := Save(dir, "auto", rec); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, "auto", rec); err != nil { // overwrite
		t.Fatal(err)
	}
	got, err := Load(dir, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("round trip\n got %+v\nwant %+v", got, rec)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "change-records"))
	if len(ents) != 1 || ents[0].Name() != "auto.json" {
		t.Errorf("leftover files: %v", ents)
	}
	for _, bad := range []string{"", "../x", "a/b", ".."} {
		if Save(dir, bad, rec) == nil {
			t.Errorf("id %q must be rejected", bad)
		}
		if _, err := Load(dir, bad); err == nil {
			t.Errorf("load id %q must be rejected", bad)
		}
	}
	if Save(dir, "n", nil) == nil {
		t.Error("nil record must fail")
	}
	if _, err := Load(dir, "missing"); err == nil {
		t.Error("missing must fail")
	}
	_ = os.WriteFile(filepath.Join(dir, "change-records", "junk.json"), []byte("{"), 0o600)
	if _, err := Load(dir, "junk"); err == nil {
		t.Error("corrupt must fail")
	}
}

func TestGitDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(n, s string) { _ = os.WriteFile(filepath.Join(dir, n), []byte(s), 0o600) }
	run("init", "-q")
	write("keep.txt", "one\n")
	write("gone.txt", "bye\n")
	run("add", ".")
	run("commit", "-qm", "base")
	write("keep.txt", "one\ntwo\n")
	_ = os.Remove(filepath.Join(dir, "gone.txt"))
	write("new_test.go", "package x\nfunc TestNew(t *testing.T) {}\n")
	run("add", "-A")
	g := GitDiff{Dir: dir, Base: "HEAD"}
	files, err := g.ChangedFiles()
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	for _, f := range files {
		st[f.Path] = f.Status
	}
	if st["keep.txt"] != StatusModified || st["gone.txt"] != StatusDeleted || st["new_test.go"] != StatusAdded {
		t.Errorf("statuses %v", st)
	}
	d, err := g.FileDiff("keep.txt")
	if err != nil || d.Before != "one\n" || d.After != "one\ntwo\n" || !strings.Contains(d.Unified, "+two") {
		t.Errorf("diff %+v %v", d, err)
	}
	rec, err := BuildFromDiff(g, nil)
	if err != nil || !reflect.DeepEqual(rec.TestsAdded, []string{"TestNew"}) {
		t.Errorf("build %+v %v", rec, err)
	}
	if _, err := (GitDiff{Dir: dir, Base: "nope"}).ChangedFiles(); err == nil {
		t.Error("bad base must fail")
	}
}

type fakeLedger struct {
	c   *CostSummary
	err error
	id  string
}

func (f *fakeLedger) AutomatonCost(id string) (*CostSummary, error) {
	f.id = id
	return f.c, f.err
}

func TestAttachCostAndMerge(t *testing.T) {
	usd := 1.5
	l := &fakeLedger{c: &CostSummary{Sessions: 2, TokensIn: 10, ToolCalls: 3, APIEquivalentUSD: &usd}}
	rec := &ChangeRecord{AutomatonID: "auto"}
	if err := AttachCost(rec, l); err != nil || l.id != "auto" || rec.Cost == nil || rec.Cost.Sessions != 2 {
		t.Fatalf("attach: %v %+v", err, rec.Cost)
	}
	if m := Merge(&ChangeRecord{}, rec); m.Cost == nil || m.Cost.TokensIn != 10 || m.Cost == rec.Cost {
		t.Errorf("merge cost %+v", m.Cost)
	}
	if Merge(&ChangeRecord{}).Cost != nil {
		t.Error("merge without cost must stay nil")
	}
	if err := AttachCost(&ChangeRecord{AutomatonID: "x"}, &fakeLedger{err: errors.New("boom")}); err == nil {
		t.Error("ledger error must surface")
	}
	none := &ChangeRecord{}
	if err := AttachCost(none, l); err != nil || none.Cost != nil {
		t.Error("no automaton id must be a no-op")
	}
}

func TestMergeSumsCostAcrossAutomatons(t *testing.T) {
	u1, u2 := 1.5, 2.0
	a := &ChangeRecord{Cost: &CostSummary{Sessions: 1, SessionIDs: []string{"s1"}, TokensIn: 10, ToolCalls: 2, SpanSeconds: 5, APIEquivalentUSD: &u1, USDLabel: "est"}}
	b := &ChangeRecord{Cost: &CostSummary{Sessions: 1, SessionIDs: []string{"s2"}, TokensIn: 5, ToolCalls: 1, SpanSeconds: 3, APIEquivalentUSD: &u2, WindowNote: "w"}}
	m := Merge(a, b)
	if m.Cost == nil || m.Cost.Sessions != 2 || m.Cost.TokensIn != 15 || m.Cost.ToolCalls != 3 || m.Cost.SpanSeconds != 8 {
		t.Fatalf("cost not summed: %+v", m.Cost)
	}
	if *m.Cost.APIEquivalentUSD != 3.5 || m.Cost.WindowNote != "w" || len(m.Cost.SessionIDs) != 2 {
		t.Fatalf("estimate/notes/ids wrong: %+v", m.Cost)
	}
}
