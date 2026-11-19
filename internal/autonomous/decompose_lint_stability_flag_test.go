package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fakes this file uses:
//
//   - a fake PATH binary whose `--help` lists only `--foo`, standing in
//     for an installed command on the real probe path;
//   - a deterministic `scriptHelpProbe` replacement, standing in for
//     the probe on the fake path;
//   - a command name no environment resolves, standing in for an
//     uninstalled command.
//
// Both new stability rules follow the same convention as the other
// verify-script rules: they read the verification script (and the
// following sources) off the real filesystem from a temp dir, and a
// task with no script never fires.

const (
	stabilityFakeScriptPath = "verify.sh"
	stabilityFakeBinary     = "faketool"
	stabilityUnknownBinary  = "nosuchtool123"
)

// stabilityRule returns one of the new rules from the built-in pack.
func stabilityRule(t *testing.T, id string) Rule {
	t.Helper()
	r := verifyStableRule(t, id)
	if r.Severity != "block" {
		t.Fatalf(`rule %s severity = %q, want "block"`, id, r.Severity)
	}
	return r
}

// stabilityLint runs dir-scoped lint with only the given rule.
func stabilityLint(t *testing.T, dir, id string, task Task) []LintFinding {
	t.Helper()
	return verifyStableLint(dir, id, stabilityRule(t, id), task)
}

// stabilityWrite lays out a verification script in a fresh dir.
func stabilityWrite(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stabilityFakeScriptPath), []byte(body), 0o755); err != nil {
		t.Fatalf("writing verification script: %v", err)
	}
	return dir
}

func stabilityTask(spec, script string) Task {
	return mkDecomposeLintTask("t1", "Stability", spec, nil, false, script)
}

// installFakeBinary writes an executable `faketool` whose --help lists
// only `--foo` (plus --help) onto a temp dir.
func installFakeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--help\" ]; then\n" +
		"  echo 'usage: faketool [flags]'\n" +
		"  echo '  --foo  do a foo'\n" +
		"  echo '  --help show this help'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo 'faketool: unknown argument: $1' >&2\n" +
		"exit 2\n"
	if err := os.WriteFile(filepath.Join(dir, stabilityFakeBinary), []byte(bin), 0o755); err != nil {
		t.Fatalf("writing fake binary: %v", err)
	}
	return dir
}

func TestStabilityUnverifiedFlags(t *testing.T) {
	const id = "verify_script_unknown_flag"

	t.Run("fake probe / unknown long flag fires", func(t *testing.T) {
		old := scriptHelpProbe
		scriptHelpProbe = func(ctx context.Context, line string) (string, error) {
			switch line {
			case "command -v " + stabilityFakeBinary:
				return "/opt/fake/" + stabilityFakeBinary, nil
			case stabilityFakeBinary + " --help":
				return "usage: faketool [flags]\n  --foo  do a foo\n  --help show this help\n", nil
			case "command -v " + stabilityUnknownBinary:
				return "", errors.New("command not found")
			default:
				t.Fatalf("fake probe saw unexpected line %q", line)
				return "", errors.New("unexpected line")
			}
		}
		defer func() { scriptHelpProbe = old }()

		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+stabilityFakeBinary+" --bar\n")
		findings := stabilityLint(t, dir, id, stabilityTask("run "+stabilityFakeBinary+" with the bar option", stabilityFakeScriptPath))
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1; got %+v", len(findings), findings)
		}
		if f := findings[0]; f.RuleID != id || f.Severity != "block" || f.Script != stabilityFakeScriptPath {
			t.Errorf("finding = %+v", f)
		}
		if !strings.Contains(findings[0].Matched, "--bar") || !strings.Contains(findings[0].Matched, stabilityFakeBinary) {
			t.Errorf("matched excerpt %q should name the unknown flag and the command", findings[0].Matched)
		}
	})

	t.Run("fake probe / known flag does not fire", func(t *testing.T) {
		old := scriptHelpProbe
		scriptHelpProbe = func(ctx context.Context, line string) (string, error) {
			switch line {
			case "command -v " + stabilityFakeBinary:
				return "/opt/fake/" + stabilityFakeBinary, nil
			case stabilityFakeBinary + " --help":
				return "usage: faketool [flags]\n  --foo  do a foo\n", nil
			default:
				t.Fatalf("fake probe saw unexpected line %q", line)
				return "", errors.New("unexpected line")
			}
		}
		defer func() { scriptHelpProbe = old }()

		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+stabilityFakeBinary+" --foo=1\n")
		if fs := stabilityLint(t, dir, id, stabilityTask("run "+stabilityFakeBinary, stabilityFakeScriptPath)); len(fs) != 0 {
			t.Fatalf("known flag --foo findings = %+v, want none", fs)
		}
	})

	t.Run("fake probe / uninstalled command skips silently", func(t *testing.T) {
		old := scriptHelpProbe
		scriptHelpProbe = func(ctx context.Context, line string) (string, error) {
			if strings.HasPrefix(line, "command -v ") {
				return "", errors.New("command not found")
			}
			t.Fatalf("an uninstalled command must never be probed for help; saw %q", line)
			return "", errors.New("unexpected line")
		}
		defer func() { scriptHelpProbe = old }()

		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+stabilityUnknownBinary+" --bar\n")
		if fs := stabilityLint(t, dir, id, stabilityTask("run "+stabilityUnknownBinary, stabilityFakeScriptPath)); len(fs) != 0 {
			t.Fatalf("uninstalled command findings = %+v, want none (skipped)", fs)
		}
	})

	t.Run("fake probe / subcommand flag falls back to sub help", func(t *testing.T) {
		old := scriptHelpProbe
		scriptHelpProbe = func(ctx context.Context, line string) (string, error) {
			switch line {
			case "command -v subtool":
				return "/opt/fake/subtool", nil
			case "subtool --help":
				return "usage: subtool <subcommand> [flags]\nsubcommands: list\n", nil
			case "subtool list --help":
				return "usage: subtool list [flags]\n  --every show everything\n", nil
			default:
				t.Fatalf("fake probe saw unexpected line %q", line)
				return "", errors.New("unexpected line")
			}
		}
		defer func() { scriptHelpProbe = old }()

		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\nsubtool list --every\n")
		if fs := stabilityLint(t, dir, id, stabilityTask("list everything", stabilityFakeScriptPath)); len(fs) != 0 {
			t.Fatalf("subcommand flag in sub help findings = %+v, want none", fs)
		}
	})

	t.Run("real probe / fake PATH binary, help lists only --foo", func(t *testing.T) {
		bindir := installFakeBinary(t)
		t.Setenv("PATH", bindir+string(filepath.ListSeparator)+os.Getenv("PATH"))

		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+stabilityFakeBinary+" --bar\n")
		findings := stabilityLint(t, dir, id, stabilityTask("run "+stabilityFakeBinary, stabilityFakeScriptPath))
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1 (binary's --help lists only --foo); got %+v", len(findings), findings)
		}
		if !strings.Contains(findings[0].Matched, "--bar") {
			t.Errorf("matched excerpt %q should name --bar", findings[0].Matched)
		}

		dir = stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+stabilityFakeBinary+" --foo\n")
		if fs := stabilityLint(t, dir, id, stabilityTask("run "+stabilityFakeBinary, stabilityFakeScriptPath)); len(fs) != 0 {
			t.Fatalf("known flag --foo findings = %+v, want none", fs)
		}
	})

	t.Run("real probe / uninstalled command is skipped", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+stabilityUnknownBinary+" --bar\n")
		if fs := stabilityLint(t, dir, id, stabilityTask("run "+stabilityUnknownBinary, stabilityFakeScriptPath)); len(fs) != 0 {
			t.Fatalf("uninstalled command findings = %+v, want none (skipped)", fs)
		}
	})

	t.Run("no script never fires", func(t *testing.T) {
		dir := t.TempDir()
		if fs := stabilityLint(t, dir, id, stabilityTask("no script", "")); len(fs) != 0 {
			t.Fatalf("no-script findings = %+v, want none", fs)
		}
	})
}

func TestStabilityHeadingNotInSpec(t *testing.T) {
	const id = "verify_script_text_not_in_spec"
	const heading = "# Title A"

	t.Run("heading asserted without appearing in spec fires", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q '^"+heading+"' docs/result.md\n")
		task := stabilityTask("write the result document", stabilityFakeScriptPath)
		findings := stabilityLint(t, dir, id, task)
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1; got %+v", len(findings), findings)
		}
		if f := findings[0]; f.RuleID != id || f.Severity != "block" || f.Script != stabilityFakeScriptPath {
			t.Errorf("finding = %+v", f)
		}
		if !strings.Contains(findings[0].Matched, heading) {
			t.Errorf("matched excerpt %q should name the heading", findings[0].Matched)
		}
	})

	t.Run("heading stated verbatim in spec does not fire", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q '^"+heading+"' docs/result.md\n")
		task := stabilityTask("write the result document under the `"+heading+"` heading", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (the spec states the heading)", fs)
		}
	})

	t.Run("multi-word phrase not in spec fires", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\nout=$(run build 2>&1)\nprintf '%s\\n' \"$out\" | grep -q 'all dependency checks passed'\n")
		task := stabilityTask("the build must succeed", stabilityFakeScriptPath)
		findings := stabilityLint(t, dir, id, task)
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1; got %+v", len(findings), findings)
		}
		if !strings.Contains(findings[0].Matched, "all dependency checks passed") {
			t.Errorf("matched excerpt %q should name the phrase", findings[0].Matched)
		}
	})

	t.Run("phrase stated verbatim in spec does not fire", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\nout=$(run build 2>&1)\nprintf '%s\\n' \"$out\" | grep -q 'all dependency checks passed'\n")
		task := stabilityTask("the build must print the line all dependency checks passed", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (the spec states the phrase)", fs)
		}
	})

	t.Run("text the script generates is not an assertion", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\n"+
			"echo 'the report is being written'\n"+
			"out=$(run build 2>&1)\n"+
			"printf '%s\\n' \"$out\" | grep -q 'build ok'\n")
		task := stabilityTask("the build must succeed", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (single-word checked text and self-generated text are not flagged)", fs)
		}
	})

	t.Run("shebang is not a claimed heading", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q 'bash' verify.sh\n")
		task := stabilityTask("re-run the verification", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (the shebang is not asserted)", fs)
		}
	})
}
