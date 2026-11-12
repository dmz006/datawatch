package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func srcMapping(pattern string) PendingItemSourceMapping {
	return PendingItemSourceMapping{
		IDColumn:    "1",
		TitleColumn: "2",
		IDPattern:   regexp.MustCompile(pattern),
		DoneValues:  []string{"done", "closed", "shipped"},
	}
}

func TestPendingItemSourceParseDefaultPattern(t *testing.T) {
	content := "# Registry\n\n" +
		"| id | title |\n" +
		"|---|:--:|\n" +
		"| alpha-1 | First thing |\n" +
		"| 42 | Second thing |\n" +
		"|  | blank id |\n" +
		"\ntrailing prose\n"
	rows, skipped, err := ParsePendingItemTable(content, srcMapping(`^\S+$`))
	if err != nil {
		t.Fatal(err)
	}
	want := []PendingItemSourceRow{
		{Ref: "alpha-1", Title: "First thing", Line: 5},
		{Ref: "42", Title: "Second thing", Line: 6},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0 (a blank id is not counted)", skipped)
	}
}

func TestPendingItemSourceParseDifferentIDPattern(t *testing.T) {
	content := "| ref | name |\n|---|---|\n" +
		"| TICKET-12 | one |\n" +
		"| ab123 | two |\n" +
		"| other | three |\n" +
		"| TICKET-x | four |\n"
	rows, skipped, err := ParsePendingItemTable(content, srcMapping(`^TICKET-[0-9]+$`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Ref != "TICKET-12" || skipped != 3 {
		t.Fatalf("pattern 1: rows=%+v skipped=%d", rows, skipped)
	}
	rows, skipped, err = ParsePendingItemTable(content, srcMapping(`^[a-z]{2}[0-9]{3}$`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Ref != "ab123" || rows[0].Title != "two" || skipped != 3 {
		t.Fatalf("pattern 2: rows=%+v skipped=%d", rows, skipped)
	}
}

func TestPendingItemSourceParseColumnByName(t *testing.T) {
	content := "| Owner | Work Item | Key | Status |\n|---|---|---|---|\n" +
		"| me | Do the thing | k-1 | Open |\n" +
		"| you | Other thing | k-2 | DONE |\n"
	m := srcMapping(`^\S+$`)
	m.IDColumn, m.TitleColumn, m.StateColumn = "key", "WORK ITEM", "status"
	rows, _, err := ParsePendingItemTable(content, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Ref != "k-1" || rows[0].Title != "Do the thing" || rows[0].State != "Open" || rows[0].Done {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].Ref != "k-2" || !rows[1].Done {
		t.Fatalf("row 1 = %+v", rows[1])
	}
	// An unknown header name leaves the table unusable.
	m.IDColumn = "nope"
	if _, _, err := ParsePendingItemTable(content, m); !errors.Is(err, ErrPendingItemNoTable) {
		t.Fatalf("err = %v, want ErrPendingItemNoTable", err)
	}
}

func TestPendingItemSourceParseColumnByNumber(t *testing.T) {
	content := "| a | b | c | d |\n|---|---|---|---|\n" +
		"| x | y | w1 | closed |\n" +
		"| x | y | w2 | todo |\n"
	m := srcMapping(`^w[0-9]$`)
	m.IDColumn, m.TitleColumn, m.StateColumn = "3", "1", "4"
	rows, skipped, err := ParsePendingItemTable(content, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || skipped != 0 {
		t.Fatalf("rows=%+v skipped=%d", rows, skipped)
	}
	if rows[0].Ref != "w1" || rows[0].Title != "x" || !rows[0].Done || rows[1].Done {
		t.Fatalf("rows = %+v", rows)
	}
	// A number past the header width does not resolve.
	m.TitleColumn = "9"
	if _, _, err := ParsePendingItemTable(content, m); !errors.Is(err, ErrPendingItemNoTable) {
		t.Fatalf("err = %v, want ErrPendingItemNoTable", err)
	}
	// An unresolvable state column means no row is done, not an error.
	m.TitleColumn, m.StateColumn = "2", "missing"
	rows, _, err = ParsePendingItemTable(content, m)
	if err != nil || len(rows) != 2 || rows[0].Done || rows[0].State != "" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	// Empty id and title settings fall back to columns 1 and 2.
	m = PendingItemSourceMapping{IDPattern: regexp.MustCompile(`^x$`)}
	rows, _, err = ParsePendingItemTable(content, m)
	if err != nil || len(rows) != 2 || rows[0].Title != "y" {
		t.Fatalf("defaults: rows=%+v err=%v", rows, err)
	}
}

func TestPendingItemSourceParseDoneValues(t *testing.T) {
	content := "| id | title | state |\n|---|---|---|\n" +
		"| a | A | Done |\n" +
		"| b | B | **shipped** |\n" +
		"| c | C |  CLOSED  |\n" +
		"| d | D | in progress |\n" +
		"| e | E |  |\n"
	m := srcMapping(`^\S+$`)
	m.StateColumn = "3"
	rows, _, err := ParsePendingItemTable(content, m)
	if err != nil {
		t.Fatal(err)
	}
	got := []bool{}
	for _, r := range rows {
		got = append(got, r.Done)
	}
	if want := []bool{true, true, true, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("done = %v, want %v", got, want)
	}
	// Custom done values replace the list.
	m.DoneValues = []string{"in progress"}
	rows, _, _ = ParsePendingItemTable(content, m)
	if rows[0].Done || !rows[3].Done {
		t.Fatalf("custom done values: %+v", rows)
	}
	// No state column: no row is ever done.
	m = srcMapping(`^\S+$`)
	rows, _, _ = ParsePendingItemTable(content, m)
	for _, r := range rows {
		if r.Done || r.State != "" {
			t.Fatalf("row done without a state column: %+v", r)
		}
	}
}

func TestPendingItemSourceParseNoTable(t *testing.T) {
	for _, content := range []string{
		"",
		"just prose\nwith a | pipe in it\n",
		"| a | b |\n| not | a separator |\n",
		"| a | b |\n|---|\n", // separator width differs from header
		"```\n| id | title |\n|---|---|\n| a | b |\n```\n",
	} {
		rows, skipped, err := ParsePendingItemTable(content, srcMapping(`^\S+$`))
		if !errors.Is(err, ErrPendingItemNoTable) || len(rows) != 0 || skipped != 0 {
			t.Fatalf("content %q: rows=%+v skipped=%d err=%v", content, rows, skipped, err)
		}
	}
}

func TestPendingItemSourceParseSkipsNonMatching(t *testing.T) {
	content := "| id | title |\n|---|---|\n" +
		"| ok-1 | fine |\n" +
		"| has space | skipped |\n" +
		"| ok-2 |  |\n" + // empty title
		"| ok-3 | fine too |\n" +
		"\n" +
		"| id | other |\n|---|---|\n" +
		"| ok-4 | second table |\n"
	rows, skipped, err := ParsePendingItemTable(content, srcMapping(`^ok-[0-9]$`))
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, r := range rows {
		refs = append(refs, r.Ref)
	}
	if want := []string{"ok-1", "ok-3", "ok-4"}; !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2", skipped)
	}
}

func TestPendingItemSourceParseInvalidPattern(t *testing.T) {
	m := srcMapping(`^\S+$`)
	m.IDPattern = nil
	rows, skipped, err := ParsePendingItemTable("| a | b |\n|---|---|\n| x | y |\n", m)
	if err == nil || errors.Is(err, ErrPendingItemNoTable) || len(rows) != 0 || skipped != 0 {
		t.Fatalf("rows=%+v skipped=%d err=%v", rows, skipped, err)
	}
}

func TestPendingItemSourceParseEscapedPipe(t *testing.T) {
	content := "| id | title |\r\n|---|---|\r\n" +
		"| `a-1` | left \\| right |\r\n" +
		"| [b-2](https://example.invalid/x) | _**bold title**_ |\r\n" +
		"c-3 | no outer pipes\r\n" +
		"| d-4 | [link text](u) tail |\r\n"
	rows, _, err := ParsePendingItemTable(content, srcMapping(`^\S+$`))
	if err != nil {
		t.Fatal(err)
	}
	want := []PendingItemSourceRow{
		{Ref: "a-1", Title: "left | right", Line: 3},
		{Ref: "b-2", Title: "bold title", Line: 4},
		{Ref: "c-3", Title: "no outer pipes", Line: 5},
		{Ref: "d-4", Title: "link text tail", Line: 6},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
}

func TestPendingItemSourceReadUnreadable(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	write := func(dir, name string, size int) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Repeat("a", size)), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	reg := write(base, "registry-file", 0)
	if err := os.WriteFile(reg, []byte("| a | b |\n|---|---|\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Readable: relative and absolute.
	for _, p := range []string{"registry-file", "./registry-file", reg} {
		got, err := ReadPendingItemSourceFile(base, p)
		if err != nil || !strings.HasPrefix(got, "| a | b |") {
			t.Fatalf("read %q: %q %v", p, got, err)
		}
	}
	// A path inside a subdirectory and a link that stays inside are fine.
	if err := os.Mkdir(filepath.Join(base, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(base, "sub"), "inner", 3)
	if err := os.Symlink(filepath.Join(base, "sub", "inner"), filepath.Join(base, "inside-link")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"sub/inner", "inside-link", "sub/../registry-file"} {
		if _, err := ReadPendingItemSourceFile(base, p); err != nil {
			t.Fatalf("read %q: %v", p, err)
		}
	}

	// Missing file.
	if _, err := ReadPendingItemSourceFile(base, "absent"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing relative: %v", err)
	}
	if _, err := ReadPendingItemSourceFile(base, filepath.Join(base, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing absolute: %v", err)
	}
	// Directory.
	if _, err := ReadPendingItemSourceFile(base, "sub"); !errors.Is(err, ErrPendingItemSourceNotRegular) {
		t.Fatalf("directory relative: %v", err)
	}
	if _, err := ReadPendingItemSourceFile(base, base); !errors.Is(err, ErrPendingItemSourceNotRegular) {
		t.Fatalf("directory absolute: %v", err)
	}
	// Too large: exactly the limit is read, one byte more is refused.
	exact := write(base, "exact", PendingItemSourceMaxBytes)
	if got, err := ReadPendingItemSourceFile(base, exact); err != nil || len(got) != PendingItemSourceMaxBytes {
		t.Fatalf("exact limit: len=%d err=%v", len(got), err)
	}
	big := write(base, "big", PendingItemSourceMaxBytes+1)
	if _, err := ReadPendingItemSourceFile(base, "big"); !errors.Is(err, ErrPendingItemSourceTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	if _, err := ReadPendingItemSourceFile(base, big); !errors.Is(err, ErrPendingItemSourceTooLarge) {
		t.Fatalf("too large absolute: %v", err)
	}
	// Relative escape by .. and by a symbolic link.
	secret := write(outside, "secret", 4)
	rel, err := filepath.Rel(base, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingItemSourceFile(base, rel); !errors.Is(err, ErrPendingItemSourceEscapes) {
		t.Fatalf("dot-dot escape: %v", err)
	}
	if _, err := ReadPendingItemSourceFile(base, "sub/../../x"); !errors.Is(err, ErrPendingItemSourceEscapes) {
		t.Fatalf("dot-dot escape 2: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(base, "escape-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingItemSourceFile(base, "escape-link"); !errors.Is(err, ErrPendingItemSourceEscapes) {
		t.Fatalf("symlink escape: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "dir-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingItemSourceFile(base, "dir-link/secret"); !errors.Is(err, ErrPendingItemSourceEscapes) {
		t.Fatalf("symlinked directory escape: %v", err)
	}
	// Empty path, relative path without a base.
	if _, err := ReadPendingItemSourceFile(base, "  "); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := ReadPendingItemSourceFile("", "registry-file"); err == nil {
		t.Fatal("relative path with no base accepted")
	}
	if _, err := ReadPendingItemSourceFile(filepath.Join(base, "no-such-base"), "registry-file"); err == nil {
		t.Fatal("missing base accepted")
	}
}
