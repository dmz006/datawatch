package gitguard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFile creates rel under root, making parent directories as needed.
func writeFile(t *testing.T, root, rel string) string {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

// sparseFile creates rel under root with the given logical size, using a
// truncated file so no real disk space is consumed.
func sparseFile(t *testing.T, root, rel string, size int64) string {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestKeepSizeCap(t *testing.T) {
	const mb = 1024 * 1024
	tests := []struct {
		name       string
		size       int64
		wantKept   bool
		wantReason string
	}{
		{name: "51MB skipped", size: 51 * mb, wantKept: false, wantReason: "size"},
		{name: "50MB kept", size: 50 * mb, wantKept: true},
		{name: "small kept", size: 10, wantKept: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			rel := sparseFile(t, root, "blob.dat", tt.size)
			kept, skipped := Default.Keep(root, []string{rel})
			if tt.wantKept {
				if !reflect.DeepEqual(kept, []string{rel}) || len(skipped) != 0 {
					t.Fatalf("kept=%v skipped=%v, want %q kept", kept, skipped, rel)
				}
				return
			}
			if len(kept) != 0 || len(skipped) != 1 {
				t.Fatalf("kept=%v skipped=%v, want one skip", kept, skipped)
			}
			if skipped[0] != (Skip{File: rel, Reason: tt.wantReason}) {
				t.Fatalf("skip = %+v, want reason %q", skipped[0], tt.wantReason)
			}
		})
	}
}

func TestKeepIgnoreDirs(t *testing.T) {
	required := []string{".cache", ".ollama", ".gradle", ".android", "go", "snap", "node_modules", "model", "models"}
	for _, dir := range required {
		if !contains(Default.IgnoreDirs, dir) {
			t.Fatalf("Default.IgnoreDirs missing required entry %q", dir)
		}
	}
	for _, dir := range Default.IgnoreDirs {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			rel := writeFile(t, root, filepath.Join(dir, "x.txt"))
			kept, skipped := Default.Keep(root, []string{rel})
			if len(kept) != 0 || len(skipped) != 1 || skipped[0].Reason != "ignore-dir" {
				t.Fatalf("kept=%v skipped=%v, want ignore-dir skip", kept, skipped)
			}
		})
	}
}

func TestKeepIgnorePatterns(t *testing.T) {
	required := []string{"*.gguf", "*.safetensors", "*.bin"}
	for _, pat := range required {
		if !contains(Default.IgnorePatterns, pat) {
			t.Fatalf("Default.IgnorePatterns missing required entry %q", pat)
		}
	}
	for _, pat := range Default.IgnorePatterns {
		t.Run(pat, func(t *testing.T) {
			root := t.TempDir()
			rel := writeFile(t, root, strings.Replace(pat, "*", "weights", 1))
			kept, skipped := Default.Keep(root, []string{rel})
			if len(kept) != 0 || len(skipped) != 1 || skipped[0].Reason != "pattern" {
				t.Fatalf("kept=%v skipped=%v, want pattern skip", kept, skipped)
			}
		})
	}
}

func TestKeepTable(t *testing.T) {
	tests := []struct {
		name       string
		files      []string // created under the repo before Keep runs
		candidates []string
		wantKept   []string
		wantSkip   []Skip
	}{
		{
			name:       "nested ignored dir component",
			files:      []string{"a/b/.cache/x"},
			candidates: []string{"a/b/.cache/x"},
			wantSkip:   []Skip{{File: "a/b/.cache/x", Reason: "ignore-dir"}},
		},
		{
			name:       "relative path resolved against repo",
			files:      []string{"sub/keep.go"},
			candidates: []string{"sub/keep.go"},
			wantKept:   []string{"sub/keep.go"},
		},
		{
			name:       "missing file kept, not an error",
			candidates: []string{"gone.go"},
			wantKept:   []string{"gone.go"},
		},
		{
			name:       "input order preserved across kept and skipped",
			files:      []string{"c.go", "a.go", "b.gguf", "d.go"},
			candidates: []string{"c.go", "a.go", "b.gguf", "d.go"},
			wantKept:   []string{"c.go", "a.go", "d.go"},
			wantSkip:   []Skip{{File: "b.gguf", Reason: "pattern"}},
		},
		{
			name:       "go directory inside the repo is ignored",
			files:      []string{"go/proj/main.go"},
			candidates: []string{"go/proj/main.go"},
			wantSkip:   []Skip{{File: "go/proj/main.go", Reason: "ignore-dir"}},
		},
		{
			name:       "ignored dir is checked before pattern",
			files:      []string{"node_modules/lib.gguf"},
			candidates: []string{"node_modules/lib.gguf"},
			wantSkip:   []Skip{{File: "node_modules/lib.gguf", Reason: "ignore-dir"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for _, f := range tt.files {
				writeFile(t, root, f)
			}
			kept, skipped := Default.Keep(root, tt.candidates)
			if !reflect.DeepEqual(kept, tt.wantKept) {
				t.Fatalf("kept = %v, want %v", kept, tt.wantKept)
			}
			if !reflect.DeepEqual(skipped, tt.wantSkip) {
				t.Fatalf("skipped = %v, want %v", skipped, tt.wantSkip)
			}
		})
	}
}

func TestKeepAbsoluteUnderRepo(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "go", "proj")
	rel := writeFile(t, repo, "main.go")
	abs := filepath.Join(repo, rel)
	kept, skipped := Default.Keep(repo, []string{abs})
	if !reflect.DeepEqual(kept, []string{abs}) || len(skipped) != 0 {
		t.Fatalf("kept=%v skipped=%v, want absolute path kept", kept, skipped)
	}
}

func TestRefuseReason(t *testing.T) {
	tests := []struct {
		name    string
		policy  Policy
		n       int
		wantMsg bool
	}{
		{name: "default under limit", policy: Default, n: 42, wantMsg: false},
		{name: "default at limit", policy: Default, n: 10000, wantMsg: false},
		{name: "default over limit", policy: Default, n: 10001, wantMsg: true},
		{name: "no limit", policy: Policy{}, n: 1000000, wantMsg: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.policy.RefuseReason(tt.n)
			if !tt.wantMsg {
				if msg != "" {
					t.Fatalf("RefuseReason(%d) = %q, want empty", tt.n, msg)
				}
				return
			}
			for _, want := range []string{"auto-commit refused", "10001", "10000"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("RefuseReason(%d) = %q, missing %q", tt.n, msg, want)
				}
			}
		})
	}
}

func TestCleanTempPacks(t *testing.T) {
	repo := t.TempDir()
	pack := filepath.Join(repo, ".git", "objects", "pack")
	if err := os.MkdirAll(pack, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"tmp_pack_ab12",
		"leftover-1234.pack",
		"commit-1.pack",
		"commit-1.idx",
		"revonly.pack",
		"revonly.rev",
		"notes.txt",
	} {
		writeFile(t, filepath.Join(repo, ".git", "objects", "pack"), name)
	}

	removed, err := CleanTempPacks(repo)
	if err != nil {
		t.Fatalf("CleanTempPacks: %v", err)
	}
	wantRemoved := []string{
		filepath.Join(pack, "leftover-1234.pack"),
		filepath.Join(pack, "tmp_pack_ab12"),
	}
	if !sameSet(removed, wantRemoved) {
		t.Fatalf("removed = %v, want %v", removed, wantRemoved)
	}
	for _, name := range []string{"commit-1.pack", "commit-1.idx", "revonly.pack", "revonly.rev", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(pack, name)); err != nil {
			t.Fatalf("%s should be untouched: %v", name, err)
		}
	}
	for _, name := range []string{"tmp_pack_ab12", "leftover-1234.pack"} {
		if _, err := os.Stat(filepath.Join(pack, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s should be removed, stat err = %v", name, err)
		}
	}

	again, err := CleanTempPacks(repo)
	if err != nil || len(again) != 0 {
		t.Fatalf("second call removed=%v err=%v, want nothing and nil", again, err)
	}
}

func TestCleanTempPacksMissingDir(t *testing.T) {
	removed, err := CleanTempPacks(t.TempDir())
	if err != nil || removed != nil {
		t.Fatalf("missing pack dir: removed=%v err=%v, want nil and nil", removed, err)
	}
}

func TestCleanTempPacksRefusesSymlinkEscape(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, writeFile(t, outside, "tmp_pack_victim"))
	if err := os.MkdirAll(filepath.Join(repo, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".git", "objects", "pack")); err != nil {
		t.Skip(err)
	}
	removed, err := CleanTempPacks(repo)
	if !errors.Is(err, ErrPathEscapes) || len(removed) != 0 {
		t.Fatalf("removed=%v err=%v, want nothing and ErrPathEscapes", removed, err)
	}
	if _, serr := os.Stat(victim); serr != nil {
		t.Fatalf("file outside the repository must survive: %v", serr)
	}
}

func TestInHome(t *testing.T) {
	home := t.TempDir()
	sub := filepath.Join(home, "projects", "datawatch")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	sibling := home + "-sibling"
	resolver := func() (string, error) { return home, nil }

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "dir equals home", dir: home, want: true},
		{name: "subdirectory of home", dir: sub, want: true},
		{name: "outside home", dir: other, want: false},
		{name: "prefix sibling of home is not inside", dir: sibling, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InHome(tt.dir, resolver)
			if err != nil {
				t.Fatalf("InHome: %v", err)
			}
			if got != tt.want {
				t.Fatalf("InHome(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}

	t.Run("resolver error propagates", func(t *testing.T) {
		boom := errors.New("no home")
		if _, err := InHome(home, func() (string, error) { return "", boom }); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}

func TestKeepRefusesPathOutsideRoot(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "in.go")
	outside := t.TempDir()
	writeFile(t, outside, "out.go")
	files := []string{"in.go", filepath.Join(outside, "out.go"), "../escape.go"}
	kept, skipped := Default.Keep(repo, files)
	if !reflect.DeepEqual(kept, []string{"in.go"}) {
		t.Fatalf("kept=%v, want only the file under the repository", kept)
	}
	want := []Skip{
		{File: filepath.Join(outside, "out.go"), Reason: "outside-root"},
		{File: "../escape.go", Reason: "outside-root"},
	}
	if !reflect.DeepEqual(skipped, want) {
		t.Fatalf("skipped=%v, want %v", skipped, want)
	}
}

func TestKeepKeepsSymlinkLeafPointingOut(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "target.txt")
	if err := os.Symlink(filepath.Join(outside, "target.txt"), filepath.Join(repo, "link.txt")); err != nil {
		t.Skip(err)
	}
	kept, skipped := Default.Keep(repo, []string{"link.txt"})
	if !reflect.DeepEqual(kept, []string{"link.txt"}) || len(skipped) != 0 {
		t.Fatalf("kept=%v skipped=%v, want the link kept", kept, skipped)
	}
}

func TestKeepRefusesSymlinkDirEscape(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "f.txt")
	if err := os.Symlink(outside, filepath.Join(repo, "dir")); err != nil {
		t.Skip(err)
	}
	kept, skipped := Default.Keep(repo, []string{"dir/f.txt"})
	if len(kept) != 0 || len(skipped) != 1 || skipped[0].Reason != "outside-root" {
		t.Fatalf("kept=%v skipped=%v, want outside-root skip", kept, skipped)
	}
}

func TestReadDirWithinRefusesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt")
	if entries, err := readDirWithin(root, outside); !errors.Is(err, ErrPathEscapes) || entries != nil {
		t.Fatalf("entries=%v err=%v, want ErrPathEscapes and no entries", entries, err)
	}
	writeFile(t, root, "sub/a.txt")
	entries, err := readDirWithin(root, filepath.Join(root, "sub"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "a.txt" {
		t.Fatalf("entries=%v err=%v, want a.txt listed", entries, err)
	}
}

func TestRemoveWithinRefusesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, writeFile(t, outside, "victim.txt"))
	if err := removeWithin(root, victim); !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("err=%v, want ErrPathEscapes", err)
	}
	if err := removeWithin(root, filepath.Join(root, "..", filepath.Base(outside), "victim.txt")); !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("dotdot err=%v, want ErrPathEscapes", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("file outside root was removed: %v", err)
	}
	inside := filepath.Join(root, writeFile(t, root, "x.txt"))
	if err := removeWithin(root, inside); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inside file still present: %v", err)
	}
}

func TestRemoveWithinRemovesSymlinkLeafNotTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, writeFile(t, outside, "t.txt"))
	link := filepath.Join(root, "tmp_pack_link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if err := removeWithin(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link still present: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("link target was removed: %v", err)
	}
}
