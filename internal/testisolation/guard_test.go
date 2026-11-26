//go:build linux

package testisolation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLayout builds a guarded layout entirely under the test's own temp folder:
// the "machine temp folder", "shared temp folder" and "home" are all fakes.
func testLayout(t *testing.T) Layout {
	t.Helper()
	root := t.TempDir()
	l := Layout{
		TempRoot: filepath.Join(root, "tmp"),
		Shared:   filepath.Join(root, "tmp", "shared"),
		Home:     filepath.Join(root, "home"),
	}
	l.Allowed = []string{filepath.Join(l.Home, "src", "repo")}
	if err := os.MkdirAll(l.Allowed[0], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.Plant(); err != nil {
		t.Fatalf("Plant: %v", err)
	}
	return l
}

func snapshot(t *testing.T, l Layout) *Snapshot {
	t.Helper()
	s, err := l.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return s
}

func has(vs []Violation, what, path string) bool {
	for _, v := range vs {
		if v.What == what && v.Path == path {
			return true
		}
	}
	return false
}

func TestCheckPassesWhenTestsStayInTheirOwnFolders(t *testing.T) {
	l := testLayout(t)
	s := snapshot(t, l)
	// What a well-behaved run does: its own folder in the shared temp folder,
	// listed, written and removed again, and work in the repository.
	own, err := os.MkdirTemp(l.Shared, "TestSomething")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(own); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(own); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.Allowed[0], "out"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Check(); len(got) != 0 {
		t.Fatalf("clean run reported: %v", got)
	}
}

func TestCheckReportsEachKindOfReachOutside(t *testing.T) {
	decoy := func(l Layout, rel string) string { return filepath.Join(l.Shared, rel) }
	cases := []struct {
		name string
		do   func(l Layout) error
		what string
		path func(l Layout) string
	}{
		{"listing the shared temp folder", func(l Layout) error { _, err := os.ReadDir(l.Shared); return err },
			FindingListed, func(l Layout) string { return l.Shared }},
		{"listing the machine temp folder", func(l Layout) error { _, err := os.ReadDir(l.TempRoot); return err },
			FindingListed, func(l Layout) string { return l.TempRoot }},
		{"listing another's folder", func(l Layout) error { _, err := os.ReadDir(decoy(l, DecoyPrefix+"other-session/sub")); return err },
			FindingRead, func(l Layout) string { return decoy(l, DecoyPrefix+"other-session/sub") }},
		{"reading another's file", func(l Layout) error { _, err := os.ReadFile(decoy(l, "go-build424242/notes.txt")); return err },
			FindingRead, func(l Layout) string { return decoy(l, "go-build424242/notes.txt") }},
		{"writing another's file", func(l Layout) error {
			return os.WriteFile(decoy(l, "go-build424242/notes.txt"), []byte("changed"), 0o600)
		}, FindingChanged, func(l Layout) string { return decoy(l, "go-build424242/notes.txt") }},
		{"removing another's folder", func(l Layout) error { return os.RemoveAll(decoy(l, "go-build424242")) },
			FindingRemoved, func(l Layout) string { return decoy(l, "go-build424242") }},
		{"writing into the home", func(l Layout) error { return os.WriteFile(filepath.Join(l.Home, ".leak"), nil, 0o600) },
			FindingWrote, func(l Layout) string { return filepath.Join(l.Home, ".leak") }},
		{"writing into the machine temp folder", func(l Layout) error { return os.Mkdir(filepath.Join(l.TempRoot, "tmux-0"), 0o700) },
			FindingWrote, func(l Layout) string { return filepath.Join(l.TempRoot, "tmux-0") }},
		{"making and removing an entry in the home", func(l Layout) error {
			p := filepath.Join(l.Home, ".brief")
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				return err
			}
			return os.Remove(p)
		}, FindingWrote, func(l Layout) string { return l.Home + " (an entry made and removed again, or removed)" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := testLayout(t)
			s := snapshot(t, l)
			if err := c.do(l); err != nil {
				t.Fatal(err)
			}
			if got := s.Check(); !has(got, c.what, c.path(l)) {
				t.Fatalf("want %s %s, got %v", c.what, c.path(l), got)
			}
		})
	}
}

func TestPlantRefusesAFolderThatAlreadyHoldsADecoyName(t *testing.T) {
	l := testLayout(t)
	if err := l.Plant(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Plant: err = %v, want a refusal", err)
	}
}

func TestAnalyzeLog(t *testing.T) {
	l := Layout{
		TempRoot: "/tmp", Shared: "/tmp/shared", Home: "/srv/u",
		Allowed: []string{"/srv/u/src/repo", "/srv/u/cache"},
		Decoys:  []string{"/tmp/shared/go-build424242"},
	}
	log := strings.Join([]string{
		"# test log",
		"getenv TMPDIR",
		"open /tmp/shared/TestOne12345678/001/file",
		"stat /tmp/shared",
		"stat /tmp",                 // a parent folder
		"stat /tmp/.git",            // looking for a repository above
		"stat /srv/u/src/.git",      // the same above the repository
		"stat /srv/u/src",           // a parent folder
		"open /srv/u/src/repo/a.go", // the repository
		"open testdata/x",           // relative to the repository
		"open /usr/bin/git",         // a system path
		"stat /proj387/made-up",     // a made-up path
		// Outside:
		"open /tmp/shared/go-build424242/sub",
		"open /tmp/shared/go-build424242/sub",
		"stat /tmp/ts-fixed-name",
		"open /srv/u/.datawatch/config.yaml",
		"open /srv/u/src/CLAUDE.local.md", // opening is not looking up
		"stat /srv/u/src/secret.txt",
		"chdir /srv/u/other",
		"open notes", // relative to the chdir above
	}, "\n")
	got, err := l.AnalyzeLog(strings.NewReader(log), "/srv/u/src/repo/pkg")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, v := range got {
		lines = append(lines, v.What+" "+v.Path)
	}
	want := []string{
		"open /tmp/shared/go-build424242/sub",
		"stat /tmp/ts-fixed-name",
		"open /srv/u/.datawatch/config.yaml",
		"open /srv/u/src/CLAUDE.local.md",
		"stat /srv/u/src/secret.txt",
		"chdir /srv/u/other",
		"open /srv/u/other/notes",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("violations:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if got[0].Count != 2 || got[0].After != "TestOne" {
		t.Errorf("first violation count=%d after=%q, want 2 and TestOne", got[0].Count, got[0].After)
	}
}

func TestReportShortensAndPutsFileSystemFindingsFirst(t *testing.T) {
	out := Report("internal/x", []Violation{
		{What: "stat", Path: "/tmp/a", Count: 1},
		{What: FindingRemoved, Path: "/tmp/shared/go-build424242"},
		{What: "open", Path: "/tmp/b", Count: 3, After: "TestY"},
	}, 2)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.Contains(lines[1], "REMOVED /tmp/shared/go-build424242") ||
		!strings.Contains(lines[3], "and 1 more") {
		t.Fatalf("report:\n%s", out)
	}
}
