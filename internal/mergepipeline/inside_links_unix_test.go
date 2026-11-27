//go:build unix

package mergepipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Files read and written by merge steps are kept inside the project.

const (
	outVersion   = "var Version = \"1.0.0\"\n"
	outChangelog = "# Changelog\n\n## [Unreleased]\n\n## v1.0.0 — outside-marker\n"
	outReadme    = "**Current release: v1.0.0 (2026-01-01).** outside-marker\n"
	outHunk      = "<<<<<<< ours\noutside-marker\n||||||| base\n=======\nbranch line\n>>>>>>> theirs\n"
)

// linkOut makes a project whose file name links to a file beside the project
// holding body. viaDir makes the link a folder on the way to the file.
func linkOut(t *testing.T, name, body string, viaDir bool) (proj, outside string) {
	t.Helper()
	root := t.TempDir()
	proj = filepath.Join(root, "proj")
	other := filepath.Join(root, "other")
	for _, d := range []string{proj, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	outside = filepath.Join(other, filepath.Base(name))
	writeFile(t, other, filepath.Base(name), body)
	link, target := filepath.Join(proj, name), filepath.Join("..", "other", filepath.Base(name))
	if viaDir {
		link, target = filepath.Join(proj, filepath.Dir(name)), filepath.Join("..", "other")
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return proj, outside
}

func wantUnchanged(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("a file outside the project was changed: %q", got)
	}
}

func TestLinksReleaseFilesStayInside(t *testing.T) {
	cases := []struct {
		name, file, body string
		viaDir           bool
		run              func(r *Runner) (string, error)
	}{
		{"read version", "version.go", outVersion, false, func(r *Runner) (string, error) { return r.currentVersion() }},
		{"version carried", "version.go", outVersion, false, func(r *Runner) (string, error) {
			_, err := r.versionFilesCarry("1.0.0")
			return "", err
		}},
		{"write version", "version.go", outVersion, false, func(r *Runner) (string, error) { return "", r.writeVersion("9.9.9") }},
		{"write version through a folder", "pkg/version.go", outVersion, true, func(r *Runner) (string, error) { return "", r.writeVersion("9.9.9") }},
		{"changelog heading", "CHANGELOG.md", outChangelog, false, func(r *Runner) (string, error) { return r.insertChangelog("9.9.9", "t") }},
		{"release entry", "CHANGELOG.md", outChangelog, false, func(r *Runner) (string, error) {
			s := &runState{r: r}
			return s.insertReleaseEntry("9.9.9")
		}},
		{"readme line", "README.md", outReadme, false, func(r *Runner) (string, error) {
			s := &runState{r: r}
			s.run.ReleaseVersion = "9.9.9"
			return s.setReadmeReleaseLine()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proj, outside := linkOut(t, c.file, c.body, c.viaDir)
			r := &Runner{Dir: proj}
			if strings.HasSuffix(c.file, ".go") {
				r.VersionFiles = []string{c.file}
			}
			got, err := c.run(r)
			if err == nil {
				t.Errorf("the link out of the project was followed (result %q)", got)
			}
			wantUnchanged(t, outside, c.body)
		})
	}
}

func TestLinksValidatorReadsStayInside(t *testing.T) {
	for _, c := range []struct {
		file, body string
		viaDir     bool
	}{
		{"version.go", outVersion, false},
		{"pkg/version.go", outVersion, true},
		{"CHANGELOG.md", outChangelog, false},
		{"README.md", outReadme, false},
	} {
		proj, _ := linkOut(t, c.file, c.body, c.viaDir)
		if data, err := readRepoFile(proj, c.file); err == nil {
			t.Errorf("%s: the validator read outside the project: %q", c.file, data)
		}
	}
}

func TestLinksConflictFileReadStaysInside(t *testing.T) {
	proj, outside := linkOut(t, "a.txt", outHunk, false)
	m := &Merger{Dir: proj}
	texts, _, remaining, err := m.mechanical(context.Background(), proj, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 0 || len(remaining) != 1 || !strings.Contains(remaining[0], "unreadable") {
		t.Errorf("texts = %q, remaining = %q; want the file left as unreadable", texts, remaining)
	}
	wantUnchanged(t, outside, outHunk)
}

func TestLinksRebuiltFileReadStaysInside(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "outside.txt", "outside-marker\n")
	dir := conflictRepo(t, "gen.txt", "base\n", "main\n", "feature\n")
	// The rebuild leaves a link where the generated file was.
	writeFile(t, dir, "Makefile", "docs-index:\n\tln -sfn "+filepath.Join(root, "outside.txt")+" gen.txt\n")
	m := &Merger{Dir: dir}
	texts := map[string]string{"gen.txt": "kept\n"}
	_, err := m.regenerateDocsIndex(context.Background(), dir, texts, []string{"gen.txt"})
	if err == nil || strings.Contains(texts["gen.txt"], "outside-marker") {
		t.Errorf("err = %v, text = %q; want the read refused", err, texts["gen.txt"])
	}
}

func TestLinksStagedTextStaysInside(t *testing.T) {
	proj, outside := linkOut(t, "a.txt", "keep\n", false)
	gitIn(t, proj, "init", "-q", "-b", "main")
	m := &Merger{Dir: proj}
	if err := m.stageTexts(context.Background(), proj, map[string]string{"a.txt": "new\n"}); err == nil {
		t.Error("the link out of the project was followed")
	}
	wantUnchanged(t, outside, "keep\n")
}

// linkConflictRepo makes a repository where main and feature each changed
// name, a link, to a different target. main's leads out of the project.
func linkConflictRepo(t *testing.T, name, outsideBody string) (dir, outside string) {
	t.Helper()
	root := t.TempDir()
	dir, outside = filepath.Join(root, "proj"), filepath.Join(root, "outside.txt")
	writeFile(t, root, "outside.txt", outsideBody)
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	up, err := filepath.Rel(filepath.Dir(full), outside)
	if err != nil {
		t.Fatal(err)
	}
	link := func(target string) {
		_ = os.Remove(full)
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "-q", "-m", "link")
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "Makefile", "docs-index:\n\ttrue\n")
	link("base-target")
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	link("feature-target")
	gitIn(t, dir, "checkout", "-q", "main")
	link(up)
	return dir, outside
}

// The same through a whole merge of made-up branches.
func TestLinksWholeMergeStaysInside(t *testing.T) {
	dir, outside := linkConflictRepo(t, docsAssetDir+"index.json", "keep\n")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	o, err := merger(dir, PolicyMechanical, nil, newQuestions(t), "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	t.Logf("outcome %s, err %v", o.Status, err)
	wantUnchanged(t, outside, "keep\n")
	if o.Status != StatusAsked || gitIn(t, dir, "rev-parse", "HEAD") != head {
		t.Errorf("status = %s, want the operator asked and HEAD unmoved", o.Status)
	}
}

func TestLinksModelPromptStaysInside(t *testing.T) {
	dir, outside := linkConflictRepo(t, "a.txt", outHunk)
	model := &fakeModel{reply: "no"}
	o, err := merger(dir, PolicyMechanicalAndModel, model, newQuestions(t), "true").MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	t.Logf("outcome %s, err %v", o.Status, err)
	for _, p := range model.prompts {
		if strings.Contains(p, "outside-marker") {
			t.Error("text from outside the project was put in the model prompt")
		}
	}
	wantUnchanged(t, outside, outHunk)
}

func TestLinksMakefileReadStaysInside(t *testing.T) {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		proj, _ := linkOut(t, name, "build:\ndocs-index:\n", false)
		if makefileHasTarget(proj, "build") {
			t.Errorf("%s: a target was read from outside the project", name)
		}
		if got := projectMakefile(proj); got != "" {
			t.Errorf("%s: text read from outside the project: %q", name, got)
		}
	}
}

// Links that stay in the project work as before: relative ones, absolute
// ones, and a project folder that is itself reached through a link.
func TestLinksInsideTheProjectStillWork(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "linked project folder"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			proj := filepath.Join(root, "proj")
			if err := os.MkdirAll(filepath.Join(proj, "real"), 0o700); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(proj, "real")
			for name, body := range map[string]string{
				"version.go": outVersion, "CHANGELOG.md": outChangelog, "README.md": outReadme,
				"Makefile": "build:\ndocs-index:\n", "a.txt": outHunk,
			} {
				writeFile(t, real, name, body)
				target := filepath.Join("real", name)
				if kind == "absolute" {
					target = filepath.Join(real, name)
				}
				if err := os.Symlink(target, filepath.Join(proj, name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("real", filepath.Join(proj, "pkg")); err != nil {
				t.Fatal(err)
			}
			dir := proj
			if kind == "linked project folder" {
				dir = filepath.Join(root, "alias")
				if err := os.Symlink("proj", dir); err != nil {
					t.Fatal(err)
				}
			}
			r := &Runner{Dir: dir, VersionFiles: []string{"version.go", "pkg/version.go"}}
			if v, err := r.currentVersion(); err != nil || v != "1.0.0" {
				t.Fatalf("version = %q, %v", v, err)
			}
			if err := r.writeVersion("1.0.1"); err != nil {
				t.Fatal(err)
			}
			s := &runState{r: r}
			s.run.ReleaseVersion = "1.0.1"
			if _, err := s.insertReleaseEntry("1.0.1"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.setReadmeReleaseLine(); err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{"version.go": "1.0.1", "CHANGELOG.md": "## v1.0.1 ", "README.md": "v1.0.1 ("} {
				if got := readFile(t, real, name); !strings.Contains(got, want) {
					t.Errorf("%s was not written: %q", name, got)
				}
				if data, err := readRepoFile(dir, name); err != nil || !strings.Contains(string(data), want) {
					t.Errorf("validator read of %s = %q, %v", name, data, err)
				}
				if st, err := os.Lstat(filepath.Join(proj, name)); err != nil || st.Mode()&os.ModeSymlink == 0 {
					t.Errorf("%s is no longer a link", name)
				}
			}
			if !makefileHasTarget(dir, "build") || !MakefileHasDocsIndexTarget(projectMakefile(dir)) {
				t.Error("the linked Makefile was not read")
			}
			m := &Merger{Dir: dir}
			texts, _, remaining, err := m.mechanical(context.Background(), dir, []string{"a.txt"})
			if err != nil || len(remaining) != 0 || !strings.Contains(texts["a.txt"], "branch line") {
				t.Fatalf("mechanical = %q, %q, %v", texts, remaining, err)
			}
			gitIn(t, proj, "init", "-q", "-b", "main")
			if err := m.stageTexts(context.Background(), dir, texts); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, real, "a.txt"); got != texts["a.txt"] {
				t.Errorf("a.txt = %q", got)
			}
		})
	}
}
