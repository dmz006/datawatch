package mergeshadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func samplePlan(id, project string, created time.Time) *MergePlan {
	return &MergePlan{
		AutomatonID:       id,
		Project:           project,
		Branch:            "automaton/" + id,
		BaseRef:           "main",
		ReleaseSize:       ReleaseSizeMinor,
		PlannedVersion:    "9.113.0",
		ChangelogHeading:  "9.113.0 — shadow merge plans",
		ChangelogBody:     "Adds the plan type.",
		ChangelogSource:   "task-1",
		ReadmeLine:        "- Shadow merge plans",
		RegistryRow:       "| 436 | shadow plans |",
		ParityIssueNeeded: true,
		Conflicts: []Conflict{{
			Path: "CHANGELOG.md", FileClass: "docs", Resolution: "keep-both", ResolvedBy: "rule",
		}},
		Gates: []Gate{{
			Name: "go test", Status: "pass", Duration: 1500 * time.Millisecond, Detail: "ok",
		}},
		Findings:  []Finding{{Code: "F1", Message: "missing test", Blocking: true}},
		CreatedAt: created,
		Applied:   false,
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	in := samplePlan("auto-1", "datawatch", created)
	if err := Save(dir, "auto-1", in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir, "auto-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AutomatonID != in.AutomatonID || got.Project != in.Project || got.Branch != in.Branch ||
		got.BaseRef != in.BaseRef || got.ReleaseSize != in.ReleaseSize ||
		got.PlannedVersion != in.PlannedVersion || got.ChangelogHeading != in.ChangelogHeading ||
		got.ChangelogBody != in.ChangelogBody || got.ChangelogSource != in.ChangelogSource ||
		got.ReadmeLine != in.ReadmeLine || got.RegistryRow != in.RegistryRow ||
		got.ParityIssueNeeded != in.ParityIssueNeeded || got.Applied {
		t.Fatalf("scalar fields differ:\n got %+v\nwant %+v", got, in)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if len(got.Conflicts) != 1 || got.Conflicts[0] != in.Conflicts[0] {
		t.Fatalf("conflicts = %+v", got.Conflicts)
	}
	if len(got.Gates) != 1 || got.Gates[0] != in.Gates[0] {
		t.Fatalf("gates = %+v", got.Gates)
	}
	if len(got.Findings) != 1 || got.Findings[0] != in.Findings[0] {
		t.Fatalf("findings = %+v", got.Findings)
	}
}

func TestJSONFieldNames(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "auto-json", samplePlan("auto-json", "p", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, storeDir, "auto-json.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"automaton_id", "project", "branch", "base_ref", "release_size",
		"planned_version", "changelog_heading", "changelog_body", "changelog_source", "readme_line",
		"registry_row", "parity_issue_needed", "conflicts", "gates", "findings", "created_at", "applied"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing JSON key %q", k)
		}
	}
}

func TestSaveNormalisesNilListsToEmpty(t *testing.T) {
	dir := t.TempDir()
	p := samplePlan("auto-nil", "p", time.Now().UTC())
	p.Conflicts, p.Gates, p.Findings = nil, nil, nil
	if err := Save(dir, "auto-nil", p); err != nil {
		t.Fatal(err)
	}
	if p.Conflicts != nil {
		t.Fatal("Save must not mutate the caller's plan")
	}
	got, err := Load(dir, "auto-nil")
	if err != nil {
		t.Fatal(err)
	}
	if got.Conflicts == nil || got.Gates == nil || got.Findings == nil {
		t.Fatalf("expected empty, non-nil lists, got %+v", got)
	}
}

func TestSaveRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	cases := map[string]struct {
		id   string
		plan *MergePlan
	}{
		"nil plan":        {"a", nil},
		"id mismatch":     {"a", samplePlan("b", "p", now)},
		"applied is true": {"a", func() *MergePlan { p := samplePlan("a", "p", now); p.Applied = true; return p }()},
		"empty id":        {"", samplePlan("", "p", now)},
		"path traversal":  {"../x", samplePlan("../x", "p", now)},
		"dot id":          {".", samplePlan(".", "p", now)},
	}
	for name, c := range cases {
		if err := Save(dir, c.id, c.plan); err == nil {
			t.Errorf("%s: Save succeeded, want error", name)
		}
	}
	if _, err := Load(dir, "../x"); err == nil {
		t.Error("Load accepted a path-traversal id")
	}
	if _, err := Load("", "a"); err == nil {
		t.Error("Load accepted an empty data directory")
	}
	if err := Save("", "a", samplePlan("a", "p", now)); err == nil {
		t.Error("Save accepted an empty data directory")
	}
}

func TestLoadMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir, "nope"); !os.IsNotExist(err) {
		t.Fatalf("Load missing: err = %v, want not-exist", err)
	}
	d := filepath.Join(dir, storeDir)
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bad.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "bad"); err == nil {
		t.Fatal("Load corrupt file: want error")
	}
}

func TestReleaseSizeValid(t *testing.T) {
	for _, s := range []ReleaseSize{ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor, ReleaseSizeNone} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []ReleaseSize{"", "huge", "Patch"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestSaveOverwriteAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	v1 := samplePlan("auto-a", "p", now)
	v1.PlannedVersion = "1.0.0"
	if err := Save(dir, "auto-a", v1); err != nil {
		t.Fatal(err)
	}
	v2 := samplePlan("auto-a", "p", now)
	v2.PlannedVersion = "2.0.0"
	if err := Save(dir, "auto-a", v2); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "auto-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.PlannedVersion != "2.0.0" {
		t.Fatalf("PlannedVersion = %q, want 2.0.0", got.PlannedVersion)
	}
	// No temp files may survive a successful Save.
	entries, err := os.ReadDir(filepath.Join(dir, storeDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "auto-a.json" {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("store dir = %v, want only auto-a.json", names)
	}
}

func TestCrashLeftoverTempFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, storeDir)
	if err := Save(dir, "auto-c", samplePlan("auto-c", "p", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash mid-write: a partial temp file is left behind.
	partial := filepath.Join(d, ".plan-123456.tmp")
	if err := os.WriteFile(partial, []byte(`{"automaton_id":"auto-c","proj`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "auto-c"); err != nil {
		t.Fatalf("Load after crash leftover: %v", err)
	}
	plans, err := List(dir)
	if err != nil {
		t.Fatalf("List after crash leftover: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("List = %d plans, want 1", len(plans))
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	const writers = 8
	const rounds = 20
	now := time.Now().UTC()
	var wg sync.WaitGroup
	errs := make(chan error, writers*rounds*2)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				// Same id from every writer: last write wins, never torn.
				p := samplePlan("shared", "p", now)
				p.PlannedVersion = fmt.Sprintf("w%d-r%d", w, r)
				if err := Save(dir, "shared", p); err != nil {
					errs <- err
				}
				// A distinct id per writer, read back concurrently.
				id := fmt.Sprintf("own-%d", w)
				if err := Save(dir, id, samplePlan(id, "p", now)); err != nil {
					errs <- err
				}
				if _, err := Load(dir, "shared"); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent op: %v", err)
	}
	plans, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != writers+1 {
		t.Fatalf("List = %d plans, want %d", len(plans), writers+1)
	}
	got, err := Load(dir, "shared")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.PlannedVersion, "w") {
		t.Fatalf("shared PlannedVersion = %q", got.PlannedVersion)
	}
}

func TestListNewestFirstAndEmpty(t *testing.T) {
	dir := t.TempDir()
	plans, err := List(dir)
	if err != nil {
		t.Fatalf("List empty dir: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("List empty = %d", len(plans))
	}
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"old", "mid", "new"} {
		if err := Save(dir, id, samplePlan(id, "p", base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	// A tie on CreatedAt is broken by automaton id.
	if err := Save(dir, "a-tie", samplePlan("a-tie", "p", base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	plans, err = List(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a-tie", "new", "mid", "old"}
	if len(plans) != len(want) {
		t.Fatalf("List = %d, want %d", len(plans), len(want))
	}
	for i, id := range want {
		if plans[i].AutomatonID != id {
			t.Errorf("plans[%d] = %s, want %s", i, plans[i].AutomatonID, id)
		}
	}
}

func TestListCorruptFileErrors(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, storeDir)
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("List with corrupt file: want error")
	}
	if _, err := List(""); err == nil {
		t.Fatal("List empty dir arg: want error")
	}
}

func TestPrunePerProject(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Project a: 4 plans; project b: 2 plans.
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("a%d", i)
		if err := Save(dir, id, samplePlan(id, "a", base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("b%d", i)
		if err := Save(dir, id, samplePlan(id, "b", base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := Prune(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	plans, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]bool{}
	for _, p := range plans {
		kept[p.AutomatonID] = true
	}
	for _, id := range []string{"a2", "a3", "b0", "b1"} {
		if !kept[id] {
			t.Errorf("%s was pruned, want kept", id)
		}
	}
	for _, id := range []string{"a0", "a1"} {
		if kept[id] {
			t.Errorf("%s was kept, want pruned", id)
		}
	}
}

func TestPruneNoOpAndValidation(t *testing.T) {
	dir := t.TempDir()
	if n, err := Prune(dir, 5); err != nil || n != 0 {
		t.Fatalf("Prune empty store = %d, %v", n, err)
	}
	if err := Save(dir, "only", samplePlan("only", "p", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if n, err := Prune(dir, 5); err != nil || n != 0 {
		t.Fatalf("Prune under limit = %d, %v", n, err)
	}
	if _, err := Prune(dir, 0); err == nil {
		t.Fatal("Prune keep=0: want error")
	}
	if _, err := Prune("", 1); err == nil {
		t.Fatal("Prune empty dir: want error")
	}
	if _, err := Prune(dir, -1); err == nil {
		t.Fatal("Prune keep=-1: want error")
	}
}

func TestPruneCorruptFileErrors(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, storeDir)
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(dir, 1); err == nil {
		t.Fatal("Prune with corrupt file: want error")
	}
}

func TestStoreDirBlockedByFile(t *testing.T) {
	dir := t.TempDir()
	// A regular file where the store directory should be makes every
	// directory operation fail; the error must surface, not be swallowed.
	if err := os.WriteFile(filepath.Join(dir, storeDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := Save(dir, "a", samplePlan("a", "p", now)); err == nil {
		t.Error("Save into blocked store dir: want error")
	}
	if _, err := List(dir); err == nil {
		t.Error("List over blocked store dir: want error")
	}
	if _, err := Prune(dir, 1); err == nil {
		t.Error("Prune over blocked store dir: want error")
	}
}

// TestStoreRefusesUnsafeIDs: ids with path separators, dots, spaces, NUL
// bytes or nothing at all are refused by every id-taking store function, and
// nothing is created outside (or inside) the store directory.
func TestStoreRefusesUnsafeIDs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	bad := []string{
		"", ".", "..", "../x", "a/b", "/abs", `a\b`, "a.b", "x.json",
		".hidden", "a b", "a\x00b", "..%2f", "a/../../b",
	}
	for _, id := range bad {
		if err := Save(dir, id, samplePlan(id, "p", now)); err == nil {
			t.Errorf("Save accepted id %q", id)
		}
		if _, err := Load(dir, id); err == nil {
			t.Errorf("Load accepted id %q", id)
		}
		if _, err := planPath(dir, id); err == nil {
			t.Errorf("planPath accepted id %q", id)
		}
		if _, err := storePath(dir, compareStoreDir, id); err == nil {
			t.Errorf("storePath accepted id %q", id)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("refused ids left files behind: %v", entries)
	}
	for _, id := range []string{"auto-1", "A_b-9", "0123abcd"} {
		p, err := planPath(dir, id)
		if err != nil {
			t.Errorf("planPath refused valid id %q: %v", id, err)
			continue
		}
		if want := filepath.Join(dir, storeDir, id+".json"); p != want {
			t.Errorf("planPath(%q) = %q, want %q", id, p, want)
		}
	}
}

// TestContainedPathStaysInside: the shared containment check refuses any
// name that is not a single plain file name.
func TestContainedPathStaysInside(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", ".", "..", "../x.json", "a/b.json", "/etc/passwd"} {
		if _, err := containedPath(dir, name); err == nil {
			t.Errorf("containedPath accepted %q", name)
		}
		if _, err := readPlan(dir, name); err == nil {
			t.Errorf("readPlan accepted %q", name)
		}
		if err := writeAtomic(dir, name, []byte("x")); err == nil {
			t.Errorf("writeAtomic accepted %q", name)
		}
	}
	p, err := containedPath(dir, "ok.json")
	if err != nil || p != filepath.Join(dir, "ok.json") {
		t.Errorf("containedPath(ok.json) = %q, %v", p, err)
	}
}
