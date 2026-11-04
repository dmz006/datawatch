package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WriteGateCountsReport writes the gate's counts-only report: exactly three
// lines, passed=<n>, failed=<n>, skipped=<n>, in that order, with a newline
// after each and no other text. reportPath must be a legal report path (see
// validateReportPath), and no count may be negative. The parent directory is
// created with mode 0o755 and the file with mode 0o644.
func WriteGateCountsReport(root, reportPath string, passed, failed, skipped int) error {
	if reportPath == "" {
		return fmt.Errorf("gate report path must not be empty")
	}
	if err := validateReportPath(reportPath); err != nil {
		return err
	}
	if passed < 0 {
		return fmt.Errorf("passed count %d must not be negative", passed)
	}
	if failed < 0 {
		return fmt.Errorf("failed count %d must not be negative", failed)
	}
	if skipped < 0 {
		return fmt.Errorf("skipped count %d must not be negative", skipped)
	}
	full := filepath.Join(root, reportPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	content := "passed=" + strconv.Itoa(passed) +
		"\nfailed=" + strconv.Itoa(failed) +
		"\nskipped=" + strconv.Itoa(skipped) + "\n"
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return err
	}
	return nil
}

// ParseGateCountsReport parses the exact three-line gate counts report
// format written by WriteGateCountsReport and rejects anything else: a
// different line count, a misordered or malformed line, or any extra text.
func ParseGateCountsReport(data []byte) (passed, failed, skipped int, err error) {
	text := string(data)
	want := []string{"passed=", "failed=", "skipped="}
	lines := strings.Split(text, "\n")
	// Exactly three lines each terminated by a newline: four fields after
	// splitting, the last one empty.
	if len(lines) != 4 || lines[3] != "" {
		return 0, 0, 0, fmt.Errorf("gate report must be exactly three newline-terminated lines, got %d line(s)", len(lines)-1)
	}
	var counts [3]int
	for i := 0; i < 3; i++ {
		rest, ok := strings.CutPrefix(lines[i], want[i])
		if !ok {
			return 0, 0, 0, fmt.Errorf("gate report line %d must start with %q", i+1, want[i])
		}
		v, perr := strconv.Atoi(rest)
		if perr != nil {
			return 0, 0, 0, fmt.Errorf("gate report line %d has a non-integer count %q", i+1, rest)
		}
		if v < 0 {
			return 0, 0, 0, fmt.Errorf("gate report line %d has a negative count %d", i+1, v)
		}
		counts[i] = v
	}
	return counts[0], counts[1], counts[2], nil
}
