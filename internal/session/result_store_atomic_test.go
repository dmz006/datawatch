package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeResultFS wraps the real fs and injects a failure at a named step.
type fakeResultFS struct {
	osResultFS
	failAt string
	temps  []string
}

type fakeResultFile struct {
	resultFile
	fs *fakeResultFS
}

var errInjected = errors.New("injected")

func (f *fakeResultFS) CreateTemp(dir, pat string) (resultFile, error) {
	rf, err := f.osResultFS.CreateTemp(dir, pat)
	if err == nil {
		f.temps = append(f.temps, rf.Name())
	}
	return &fakeResultFile{rf, f}, err
}
func (f *fakeResultFile) Write(p []byte) (int, error) {
	if f.fs.failAt == "write" {
		return 0, errInjected
	}
	return f.resultFile.Write(p)
}
func (f *fakeResultFile) Sync() error {
	if f.fs.failAt == "sync" {
		return errInjected
	}
	return f.resultFile.Sync()
}
func (f *fakeResultFS) Rename(o, n string) error {
	if f.failAt == "rename" {
		return errInjected
	}
	return f.osResultFS.Rename(o, n)
}

func TestResultStoreAtomicFailureKeepsOldFile(t *testing.T) {
	for _, step := range []string{"write", "sync", "rename"} {
		t.Run(step, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "results.json")
			s, err := NewResultStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Put("a", map[string]any{"v": 1}, 0); err != nil {
				t.Fatal(err)
			}
			old, _ := os.ReadFile(path)

			fake := &fakeResultFS{failAt: step}
			s.fs = fake
			if _, err := s.Put("b", map[string]any{"v": 2}, 0); !errors.Is(err, errInjected) {
				t.Fatalf("want injected error, got %v", err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != string(old) {
				t.Fatalf("old file changed on %s failure", step)
			}
			ents, _ := os.ReadDir(dir)
			for _, e := range ents {
				if e.Name() != "results.json" && e.Name() != "results.json.lock" {
					t.Fatalf("lingering file %s", e.Name())
				}
			}
			for _, tmp := range fake.temps {
				if _, err := os.Stat(tmp); !os.IsNotExist(err) {
					t.Fatalf("temp %s lingers", tmp)
				}
			}
		})
	}
}

func TestResultStoreAtomicSuccessRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	s, _ := NewResultStore(path)
	if _, err := s.Put("a", map[string]any{"v": 1}, 0); err != nil {
		t.Fatal(err)
	}
	s2, err := NewResultStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get("a"); !ok {
		t.Fatal("entry missing after reload")
	}
}
