package mergepipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const customName = "ci/pipeline.yaml"

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFileValid(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, customName, `steps:
  - name: unit
    kind: test
    command: go test ./...
    dir: sub
    timeout: 90s
    on_failure: continue
  - name: ship-it
    kind: ship
    action: merge_branches
`)
	d, err := LoadFile(dir, customName, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Steps) != 2 || d.Steps[0].Timeout != 90*time.Second || d.Steps[0].OnFailure != OnFailureContinue {
		t.Fatalf("bad parse: %+v", d)
	}
}

func TestLoadFileErrors(t *testing.T) {
	cases := []struct {
		name  string
		setup func(dir string)
		file  string
		max   int64
		check func(error) bool
	}{
		{"missing", func(string) {}, customName, 0, func(e error) bool { var x *ErrNotFound; return errors.As(e, &x) }},
		{"malformed yaml", func(d string) { write(t, d, customName, "steps: [unclosed") }, customName, 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
		{"unknown field", func(d string) {
			write(t, d, customName, "steps:\n - name: a\n   kind: test\n   command: x\n   bogus: 1\n")
		}, customName, 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
		{"invalid definition", func(d string) { write(t, d, customName, "steps:\n - name: a\n   kind: nope\n   command: x\n") }, customName, 0, func(e error) bool {
			var x *ErrMalformed
			var v *ValidationError
			return errors.As(e, &x) && errors.As(e, &v)
		}},
		{"empty file", func(d string) { write(t, d, customName, "") }, customName, 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
		{"oversize default cap", func(d string) { write(t, d, customName, strings.Repeat("#", int(DefaultMaxFileBytes)+1)) }, customName, 0, func(e error) bool { var x *ErrTooLarge; return errors.As(e, &x) }},
		{"oversize custom cap", func(d string) { write(t, d, customName, "steps: []\n# "+strings.Repeat("x", 100)) }, customName, 50, func(e error) bool { var x *ErrTooLarge; return errors.As(e, &x) }},
		{"empty file name", func(string) {}, "", 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
		{"escaping file name", func(string) {}, "../x.yaml", 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
		{"absolute file name", func(string) {}, "/etc/x.yaml", 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
		{"path is directory", func(d string) { _ = os.MkdirAll(filepath.Join(d, "adir"), 0o755) }, "adir", 0, func(e error) bool { var x *ErrMalformed; return errors.As(e, &x) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.setup(dir)
			_, err := LoadFile(dir, c.file, c.max)
			if err == nil || !c.check(err) {
				t.Fatalf("unexpected error: %v", err)
			}
			if err.Error() == "" {
				t.Fatal("empty message")
			}
		})
	}
}

func TestLoadFileAtExactCap(t *testing.T) {
	dir := t.TempDir()
	body := "steps:\n - name: a\n   kind: test\n   command: x\n"
	write(t, dir, "p.yaml", body)
	if _, err := LoadFile(dir, "p.yaml", int64(len(body))); err != nil {
		t.Fatalf("file at the cap should load: %v", err)
	}
}

func TestLoadFileNameIsParameter(t *testing.T) {
	dir := t.TempDir()
	body := "steps:\n - name: a\n   kind: test\n   command: x\n"
	write(t, dir, "one.yaml", body)
	if _, err := LoadFile(dir, "one.yaml", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(dir, "two.yaml", 0); err == nil {
		t.Fatal("other name should be missing")
	}
}

func TestErrorUnwrap(t *testing.T) {
	inner := errors.New("boom")
	e := &ErrMalformed{Path: "p", Err: inner}
	if !errors.Is(e, inner) {
		t.Fatal("unwrap")
	}
	_ = (&ErrNotFound{Path: "p"}).Error()
}
