package supervisorloop

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dmz006/datawatch/internal/planrecords"
)

// The default build supervisor pack ships in the repository. These tests read
// it from disk and run it through the handover with fakes only: a fake session
// layer, fake plan records, a rules source that serves the pack's rules and an
// injected clock.

const defaultPackDir = "../../packs/build-supervisor"

type packManifest struct {
	Name       string            `yaml:"name"`
	Rules      string            `yaml:"rules"`
	Handover   string            `yaml:"handover"`
	Ticks      map[string]string `yaml:"ticks"`
	Automation map[string]string `yaml:"automation"`
}

func loadPackManifest(t *testing.T) packManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(defaultPackDir, "pack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var m packManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDefaultPackManifestFilesExist(t *testing.T) {
	m := loadPackManifest(t)
	if m.Name != "build-supervisor" {
		t.Fatalf("name %q", m.Name)
	}
	files := []string{m.Rules, m.Handover}
	for _, f := range m.Ticks {
		files = append(files, f)
	}
	for _, f := range m.Automation {
		files = append(files, f)
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(defaultPackDir, f))
		if err != nil || strings.TrimSpace(string(b)) == "" {
			t.Fatalf("pack file %q missing or empty: %v", f, err)
		}
	}
}

func TestDefaultPackTicksMatchTickKinds(t *testing.T) {
	m := loadPackManifest(t)
	if len(m.Ticks) != len(AllTicks) {
		t.Fatalf("ticks %v", m.Ticks)
	}
	for _, k := range AllTicks {
		file, ok := m.Ticks[string(k)]
		if !ok {
			t.Fatalf("no tick file for %s", k)
		}
		raw, _ := os.ReadFile(filepath.Join(defaultPackDir, file))
		var d struct {
			Name  string   `yaml:"name"`
			Kind  string   `yaml:"kind"`
			Steps []string `yaml:"steps"`
		}
		if err := yaml.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		if d.Kind != string(k) || d.Name != string(k) || len(d.Steps) == 0 {
			t.Fatalf("%s: %+v", k, d)
		}
	}
}

func TestDefaultPackAutomationModules(t *testing.T) {
	m := loadPackManifest(t)
	for _, stack := range []string{"go", "python", "docs_site"} {
		file, ok := m.Automation[stack]
		if !ok {
			t.Fatalf("no module for %s", stack)
		}
		raw, _ := os.ReadFile(filepath.Join(defaultPackDir, file))
		var d struct {
			Stack  string `yaml:"stack"`
			Detect struct {
				Files []string `yaml:"files"`
			} `yaml:"detect"`
			Build  string   `yaml:"build"`
			Test   string   `yaml:"test"`
			Verify []string `yaml:"verify_before_commit"`
		}
		if err := yaml.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		if d.Stack != stack || len(d.Detect.Files) == 0 || d.Build == "" || d.Test == "" || len(d.Verify) == 0 {
			t.Fatalf("%s: %+v", stack, d)
		}
	}
}

var packForbidden = []*regexp.Regexp{
	regexp.MustCompile(`\b(BL|GH|SEC|HLLM)-?\d+\b`),
	regexp.MustCompile(`\bv?\d+\.\d+\.\d+\b`),
	regexp.MustCompile(`/home/|~/`),
	regexp.MustCompile(`(?i)opus`),
}

func TestDefaultPackHasNoTrackerIdsVersionsOrPaths(t *testing.T) {
	err := filepath.Walk(defaultPackDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, re := range packForbidden {
			// The rules say "never switch to Opus"; that one line is the only allowed mention.
			text := strings.ReplaceAll(string(b), "You never switch to Opus.", "")
			if re.MatchString(text) {
				t.Errorf("%s matches %s", p, re)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type packRules struct{ pack string }

func (r packRules) DefaultPack() (string, error)                { return r.pack, nil }
func (packRules) ProjectPack(string) (string, bool, error)      { return "", false, nil }
func (packRules) ProjectRulesFile(string) (string, bool, error) { return "", false, nil }

func TestDefaultPackRulesReachTheHandover(t *testing.T) {
	m := loadPackManifest(t)
	rules, err := os.ReadFile(filepath.Join(defaultPackDir, m.Rules))
	if err != nil {
		t.Fatal(err)
	}
	a := &HandoverAssembler{
		Session: hoSession{dir: map[string]string{"alpha": "/work/alpha"}, profile: map[string]string{"alpha": "sup"}},
		Rules:   packRules{pack: string(rules)},
		Plans:   hoPlans{plans: map[string][]planrecords.PlanDigest{"alpha": {digest("Ship quartz", "ship it", 1)}}},
		Clock:   hoClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
	h, err := a.AssembleHandover("alpha")
	if err != nil {
		t.Fatal(err)
	}
	out := h.Render()
	for _, want := range []string{"Default build supervisor pack", "Decisions that stay with the operator", "2026-01-02T03:04:05Z", "Ship quartz"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
}
