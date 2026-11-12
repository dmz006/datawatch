package mergeshadow

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// testClassifier is a classifier whose version files, changelog and README
// are the ones the tests name.
func testClassifier() Classifier {
	return Classifier{
		VersionFiles:  []string{"cmd/app/main.go", "internal/server/api.go"},
		ChangelogFile: "CHANGELOG.md",
		ReadmeFile:    "README.md",
	}
}

func TestClassifyMechanicalPaths(t *testing.T) {
	c := testClassifier()
	cases := map[string]string{
		"CHANGELOG.md":                      "changelog",
		"README.md":                         "readme",
		"cmd/app/main.go":                   "version file",
		"internal/server/api.go":            "version file",
		"./internal/server/api.go":          "version file with ./ prefix",
		"web/locales/en.json":               "locale JSON",
		"web/locales/de/common.json":        "nested locale JSON",
		"i18n/fr.json":                      "i18n JSON",
		"locale/ja.json":                    "singular locale directory",
		"docs/index.json":                   "generated index JSON",
		"internal/docsindex/api.index.json": "generated *.index.json",
		"generated/search.index.yaml":       "generated index YAML",
	}
	for p, why := range cases {
		if got := c.Classify(p); got != FileClassMechanical {
			t.Errorf("Classify(%q) [%s] = %q, want %q", p, why, got, FileClassMechanical)
		}
	}
}

func TestClassifyCodePaths(t *testing.T) {
	c := testClassifier()
	for _, p := range []string{
		"internal/mergeshadow/classify.go",
		"web/app.ts",
		"web/app.jsx",
		"scripts/run.py",
		"scripts/run.sh",
		"internal/app/Main.JAVA",
	} {
		if got := c.Classify(p); got != FileClassCode {
			t.Errorf("Classify(%q) = %q, want %q", p, got, FileClassCode)
		}
	}
}

func TestClassifyDocumentPaths(t *testing.T) {
	c := testClassifier()
	for _, p := range []string{
		"docs/plans/2026-11-09-merge.md",
		"docs/architecture.rst",
		"guides/setup.markdown",
		"notes/todo.txt",
		"docs/LICENSE",
		"docs/images/diagram.svg",
	} {
		if got := c.Classify(p); got != FileClassDocument {
			t.Errorf("Classify(%q) = %q, want %q", p, got, FileClassDocument)
		}
	}
}

func TestClassifyUnknownPaths(t *testing.T) {
	c := testClassifier()
	for _, p := range []string{
		"",
		"   ",
		"logo.png",
		"config/settings.yaml",
		"web/locales/en.yaml",
		"data/records.json",
		"CHANGELOG.md.bak",
		"/etc/passwd",
		"../outside/notes.md",
		`windows\path.go`,
		".",
	} {
		if got := c.Classify(p); got != FileClassUnknown {
			t.Errorf("Classify(%q) = %q, want %q", p, got, FileClassUnknown)
		}
	}
}

func TestClassifyVersionFileOnlyWhenListed(t *testing.T) {
	c := Classifier{ChangelogFile: "CHANGELOG.md", ReadmeFile: "README.md"}
	if got := c.Classify("cmd/app/main.go"); got != FileClassCode {
		t.Errorf("unlisted version file = %q, want %q", got, FileClassCode)
	}
}

func TestClassifyChangelogAndReadmeFromFields(t *testing.T) {
	c := Classifier{ChangelogFile: "docs/CHANGES.md", ReadmeFile: "docs/READ.md"}
	if got := c.Classify("docs/CHANGES.md"); got != FileClassMechanical {
		t.Errorf("configured changelog = %q, want mechanical", got)
	}
	if got := c.Classify("docs/READ.md"); got != FileClassMechanical {
		t.Errorf("configured readme = %q, want mechanical", got)
	}
	if got := c.Classify("CHANGELOG.md"); got != FileClassDocument {
		t.Errorf("default changelog name with a different configured path = %q, want document", got)
	}
}

func TestNewClassifierReadsVersionFilesFromConfig(t *testing.T) {
	cfg := config.MergePipelineConfig{
		VersionFiles: []string{"global/version.go"},
		Projects: map[string]config.MergePipelineOverride{
			"alpha": {VersionFiles: []string{"alpha/version.go"}},
		},
	}
	alpha := NewClassifier(cfg, "alpha")
	if got := alpha.Classify("alpha/version.go"); got != FileClassMechanical {
		t.Errorf("project version file = %q, want mechanical", got)
	}
	if got := alpha.Classify("global/version.go"); got != FileClassCode {
		t.Errorf("global version file under a project override = %q, want code", got)
	}
	beta := NewClassifier(cfg, "beta")
	if got := beta.Classify("global/version.go"); got != FileClassMechanical {
		t.Errorf("global version file for a project without override = %q, want mechanical", got)
	}
	if got := beta.Classify("CHANGELOG.md"); got != FileClassMechanical {
		t.Errorf("default changelog = %q, want mechanical", got)
	}
	if got := beta.Classify("README.md"); got != FileClassMechanical {
		t.Errorf("default readme = %q, want mechanical", got)
	}
}

func TestNewClassifierWithNoVersionFiles(t *testing.T) {
	c := NewClassifier(config.MergePipelineConfig{}, "any")
	if len(c.VersionFiles) != 0 {
		t.Fatalf("VersionFiles = %v, want none", c.VersionFiles)
	}
	if got := c.Classify("cmd/app/main.go"); got != FileClassCode {
		t.Errorf("unset version files: main.go = %q, want code", got)
	}
}
