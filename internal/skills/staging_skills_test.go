package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const stagingDir = "testdata/staging"

// Words are assembled so this file does not trip its own rules.
var leakRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"product name", regexp.MustCompile(`(?i)data` + `watch`)},
	{"internal acronym", regexp.MustCompile(`(?i)\bp` + `rd\b`)},
	{"tracker id", regexp.MustCompile(`\b(?:BL|B|GH|SEC)[-#]?\d+\b`)},
	{"version string", regexp.MustCompile(`\bv\d+\.\d+(?:\.\d+)?\b`)},
	{"semantic number", regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)},
	{"private ipv4", regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`)},
	{"absolute home path", regexp.MustCompile(`(?:/home/[A-Za-z0-9._-]+|/Users/[A-Za-z0-9._-]+|~/|C:\\Users\\)`)},
	{"email address", regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)},
}

// leakCheck returns one finding per rule that matches the text.
func leakCheck(text string) []string {
	var out []string
	for _, r := range leakRules {
		if m := r.re.FindString(text); m != "" {
			out = append(out, fmt.Sprintf("%s: %q", r.name, m))
		}
	}
	return out
}

func nonEmptyFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) == "" {
		return fmt.Errorf("%s is empty", path)
	}
	return nil
}

// countCases accepts a top-level list or a mapping with a `cases` list.
func countCases(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var list []any
	if err := yaml.Unmarshal(b, &list); err == nil {
		return len(list), nil
	}
	var wrap struct {
		Cases []any `yaml:"cases"`
	}
	if err := yaml.Unmarshal(b, &wrap); err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return len(wrap.Cases), nil
}

// checkSkillFolder returns every problem found in one skill folder.
func checkSkillFolder(dir string) []string {
	var problems []string
	base := filepath.Base(dir)

	m, err := ParseManifestFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		problems = append(problems, "SKILL.md: "+err.Error())
	} else {
		if m.Name != base {
			problems = append(problems, fmt.Sprintf("name %q does not equal folder name %q", m.Name, base))
		}
		if strings.TrimSpace(m.Description) == "" {
			problems = append(problems, "description is empty")
		}
		if strings.TrimSpace(m.Version) == "" {
			problems = append(problems, "version is empty")
		}
	}

	if err := nonEmptyFile(filepath.Join(dir, "USAGE.md")); err != nil {
		problems = append(problems, "USAGE.md: "+err.Error())
	}
	casesPath := filepath.Join(dir, "tests", "cases.yaml")
	if err := nonEmptyFile(casesPath); err != nil {
		problems = append(problems, "tests/cases.yaml: "+err.Error())
	} else if n, err := countCases(casesPath); err != nil {
		problems = append(problems, err.Error())
	} else if n < 3 {
		problems = append(problems, fmt.Sprintf("tests/cases.yaml has %d cases, need at least 3", n))
	}

	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			problems = append(problems, "read "+p+": "+rerr.Error())
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		for _, f := range leakCheck(string(b)) {
			problems = append(problems, fmt.Sprintf("leak in %s: %s", rel, f))
		}
		return nil
	})
	return problems
}

func TestStagingSkills(t *testing.T) {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(stagingDir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			for _, p := range checkSkillFolder(dir) {
				t.Error(p)
			}
		})
	}
}

func writeFixture(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goodFixture(t *testing.T, name string) string {
	dir := filepath.Join(t.TempDir(), name)
	writeFixture(t, dir, "SKILL.md", "---\nname: "+name+"\ndescription: Does a thing\nversion: \"1\"\n---\nBody\n")
	writeFixture(t, dir, "USAGE.md", "Ask for the thing.\n")
	writeFixture(t, dir, "tests/cases.yaml", "cases:\n  - input: a\n  - input: b\n  - input: c\n")
	return dir
}

func TestStagingGoodFixturePasses(t *testing.T) {
	if p := checkSkillFolder(goodFixture(t, "good-skill")); len(p) != 0 {
		t.Fatalf("good fixture rejected: %v", p)
	}
}

func TestStagingLeakCheckCatchesBadContent(t *testing.T) {
	bad := map[string]string{
		"product name":       "run " + "Data" + "Watch now",
		"internal acronym":   "see the " + "PR" + "D",
		"tracker id":         "fixes " + "BL" + "123",
		"tracker id gh":      "see " + "GH" + "45",
		"tracker id sec":     "see " + "SEC" + "9",
		"version string":     "added in v" + "1.2.3",
		"semantic number":    "needs 4.5.6 or later",
		"private ipv4 10":    "host " + "10." + "0.0.5",
		"private ipv4 192":   "host " + "192.168." + "1.1",
		"private ipv4 172":   "host " + "172.16." + "0.1",
		"absolute home path": "file " + "/home/" + "someone/x",
		"email address":      "mail " + "a" + "@" + "example.com",
	}
	for name, text := range bad {
		if len(leakCheck(text)) == 0 {
			t.Errorf("%s: leak check missed %q", name, text)
		}
	}
	if f := leakCheck("Plain usage text with port 8080 and step 2."); len(f) != 0 {
		t.Errorf("false positive: %v", f)
	}
}

func TestStagingFolderCheckCatchesLeakFile(t *testing.T) {
	dir := goodFixture(t, "leaky")
	writeFixture(t, dir, "extra/notes.md", "contact "+"a"+"@"+"example.com\n")
	if p := checkSkillFolder(dir); len(p) == 0 {
		t.Fatal("expected leak finding")
	}
}

func TestStagingParseCheckCatchesBadFixtures(t *testing.T) {
	cases := map[string]func(dir string){
		"missing manifest": func(dir string) { _ = os.Remove(filepath.Join(dir, "SKILL.md")) },
		"name mismatch": func(dir string) {
			writeFixture(t, dir, "SKILL.md", "---\nname: other\ndescription: d\nversion: \"1\"\n---\n")
		},
		"empty description": func(dir string) {
			writeFixture(t, dir, "SKILL.md", "---\nname: bad\nversion: \"1\"\n---\n")
		},
		"empty version": func(dir string) {
			writeFixture(t, dir, "SKILL.md", "---\nname: bad\ndescription: d\n---\n")
		},
		"missing usage": func(dir string) { _ = os.Remove(filepath.Join(dir, "USAGE.md")) },
		"empty usage":   func(dir string) { writeFixture(t, dir, "USAGE.md", "  \n") },
		"missing cases": func(dir string) { _ = os.Remove(filepath.Join(dir, "tests", "cases.yaml")) },
		"too few cases": func(dir string) { writeFixture(t, dir, "tests/cases.yaml", "- a\n- b\n") },
		"invalid cases": func(dir string) { writeFixture(t, dir, "tests/cases.yaml", "cases: [unclosed\n") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := goodFixture(t, "bad")
			mutate(dir)
			if p := checkSkillFolder(dir); len(p) == 0 {
				t.Fatal("bad fixture was accepted")
			}
		})
	}
}
