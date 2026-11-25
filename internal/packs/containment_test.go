package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

const containmentManifest = `
name: contained
version: 1.0.0
kinds: [feature]
tasks:
  - title: Review the change
    prompt: prompts/review.md
    verification_script: verify/review.sh
`

// A name folder that links elsewhere must not let Remove delete what it
// points at.
func TestRemoveRefusesLinkedNameFolder(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	victim := filepath.Join(outside, "1.0.0", "keep.txt")
	writeFile(t, victim, "keep")
	linkOrSkip(t, outside, filepath.Join(root, "evil"))

	if err := Remove("evil", "1.0.0", root); err == nil {
		t.Error("Remove through a linked name folder: want an error")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a file outside the install root was deleted: %v", err)
	}
}

func TestRemoveMissingRootReadsAsNotInstalled(t *testing.T) {
	err := Remove("a", "1.0.0", filepath.Join(t.TempDir(), "absent"))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v, want not installed", err)
	}
}

// pack.yaml and prompt files are read from the pack folder only. A link that
// leaves it is refused, and nothing of the target shows in the error.
func TestLoadPackFolderRefusesLinksLeavingPack(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	writeFile(t, secret, "zq9secret")

	manifestLink := t.TempDir()
	linkOrSkip(t, secret, filepath.Join(manifestLink, "pack.yaml"))
	if _, err := LoadPackFolder(manifestLink); err == nil {
		t.Error("linked pack.yaml: want an error")
	} else if strings.Contains(err.Error(), "zq9") {
		t.Errorf("the error shows the target's content: %v", err)
	}

	for _, rel := range []string{"absolute", "relative"} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "pack.yaml"), containmentManifest)
		if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
			t.Fatal(err)
		}
		target := secret
		if rel == "relative" {
			var err error
			if target, err = filepath.Rel(filepath.Join(dir, "prompts"), secret); err != nil {
				t.Fatal(err)
			}
		}
		linkOrSkip(t, target, filepath.Join(dir, "prompts", "review.md"))
		if _, err := LoadPackFolder(dir); err == nil {
			t.Errorf("%s prompt link leaving the pack: want an error", rel)
		}
	}
}

// A verify folder that links out of the pack must not satisfy the rule that
// the script lives under the pack's verify/.
func TestCheckTaskRulesRefusesVerifyFolderLeavingPack(t *testing.T) {
	outside, dir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "review.sh"), "#!/bin/sh\n")
	linkOrSkip(t, outside, filepath.Join(dir, "verify"))
	task := PackTask{Title: "t", VerificationScript: "verify/review.sh"}
	if err := CheckTaskRules(&Pack{Dir: dir}, task); err == nil {
		t.Fatal("verify folder leaving the pack: want an error")
	}
}

// Links that stay inside the pack keep working, absolute or relative, in the
// load, the rule checks and the copy; so does a pack folder reached through a
// link.
func TestPackLinksInsideStillWork(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real", "pack.yaml"), containmentManifest)
	writeFile(t, filepath.Join(dir, "real", "shared", "review.md"), "prompt")
	writeFile(t, filepath.Join(dir, "real", "shared", "review.sh"), "#!/bin/sh\n")
	real := filepath.Join(dir, "real")
	for _, d := range []string{"prompts", "verify"} {
		if err := os.MkdirAll(filepath.Join(real, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	linkOrSkip(t, filepath.Join(real, "shared", "review.md"), filepath.Join(real, "prompts", "review.md"))
	linkOrSkip(t, filepath.Join("..", "shared", "review.sh"), filepath.Join(real, "shared", "alias.sh"))
	writeFile(t, filepath.Join(real, "verify", "review.sh"), "#!/bin/sh\n")
	linkOrSkip(t, real, filepath.Join(dir, "via-link"))

	root := t.TempDir()
	inst, err := Install(filepath.Join(dir, "real"), root)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(inst.Dir, "prompts", "review.md"))
	if err != nil || string(got) != "prompt" {
		t.Fatalf("installed prompt: %q %v", got, err)
	}
	if _, err := LoadPackFolder(filepath.Join(dir, "via-link")); err != nil {
		t.Fatalf("pack folder reached through a link: %v", err)
	}
}
