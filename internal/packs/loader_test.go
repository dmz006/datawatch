package packs

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// writePackDir creates a temporary pack folder containing pack.yaml plus the
// given files (relative path → content, subdirectories created as needed).
func writePackDir(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("writing pack.yaml: %v", err)
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("creating %q: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %q: %v", rel, err)
		}
	}
	return dir
}

const loaderManifest = `
name: test-pack
version: 1.0.0
description: A pack for loader tests
kinds: [feature]
tasks:
  - title: Review the change
    prompt: prompts/review.md
  - title: Deep check
    prompt: prompts/sub/deep.md
  - title: No prompt file
`

func TestLoadPackFolderValid(t *testing.T) {
	dir := writePackDir(t, loaderManifest, map[string]string{
		"prompts/review.md":   "review prompt",
		"prompts/sub/deep.md": "deep prompt",
		"verify/review.sh":    "echo ok",
	})

	p, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: unexpected error: %v", err)
	}
	if p.Name != "test-pack" || p.Version != "1.0.0" || p.Description != "A pack for loader tests" {
		t.Errorf("pack identity = %q / %q / %q", p.Name, p.Version, p.Description)
	}
	if len(p.Kinds) != 1 || p.Kinds[0] != KindFeature {
		t.Errorf("Kinds = %v", p.Kinds)
	}
	if len(p.Tasks) != 3 || p.Tasks[0].Title != "Review the change" || p.Tasks[0].Prompt != "prompts/review.md" {
		t.Errorf("Tasks = %+v", p.Tasks)
	}
	if !digestPattern.MatchString(p.Digest) {
		t.Errorf("Digest = %q, want a 64-char hex sha256", p.Digest)
	}
}

const digestManifest = "name: pack\nversion: 1.0.0\ntasks:\n  - title: Review\n    prompt: prompts/review.md\n"

func TestLoadPackFolderDigest(t *testing.T) {
	files := map[string]string{
		"prompts/review.md": "review prompt",
		"data/notes.md":     "notes",
	}

	base := writePackDir(t, digestManifest, files)
	pBase, err := LoadPackFolder(base)
	if err != nil {
		t.Fatalf("LoadPackFolder(base): %v", err)
	}

	// An identical folder in a different location has the same digest.
	same := writePackDir(t, digestManifest, files)
	pSame, err := LoadPackFolder(same)
	if err != nil {
		t.Fatalf("LoadPackFolder(same): %v", err)
	}
	if pSame.Digest != pBase.Digest {
		t.Errorf("identical folders: digest %q != %q", pSame.Digest, pBase.Digest)
	}

	// Editing a file changes the digest.
	changed := map[string]string{
		"prompts/review.md": "a different review prompt",
		"data/notes.md":     "notes",
	}
	pChanged, err := LoadPackFolder(writePackDir(t, digestManifest, changed))
	if err != nil {
		t.Fatalf("LoadPackFolder(changed): %v", err)
	}
	if pChanged.Digest == pBase.Digest {
		t.Error("editing a file did not change the digest")
	}

	// Adding a file changes the digest.
	added := map[string]string{
		"prompts/review.md": "review prompt",
		"data/notes.md":     "notes",
		"data/extra.md":     "extra",
	}
	pAdded, err := LoadPackFolder(writePackDir(t, digestManifest, added))
	if err != nil {
		t.Fatalf("LoadPackFolder(added): %v", err)
	}
	if pAdded.Digest == pBase.Digest {
		t.Error("adding a file did not change the digest")
	}

	// Renaming a file (same content, different relative path) changes it.
	renamed := map[string]string{
		"prompts/review.md": "review prompt",
		"data/moved.md":     "notes",
	}
	pRenamed, err := LoadPackFolder(writePackDir(t, digestManifest, renamed))
	if err != nil {
		t.Fatalf("LoadPackFolder(renamed): %v", err)
	}
	if pRenamed.Digest == pBase.Digest {
		t.Error("renaming a file did not change the digest")
	}

	// Editing pack.yaml itself changes the digest.
	pManifest, err := LoadPackFolder(writePackDir(t, "name: pack\nversion: 1.0.0\ndescription: different\ntasks:\n  - title: Review\n    prompt: prompts/review.md\n", files))
	if err != nil {
		t.Fatalf("LoadPackFolder(manifest): %v", err)
	}
	if pManifest.Digest == pBase.Digest {
		t.Error("editing pack.yaml did not change the digest")
	}
}

func TestLoadPackFolderDigestIsHexSha256(t *testing.T) {
	dir := writePackDir(t, loaderManifest, map[string]string{
		"prompts/review.md":   "review prompt",
		"prompts/sub/deep.md": "deep prompt",
	})
	p, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: %v", err)
	}
	if _, err := hex.DecodeString(p.Digest); err != nil {
		t.Errorf("Digest %q is not valid hex: %v", p.Digest, err)
	}
}

func TestLoadPackFolderRejectsBadDirectory(t *testing.T) {
	// Missing directory.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := LoadPackFolder(missing); err == nil {
		t.Error("missing directory: expected error, got pack")
	} else if !strings.Contains(err.Error(), "pack folder:") {
		t.Errorf("missing directory: error %q missing prefix", err)
	}

	// A regular file is not a pack folder.
	plain := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(plain, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPackFolder(plain); err == nil {
		t.Error("regular file: expected error, got pack")
	} else if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("regular file: error = %q, want it to name the problem", err)
	}
}

func TestLoadPackFolderManifestProblems(t *testing.T) {
	// No pack.yaml at all.
	empty := t.TempDir()
	if _, err := LoadPackFolder(empty); err == nil {
		t.Error("missing pack.yaml: expected error, got pack")
	}

	// pack.yaml that fails manifest validation.
	dir := writePackDir(t, "version: 1.0.0\n", nil)
	if _, err := LoadPackFolder(dir); err == nil {
		t.Error("invalid manifest: expected error, got pack")
	} else if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("invalid manifest: error = %q, want manifest error", err)
	}
}

func TestLoadPackFolderPromptPathProblems(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		files    map[string]string
		outside  string // file created in a sibling of the pack folder
		wantErr  string
	}{
		{
			name:     "absolute prompt path",
			manifest: "name: pack\nversion: 1.0.0\ntasks:\n  - title: A\n    prompt: /etc/passwd\n",
			wantErr:  "must be relative to the pack folder",
		},
		{
			name:     "prompt escapes the folder",
			manifest: "name: pack\nversion: 1.0.0\ntasks:\n  - title: A\n    prompt: ../evil.md\n",
			files:    map[string]string{"prompts/kept.md": "kept"},
			outside:  "evil.md",
			wantErr:  "escapes the pack folder",
		},
		{
			name:     "prompt is dotdot alone",
			manifest: "name: pack\nversion: 1.0.0\ntasks:\n  - title: A\n    prompt: ..\n",
			wantErr:  "escapes the pack folder",
		},
		{
			name:     "prompt file missing",
			manifest: "name: pack\nversion: 1.0.0\ntasks:\n  - title: A\n    prompt: prompts/absent.md\n",
			wantErr:  "prompts/absent.md",
		},
		{
			name:     "prompt path is a directory",
			manifest: "name: pack\nversion: 1.0.0\ntasks:\n  - title: A\n    prompt: prompts\n",
			files:    map[string]string{"prompts/real.md": "real"},
			wantErr:  "is not a regular file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packDir := filepath.Join(t.TempDir(), "pack")
			if err := os.MkdirAll(packDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(packDir, "pack.yaml"), []byte(tc.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			for rel, content := range tc.files {
				p := filepath.Join(packDir, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.outside != "" {
				outside := filepath.Join(filepath.Dir(packDir), tc.outside)
				if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			p, err := LoadPackFolder(packDir)
			if err == nil {
				t.Fatalf("expected error containing %q, got pack %+v", tc.wantErr, p)
			}
			if p != nil {
				t.Errorf("returned pack alongside error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadPackFolderAcceptsTaskWithoutPrompt(t *testing.T) {
	dir := writePackDir(t, "name: pack\nversion: 1.0.0\ntasks:\n  - title: No prompt\n", nil)
	p, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: unexpected error: %v", err)
	}
	if len(p.Tasks) != 1 || p.Tasks[0].Title != "No prompt" {
		t.Errorf("Tasks = %+v", p.Tasks)
	}
	if p.Digest == "" {
		t.Error("Digest is empty, want a digest over the folder's files")
	}
}

func TestLoadPackFolderStopsAtFirstBadPrompt(t *testing.T) {
	// Two tasks, first bad: the error must name the first task.
	dir := writePackDir(t,
		"name: pack\nversion: 1.0.0\ntasks:\n  - title: Bad\n    prompt: prompts/absent.md\n  - title: Good\n    prompt: prompts/good.md\n",
		map[string]string{"prompts/good.md": "good"})
	_, err := LoadPackFolder(dir)
	if err == nil {
		t.Fatal("expected error, got pack")
	}
	if !strings.Contains(err.Error(), "Bad") || !strings.Contains(err.Error(), "prompts/absent.md") {
		t.Errorf("error = %q, want it to name the failing task and prompt", err)
	}
}
