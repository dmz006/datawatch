package mergeshadow

import (
	"testing"
)

const entryOutput = "## [9.200.0] - 2026-11-09\n" +
	"\n" +
	"### Fixed\n" +
	"\n" +
	"- **Merge plans** show the entry text.\n" +
	"  Second line kept as written.\n" +
	"\n" +
	"```text\n" +
	"plain code line\n" +
	"```\n"

func TestReadChangelogEntry_PresentEntry(t *testing.T) {
	tasks := []ChangelogTask{
		{ID: "task-build", Kind: "", Output: "## [1.0.0] - 2026-01-01\n\nignored, no marker"},
		{ID: "task-entry", Kind: ChangelogTaskKind, Output: "Preface the worker wrote.\n\n" + entryOutput},
	}
	got := ReadChangelogEntry(tasks)
	if got.Source != "task-entry" {
		t.Fatalf("Source = %q, want task-entry", got.Source)
	}
	if got.Heading != "## [9.200.0] - 2026-11-09" {
		t.Fatalf("Heading = %q", got.Heading)
	}
	wantBody := "### Fixed\n\n- **Merge plans** show the entry text.\n  Second line kept as written.\n\n```text\nplain code line\n```"
	if got.Body != wantBody {
		t.Fatalf("Body mismatch\n got: %q\nwant: %q", got.Body, wantBody)
	}
}

func TestReadChangelogEntry_BodyStopsAtNextVersionHeading(t *testing.T) {
	out := "## [2.0.0] - 2026-02-02\n\nNew thing.\n\n## [1.9.0] - 2026-01-01\n\nOld thing.\n"
	got := ReadChangelogEntry([]ChangelogTask{{ID: "t1", Kind: ChangelogTaskKind, Output: out}})
	if got.Source != "t1" || got.Heading != "## [2.0.0] - 2026-02-02" || got.Body != "New thing." {
		t.Fatalf("got %+v", got)
	}
}

func TestReadChangelogEntry_CRLFOutput(t *testing.T) {
	out := "## [3.1.0] - 2026-03-03\r\n\r\nFirst.\r\nSecond.\r\n"
	got := ReadChangelogEntry([]ChangelogTask{{ID: "t1", Kind: ChangelogTaskKind, Output: out}})
	if got.Source != "t1" || got.Heading != "## [3.1.0] - 2026-03-03" || got.Body != "First.\nSecond." {
		t.Fatalf("got %+v", got)
	}
}

func TestReadChangelogEntry_AbsentTask(t *testing.T) {
	cases := map[string][]ChangelogTask{
		"no tasks":        nil,
		"no marker":       {{ID: "t1", Kind: "docs", Output: entryOutput}},
		"marker no id":    {{ID: "", Kind: ChangelogTaskKind, Output: entryOutput}},
		"marker no entry": {{ID: "t1", Kind: ChangelogTaskKind, Output: "Done, nothing to add."}},
	}
	for name, tasks := range cases {
		t.Run(name, func(t *testing.T) {
			got := ReadChangelogEntry(tasks)
			if got.Source != ChangelogSourceMissing || got.Heading != "" || got.Body != "" {
				t.Fatalf("got %+v, want missing with empty heading and body", got)
			}
		})
	}
}

func TestReadChangelogEntry_MalformedEntry(t *testing.T) {
	cases := map[string]string{
		"title instead of version and date": "## v9.200.0 — Some title\n\nBody text.\n",
		"version without date":              "## [9.200.0]\n\nBody text.\n",
		"date without brackets":             "## 9.200.0 - 2026-11-09\n\nBody text.\n",
		"bad date shape":                    "## [9.200.0] - 11/09/2026\n\nBody text.\n",
		"trailing text after date":          "## [9.200.0] - 2026-11-09 extra\n\nBody text.\n",
		"no heading at all":                 "Just prose without a heading.\n",
		"heading with no body":              "## [9.200.0] - 2026-11-09\n\n\n",
		"non-changelog heading first":       "## Summary\n\n## [9.200.0] - 2026-11-09\n\nBody.\n",
		"empty output":                      "",
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			got := ReadChangelogEntry([]ChangelogTask{{ID: "t1", Kind: ChangelogTaskKind, Output: out}})
			if got.Source != ChangelogSourceMissing || got.Heading != "" || got.Body != "" {
				t.Fatalf("got %+v, want missing with empty heading and body", got)
			}
		})
	}
}

func TestReadChangelogEntry_SkipsMalformedThenUsesValid(t *testing.T) {
	tasks := []ChangelogTask{
		{ID: "bad", Kind: ChangelogTaskKind, Output: "## v1 — nope\n\nbody\n"},
		{ID: "good", Kind: ChangelogTaskKind, Output: entryOutput},
	}
	got := ReadChangelogEntry(tasks)
	if got.Source != "good" || got.Heading != "## [9.200.0] - 2026-11-09" {
		t.Fatalf("got %+v", got)
	}
}

func TestChangelogTaskKindValue(t *testing.T) {
	if ChangelogTaskKind != "defect_changelog" {
		t.Fatalf("ChangelogTaskKind = %q, want %q", ChangelogTaskKind, "defect_changelog")
	}
}
