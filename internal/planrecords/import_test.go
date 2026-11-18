package planrecords

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// fixtureRoot is the absolute path of the fixture project.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "project"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// hashTree returns a hash of every file under dir (path and bytes).
func hashTree(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(b)
	}
	return string(h.Sum(nil))
}

func newImporter(t *testing.T, root string) (*Importer, *[]auditEv) {
	t.Helper()
	s, _, evs, _ := newTestStore(t, nil)
	return &Importer{
		Store:      s,
		ProjectDir: func(a string) (string, bool) { return root, a == "datawatch" || a == "other" },
		Roots:      func() []string { return []string{root} },
	}, evs
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func byTitle(t *testing.T, s *Store, title string) Record {
	t.Helper()
	r, err := s.Resolve(title, "datawatch")
	if err != nil {
		t.Fatalf("resolve %q: %v", title, err)
	}
	return r
}

func TestImportDryRunEqualsRealRun(t *testing.T) {
	root := fixtureRoot(t)
	before := hashTree(t, root)

	imp, evs := newImporter(t, root)
	dry, err := imp.Import(ImportOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Store.Len() != 0 || len(*evs) != 0 {
		t.Fatalf("dry run wrote: %d records, %d audit events", imp.Store.Len(), len(*evs))
	}
	real, err := imp.Import(ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Counts != real.Counts {
		t.Fatalf("dry %+v != real %+v", dry.Counts, real.Counts)
	}
	want := ImportCounts{Create: 14, Unchanged: 0, Changed: 0, Conflicts: 3, Skipped: 1}
	if real.Counts != want {
		t.Fatalf("counts %+v, want %+v", real.Counts, want)
	}
	if imp.Store.Len() != 14 {
		t.Fatalf("store has %d records, want 14", imp.Store.Len())
	}
	if !reflect.DeepEqual(conflictSources(dry), conflictSources(real)) {
		t.Fatalf("dry and real conflict lists differ:\n%v\n%v", conflictSources(dry), conflictSources(real))
	}
	if len(real.Flagged) == 0 {
		t.Fatal("no flagged records returned")
	}
	for _, f := range real.Flagged {
		if f.ID == "" {
			t.Fatalf("flagged record %q has no id after a real run", f.Title)
		}
	}
	if len(dry.Unparseable) != 1 || !strings.Contains(dry.Unparseable[0].Source, "row 9") {
		t.Fatalf("unparseable: %+v", dry.Unparseable)
	}
	if after := hashTree(t, root); after != before {
		t.Fatal("fixture files changed")
	}
	var sawSummary bool
	for _, e := range *evs {
		if e.action == AuditImported {
			sawSummary = true
		}
	}
	if !sawSummary {
		t.Fatal("no import summary audit event")
	}
}

func conflictSources(r *ImportResult) []string {
	var out []string
	for _, c := range r.Conflicts {
		out = append(out, c.Source+"|"+c.Reason+"|"+c.Action+"|"+c.NewTitle)
	}
	return out
}

func TestImportSecondRunIsNoOp(t *testing.T) {
	root := fixtureRoot(t)
	before := hashTree(t, root)
	imp, evs := newImporter(t, root)
	if _, err := imp.Import(ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	n, nev := imp.Store.Len(), len(*evs)
	second, err := imp.Import(ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Counts.Create != 0 || second.Counts.Changed != 0 || second.Counts.Unchanged != 14 || second.Counts.Conflicts != 0 {
		t.Fatalf("second run counts %+v", second.Counts)
	}
	if imp.Store.Len() != n {
		t.Fatalf("second run created records: %d -> %d", n, imp.Store.Len())
	}
	// Only the summary event of the second run may appear.
	for _, e := range (*evs)[nev:] {
		if e.action == AuditCreated || e.action == AuditUpdated {
			t.Fatalf("second run wrote a record: %s", e.action)
		}
	}
	// A dry run after the real run reports the same.
	dry, _ := imp.Import(ImportOptions{DryRun: true})
	if dry.Counts != second.Counts {
		t.Fatalf("dry %+v != second %+v", dry.Counts, second.Counts)
	}
	if hashTree(t, root) != before {
		t.Fatal("fixture files changed")
	}
}

func TestImportRecordMapping(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	if _, err := imp.Import(ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	s := imp.Store

	a := byTitle(t, s, "Alpha feature works")
	if a.LegacyNumber != 1 || a.State != StateDone || a.ShippedVersion != "shipped in 9.1.0" || a.Kind != KindRegistryRow {
		t.Fatalf("row 1: %+v", a)
	}
	if a.LegacySource != "docs/plans/README.md row 1" {
		t.Fatalf("legacy source %q", a.LegacySource)
	}
	if len(a.Links) == 0 || a.Links[0] != (Link{Kind: "doc", Ref: "docs/plans/2026-01-01-alpha.md"}) {
		t.Fatalf("doc link missing: %+v", a.Links)
	}
	if !strings.Contains(a.Body, "More text follows") || !strings.Contains(a.Body, "Original item cell:") {
		t.Fatalf("body lost the rest of the cell: %q", a.Body)
	}
	if strings.Contains(a.Title, "BL1") || strings.Contains(a.Title, "[") {
		t.Fatalf("title not cleaned: %q", a.Title)
	}

	// Column mapping from each table's own header.
	if b := byTitle(t, s, "Beta thing"); b.State != StateInProgress || b.LegacyNumber != 2 || !strings.Contains(b.Body, "Depends on: 1") {
		t.Fatalf("row 2: %+v", b)
	}
	for title, want := range map[string]State{"Delta": StateBlocked, "Epsilon": StateInProgress, "Zeta": StateOpen, "Eta": StateShipped} {
		if r := byTitle(t, s, title); r.State != want {
			t.Fatalf("%s state %s, want %s", title, r.State, want)
		}
	}
	if d := byTitle(t, s, "Delta"); d.LegacyNumber != 6 || !strings.Contains(d.Body, "Note: waits for review") {
		t.Fatalf("Delta: %+v", d)
	}
	if e := byTitle(t, s, "Eta"); e.ShippedVersion != "Shipped 9.2.0" {
		t.Fatalf("Eta version %q", e.ShippedVersion)
	}
	// An odd status word: open, flagged.
	if g := byTitle(t, s, "Gamma"); g.State != StateOpen || !g.NeedsReview || !strings.Contains(g.ReviewReason, "wobbly") {
		t.Fatalf("Gamma: %+v", g)
	}
	// "row 4" resolves through the legacy number and was suffixed.
	r4, err := s.Resolve("row 4", "datawatch")
	if err != nil || r4.Title != "Alpha feature works (row 4)" || !r4.NeedsReview || r4.State != StateDeferred {
		t.Fatalf("row 4: %+v %v", r4, err)
	}
	// B# rows are not numbered registry rows.
	if _, err := s.Resolve("A defect row", "datawatch"); err == nil {
		t.Fatal("a B# row was imported")
	}

	// Findings.
	xa, err := s.Resolve("Alpha", "datawatch")
	if err != nil || xa.Kind != KindFinding || xa.LegacySource != "docs/plans/sub/x-findings.md#alpha" {
		t.Fatalf("Alpha finding: %+v %v", xa, err)
	}
	if !strings.Contains(xa.Body, "Date: 2026-10-24") || !strings.Contains(xa.Body, "First section body") {
		t.Fatalf("Alpha body: %q", xa.Body)
	}
	if strings.Contains(xa.Body, "Second section body") {
		t.Fatal("sections not split")
	}
	beta := byTitle(t, s, "Beta")
	if !strings.Contains(beta.Body, "not a heading inside a fence") || beta.LegacySource != "docs/plans/sub/x-findings.md#beta" {
		t.Fatalf("Beta: %+v", beta)
	}
	ya := byTitle(t, s, "Alpha (y-findings.md)")
	if !ya.NeedsReview || ya.LegacySource != "docs/plans/sub/y-findings.md#alpha" {
		t.Fatalf("suffixed finding: %+v", ya)
	}
	plain := byTitle(t, s, "Plain findings file")
	if plain.LegacySource != "docs/plans/2026-10-02-plain-findings.md" || !strings.Contains(plain.Body, "Date: 2026-10-02") {
		t.Fatalf("plain: %+v", plain)
	}
	nh := byTitle(t, s, "nohead-findings")
	if nh.LegacySource != "docs/plans/nohead-findings.md" {
		t.Fatalf("no heading file: %+v", nh)
	}
	if _, err := s.Resolve("Not a findings file", "datawatch"); err == nil {
		t.Fatal("unrelated.md was imported")
	}
}

func TestImportPolicySuffix(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	res, err := imp.Import(ImportOptions{Policy: PolicySuffix, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ImportConflict{}
	for _, c := range res.Conflicts {
		got[c.Source] = c
	}
	if c := got["docs/plans/README.md row 4"]; c.Action != "suffix" || c.NewTitle != "Alpha feature works (row 4)" || c.Reason != "title" {
		t.Fatalf("row 4 conflict: %+v", c)
	}
	if c := got["docs/plans/sub/y-findings.md#alpha"]; c.NewTitle != "Alpha (y-findings.md)" {
		t.Fatalf("finding conflict: %+v", c)
	}
	var dup *ImportConflict
	for i := range res.Conflicts {
		if res.Conflicts[i].Reason == "duplicate_row_number" {
			dup = &res.Conflicts[i]
		}
	}
	if dup == nil || dup.Action != "suffix" || !strings.Contains(dup.NewTitle, "row 3") {
		t.Fatalf("duplicate row conflict: %+v", dup)
	}
}

func TestImportPolicySkip(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	dry, _ := imp.Import(ImportOptions{Policy: PolicySkip, DryRun: true})
	real, err := imp.Import(ImportOptions{Policy: PolicySkip})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Counts != real.Counts {
		t.Fatalf("dry %+v != real %+v", dry.Counts, real.Counts)
	}
	want := ImportCounts{Create: 11, Conflicts: 3, Skipped: 4}
	if real.Counts != want {
		t.Fatalf("counts %+v, want %+v", real.Counts, want)
	}
	if imp.Store.Len() != 11 {
		t.Fatalf("store has %d, want 11", imp.Store.Len())
	}
	if _, err := imp.Store.Resolve("row 4", "datawatch"); err == nil {
		t.Fatal("skipped row 4 was created")
	}
	for _, c := range real.Conflicts {
		if c.Action != "skip" || c.NewTitle != "" {
			t.Fatalf("conflict not skipped: %+v", c)
		}
	}
	// A later run with the suffix policy picks up the skipped items.
	again, err := imp.Import(ImportOptions{Policy: PolicySuffix})
	if err != nil {
		t.Fatal(err)
	}
	if again.Counts.Create != 3 || again.Counts.Unchanged != 11 {
		t.Fatalf("follow-up run %+v", again.Counts)
	}
}

func TestImportPolicyRename(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	renames := map[string]string{
		"row 4":                              "Alpha successor",
		"docs/plans/sub/y-findings.md#alpha": "Alpha, the Y file",
	}
	dry, err := imp.Import(ImportOptions{Policy: PolicyRename, Renames: renames, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	real, err := imp.Import(ImportOptions{Policy: PolicyRename, Renames: renames})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Counts != real.Counts {
		t.Fatalf("dry %+v != real %+v", dry.Counts, real.Counts)
	}
	// The repeated row 3 has no supplied title: skipped, listed, not failed.
	want := ImportCounts{Create: 13, Conflicts: 3, Skipped: 2}
	if real.Counts != want {
		t.Fatalf("counts %+v, want %+v", real.Counts, want)
	}
	if r := byTitle(t, imp.Store, "Alpha successor"); r.LegacyNumber != 4 || r.NeedsReview {
		t.Fatalf("renamed row: %+v", r)
	}
	byTitle(t, imp.Store, "Alpha, the Y file")
	var skipped bool
	for _, c := range real.Conflicts {
		if c.Reason == "duplicate_row_number" && c.Action == "skip" && c.Note != "" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("duplicate row without a rename was not reported as skipped: %+v", real.Conflicts)
	}

	// A supplied title that is itself taken never fails the run.
	imp2, _ := newImporter(t, root)
	res, err := imp2.Import(ImportOptions{Policy: PolicyRename, Renames: map[string]string{"row 4": "Beta thing"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts.Skipped < 1 || len(res.Failed) != 0 {
		t.Fatalf("taken rename title: %+v failed=%v", res.Counts, res.Failed)
	}
}

func TestImportChangedSourceAndEditedRecord(t *testing.T) {
	root := t.TempDir()
	copyTree(t, fixtureRoot(t), root)
	imp, _ := newImporter(t, root)
	if _, err := imp.Import(ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	// Edit record row 2 by hand; leave row 6 alone.
	edited := "Edited by an operator"
	if _, err := imp.Store.Update("row 2", Patch{Body: &edited}); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(root, "docs/plans/README.md")
	b, _ := os.ReadFile(readme)
	changed := strings.Replace(string(b), "| in progress | 1 |", "| in progress | 1, 6 |", 1)
	changed = strings.Replace(changed, "| 6 | blocked | Delta | waits for review |", "| 6 | done | Delta | waits for review |", 1)
	if changed == string(b) {
		t.Fatal("fixture edit did not apply")
	}
	if err := os.WriteFile(readme, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	dry, _ := imp.Import(ImportOptions{DryRun: true})
	res, err := imp.Import(ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Counts != res.Counts || res.Counts.Changed != 2 || res.Counts.Create != 0 {
		t.Fatalf("dry %+v real %+v", dry.Counts, res.Counts)
	}
	row2, _ := imp.Store.Resolve("row 2", "datawatch")
	if row2.Body != edited {
		t.Fatalf("an edited record was overwritten: %q", row2.Body)
	}
	row6, _ := imp.Store.Resolve("row 6", "datawatch")
	if row6.State != StateDone || row6.ClosedAt == "" {
		t.Fatalf("an unedited record was not brought up to date: %+v", row6)
	}
	for _, c := range res.Changed {
		switch c.Source {
		case "docs/plans/README.md row 2":
			if !c.Edited || c.Updated {
				t.Fatalf("row 2 change: %+v", c)
			}
		case "docs/plans/README.md row 6":
			if c.Edited || !c.Updated {
				t.Fatalf("row 6 change: %+v", c)
			}
		}
	}
	// Once updated, the next run sees row 6 as unchanged; row 2 stays changed.
	third, _ := imp.Import(ImportOptions{})
	if third.Counts.Changed != 1 {
		t.Fatalf("third run %+v", third.Counts)
	}
}

func TestImportUnknownAlias(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	_, err := imp.Import(ImportOptions{Project: "nosuchproject"})
	var up *ErrUnknownProject
	if !errors.As(err, &up) || !strings.Contains(err.Error(), "nosuchproject") {
		t.Fatalf("want ErrUnknownProject naming the alias, got %v", err)
	}
	if imp.Store.Len() != 0 {
		t.Fatal("records written for an unknown alias")
	}
}

func TestImportRootOutsideAllowedRoots(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	imp.Roots = func() []string { return []string{t.TempDir()} }
	_, err := imp.Import(ImportOptions{})
	if !errors.Is(err, gitguard.ErrOutsideAllowedRoots) {
		t.Fatalf("want ErrOutsideAllowedRoots, got %v", err)
	}
	imp.Roots = func() []string { return nil }
	if _, err := imp.Import(ImportOptions{}); !errors.Is(err, gitguard.ErrOutsideAllowedRoots) {
		t.Fatalf("no roots must refuse, got %v", err)
	}
	if imp.Store.Len() != 0 {
		t.Fatal("records written for a refused root")
	}
}

func TestImportFindingsGlobParameter(t *testing.T) {
	root := fixtureRoot(t)
	imp, _ := newImporter(t, root)
	res, err := imp.Import(ImportOptions{FindingsGlob: "docs/plans/sub/*findings*.md", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	// 9 rows + 3 findings in sub/.
	if res.Counts.Create != 12 {
		t.Fatalf("counts %+v", res.Counts)
	}
	if _, err := imp.Import(ImportOptions{FindingsGlob: "../outside/*.md", DryRun: true}); err == nil {
		t.Fatal("a glob that leaves the project was accepted")
	}
	if _, err := imp.Import(ImportOptions{FindingsGlob: "/etc/*.md", DryRun: true}); err == nil {
		t.Fatal("an absolute glob was accepted")
	}
}

func TestImportGlobMatch(t *testing.T) {
	cases := []struct {
		pat, name string
		want      bool
	}{
		{"docs/plans/**/*findings*.md", "docs/plans/a-findings.md", true},
		{"docs/plans/**/*findings*.md", "docs/plans/x/y/a-findings.md", true},
		{"docs/plans/**/*findings*.md", "docs/plans/unrelated.md", false},
		{"docs/plans/**/*findings*.md", "docs/other/a-findings.md", false},
		{"docs/**", "docs/a/b.md", true},
	}
	for _, c := range cases {
		if got := globMatch(strings.Split(c.pat, "/"), strings.Split(c.name, "/")); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pat, c.name, got, c.want)
		}
	}
}

func TestImportTitleCleaning(t *testing.T) {
	long := strings.Repeat("word ", 40)
	cases := []struct{ cell, title string }{
		{"[BL441](x.md) An Automaton must not sit: watchdog. Second sentence.", "An Automaton must not sit: watchdog"},
		{"**BL462** A running task's session", "A running task's session"},
		{"Plain `code` title", "Plain code title"},
		{"GH12: Leading colon id", "Leading colon id"},
	}
	for _, c := range cases {
		if got, _ := splitTitle(c.cell); got != c.title {
			t.Errorf("splitTitle(%q) = %q, want %q", c.cell, got, c.title)
		}
	}
	title, rest := splitTitle(long)
	if utf8Len(title) > titleCutRunes || rest == "" {
		t.Errorf("long title not cut: %d runes, rest %q", utf8Len(title), rest)
	}
	if got, _ := splitTitle("**BL5**"); got != "" {
		t.Errorf("a bare id is not a title, got %q", got)
	}
}

func TestImportStatusWords(t *testing.T) {
	cases := map[string]State{
		"open": StateOpen, "planned": StatePlanned, "in progress": StateInProgress, "in review": StateOpen,
		"**done 2026-10-18**": StateDone, "shipped in 9.2.0": StateShipped, "blocked (PRD x)": StateBlocked,
		"paused": StateDeferred, "frozen": StateDeferred, "deferred": StateDeferred, "partial": StateInProgress,
		"needs_review (design done)": StateOpen,
	}
	for in, want := range cases {
		if got, _ := parseState(in); got != want {
			t.Errorf("parseState(%q) = %s, want %s", in, got, want)
		}
	}
	if _, known := parseState("wobbly"); known {
		t.Error("wobbly should not be a known status")
	}
}
