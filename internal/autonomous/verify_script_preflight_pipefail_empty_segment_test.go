package autonomous

import "testing"

// A shell `||` splits on "|" into an empty segment. pipedGrepQuiet used to
// read fields[0] of that empty segment and panic with "index out of range
// [0] with length 0", which crashed the whole daemon whenever a decompose
// ran the verification-script pre-flight over a script containing `||`.
func TestPipedGrepQuietEmptySegments(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"OrOperatorHasNoPipe", "cmd || true", false},
		{"OrOperatorThenGrepQ", "cmd || grep -q x", false},
		{"TrailingOrOperator", "cmd ||", false},
		{"OrOperatorAfterAPipeline", "a | b || grep -q x", false},
		{"PipeThenOrThenQuietGrepPipe", "a || b | grep -q x", true},
		{"BarePipeAtEnd", "cmd |", false},
		{"BarePipeThenSpaces", "cmd |   ", false},
		{"AmpersandPipeAlone", "cmd |&", false},
		{"PlainPipeIntoQuietGrep", "seq 1 10 | grep -q 5", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pipedGrepQuiet(c.line); got != c.want {
				t.Fatalf("pipedGrepQuiet(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

func TestPreflightPipefailGrepFindingsOrOperatorDoesNotPanic(t *testing.T) {
	script := "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"make build || exit 1\n" +
		"test -f out.txt || { echo missing; exit 1; }\n"
	if f := preflightPipefailGrepFindings("scripts/verify.sh", []byte(script)); len(f) != 0 {
		t.Fatalf("findings = %+v, want none for a script that only uses ||", f)
	}
}
