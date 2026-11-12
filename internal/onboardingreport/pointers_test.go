package onboardingreport

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func readAll(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		sb.Write(b)
	}
	return sb.String()
}

func slug(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.TrimLeft(h, "# ")))
	var sb strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			sb.WriteRune(r)
		case r == ' ':
			sb.WriteRune('-')
		}
	}
	return sb.String()
}

// Every pointer a next step names must resolve to something real.
func TestNextStepPointersResolve(t *testing.T) {
	root := repoRoot(t)
	cli := readAll(t, filepath.Join(root, "cmd", "datawatch"))
	mcp := readAll(t, filepath.Join(root, "internal", "mcp"))
	for _, d := range checkDefs() {
		s := d.Step
		if _, err := os.Stat(filepath.Join(root, s.HowTo)); err != nil {
			t.Errorf("%s: how-to %s missing: %v", d.ID, s.HowTo, err)
		}
		page, anchor, _ := strings.Cut(s.Anchor, "#")
		doc, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			t.Errorf("%s: definitions page %s missing: %v", d.ID, page, err)
		} else {
			found := false
			for _, line := range strings.Split(string(doc), "\n") {
				if strings.HasPrefix(line, "#") && slug(line) == anchor {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: anchor #%s not found in %s", d.ID, anchor, page)
			}
		}
		if s.MCPTool != "" && !strings.Contains(mcp, `"`+s.MCPTool+`"`) {
			t.Errorf("%s: MCP tool %s is not registered", d.ID, s.MCPTool)
		}
		if s.Command != "" {
			words := regexp.MustCompile(`^[a-z][a-z-]*$`)
			for _, w := range strings.Fields(strings.TrimPrefix(s.Command, "datawatch ")) {
				if !words.MatchString(w) {
					break // a placeholder or setting key: the arguments start here
				}
				if !strings.Contains(cli, `Use:   "`+w) && !strings.Contains(cli, `Use: "`+w) {
					t.Errorf("%s: command word %q in %q has no subcommand", d.ID, w, s.Command)
				}
			}
		}
	}
}
