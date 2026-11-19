package autonomous

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func withFakeShellCheck(t *testing.T, fn func(interp, text string) (string, error)) {
	t.Helper()
	orig := preflightRunShellCheck
	preflightRunShellCheck = fn
	t.Cleanup(func() { preflightRunShellCheck = orig })
}

func TestVerifyScriptPreflightSyntaxFakeError(t *testing.T) {
	var got string
	withFakeShellCheck(t, func(interp, text string) (string, error) {
		got = interp
		return "/x/script.sh: line 4: syntax error: unexpected end of file\n", errors.New("exit status 2")
	})
	f := preflightSyntaxFindings("scripts/verify.sh", []byte("#!/usr/bin/env bash\nif true; then\n  echo hi\n"))
	if got != "bash" {
		t.Fatalf("interpreter = %q", got)
	}
	if len(f) != 1 || f[0].Line != 4 || f[0].RuleID != RuleVerifyScriptSyntax || f[0].Script != "scripts/verify.sh" || f[0].Message != "syntax error: unexpected end of file" {
		t.Fatalf("findings = %+v", f)
	}
}

func TestVerifyScriptPreflightSyntaxFakeDashFormat(t *testing.T) {
	withFakeShellCheck(t, func(interp, text string) (string, error) {
		return "/x/script.sh: 7: Syntax error: end of file unexpected (expecting \"fi\")\n", errors.New("exit status 2")
	})
	f := preflightSyntaxFindings("v.sh", []byte("#!/bin/sh\nif true; then\n"))
	if len(f) != 1 || f[0].Line != 7 {
		t.Fatalf("findings = %+v", f)
	}
}

func TestVerifyScriptPreflightSyntaxFakeClean(t *testing.T) {
	withFakeShellCheck(t, func(interp, text string) (string, error) { return "", nil })
	if f := preflightSyntaxFindings("v.sh", []byte("#!/bin/bash\necho ok\n")); len(f) != 0 {
		t.Fatalf("findings = %+v", f)
	}
}

func TestVerifyScriptPreflightSyntaxSkipsNonShell(t *testing.T) {
	called := false
	withFakeShellCheck(t, func(interp, text string) (string, error) { called = true; return "", errors.New("x") })
	for _, s := range []string{"echo no shebang\n", "#!/usr/bin/env python3\nprint(1)\n", "#!/usr/bin/perl\n"} {
		if f := preflightSyntaxFindings("v", []byte(s)); len(f) != 0 {
			t.Fatalf("%q: findings = %+v", s, f)
		}
	}
	if called {
		t.Fatal("runner called for non-shell script")
	}
}

func TestVerifyScriptPreflightSyntaxInterpreterMissing(t *testing.T) {
	withFakeShellCheck(t, func(interp, text string) (string, error) {
		return "", errPreflightInterpreterMissing{exec.ErrNotFound}
	})
	if f := preflightSyntaxFindings("v.sh", []byte("#!/bin/zsh\nif\n")); len(f) != 0 {
		t.Fatalf("findings = %+v", f)
	}
}

func TestVerifyScriptPreflightSyntaxRealBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	bad := "#!/usr/bin/env bash\necho start\nif true; then\n  echo inside\n"
	p := filepath.Join(dir, "bad.sh")
	if err := os.WriteFile(p, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(p)
	f := preflightSyntaxFindings("scripts/bad.sh", text)
	if len(f) != 1 || f[0].Line != 5 || f[0].RuleID != RuleVerifyScriptSyntax {
		t.Fatalf("findings = %+v", f)
	}
	if f := preflightSyntaxFindings("scripts/ok.sh", []byte("#!/bin/bash\nif true; then\n  echo ok\nfi\n")); len(f) != 0 {
		t.Fatalf("clean findings = %+v", f)
	}
}
