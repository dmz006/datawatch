package mergepipeline

import (
	"strings"
	"testing"
)

func TestMechanicalLineRules(t *testing.T) {
	t.Run("version line", func(t *testing.T) {
		c := hunkText("head\n", "const Version = \"1.2.4\"\n", "const Version = \"1.2.3\"\n", "const Version = \"1.3.0\"\n", "tail\n")
		got, settled, _, err := ResolveMechanicalFile(ClassVersionLine, "v.go", c, "", "")
		if err != nil || !settled {
			t.Fatalf("settled=%v err=%v", settled, err)
		}
		if got != "head\nconst Version = \"1.2.4\"\ntail\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("readme release line", func(t *testing.T) {
		c := hunkText("# X\n", "Latest release: v2.0.1\n", "Latest release: v2.0.0\n", "Latest release: v2.1.0\n", "text\n")
		got, settled, _, err := ResolveMechanicalFile(ClassReadmeLine, "README.md", c, "", "")
		if err != nil || !settled || got != "# X\nLatest release: v2.0.1\ntext\n" {
			t.Fatalf("got %q settled=%v err=%v", got, settled, err)
		}
	})
	t.Run("non-line hunk stays unsettled", func(t *testing.T) {
		c := hunkText("", "Intro rewritten by us\n", "Intro\n", "Intro rewritten by them\n", "")
		got, settled, _, err := ResolveMechanicalFile(ClassReadmeLine, "README.md", c, "", "")
		if err != nil || settled {
			t.Fatalf("settled=%v err=%v", settled, err)
		}
		if !strings.Contains(got, "<<<<<<<") {
			t.Fatalf("markers lost: %q", got)
		}
	})
	t.Run("other class", func(t *testing.T) {
		_, settled, _, err := ResolveMechanicalFile(ClassCode, "a.go", "x", "", "")
		if err != nil || settled {
			t.Fatalf("settled=%v err=%v", settled, err)
		}
	})
	t.Run("changelog", func(t *testing.T) {
		c := hunkText("", "m\n", "", "f\n", "")
		got, settled, _, err := ResolveMechanicalFile(ClassChangelog, "CHANGELOG.md", c, "", "")
		if err != nil || !settled || got != "m\nf\n" {
			t.Fatalf("got %q settled=%v err=%v", got, settled, err)
		}
	})
}

func TestMechanicalLocaleRule(t *testing.T) {
	ours := `{"a":"A","ours_new":"O","shared":"ours"}`
	theirs := `{"a":"A","theirs_new":"T","shared":"theirs"}`
	got, settled, notes, err := ResolveMechanicalFile(ClassLocale, "en.json", "", ours, theirs)
	if err != nil || !settled {
		t.Fatalf("settled=%v err=%v", settled, err)
	}
	want := "{\n  \"a\": \"A\",\n  \"ours_new\": \"O\",\n  \"shared\": \"ours\",\n  \"theirs_new\": \"T\"\n}\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], `"shared"`) {
		t.Fatalf("notes %v", notes)
	}
	for _, bad := range [][2]string{{"{", "{}"}, {`{"a":{"b":"c"}}`, "{}"}, {"{}", `{"a":1}`}} {
		_, settled, notes, err := ResolveMechanicalFile(ClassLocale, "en.json", "", bad[0], bad[1])
		if err == nil || settled || len(notes) == 0 {
			t.Fatalf("bad input %v: settled=%v err=%v notes=%v", bad, settled, err, notes)
		}
	}
}

func TestMechanicalGeneratedRule(t *testing.T) {
	c := hunkText("", "ours index\n", "base index\n", "theirs index\n", "")
	got, settled, notes, err := ResolveMechanicalFile(ClassGenerated, "internal/docsindex/assets/index.json", c, "ours index\n", "theirs index\n")
	if err != nil || !settled {
		t.Fatalf("settled=%v err=%v", settled, err)
	}
	if got != "ours index\n" {
		t.Fatalf("got %q want the ours side", got)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "regenerate") {
		t.Fatalf("notes %v", notes)
	}

	if !MakefileHasDocsIndexTarget("build:\n\tgo build\n\ndocs-index: docs\n\tgen\n") {
		t.Fatal("docs-index target not found")
	}
	for _, no := range []string{"", "build:\n\tgo build\n", "# docs-index: note\n", "  docs-index: indented\n"} {
		if MakefileHasDocsIndexTarget(no) {
			t.Fatalf("false positive for %q", no)
		}
	}
}
