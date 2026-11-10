package server

// TestStandardTaskNaming guards the operator-facing name of the built-in task
// definitions. They are called "standard tasks"; the old name "template task"
// (and "task template") must not reappear in locale strings, how-to docs or the
// definitions glossary unless the same line says what the old name was.
// It also keeps the internal term "PRD" out of locale strings.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// standardTaskOldNameRe matches the retired name, plural forms included.
var standardTaskOldNameRe = regexp.MustCompile(`(?i)\b(?:template tasks?|task templates?)\b`)

// standardTaskOldNameMarkerRe matches a word that marks the old name as historical.
var standardTaskOldNameMarkerRe = regexp.MustCompile(`(?i)\b(?:formerly|previously|renamed|was called)\b`)

// prdTermRe matches the internal acronym PRD as a whole word (case-sensitive).
var prdTermRe = regexp.MustCompile(`\bPRD\b`)

// standardTaskRepoRoot walks up from the test's working directory until it
// finds go.mod, and returns that directory.
func standardTaskRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root (go.mod) not found above working directory")
		}
		dir = parent
	}
}

// standardTaskCollectStrings returns every string value found in a decoded JSON document.
func standardTaskCollectStrings(v interface{}, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []interface{}:
		for _, e := range x {
			standardTaskCollectStrings(e, out)
		}
	case map[string]interface{}:
		for _, e := range x {
			standardTaskCollectStrings(e, out)
		}
	}
}

// standardTaskCheckLine reports the old-name violation on one line, if any.
func standardTaskCheckLine(line string) bool {
	return standardTaskOldNameRe.MatchString(line) && !standardTaskOldNameMarkerRe.MatchString(line)
}

func TestStandardTaskNaming(t *testing.T) {
	root := standardTaskRepoRoot(t)

	var failures []string

	// 1. Locale bundles: old name and PRD must not appear in any string value.
	localeDir := filepath.Join(root, "internal", "server", "web", "locales")
	localeFiles, err := filepath.Glob(filepath.Join(localeDir, "*.json"))
	if err != nil {
		t.Fatalf("glob locales: %v", err)
	}
	if len(localeFiles) == 0 {
		t.Fatalf("no locale bundles found under %s", localeDir)
	}
	for _, path := range localeFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc interface{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		var strs []string
		standardTaskCollectStrings(doc, &strs)
		rel, _ := filepath.Rel(root, path)
		for _, s := range strs {
			if standardTaskCheckLine(s) {
				failures = append(failures, rel+": old task name without a historical marker: "+s)
			}
			if prdTermRe.MatchString(s) {
				failures = append(failures, rel+": internal term PRD in locale string: "+s)
			}
		}
	}

	// 2. How-to docs and the definitions glossary: check line by line.
	var textFiles []string
	howtoDir := filepath.Join(root, "docs", "howto")
	entries, err := os.ReadDir(howtoDir)
	if err != nil {
		t.Fatalf("read %s: %v", howtoDir, err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			textFiles = append(textFiles, filepath.Join(howtoDir, e.Name()))
		}
	}
	textFiles = append(textFiles, filepath.Join(root, "docs", "datawatch-definitions.md"))

	for _, path := range textFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(raw), "\n") {
			if standardTaskCheckLine(line) {
				failures = append(failures, rel+":"+strconv.Itoa(i+1)+": old task name without a historical marker: "+strings.TrimSpace(line))
			}
		}
	}

	if len(failures) > 0 {
		t.Errorf("standard task naming violations (%d):\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
}
