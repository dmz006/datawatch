package mergepipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func customDef() Definition {
	d := DefaultDefinition()
	d.Steps = d.Steps[:1]
	return d
}

type fixture struct {
	dir string
	rs  *RecordStore
	set Settings
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	rs, err := NewRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{dir: t.TempDir(), rs: rs, set: DefaultSettings()}
}

func (f *fixture) writeFile(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(DefaultFileName))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) resolver() *Resolver {
	return &Resolver{
		Records:    f.rs,
		ProjectDir: func(string) (string, bool) { return f.dir, true },
		Settings:   func(string) Settings { return f.set },
	}
}

func validFileBody(t *testing.T) string {
	b, err := Marshal(customDef())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestResolve_SourceCombinations(t *testing.T) {
	cases := []struct {
		name      string
		file, rec bool
		want      Source
		wantSteps int
	}{
		{"file and record: file wins", true, true, SourceFile, 1},
		{"file only", true, false, SourceFile, 1},
		{"record only", false, true, SourceRecord, 2},
		{"neither: default", false, false, SourceDefault, len(DefaultDefinition().Steps)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.file {
				f.writeFile(t, validFileBody(t))
			}
			if tc.rec {
				d := DefaultDefinition()
				d.Steps = d.Steps[:2]
				if _, err := f.rs.Set("p", d); err != nil {
					t.Fatal(err)
				}
			}
			res, err := f.resolver().Resolve("p")
			if err != nil {
				t.Fatal(err)
			}
			if res.Source != tc.want || len(res.Definition.Steps) != tc.wantSteps || res.Reason == "" || res.FileError != nil {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestResolve_RecordFirstOrder(t *testing.T) {
	f := newFixture(t)
	f.writeFile(t, validFileBody(t))
	d := DefaultDefinition()
	d.Steps = d.Steps[:2]
	_, _ = f.rs.Set("p", d)
	f.set.SourceOrder = []string{"record", "file"}
	res, err := f.resolver().Resolve("p")
	if err != nil || res.Source != SourceRecord {
		t.Fatalf("got %+v, %v", res, err)
	}
	// With the record absent the file is used.
	_ = f.rs.Delete("p")
	res, _ = f.resolver().Resolve("p")
	if res.Source != SourceFile {
		t.Fatalf("got %+v", res)
	}
}

func TestResolve_FileOnlyOrderIgnoresRecord(t *testing.T) {
	f := newFixture(t)
	_, _ = f.rs.Set("p", customDef())
	f.set.SourceOrder = []string{"file"}
	res, _ := f.resolver().Resolve("p")
	if res.Source != SourceDefault {
		t.Fatalf("got %+v", res)
	}
}

func TestResolve_BadFile(t *testing.T) {
	cases := []struct {
		name, fallback string
		record         bool
		want           Source
		wantErr        bool
	}{
		{"record fallback uses record", FallbackRecord, true, SourceRecord, false},
		{"default setting is record", "", true, SourceRecord, false},
		{"record fallback without record errors", FallbackRecord, false, SourceFile, true},
		{"default fallback", FallbackDefault, true, SourceDefault, false},
		{"none: error, no substitute", FallbackNone, true, SourceFile, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeFile(t, "steps: [unterminated")
			if tc.record {
				_, _ = f.rs.Set("p", customDef())
			}
			f.set.FallbackOnBadFile = tc.fallback
			res, err := f.resolver().Resolve("p")
			var mal *ErrMalformed
			if !errors.As(res.FileError, &mal) {
				t.Fatalf("FileError = %v, want malformed", res.FileError)
			}
			if (err != nil) != tc.wantErr || res.Source != tc.want {
				t.Fatalf("err=%v res=%+v", err, res)
			}
			if tc.wantErr && len(res.Definition.Steps) != 0 {
				t.Fatal("a definition was substituted despite the error")
			}
			if !tc.wantErr && !strings.Contains(res.Reason, "unusable") {
				t.Fatalf("reason does not mention the bad file: %q", res.Reason)
			}
		})
	}
}

func TestResolve_InvalidDefinitionFileIsBad(t *testing.T) {
	f := newFixture(t)
	f.writeFile(t, "steps: []\n")
	if _, err := f.resolver().Resolve("p"); err == nil {
		t.Fatal("an invalid file with no record must be an error")
	}
}

func TestResolve_TooLargeFileIsBad(t *testing.T) {
	f := newFixture(t)
	f.writeFile(t, validFileBody(t))
	f.set.MaxFileBytes = 4
	res, err := f.resolver().Resolve("p")
	var tl *ErrTooLarge
	if err == nil || !errors.As(res.FileError, &tl) {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestResolve_NilPartsAndUnknownProject(t *testing.T) {
	r := &Resolver{}
	res, err := r.Resolve("p")
	if err != nil || res.Source != SourceDefault {
		t.Fatalf("got %+v, %v", res, err)
	}
	r = &Resolver{ProjectDir: func(string) (string, bool) { return "", false }}
	if res, _ := r.Resolve("p"); res.Source != SourceDefault {
		t.Fatalf("got %+v", res)
	}
}

func TestResolve_SettingsDefaultsAndUnknownSource(t *testing.T) {
	f := newFixture(t)
	f.writeFile(t, validFileBody(t))
	f.set = Settings{} // empty file name and order read their defaults
	if res, _ := f.resolver().Resolve("p"); res.Source != SourceFile {
		t.Fatalf("got %+v", res)
	}
	f.set = Settings{SourceOrder: []string{"bogus"}}
	res, _ := f.resolver().Resolve("p")
	if res.Source != SourceDefault || !strings.Contains(res.Reason, "bogus") {
		t.Fatalf("got %+v", res)
	}
	if got := joinNotes(nil); got == "" {
		t.Fatal("joinNotes(nil) empty")
	}
}
