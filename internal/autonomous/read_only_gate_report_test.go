package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteGateCountsReport_WritesExactlyThreeLines(t *testing.T) {
	root := t.TempDir()
	p := ".datawatch/reports/gates/counts.txt"
	if err := WriteGateCountsReport(root, p, 7, 2, 3); err != nil {
		t.Fatalf("WriteGateCountsReport: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	want := "passed=7\nfailed=2\nskipped=3\n"
	if string(got) != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func TestWriteGateCountsReport_OutsidePathRejected(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		"reports/counts.txt",
		".datawatch/other/counts.txt",
		"/etc/passwd",
		".datawatch/reports/../../escape.txt",
	} {
		if err := WriteGateCountsReport(root, p, 1, 0, 0); err == nil {
			t.Errorf("WriteGateCountsReport(%q): expected error, got nil", p)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.Clean(p))); !os.IsNotExist(err) {
			// Only meaningful for in-root candidates; the /etc one resolves
			// outside root where stat may legitimately exist.
			if strings.HasPrefix(p, ".datawatch/") || strings.HasPrefix(p, "reports/") {
				t.Errorf("no error for %q but file exists at %s", p, filepath.Join(root, filepath.Clean(p)))
			}
		}
	}
	// Nothing under .datawatch/reports/ may have been created.
	if entries, _ := os.ReadDir(filepath.Join(root, ".datawatch/reports")); len(entries) != 0 {
		t.Fatalf("expected no entries under .datawatch/reports, got %v", entries)
	}
}

func TestWriteGateCountsReport_EmptyPathRejected(t *testing.T) {
	root := t.TempDir()
	if err := WriteGateCountsReport(root, "", 1, 0, 0); err == nil {
		t.Fatal("expected error for empty report path")
	}
}

func TestWriteGateCountsReport_NegativeCountsRejected(t *testing.T) {
	root := t.TempDir()
	p := ".datawatch/reports/gates/counts.txt"
	cases := []struct {
		name   string
		passed int
		failed int
		skip   int
	}{
		{"negative passed", -1, 0, 0},
		{"negative failed", 0, -1, 0},
		{"negative skipped", 0, 0, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := WriteGateCountsReport(root, p, c.passed, c.failed, c.skip); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
			if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
				t.Fatalf("file must not be written for %s", c.name)
			}
		})
	}
}

func TestGateCountsReport_RoundTrip(t *testing.T) {
	root := t.TempDir()
	p := ".datawatch/reports/sub/dir/counts.txt"
	if err := WriteGateCountsReport(root, p, 12, 0, 4); err != nil {
		t.Fatalf("WriteGateCountsReport: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	passed, failed, skipped, err := ParseGateCountsReport(data)
	if err != nil {
		t.Fatalf("ParseGateCountsReport: %v", err)
	}
	if passed != 12 || failed != 0 || skipped != 4 {
		t.Fatalf("got passed=%d failed=%d skipped=%d, want 12 0 4", passed, failed, skipped)
	}
}

func TestParseGateCountsReport_RejectsBadFormats(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"missing line", "passed=1\nfailed=2\n"},
		{"extra text", "passed=1\nfailed=2\nskipped=3\nok\n"},
		{"extra text on one line", "passed=1\nfailed=2 all good\nskipped=3\n"},
		{"wrong order", "failed=1\npassed=2\nskipped=3\n"},
		{"label typo", "pass=1\nfailed=2\nskipped=3\n"},
		{"non-integer", "passed=x\nfailed=2\nskipped=3\n"},
		{"no trailing newline", "passed=1\nfailed=2\nskipped=3"},
		{"blank line in the middle", "passed=1\n\nskipped=3"},
		{"negative", "passed=1\nfailed=2\nskipped=-3\n"},
		{"empty", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, _, err := ParseGateCountsReport([]byte(c.data)); err == nil {
				t.Fatalf("ParseGateCountsReport(%q): expected error, got nil", c.data)
			}
		})
	}
}
