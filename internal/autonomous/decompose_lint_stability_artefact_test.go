package autonomous

import "testing"

const artefactExempt = "grep -v -e docs/plans/drafts/ -e docs/plans/parity-issues/ -e docs/plans/reports/ -e CHANGELOG.md"

func stabilityRun(t *testing.T, ruleID string, scripts map[string]string, want map[string]int) {
	t.Helper()
	rule := verifyStableRule(t, ruleID)
	dir := t.TempDir()
	for name, body := range scripts {
		verifyStableWrite(t, dir, name, body)
	}
	for name, n := range want {
		got := verifyStableLint(dir, "", rule, Task{ID: "t", VerificationScript: name})
		if len(got) != n {
			t.Errorf("%s: %d findings, want %d: %+v", name, len(got), n, got)
		}
	}
}

func TestStabilityArtefactFiles(t *testing.T) {
	stabilityRun(t, "artefact_files_allowed", map[string]string{
		"v/bad.sh":     "#!/bin/sh\nallowed='a.go'\ngit diff --name-only HEAD | grep -v \"$allowed\" && echo unexpected files && exit 1\n",
		"v/partial.sh": "#!/bin/sh\ngit diff --name-only | grep -v CHANGELOG.md && echo unexpected\n",
		"v/ok.sh":      "#!/bin/sh\ngit diff --name-only | " + artefactExempt + " | grep -v a.go && echo unexpected\n",
		"v/named.sh":   "#!/bin/sh\ngit diff --name-only | grep -qx a.go\n",
		"v/none.sh":    "#!/bin/sh\ngo test ./...\n",
	}, map[string]int{"v/bad.sh": 1, "v/partial.sh": 1, "v/ok.sh": 0, "v/named.sh": 0, "v/none.sh": 0, "v/missing.sh": 0})
}

func TestStabilityFinalGateRerun(t *testing.T) {
	stabilityRun(t, "final_gate_artefact_allowed", map[string]string{
		"v/bad.sh":      "#!/bin/sh\nallowed='a.go b.go'\ngit diff --name-only | grep -v -x -e a.go -e b.go && echo unexpected && exit 1\n",
		"v/envguard.sh": "#!/bin/sh\n[ -n \"$FINAL_GATE\" ] && exit 0\nallowed='a.go'\ngit diff --name-only | grep -v a.go && echo unexpected\n",
		"v/argguard.sh": "#!/bin/sh\n[ \"$1\" = final ] && exit 0\nallowed='a.go'\ngit diff --name-only | grep -v a.go && echo unexpected\n",
		"v/addonly.sh":  "#!/bin/sh\nallowed='a.go'\ngit diff --name-only --diff-filter=A | grep -v a.go && echo unexpected\n",
		"v/named.sh":    "#!/bin/sh\ngit diff --name-only | grep -qx a.go\n",
	}, map[string]int{"v/bad.sh": 1, "v/envguard.sh": 0, "v/argguard.sh": 0, "v/addonly.sh": 0, "v/named.sh": 0})
}

func TestStabilityArtefactFilesFixMessage(t *testing.T) {
	for _, id := range []string{"artefact_files_allowed", "final_gate_artefact_allowed"} {
		r := verifyStableRule(t, id)
		if r.Severity != "block" || len(r.Message) < 80 {
			t.Errorf("%s: bad rule %+v", id, r)
		}
	}
}
