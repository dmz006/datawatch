package genartifacts

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
)

func summaryRec() *changerecord.ChangeRecord {
	return &changerecord.ChangeRecord{
		AutomatonID: "ab12",
		Timestamp:   time.Now(),
		Files: []changerecord.FileChange{
			{Path: "internal/server/web/app.js", Status: "modified"},
			{Path: "internal/mcp/tools.go", Status: "modified"},
		},
		Packages:   []string{"internal/mcp"},
		TestsAdded: []string{"TestX"},
		Commands:   []changerecord.CommandRun{{Command: "go test", Pass: 3}, {Command: "go vet", ExitCode: 1}},
	}
}

func TestBuildDataDerived(t *testing.T) {
	v := buildData(ChangelogDraft, summaryRec(), Options{})
	if v.Title != "Changes to MCP, Web UI" {
		t.Fatalf("title %q", v.Title)
	}
	if !strings.Contains(v.Summary, "MCP, Web UI") || !strings.Contains(v.Summary, "2 files in 1 packages") {
		t.Fatalf("summary %q", v.Summary)
	}
	all := strings.Join(v.Bullets, "\n")
	for _, w := range []string{"MCP: ", "Web UI: ", "1 tests added.", "2 commands run, 1 passed."} {
		if !strings.Contains(all, w) {
			t.Fatalf("missing %q in %q", w, all)
		}
	}
	if regexp.MustCompile(`v\d+\.\d+\.\d+`).MatchString(v.Title + v.Summary + all) {
		t.Fatal("fixed version emitted")
	}
}

func TestBuildDataSet(t *testing.T) {
	r := summaryRec()
	r.Title, r.Summary, r.Highlights = "My title", "My summary.", []string{"First", "Second"}
	v := buildData(ChangelogDraft, r, Options{})
	if v.Title != "My title" || v.Summary != "My summary." {
		t.Fatalf("%q %q", v.Title, v.Summary)
	}
	if len(v.Bullets) < 3 || v.Bullets[0] != "First" || v.Bullets[1] != "Second" || !strings.HasPrefix(v.Bullets[2], "MCP") {
		t.Fatalf("bullets %v", v.Bullets)
	}
}

func TestBuildDataEmptyRecord(t *testing.T) {
	v := buildData(ChangelogDraft, &changerecord.ChangeRecord{}, Options{})
	if v.Title != "Automaton unknown" {
		t.Fatalf("title %q", v.Title)
	}
	if v.Summary != "Change from Automaton unknown: 0 files in 0 packages." {
		t.Fatalf("summary %q", v.Summary)
	}
	if len(v.Bullets) != 0 {
		t.Fatalf("bullets %v", v.Bullets)
	}
}
