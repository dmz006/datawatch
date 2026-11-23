package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const installManifest = `
name: install-pack
version: 1.2.3
description: A pack for install tests
kinds: [feature]
tasks:
  - title: Review the change
    prompt: prompts/review.md
    verification_script: verify/review.sh
  - title: Summarise the tree
    read_only: true
    report_path: .datawatch/reports/summary.md
`

// installSource writes a pack folder for install tests and returns its path.
func installSource(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	return writePackDir(t, manifest, files)
}

// validInstallFiles holds the files a passing install pack needs.
func validInstallFiles() map[string]string {
	return map[string]string{
		"prompts/review.md": "Review the change.\n",
		"verify/review.sh":  shellScript,
	}
}

func TestInstallCopiesPassingPack(t *testing.T) {
	src := installSource(t, installManifest, validInstallFiles())
	// The fixture writer uses 0644; make the script executable as a real pack would.
	if err := os.Chmod(filepath.Join(src, "verify", "review.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	got, err := Install(src, root)
	if err != nil {
		t.Fatalf("Install: unexpected error: %v", err)
	}

	wantDir := filepath.Join(root, "install-pack", "1.2.3")
	if got.Dir != wantDir {
		t.Errorf("Dir = %q, want %q", got.Dir, wantDir)
	}
	if got.Name != "install-pack" || got.Version != "1.2.3" {
		t.Errorf("identity = %q %q, want install-pack 1.2.3", got.Name, got.Version)
	}
	if got.Digest == "" {
		t.Error("Digest is empty; want the source folder digest")
	}
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceDir != srcAbs {
		t.Errorf("SourceDir = %q, want %q", got.SourceDir, srcAbs)
	}

	for rel, want := range map[string]string{
		"pack.yaml":         installManifest,
		"prompts/review.md": "Review the change.\n",
		"verify/review.sh":  shellScript,
	} {
		data, err := os.ReadFile(filepath.Join(wantDir, rel))
		if err != nil {
			t.Errorf("copied %s: %v", rel, err)
			continue
		}
		if string(data) != want {
			t.Errorf("copied %s = %q, want %q", rel, data, want)
		}
	}

	fi, err := os.Stat(filepath.Join(wantDir, "verify", "review.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("verify/review.sh lost its execute bit: mode %v", fi.Mode())
	}

	// The copy must load and carry the same digest as the source.
	reloaded, err := LoadPackFolder(wantDir)
	if err != nil {
		t.Fatalf("LoadPackFolder(installed copy): %v", err)
	}
	if reloaded.Digest != got.Digest {
		t.Errorf("installed digest = %q, want %q", reloaded.Digest, got.Digest)
	}
}

func TestInstallRefusesRuleFailureNamingRule(t *testing.T) {
	manifest := `
name: bad-pack
version: 1.0.0
tasks:
  - title: Good task
    read_only: true
    report_path: .datawatch/reports/ok.md
  - title: Missing script
    prompt: prompts/review.md
`
	src := installSource(t, manifest, map[string]string{"prompts/review.md": "x\n"})
	root := t.TempDir()

	_, err := Install(src, root)
	if err == nil {
		t.Fatal("Install of a pack with a rule-breaking task: expected error")
	}
	if !strings.Contains(err.Error(), "must name a verification_script") {
		t.Errorf("error = %q, want the verification_script rule named", err)
	}

	// The error text is CheckTaskRules' own text, unchanged.
	pack, lerr := LoadPackFolder(src)
	if lerr != nil {
		t.Fatal(lerr)
	}
	wantErr := CheckTaskRules(pack, pack.Tasks[1])
	if wantErr == nil {
		t.Fatal("test setup: CheckTaskRules passed the bad task")
	}
	if err.Error() != wantErr.Error() {
		t.Errorf("error = %q, want CheckTaskRules text %q", err, wantErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "bad-pack")); !os.IsNotExist(statErr) {
		t.Errorf("refused install left files behind: stat err = %v", statErr)
	}
}

func TestInstallRefusesFirstFailingTask(t *testing.T) {
	manifest := `
name: two-bad
version: 1.0.0
tasks:
  - title: First bad
    read_only: true
    file_scopes: [internal/**]
    report_path: .datawatch/reports/a.md
  - title: Second bad
    read_only: true
`
	src := installSource(t, manifest, nil)
	_, err := Install(src, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `"First bad"`) {
		t.Fatalf("error = %v, want the first failing task named", err)
	}
}

func TestInstallRefusesScriptWithShellSyntax(t *testing.T) {
	manifest := `
name: shell-pack
version: 1.0.0
tasks:
  - title: Build
    prompt: prompts/build.md
    verification_script: go build && go test
`
	src := installSource(t, manifest, map[string]string{"prompts/build.md": "x\n"})
	_, err := Install(src, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "pack rule: verification_script must be a script file under .datawatch/verify/") {
		t.Fatalf("error = %v, want the verification_script rule named", err)
	}
}

func TestInstallRefusesExistingVersion(t *testing.T) {
	src := installSource(t, installManifest, validInstallFiles())
	root := t.TempDir()
	if _, err := Install(src, root); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	_, err := Install(src, root)
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("second Install: error = %v, want already installed", err)
	}
}

func TestInstallRefusesUnsafeName(t *testing.T) {
	for _, name := range []string{"..", "a/b", `a\b`} {
		// Single-quoted YAML keeps backslashes literal.
		manifest := "name: '" + name + "'\nversion: 1.0.0\n"
		src := installSource(t, manifest, nil)
		_, err := Install(src, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "not a safe folder name") {
			t.Errorf("name %q: error = %v, want unsafe name refusal", name, err)
		}
	}
}

func TestInstallRefusesMissingSourceAndBadManifest(t *testing.T) {
	if _, err := Install(filepath.Join(t.TempDir(), "absent"), t.TempDir()); err == nil {
		t.Error("missing source folder: expected error")
	}
	src := installSource(t, "not: [valid", nil)
	if _, err := Install(src, t.TempDir()); err == nil {
		t.Error("unparseable manifest: expected error")
	}
}

func TestInstallRefusesLinkLeavingPack(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := installSource(t, "name: link-pack\nversion: 1.0.0\n", nil)
	if err := os.Symlink(outside, filepath.Join(src, "notes.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Install(src, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "leaves the pack folder") {
		t.Fatalf("error = %v, want link refusal", err)
	}
}

func TestInstallFollowsLinkInsidePack(t *testing.T) {
	src := installSource(t, "name: inner-link\nversion: 1.0.0\n", map[string]string{"docs/real.md": "hello\n"})
	if err := os.Symlink(filepath.Join(src, "docs", "real.md"), filepath.Join(src, "alias.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := t.TempDir()
	got, err := Install(src, root)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(got.Dir, "alias.md"))
	if err != nil {
		t.Fatalf("copied alias: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("alias content = %q, want hello", data)
	}
}
