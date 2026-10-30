package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

var settingsArgRe = regexp.MustCompile(`--settings '([^']*)'`)

// ancestorTree builds root/CLAUDE.md, root/a/CLAUDE.md and root/a/proj with
// its own CLAUDE.md plus a nested one, returning (root, proj, ancestors).
func ancestorTree(t *testing.T) (string, string, []string) {
	t.Helper()
	root := t.TempDir()
	proj := filepath.Join(root, "a", "proj")
	if err := os.MkdirAll(filepath.Join(proj, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	anc := []string{filepath.Join(root, "CLAUDE.md"), filepath.Join(root, "a", "CLAUDE.md")}
	for _, p := range append([]string{filepath.Join(proj, "CLAUDE.md"), filepath.Join(proj, "sub", "CLAUDE.md")}, anc...) {
		if err := os.WriteFile(p, []byte("Session: other\nTask: other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, proj, anc
}

// excludesIn parses the claudeMdExcludes list out of a recorded command and
// keeps only the entries living under root (the real parents of t.TempDir()
// may legitimately hold CLAUDE.md files too).
func excludesIn(t *testing.T, cmd, root string) []string {
	t.Helper()
	m := settingsArgRe.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("no --settings '<json>' in command: %s", cmd)
	}
	var s struct {
		Excludes []string `json:"claudeMdExcludes"`
	}
	if err := json.Unmarshal([]byte(m[1]), &s); err != nil {
		t.Fatalf("settings JSON %q: %v", m[1], err)
	}
	var out []string
	for _, p := range s.Excludes {
		if strings.HasPrefix(p, root) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func sameSet(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}

func TestLaunch_ExcludesExactlyAncestorClaudeMds(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	argv := startFakeTmux(t, false)
	root, proj, anc := ancestorTree(t)
	b := backendOf()
	b.SetExcludeAncestorClaudeMd(true)
	if err := b.Launch(context.Background(), "do it", "cs-anc1", proj, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedCmd(t, argv)
	if got := excludesIn(t, cmd, root); !sameSet(got, anc) {
		t.Errorf("excludes under temp root = %v, want exactly %v", got, anc)
	}
}

func TestLaunch_NeverExcludesProjectOwnClaudeMd(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	argv := startFakeTmux(t, false)
	_, proj, _ := ancestorTree(t)
	b := backendOf()
	b.SetExcludeAncestorClaudeMd(true)
	if err := b.Launch(context.Background(), "do it", "cs-anc2", proj, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	m := settingsArgRe.FindStringSubmatch(recordedCmd(t, argv))
	if m == nil {
		t.Fatal("no --settings flag")
	}
	if strings.Contains(m[1], filepath.Join(proj, "CLAUDE.md")) || strings.Contains(m[1], filepath.Join(proj, "sub")) {
		t.Errorf("project's own CLAUDE.md leaked into excludes: %s", m[1])
	}
}

func TestLaunch_OptionOff_NoSettings(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir
	argv := startFakeTmux(t, false)
	_, proj, _ := ancestorTree(t)
	if err := backendOf().Launch(context.Background(), "do it", "cs-anc3", proj, "/nonexistent/log"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedCmd(t, argv)
	if strings.Contains(cmd, "--settings") || strings.Contains(cmd, "claudeMdExcludes") {
		t.Errorf("option off but command carries settings: %s", cmd)
	}
}

func TestLaunchResume_CarriesSameExcludes(t *testing.T) {
	argv := startFakeTmux(t, false)
	root, proj, anc := ancestorTree(t)
	b := backendOf()
	b.SetExcludeAncestorClaudeMd(true)
	if err := b.LaunchResume(context.Background(), "", "cs-anc4", proj, "/nonexistent/log", "cs-anc4"); err != nil {
		t.Fatalf("LaunchResume: %v", err)
	}
	cmd := recordedCmd(t, argv)
	if !strings.Contains(cmd, "--resume") {
		t.Errorf("expected a resume command, got: %s", cmd)
	}
	if got := excludesIn(t, cmd, root); !sameSet(got, anc) {
		t.Errorf("resume excludes = %v, want %v", got, anc)
	}

	// And with the option off, resume carries nothing.
	argv2 := startFakeTmux(t, false)
	if err := backendOf().LaunchResume(context.Background(), "", "cs-anc5", proj, "/nonexistent/log", "cs-anc5"); err != nil {
		t.Fatalf("LaunchResume off: %v", err)
	}
	if c := recordedCmd(t, argv2); strings.Contains(c, "--settings") {
		t.Errorf("resume with option off carries --settings: %s", c)
	}
}
