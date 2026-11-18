package autonomous

import (
	"strings"
	"testing"
)

func TestVerifyScriptPreflightPipefail(t *testing.T) {
	t.Run("PipeIntoGrepQYieldsOneFindingAtThatLine", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -euo pipefail\n" +
			"seq 1 1000000 | grep -q 12345\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 1 {
			t.Fatalf("findings = %+v, want exactly one", f)
		}
		if f[0].Script != "scripts/verify.sh" || f[0].Line != 3 {
			t.Fatalf("finding = %+v, want script scripts/verify.sh at line 3", f[0])
		}
		if f[0].RuleID != RuleVerifyScriptPipefailGrepQuiet {
			t.Fatalf("rule = %q, want %q", f[0].RuleID, RuleVerifyScriptPipefailGrepQuiet)
		}
		for _, want := range []string{"pipefail", "first match", "false no-match", "file", "grep"} {
			if !strings.Contains(f[0].Message, want) {
				t.Fatalf("message %q does not mention %q", f[0].Message, want)
			}
		}
	})

	t.Run("MixedQuietClustersYieldOneFindingPerLine", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -o pipefail\n" +
			"cat log.txt | grep -qE 'ERR|FATAL'\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 1 || f[0].Line != 3 {
			t.Fatalf("findings = %+v, want one at line 3", f)
		}
	})

	t.Run("SameLineWithoutPipefailYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -eu\n" +
			"seq 1 1000000 | grep -q 12345\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("FileArgumentGrepQYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -euo pipefail\n" +
			"grep -q wanted results.txt\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("CleanScriptYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -euo pipefail\n" +
			"main() { echo ok; }\n" +
			"main\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("NonQuietPipedGrepYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -euo pipefail\n" +
			"seq 1 3 | grep -E '1|2'\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})

	t.Run("PipedGrepInCommentYieldsNone", func(t *testing.T) {
		script := "#!/bin/bash\n" +
			"set -euo pipefail\n" +
			"# seq 1 3 | grep -q 3\n" +
			"echo ok\n"
		f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script))
		if len(f) != 0 {
			t.Fatalf("findings = %+v, want none", f)
		}
	})
}
