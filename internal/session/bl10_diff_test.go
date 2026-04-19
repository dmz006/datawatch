// BL10 — git diff shortstat parsing.

package session

import "testing"

func TestBL10_ParseShortstat_Standard(t *testing.T) {
	in := "3 files changed, 47 insertions(+), 12 deletions(-)"
	got := parseShortstat(in)
	if got.Files != 3 || got.Insertions != 47 || got.Deletions != 12 {
		t.Errorf("parse mismatch: %+v", got)
	}
	if got.Summary == "" {
		t.Error("Summary should be populated")
	}
}

func TestBL10_ParseShortstat_OnlyInsertions(t *testing.T) {
	got := parseShortstat("1 file changed, 5 insertions(+)")
	if got.Files != 1 || got.Insertions != 5 || got.Deletions != 0 {
		t.Errorf("parse mismatch: %+v", got)
	}
}

func TestBL10_ParseShortstat_Empty(t *testing.T) {
	got := parseShortstat("")
	if !got.IsZero() {
		t.Errorf("empty should be zero, got %+v", got)
	}
	if got.Summary != "" {
		t.Errorf("zero diff should have empty summary")
	}
}

func TestBL10_DiffStat_NotARepo(t *testing.T) {
	g := NewProjectGit(t.TempDir())
	stat, err := g.DiffStat()
	if err != nil {
		t.Errorf("non-repo should return zero, no error: %v", err)
	}
	if !stat.IsZero() {
		t.Errorf("non-repo should be zero, got %+v", stat)
	}
}
