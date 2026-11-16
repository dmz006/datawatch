package autonomous

import (
	"path"
	"regexp"
	"strings"
)

// isoCalendarDate matches a calendar date written as year-month-day.
var isoCalendarDate = regexp.MustCompile(`\b\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])\b`)

// testWordRe matches the word "test" (or tests) in a title.
var testWordRe = regexp.MustCompile(`(?i)\btests?\b`)

// isTestWritingTask reports whether the task writes a test: a test file
// in its files list, or a title starting with Add or Write that names a test.
func isTestWritingTask(t Task) bool {
	for _, f := range t.FilesPlanned {
		f = strings.TrimSpace(f)
		base := path.Base(strings.ReplaceAll(f, "\\", "/"))
		switch {
		case strings.HasSuffix(f, "_test.go"),
			strings.HasSuffix(f, ".test.js"),
			strings.HasSuffix(f, ".spec.js"),
			strings.HasSuffix(f, ".spec.ts"),
			strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
			return true
		}
	}
	title := strings.TrimSpace(t.Title)
	if strings.HasPrefix(title, "Add") || strings.HasPrefix(title, "Write") {
		return testWordRe.MatchString(title)
	}
	return false
}

// fixedCalendarDateInTest returns the first fixed ISO calendar date in a
// test-writing task's spec.
func fixedCalendarDateInTest(t Task) (string, bool) {
	if !isTestWritingTask(t) {
		return "", false
	}
	d := isoCalendarDate.FindString(t.Spec)
	return d, d != ""
}
