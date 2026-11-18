package mergeshadow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/genartifacts"
)

const entryOutput = "## [9.200.0] - 2026-11-09\n\n### Added\n- entry one\n- an other entry\n"

func writeDraft(t *testing.T, repo, automatonID, body string) string {
	t.Helper()
	p := filepath.Join(repo, changelogDraftPath(automatonID))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return changelogDraftPath(automatonID)
}

// fallbackRepo is an empty repository checkout: no changelog draft file.
func fallbackRepo(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestReadChangelogEntry_PresentEntry(t *testing.T) {
	repo := fallbackRepo(t)
	tasks := []ChangelogTask{
		{ID: "other", Kind: "defect", Output: "## [1.0.0] - 2026-01-01\n\nx\n"},
		{ID: "task-entry", Kind: ChangelogTaskKind, Output: entryOutput},
		{ID: "after", Kind: ChangelogTaskKind, Output: "## [9.9.9] - 2026-12-31\n\ny\n"},
	}
	got := ReadChangelogEntry("a1", repo, tasks)
	if got.Source != "task-entry" {
		t.Fatalf("Source = %q, want task-entry", got.Source)
	}
	if got.Heading != "## [9.200.0] - 2026-11-09" {
		t.Errorf("Heading = %q", got.Heading)
	}
	want := "### Added\n- entry one\n- an other entry"
	if got.Body != want {
		t.Errorf("Body = %q, want %q", got.Body, want)
	}
}

func TestReadChangelogEntry_BodyStopsAtNextHeading(t *testing.T) {
	repo := fallbackRepo(t)
	out := "## [2.0.0] - 2026-02-02\n\nNew thing.\n\n## [1.9.0] - 2026-01-01\n\nOld thing.\n"
	got := ReadChangelogEntry("a1", repo, []ChangelogTask{{ID: "t1", Kind: ChangelogTaskKind, Output: out}})
	if got.Source != "t1" || got.Heading != "## [2.0.0] - 2026-02-02" {
		t.Fatalf("got %+v", got)
	}
	if got.Body != "New thing." {
		t.Errorf("Body = %q, want New thing.", got.Body)
	}
}

func TestReadChangelogEntry_CRLFBodyIsTrimmed(t *testing.T) {
	repo := fallbackRepo(t)
	out := "## [3.1.0] - 2026-03-03\r\n\r\nFirst.\r\nSecond.\r\n"
	got := ReadChangelogEntry("a1", repo, []ChangelogTask{{ID: "t1", Kind: ChangelogTaskKind, Output: out}})
	if got.Source != "t1" || got.Heading != "## [3.1.0] - 2026-03-03" {
		t.Fatalf("got %+v", got)
	}
	if got.Body != "First.\nSecond." {
		t.Errorf("Body = %q", got.Body)
	}
}

func TestReadChangelogEntry_DraftFileIsReadFromTheRepository(t *testing.T) {
	repo := fallbackRepo(t)
	name := writeDraft(t, repo, "a1", "## [9.201.0] - 2026-11-10\n\n### Added\n- draft entry\n")
	tasks := []ChangelogTask{{ID: "task-entry", Kind: ChangelogTaskKind, Output: entryOutput}}
	got := ReadChangelogEntry("a1", repo, tasks)
	if got.Source != name || name != "docs/plans/drafts/a1-changelog.md" {
		t.Fatalf("Source = %q (path %q)", got.Source, name)
	}
	if got.Heading != "## [9.201.0] - 2026-11-10" {
		t.Errorf("Heading = %q", got.Heading)
	}
	if got.Body != "### Added\n- draft entry" {
		t.Errorf("Body = %q", got.Body)
	}
}

func TestReadChangelogEntry_DraftFileWinsOverTaskOutput(t *testing.T) {
	repo := fallbackRepo(t)
	writeDraft(t, repo, "4f2a", "## [7.8.9] - 2026-08-07\n\nfrom the draft file\n")
	tasks := []ChangelogTask{
		{ID: "task-entry", Kind: ChangelogTaskKind, Output: entryOutput},
	}
	got := ReadChangelogEntry("4f2a", repo, tasks)
	if got.Source != "docs/plans/drafts/4f2a-changelog.md" {
		t.Fatalf("Source = %q, want the draft path", got.Source)
	}
	if got.Body != "from the draft file" {
		t.Errorf("Body = %q", got.Body)
	}
}

func TestReadChangelogEntry_DraftFallsBackToTaskWhenAbsent(t *testing.T) {
	repo := fallbackRepo(t) // no draft anywhere
	got := ReadChangelogEntry("a1", repo, []ChangelogTask{{ID: "task-entry", Kind: ChangelogTaskKind, Output: entryOutput}})
	if got.Source != "task-entry" {
		t.Fatalf("Source = %q, want task-entry", got.Source)
	}
}

func TestReadChangelogEntry_DraftFallsBackToTaskWhenMalformed(t *testing.T) {
	repo := fallbackRepo(t)
	writeDraft(t, repo, "a1", "## Added\n\na heading with no version\n")
	got := ReadChangelogEntry("a1", repo, []ChangelogTask{{ID: "task-entry", Kind: ChangelogTaskKind, Output: entryOutput}})
	if got.Source != "task-entry" || got.Heading != "## [9.200.0] - 2026-11-09" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadChangelogEntry_DraftHeadingOnlyFallsBackToTask(t *testing.T) {
	repo := fallbackRepo(t)
	writeDraft(t, repo, "a1", "## [9.202.0] - 2026-11-11\n") // heading, no body
	got := ReadChangelogEntry("a1", repo, []ChangelogTask{{ID: "task-entry", Kind: ChangelogTaskKind, Output: entryOutput}})
	if got.Source != "task-entry" {
		t.Fatalf("Source = %q", got.Source)
	}
}

func TestReadChangelogEntry_DraftMalformedAndNoTaskIsMissing(t *testing.T) {
	repo := fallbackRepo(t)
	writeDraft(t, repo, "a1", "no heading at all\n")
	got := ReadChangelogEntry("a1", repo, []ChangelogTask{
		{ID: "wrongkind", Kind: "defect", Output: entryOutput},
	})
	if got.Source != ChangelogSourceMissing || got.Heading != "" || got.Body != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadChangelogEntry_AbsentTask(t *testing.T) {
	skip := []ChangelogTask{
		{ID: "noperm", Kind: "defect", Output: "no permission"},
		{ID: "nodoctask", Kind: ChangelogTaskKind, Output: "no doc"},
	}
	for _, tc := range skip {
		repo := fallbackRepo(t)
		got := ReadChangelogEntry("a1", repo, []ChangelogTask{tc})
		if got.Source != ChangelogSourceMissing {
			t.Errorf("%+v: Source = %q", tc, got.Source)
		}
	}
}

func TestReadChangelogEntry_MalformedEntry(t *testing.T) {
	for _, out := range []string{
		"",
		"nothing here",
		"## Added\n\ntext, but no version",
		"## [9.200.0] - 2026-11-09",
		"## [9.200.0] - 2026-11-09\n\n\n",
	} {
		repo := fallbackRepo(t)
		got := ReadChangelogEntry("a1", repo, []ChangelogTask{{ID: "t1", Kind: ChangelogTaskKind, Output: out}})
		if got.Source != ChangelogSourceMissing {
			t.Errorf("%q: Source = %q, want %s", out, got.Source, ChangelogSourceMissing)
		}
		if got.Heading != "" || got.Body != "" {
			t.Errorf("%q: %+v, want empty entry", out, got)
		}
	}
}

func TestChangelogDraftPathUsesTheTemplate(t *testing.T) {
	if got := changelogDraftPath("4f2a"); got != "docs/plans/drafts/4f2a-changelog.md" {
		t.Fatalf("changelogDraftPath(4f2a) = %q", got)
	}
	if got := changelogDraftPath("a1"); got != "docs/plans/drafts/a1-changelog.md" {
		t.Fatalf("changelogDraftPath(a1) = %q", got)
	}
	// the directory must come from the generated-artifacts template, not a
	// local copy of it
	tpl := genartifacts.DefaultPaths()[genartifacts.ChangelogDraft]
	if !strings.Contains(tpl, "docs/plans/drafts/") || !strings.HasSuffix(tpl, "-changelog.md") {
		t.Fatalf("template %q no longer matches the expected shape", tpl)
	}
}
