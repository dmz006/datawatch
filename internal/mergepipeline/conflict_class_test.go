package mergepipeline

import "testing"

func TestClassifyConflictFile(t *testing.T) {
	versions := []string{"internal/server/api.go", "cmd/datawatch/main.go"}
	cases := []struct {
		name string
		rel  string
		want FileClass
	}{
		{"changelog", "CHANGELOG.md", ClassChangelog},
		{"version file", "internal/server/api.go", ClassVersionLine},
		{"second version file", "cmd/datawatch/main.go", ClassVersionLine},
		{"readme", "README.md", ClassReadmeLine},
		{"locale", "internal/server/web/locales/en.json", ClassLocale},
		{"locale nested", "internal/server/web/locales/x/en.json", ClassCode},
		{"locale not json", "internal/server/web/locales/en.txt", ClassCode},
		{"docs index asset", "internal/docsindex/assets/index.json", ClassGenerated},
		{"web docs", "internal/server/web/docs/howto.md", ClassGenerated},
		{"other markdown", "internal/foo/NOTES.md", ClassDocument},
		{"docs dir", "docs/plans/plan.txt", ClassDocument},
		{"code", "internal/mergepipeline/run.go", ClassCode},
		{"absolute", "/etc/passwd", ClassCode},
		{"absolute markdown", "/tmp/NOTES.md", ClassCode},
		{"dot-dot escape", "../docs/x.md", ClassCode},
		{"bare dot-dot", "..", ClassCode},
		{"empty", "", ClassCode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyConflictFile(c.rel, versions); got != c.want {
				t.Errorf("ClassifyConflictFile(%q) = %q, want %q", c.rel, got, c.want)
			}
		})
	}
}
