package autonomous

import (
	"strings"
	"testing"
)

// shellMismatchRead builds a readCalled from an in-memory map so the
// callee reads never touch disk.
func shellMismatchRead(t *testing.T, scripts map[string]string) func(rel string) ([]byte, bool) {
	t.Helper()
	return func(rel string) ([]byte, bool) {
		body, ok := scripts[rel]
		return []byte(body), ok
	}
}

func TestVerifyScriptPreflightShell(t *testing.T) {
	t.Run("BashCalleeInvokedWithSh", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{
			"lib/helper.sh": "#!/usr/bin/env bash\necho hi\n",
		})
		caller := "#!/bin/bash\nsh lib/helper.sh\n"
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte(caller), read)
		if len(f) != 1 {
			t.Fatalf("findings = %+v, want exactly one", f)
		}
		if f[0].Script != "scripts/verify.sh" || f[0].Line != 2 || f[0].RuleID != RuleVerifyScriptShellMismatch {
			t.Fatalf("finding = %+v", f[0])
		}
		for _, want := range []string{"lib/helper.sh", "sh", "bash"} {
			if !strings.Contains(f[0].Message, want) {
				t.Fatalf("message %q does not name %q", f[0].Message, want)
			}
		}
	})

	t.Run("SameInterpreterYieldsNone", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{
			"lib/helper.sh": "#!/usr/bin/env bash\necho hi\n",
		})
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte("#!/bin/bash\nbash lib/helper.sh\n"), read)
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("ShCalleeInvokedWithSh", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{
			"lib/helper.sh": "#!/bin/sh\nexit 0\n",
		})
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte("#!/bin/sh\nsh lib/helper.sh\n"), read)
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("MissingCalleeSkipped", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{})
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte("#!/bin/bash\nsh lib/absent.sh\n"), read)
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("CleanCallerYieldsNone", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{
			"lib/helper.sh": "#!/bin/bash\necho hi\n",
		})
		caller := "#!/bin/bash\nset -eu\nmain() { echo ok; }\nmain\n"
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte(caller), read)
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("FlagsThenMismatch", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{
			"lib/helper.sh": "#!/bin/dash\nexit 0\n",
		})
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte("#!/bin/bash\nbash -u lib/helper.sh\n"), read)
		if len(f) != 1 || f[0].Line != 2 || f[0].RuleID != RuleVerifyScriptShellMismatch {
			t.Fatalf("findings = %+v", f)
		}
		for _, want := range []string{"bash", "dash"} {
			if !strings.Contains(f[0].Message, want) {
				t.Fatalf("message %q does not name %q", f[0].Message, want)
			}
		}
	})

	t.Run("CalleeWithoutShellShebangSkipped", func(t *testing.T) {
		read := shellMismatchRead(t, map[string]string{
			"lib/helper.sh": "echo no shebang\n",
		})
		f := preflightShellMismatchFindings("scripts/verify.sh", []byte("#!/bin/bash\nzsh lib/helper.sh\n"), read)
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})
}
