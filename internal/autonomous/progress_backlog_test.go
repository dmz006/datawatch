package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const backlogFixture = "testdata/progress/backlog.md"

func loadFixtureRows(t *testing.T) map[string]BacklogRow {
	t.Helper()
	rows, err := LoadBacklog(backlogFixture)
	if err != nil {
		t.Fatal(err)
	}
	byNum := map[string]BacklogRow{}
	for _, r := range rows {
		byNum[r.Number] = r
	}
	return byNum
}

func TestProgressBacklogFixtureRowSet(t *testing.T) {
	rows, err := LoadBacklog(backlogFixture)
	if err != nil {
		t.Fatal(err)
	}
	var nums []string
	for _, r := range rows {
		nums = append(nums, r.Number)
	}
	want := []string{"1", "2", "3", "4", "5", "6", "7", "8", "12a", "13", "14", "15"}
	if !reflect.DeepEqual(nums, want) {
		t.Errorf("row numbers = %v, want %v (header, separator, blank, prose, short and non-numbered rows skipped)", nums, want)
	}
}

func TestProgressBacklogClassification(t *testing.T) {
	rows := loadFixtureRows(t)
	cases := []struct {
		num     string
		open    bool
		unfiled bool
		ids     []string
	}{
		{"1", true, false, []string{"aaaa1111"}},
		{"2", true, true, nil},
		{"3", false, false, nil},
		{"4", false, false, nil}, // deferred past the ten-zero release
		{"5", true, false, []string{"bbbb2222", "cccc3333"}},
		{"6", true, false, []string{"dddd4444"}},
		{"7", true, true, nil},
		{"8", true, true, nil},
		{"12a", true, false, []string{"eeee5555"}},
		{"13", true, false, []string{"ffff6666"}}, // completed, merge pending stays open
		{"14", true, true, nil},
		{"15", false, false, nil},
	}
	for _, c := range cases {
		r, ok := rows[c.num]
		if !ok {
			t.Errorf("row %s missing", c.num)
			continue
		}
		if r.Open != c.open || r.Unfiled != c.unfiled {
			t.Errorf("row %s: open=%v unfiled=%v, want open=%v unfiled=%v", c.num, r.Open, r.Unfiled, c.open, c.unfiled)
		}
		if !reflect.DeepEqual(r.PRDIDs, c.ids) {
			t.Errorf("row %s: ids = %v, want %v", c.num, r.PRDIDs, c.ids)
		}
	}
}

func TestProgressBacklogMarkers(t *testing.T) {
	rows := loadFixtureRows(t)
	cases := []struct {
		num    string
		item   string
		kind   string
		phases int
		id     string
	}{
		{"1", "[AB101](plans/alpha.md) Alpha import pipeline", "feature", 1, "AB101"},
		{"5", "[AB105](plans/epsilon.md) Epsilon rollout", "feature", 3, "AB105"},
		{"6", "[AB106](plans/zeta.md) Zeta cleanup", "bug", 1, "AB106"},
		{"7", "Fix flaky retry in the sender", "bug", 1, ""},
		{"8", "Eta dashboard", "other", 1, ""},
		{"12a", "[AB112](plans/theta.md) Theta follow-up", "feature", 1, "AB112"},
		{"14", "[AB114](plans/kappa.md) Kappa review", "feature", 1, "AB114"}, // invalid kind marker ignored
	}
	for _, c := range cases {
		r := rows[c.num]
		if r.Item != c.item {
			t.Errorf("row %s: item = %q, want %q", c.num, r.Item, c.item)
		}
		if strings.Contains(r.Item, "{") {
			t.Errorf("row %s: markers not stripped from %q", c.num, r.Item)
		}
		if r.Kind != c.kind || r.Phases != c.phases || r.ID != c.id {
			t.Errorf("row %s: kind=%q phases=%d id=%q, want kind=%q phases=%d id=%q",
				c.num, r.Kind, r.Phases, r.ID, c.kind, c.phases, c.id)
		}
	}
	if rows["2"].Status != "not started" {
		t.Errorf("row 2 status = %q", rows["2"].Status)
	}
}

func TestProgressBacklogMarkerParsing(t *testing.T) {
	cases := []struct {
		in     string
		clean  string
		kind   string
		phases int
	}{
		{"Plain item", "Plain item", "", 1},
		{"Item {phases=10}", "Item", "", 10},
		{"Item {kind=bug}", "Item", "bug", 1},
		{"Item {kind=BUG} {phases=2}", "Item", "bug", 2},
		{"{kind=other} Item", "Item", "other", 1},
		{"Item {phases=0}", "Item", "", 1},
		{"Item {phases=x}", "Item", "", 1},
		{"Item {phases=99999999999999999999}", "Item", "", 1},
		{"Item {kind=chore}", "Item", "", 1},
		{"Item {kind=}", "Item", "", 1},
		{"Item {phases=3}: tail", "Item: tail", "", 3},
		{"Item {kind=feature}{phases=4}", "Item", "feature", 4},
	}
	for _, c := range cases {
		clean, kind, phases := splitBacklogMarkers(c.in)
		if clean != c.clean || kind != c.kind || phases != c.phases {
			t.Errorf("splitBacklogMarkers(%q) = (%q, %q, %d), want (%q, %q, %d)",
				c.in, clean, kind, phases, c.clean, c.kind, c.phases)
		}
	}
}

func TestProgressBacklogStatusRules(t *testing.T) {
	cases := []struct {
		name   string
		status string
		cells  string
		open   bool
	}{
		{"not started", "not started", "", true},
		{"not filed", "Not Filed yet", "", true},
		{"draft", "draft", "", true},
		{"running", "running", "", true},
		{"blocked", "blocked on x", "", true},
		{"needs design", "needs design", "", true},
		{"done bold", "**done**", "", false},
		{"done word", "all done", "", false},
		{"done is word bound", "abandoned, not started", "", true},
		{"shipped", "shipped; running again", "", false},
		{"superseded", "superseded", "", false},
		{"closed", "closed", "", false},
		{"completed", "completed", "", false},
		{"completed merge pending", "completed, merge pending", "", true},
		{"completed then more", "completed, merge pending; later completed", "", true}, // merge pending anywhere keeps it open
		{"completed twice, second plain", "completed, merge pending", "", true},
		{"unrecognised", "waiting for a decision", "", false},
		{"deferred in item", "not started", "deferred item", false},
		{"post ten in status", "not started after 10.0.0", "", false},
		{"post ten phrase", "not started", "post 10.0 work", false},
	}
	for _, c := range cases {
		line := "| 1 | Item " + c.cells + " | " + c.status + " |"
		r, ok := parseBacklogLine(line)
		if !ok {
			t.Errorf("%s: row not parsed", c.name)
			continue
		}
		if r.Open != c.open {
			t.Errorf("%s: open = %v, want %v", c.name, r.Open, c.open)
		}
	}
}

func TestProgressBacklogDoneLookahead(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"completed", true},
		{"Completed.", true},
		{"completed, merge pending", false},
		{"COMPLETED, MERGE PENDING", false},
		{"completed, merge pending then completed", true},
		{"nothing here", false},
		{"", false},
	}
	for _, c := range cases {
		if got := statusIsDone(c.s); got != c.want {
			t.Errorf("statusIsDone(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestProgressBacklogWindows(t *testing.T) {
	// The done check looks only at the first 140 characters of the status.
	farDone := strings.Repeat("x", 150) + " shipped running"
	r, _ := parseBacklogLine("| 1 | Item | " + farDone + " |")
	if !r.Open {
		t.Errorf("done word past 140 characters should not close the row")
	}
	nearDone := "shipped " + strings.Repeat("x", 150) + " running"
	r, _ = parseBacklogLine("| 1 | Item | " + nearDone + " |")
	if r.Open {
		t.Errorf("done word inside 140 characters closes the row")
	}
	// The open check looks only at the first 200 characters.
	farOpen := strings.Repeat("é", 201) + " running"
	r, _ = parseBacklogLine("| 1 | Item | " + farOpen + " |")
	if r.Open {
		t.Errorf("open word past 200 characters should not open the row")
	}
	nearOpen := strings.Repeat("é", 190) + " running"
	r, _ = parseBacklogLine("| 1 | Item | " + nearOpen + " |")
	if !r.Open {
		t.Errorf("open word inside 200 characters opens the row")
	}
}

func TestProgressBacklogLinkedIDs(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"| 1 | Item `aaaa1111` | running |", []string{"aaaa1111"}},
		{"| 1 | Item | running `aaaa1111` and `bbbb2222` |", []string{"aaaa1111", "bbbb2222"}},
		{"| 1 | Item `aaaa1111` | running `bbbb2222` `aaaa1111` | `cccc3333` |", []string{"aaaa1111", "bbbb2222", "cccc3333"}},
		{"| 1 | Item | running aaaa1111 |", nil},    // not backticked
		{"| 1 | Item | running `AAAA1111` |", nil},  // upper case is not an id
		{"| 1 | Item | running `aaaa111` |", nil},   // too short
		{"| 1 | Item | running `aaaa11111` |", nil}, // too long
		{"| 1 | Item | running `gggg1111` |", nil},  // not hex
		{"| 1 | Item | running PRD `abcdef01` |", []string{"abcdef01"}},
	}
	for _, c := range cases {
		r, ok := parseBacklogLine(c.line)
		if !ok {
			t.Errorf("%q not parsed", c.line)
			continue
		}
		if !reflect.DeepEqual(r.PRDIDs, c.want) {
			t.Errorf("%q: ids = %v, want %v", c.line, r.PRDIDs, c.want)
		}
	}
}

func TestProgressBacklogSkippedLines(t *testing.T) {
	skipped := []string{
		"",
		"plain prose",
		"| # | Item | Status |",
		"|---|------|--------|",
		"| x | letter only | running |",
		"| 12ab | two letters | running |",
		"| 1 |",
		"| 1 | only item |",
		"|1| no spaces | running |",
		"  | 1 | indented | running |",
		"## | 1 | heading | running |",
	}
	for _, l := range skipped {
		if r, ok := parseBacklogLine(l); ok {
			t.Errorf("%q should be skipped, parsed as %+v", l, r)
		}
	}
	// A trailing carriage return does not break a row.
	if r, ok := parseBacklogLine("| 3 | Item | running |\r"); !ok || r.Status != "running" {
		t.Errorf("CRLF row: ok=%v status=%q", ok, r.Status)
	}
	// An empty reader and a prose-only reader give no rows.
	if rows := ParseBacklog(strings.NewReader("")); len(rows) != 0 {
		t.Errorf("empty input gave %d rows", len(rows))
	}
	if rows := ParseBacklog(strings.NewReader("just prose\nmore prose\n")); len(rows) != 0 {
		t.Errorf("prose gave %d rows", len(rows))
	}
}

func TestProgressBacklogUnfiledAndRowKinds(t *testing.T) {
	rows, err := LoadBacklog(backlogFixture)
	if err != nil {
		t.Fatal(err)
	}
	var unfiled []string
	for _, r := range UnfiledRows(rows) {
		unfiled = append(unfiled, r.Number)
	}
	if want := []string{"2", "7", "8", "14"}; !reflect.DeepEqual(unfiled, want) {
		t.Errorf("unfiled rows = %v, want %v", unfiled, want)
	}
	if got := UnfiledRows(nil); len(got) != 0 {
		t.Errorf("UnfiledRows(nil) = %v", got)
	}

	kinds := RowKinds(rows)
	want := map[string]string{
		"aaaa1111": "feature",
		"bbbb2222": "feature",
		"cccc3333": "feature",
		"dddd4444": "bug",
		"eeee5555": "feature",
		"ffff6666": "feature",
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("RowKinds = %v, want %v", kinds, want)
	}

	// The first row that lists an id decides its kind.
	dup := []BacklogRow{
		{Number: "1", Kind: "bug", PRDIDs: []string{"aaaa1111"}},
		{Number: "2", Kind: "other", PRDIDs: []string{"aaaa1111", "bbbb2222"}},
	}
	if got := RowKinds(dup); got["aaaa1111"] != "bug" || got["bbbb2222"] != "other" {
		t.Errorf("RowKinds duplicates = %v", got)
	}
	if got := RowKinds(nil); len(got) != 0 {
		t.Errorf("RowKinds(nil) = %v", got)
	}

	// The row kind reaches EffectiveKind for a record without its own kind.
	p := &PRD{ID: "dddd4444", Title: "Add a screen"}
	if got := EffectiveKind(p, kinds); got != PRDKindBug {
		t.Errorf("EffectiveKind through the row = %q, want bug", got)
	}
}

func TestProgressBacklogUnfiledComputedFromFields(t *testing.T) {
	rows := []BacklogRow{
		{Number: "1", Open: true},
		{Number: "2", Open: true, PRDIDs: []string{"aaaa1111"}},
		{Number: "3", Open: false},
	}
	got := UnfiledRows(rows)
	if len(got) != 1 || got[0].Number != "1" {
		t.Errorf("UnfiledRows = %+v, want only row 1", got)
	}
}

func TestProgressBacklogLoadMissingFile(t *testing.T) {
	if _, err := LoadBacklog(filepath.Join(t.TempDir(), "absent.md")); err == nil {
		t.Error("LoadBacklog on a missing file should fail")
	}
	path := filepath.Join(t.TempDir(), "r.md")
	if err := os.WriteFile(path, []byte("| 1 | Item | not started |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadBacklog(path)
	if err != nil || len(rows) != 1 || !rows[0].Unfiled {
		t.Errorf("LoadBacklog = %+v, %v", rows, err)
	}
}

func TestProgressBacklogJSONShape(t *testing.T) {
	r, ok := parseBacklogLine("| 7 | [AB107](x.md) Item {phases=2} | running `aaaa1111` |")
	if !ok {
		t.Fatal("row not parsed")
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"number", "id", "item", "status", "kind", "prd_ids", "phases", "open", "unfiled"} {
		if _, present := m[key]; !present {
			t.Errorf("json key %q missing from %s", key, data)
		}
	}
	var back BacklogRow
	if err := json.Unmarshal(data, &back); err != nil || !reflect.DeepEqual(back, r) {
		t.Errorf("round trip = %+v (%v), want %+v", back, err, r)
	}
}

func TestProgressBacklogSharedRowsInRegistry(t *testing.T) {
	// The two multi-phase registry rows carry their phase counts as markers, so
	// the engine no longer needs to know them by name. The test reads a literal
	// copy of the row shape, not the live registry.
	lines := "| 39 | [AB139](p.md) LLM enhancements, phases 0-10 {phases=10} | audited; remaining: Phase 0 | 10 |\n" +
		"| 40 | Security remediation {phases=3}: AB140, AB141 (detail) | fully decided; no PRDs yet | - |\n"
	rows := ParseBacklog(strings.NewReader(lines))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Phases != 10 || rows[1].Phases != 3 {
		t.Errorf("phases = %d, %d; want 10, 3", rows[0].Phases, rows[1].Phases)
	}
	if !strings.HasPrefix(rows[1].Item, "Security remediation:") {
		t.Errorf("security item = %q", rows[1].Item)
	}
}
