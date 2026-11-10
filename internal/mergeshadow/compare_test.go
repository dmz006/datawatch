package mergeshadow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func compareSamplePlan() *MergePlan {
	return &MergePlan{
		AutomatonID:       "auto-x",
		Project:           "datawatch",
		Branch:            "automaton/auto-x",
		BaseRef:           "main",
		ReleaseSize:       ReleaseSizeMinor,
		PlannedVersion:    "9.113.0",
		ChangelogHeading:  "## [9.113.0] - 2026-11-09",
		ChangelogBody:     "Adds the shadow compare.",
		ChangelogSource:   "task-1",
		ReadmeLine:        "- **Current release: 9.113.0 (2026-11-09).** Adds the shadow compare.",
		RegistryRow:       "| 437 | auto-x |",
		ParityIssueNeeded: true,
		Gates: []Gate{
			{Name: "build", Status: string(GatePass), Duration: time.Second},
			{Name: "lint", Status: string(GatePass), Duration: time.Second},
			{Name: "test", Status: string(GatePass), Duration: 2 * time.Second},
		},
		CreatedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
	}
}

// matchingLanded is the landed state that agrees with compareSamplePlan on
// every field the comparison reads.
func matchingLanded() Landed {
	return Landed{
		ChangelogHeading: Value{Value: "## [9.113.0] - 2026-11-09", Found: true},
		Versions: map[string]Value{
			"cmd/datawatch/main.go":     {Value: "9.113.0", Found: true},
			"internal/server/server.go": {Value: "9.113.0", Found: true},
		},
		ReadmeLine:  Value{Value: "- **Current release: 9.113.0 (2026-11-09).** Adds the shadow compare.", Found: true},
		RegistryRow: Value{Value: "| 437 | auto-x |", Found: true},
	}
}

func matchingGates() []GateOutcome {
	return []GateOutcome{
		{Name: "build", Status: string(GatePass)},
		{Name: "lint", Status: string(GatePass)},
		{Name: "test", Status: string(GatePass)},
	}
}

func TestCompareAllMatched(t *testing.T) {
	c, err := Compare(compareSamplePlan(), matchingLanded(), true, matchingGates())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if c.AutomatonID != "auto-x" || c.ReleaseSize != ReleaseSizeMinor {
		t.Fatalf("identity = %+v", c)
	}
	if c.Matched != 6 || c.Different != 0 || c.NotCompared != 0 {
		t.Fatalf("counts = matched %d different %d not-compared %d, want 6/0/0", c.Matched, c.Different, c.NotCompared)
	}
	if c.Differences == nil || len(c.Differences) != 0 {
		t.Fatalf("Differences = %v", c.Differences)
	}
}

func TestCompareNilPlan(t *testing.T) {
	if _, err := Compare(nil, matchingLanded(), true, matchingGates()); err == nil {
		t.Fatal("Compare(nil plan): want error")
	}
}

func TestCompareOneDifferencePerField(t *testing.T) {
	want := func(field, planned, landed string, sev Severity) Difference {
		return Difference{Field: field, Planned: planned, Landed: landed, Severity: sev}
	}
	cases := []struct {
		name   string
		mutate func(p *MergePlan, l *Landed)
		parity bool
		gates  []GateOutcome
		want   Difference
	}{
		{
			name: "version mismatch",
			mutate: func(p *MergePlan, l *Landed) {
				l.Versions["internal/server/server.go"] = Value{Value: "9.114.0", Found: true}
			},
			parity: true,
			gates:  matchingGates(),
			want: want(
				"version", "9.113.0",
				"cmd/datawatch/main.go=9.113.0; internal/server/server.go=9.114.0",
				SeverityMajor),
		},
		{
			name: "changelog heading drift",
			mutate: func(p *MergePlan, l *Landed) {
				l.ChangelogHeading = Value{Value: "## [9.113.0] - 2026-11-10", Found: true}
			},
			parity: true,
			gates:  matchingGates(),
			want:   want("changelog_heading", "## [9.113.0] - 2026-11-09", "## [9.113.0] - 2026-11-10", SeverityMinor),
		},
		{
			name: "readme line drift",
			mutate: func(p *MergePlan, l *Landed) {
				l.ReadmeLine = Value{Value: "- **Current release: 9.113.0 (2026-11-09).**", Found: true}
			},
			parity: true,
			gates:  matchingGates(),
			want:   want("readme_line", "- **Current release: 9.113.0 (2026-11-09).** Adds the shadow compare.", "- **Current release: 9.113.0 (2026-11-09).**", SeverityMinor),
		},
		{
			name:   "registry row drift",
			mutate: func(p *MergePlan, l *Landed) { l.RegistryRow = Value{Value: "| 438 | auto-x |", Found: true} },
			parity: true,
			gates:  matchingGates(),
			want:   want("registry_row", "| 437 | auto-x |", "| 438 | auto-x |", SeverityMinor),
		},
		{
			name:   "required parity issue never landed",
			parity: false,
			gates:  matchingGates(),
			want:   want("parity_issue", "true", "false", SeverityMajor),
		},
		{
			name: "parity issue landed without being required",
			mutate: func(p *MergePlan, l *Landed) {
				p.ParityIssueNeeded = false
			},
			parity: true,
			gates:  matchingGates(),
			want:   want("parity_issue", "false", "true", SeverityMinor),
		},
		{
			name:   "gate outcome differs",
			parity: true,
			gates:  []GateOutcome{{Name: "build", Status: string(GatePass)}, {Name: "lint", Status: string(GateFail)}, {Name: "test", Status: string(GatePass)}},
			want:   want("gates", "lint=pass", "lint=fail", SeverityMajor),
		},
		{
			name:   "gate missing from the manual record",
			parity: true,
			gates:  []GateOutcome{{Name: "build", Status: string(GatePass)}, {Name: "test", Status: string(GatePass)}},
			want:   want("gates", "lint=pass", "lint=(missing)", SeverityMajor),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := compareSamplePlan()
			l := matchingLanded()
			if tc.mutate != nil {
				tc.mutate(p, &l)
			}
			c, err := Compare(p, l, tc.parity, tc.gates)
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			if c.Different != 1 || c.Matched != 5 || c.NotCompared != 0 {
				t.Fatalf("counts = matched %d different %d not-compared %d, want 5/1/0", c.Matched, c.Different, c.NotCompared)
			}
			if len(c.Differences) != 1 {
				t.Fatalf("Differences = %v, want exactly one", c.Differences)
			}
			if !reflect.DeepEqual(c.Differences[0], tc.want) {
				t.Fatalf("Difference = %+v, want %+v", c.Differences[0], tc.want)
			}
		})
	}
}

func TestCompareMissingLandedValues(t *testing.T) {
	p := compareSamplePlan()
	// Every landed value the comparison reads is absent.
	l := Landed{
		ChangelogHeading: Value{Found: false},
		Versions:         map[string]Value{"cmd/datawatch/main.go": {Found: false}},
		ReadmeLine:       Value{Found: false},
		RegistryRow:      Value{Found: false},
	}
	c, err := Compare(p, l, true, matchingGates())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if c.Different != 4 || c.Matched != 2 || c.NotCompared != 0 {
		t.Fatalf("counts = matched %d different %d not-compared %d, want 2/4/0", c.Matched, c.Different, c.NotCompared)
	}
	byField := map[string]Difference{}
	for _, d := range c.Differences {
		byField[d.Field] = d
	}
	for field, severity := range map[string]Severity{
		"changelog_heading": SeverityMajor,
		"readme_line":       SeverityMajor,
		"registry_row":      SeverityMajor,
	} {
		d, ok := byField[field]
		if !ok {
			t.Fatalf("no difference for %s: %+v", field, c.Differences)
		}
		if d.Landed != missingMarker {
			t.Errorf("%s landed = %q, want %q", field, d.Landed, missingMarker)
		}
		if d.Severity != severity {
			t.Errorf("%s severity = %q, want %q", field, d.Severity, severity)
		}
	}
	v, ok := byField["version"]
	if !ok {
		t.Fatalf("no difference for version: %+v", c.Differences)
	}
	if v.Planned != "9.113.0" {
		t.Errorf("version planned = %q, want 9.113.0", v.Planned)
	}
	if v.Landed != "cmd/datawatch/main.go=(missing)" {
		t.Errorf("version landed = %q, want %q", v.Landed, "cmd/datawatch/main.go=(missing)")
	}
	if v.Severity != SeverityMajor {
		t.Errorf("version severity = %q, want %q", v.Severity, SeverityMajor)
	}
}

func TestCompareNoVersionFilesAtAll(t *testing.T) {
	p := compareSamplePlan()
	l := matchingLanded()
	l.Versions = nil
	c, err := Compare(p, l, true, matchingGates())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if c.Different != 1 || c.Matched != 5 {
		t.Fatalf("counts = matched %d different %d, want 5/1", c.Matched, c.Different)
	}
	if c.Differences[0].Field != "version" || c.Differences[0].Landed != missingMarker || c.Differences[0].Severity != SeverityMajor {
		t.Fatalf("difference = %+v", c.Differences[0])
	}
}

func TestCompareGatesNotComparedWithoutRecord(t *testing.T) {
	p := compareSamplePlan()
	l := matchingLanded()
	for name, gates := range map[string][]GateOutcome{"nil": nil, "empty": {}} {
		c, err := Compare(p, l, true, gates)
		if err != nil {
			t.Fatalf("%s: Compare: %v", name, err)
		}
		if c.NotCompared != 1 || c.Matched != 5 || c.Different != 0 {
			t.Fatalf("%s: counts = matched %d different %d not-compared %d, want 5/0/1", name, c.Matched, c.Different, c.NotCompared)
		}
		if len(c.Differences) != 0 {
			t.Fatalf("%s: Differences = %v, want none", name, c.Differences)
		}
	}
}

func TestCompareGatesFieldMatchesWithPlanWithoutGates(t *testing.T) {
	p := compareSamplePlan()
	p.Gates = nil
	l := matchingLanded()
	c, err := Compare(p, l, true, matchingGates())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if c.Matched != 6 || c.Different != 0 || c.NotCompared != 0 {
		t.Fatalf("counts = matched %d different %d not-compared %d, want 6/0/0", c.Matched, c.Different, c.NotCompared)
	}
}

func sampleComparison(created time.Time) *Comparison {
	return &Comparison{
		AutomatonID: "auto-x",
		ReleaseSize: ReleaseSizeMinor,
		Matched:     5,
		Different:   1,
		Differences: []Difference{{
			Field: "readme_line", Planned: "a", Landed: "b", Severity: SeverityMinor,
		}},
		CreatedAt: created,
	}
}

func TestSaveLoadComparisonRoundTrip(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	in := sampleComparison(created)
	if err := SaveComparison(dir, "auto-x", in); err != nil {
		t.Fatalf("SaveComparison: %v", err)
	}
	got, err := LoadComparison(dir, "auto-x")
	if err != nil {
		t.Fatalf("LoadComparison: %v", err)
	}
	if got.AutomatonID != "auto-x" || got.ReleaseSize != ReleaseSizeMinor ||
		got.Matched != 5 || got.Different != 1 || got.NotCompared != 0 {
		t.Fatalf("scalar fields differ: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if len(got.Differences) != 1 || !reflect.DeepEqual(got.Differences[0], in.Differences[0]) {
		t.Fatalf("Differences = %v", got.Differences)
	}
}

func TestComparisonJSONFieldNames(t *testing.T) {
	dir := t.TempDir()
	if err := SaveComparison(dir, "auto-x", sampleComparison(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, compareStoreDir, "auto-x.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"automaton_id", "release_size", "matched", "different", "not_compared", "differences", "created_at"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing JSON key %q", k)
		}
	}
	diff, ok := raw["differences"].([]any)
	if !ok || len(diff) != 1 {
		t.Fatalf("differences = %v", raw["differences"])
	}
	fields, ok := diff[0].(map[string]any)
	if !ok {
		t.Fatalf("differences[0] = %v", diff[0])
	}
	for _, k := range []string{"field", "planned", "landed", "severity"} {
		if _, ok := fields[k]; !ok {
			t.Errorf("difference missing JSON key %q", k)
		}
	}
}

func TestSaveComparisonNormalisesNilDifferences(t *testing.T) {
	dir := t.TempDir()
	c := sampleComparison(time.Now().UTC())
	c.Differences = nil
	if err := SaveComparison(dir, "auto-x", c); err != nil {
		t.Fatal(err)
	}
	if c.Differences != nil {
		t.Fatal("SaveComparison must not mutate the caller's comparison")
	}
	got, err := LoadComparison(dir, "auto-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Differences == nil || len(got.Differences) != 0 {
		t.Fatalf("Differences = %v, want empty non-nil", got.Differences)
	}
}

func TestSaveComparisonRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	cases := map[string]struct {
		id string
		c  *Comparison
	}{
		"nil comparison": {"a", nil},
		"id mismatch":    {"a", sampleComparisonWithID("b", now)},
		"empty id":       {"", sampleComparisonWithID("", now)},
		"path traversal": {"../x", sampleComparisonWithID("../x", now)},
		"dot id":         {".", sampleComparisonWithID(".", now)},
	}
	for name, c := range cases {
		if err := SaveComparison(dir, c.id, c.c); err == nil {
			t.Errorf("%s: SaveComparison succeeded, want error", name)
		}
	}
	if _, err := LoadComparison(dir, "../x"); err == nil {
		t.Error("LoadComparison accepted a path-traversal id")
	}
	if _, err := LoadComparison("", "a"); err == nil {
		t.Error("LoadComparison accepted an empty data directory")
	}
	if err := SaveComparison("", "a", sampleComparisonWithID("a", now)); err == nil {
		t.Error("SaveComparison accepted an empty data directory")
	}
}

func sampleComparisonWithID(id string, created time.Time) *Comparison {
	c := sampleComparison(created)
	c.AutomatonID = id
	return c
}

func TestLoadComparisonMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadComparison(dir, "nope"); err == nil {
		t.Fatal("LoadComparison missing: want error")
	}
	d := filepath.Join(dir, compareStoreDir)
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bad.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadComparison(dir, "bad"); err == nil {
		t.Fatal("LoadComparison corrupt file: want error")
	}
}

func TestSaveComparisonOverwriteAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	v1 := sampleComparisonWithID("auto-a", now)
	v1.Matched = 6
	if err := SaveComparison(dir, "auto-a", v1); err != nil {
		t.Fatal(err)
	}
	v2 := sampleComparisonWithID("auto-a", now)
	v2.Matched = 4
	if err := SaveComparison(dir, "auto-a", v2); err != nil {
		t.Fatal(err)
	}
	got, err := LoadComparison(dir, "auto-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Matched != 4 {
		t.Fatalf("Matched = %d, want 4", got.Matched)
	}
	// No temp files may survive a successful SaveComparison.
	entries, err := os.ReadDir(filepath.Join(dir, compareStoreDir))
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

func TestComparisonStoreCrashLeftoverIgnored(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, compareStoreDir)
	if err := SaveComparison(dir, "auto-c", sampleComparisonWithID("auto-c", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(d, ".plan-123456.tmp")
	if err := os.WriteFile(partial, []byte(`{"automaton_id":"auto-c","mat`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadComparison(dir, "auto-c"); err != nil {
		t.Fatalf("LoadComparison after crash leftover: %v", err)
	}
	got, err := ListComparisons(dir)
	if err != nil {
		t.Fatalf("ListComparisons after crash leftover: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListComparisons = %d, want 1", len(got))
	}
}

func TestListComparisonsNewestFirstAndEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := ListComparisons(dir)
	if err != nil {
		t.Fatalf("ListComparisons empty dir: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("ListComparisons empty = %v", got)
	}
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"old", "mid", "new"} {
		if err := SaveComparison(dir, id, sampleComparisonWithID(id, base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	// A tie on CreatedAt is broken by automaton id.
	if err := SaveComparison(dir, "a-tie", sampleComparisonWithID("a-tie", base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	got, err = ListComparisons(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a-tie", "new", "mid", "old"}
	if len(got) != len(want) {
		t.Fatalf("ListComparisons = %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].AutomatonID != id {
			t.Errorf("got[%d] = %s, want %s", i, got[i].AutomatonID, id)
		}
	}
	if _, err := ListComparisons(""); err == nil {
		t.Fatal("ListComparisons empty dir arg: want error")
	}
}

func TestListComparisonsCorruptFileErrors(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, compareStoreDir)
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListComparisons(dir); err == nil {
		t.Fatal("ListComparisons with corrupt file: want error")
	}
}

func TestComparisonStoreDirBlockedByFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, compareStoreDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveComparison(dir, "a", sampleComparisonWithID("a", time.Now().UTC())); err == nil {
		t.Error("SaveComparison into blocked store dir: want error")
	}
	if _, err := LoadComparison(dir, "a"); err == nil {
		t.Error("LoadComparison over blocked store dir: want error")
	}
	if _, err := ListComparisons(dir); err == nil {
		t.Error("ListComparisons over blocked store dir: want error")
	}
}
