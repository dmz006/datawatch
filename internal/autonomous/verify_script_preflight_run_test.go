package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func preflightRunFixture(t *testing.T) (string, []Story) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"v/syntax.sh":   "#!/bin/bash\nif true; then\n  echo hi\n",
		"v/caller.sh":   "#!/bin/bash\nsh v/bashonly.sh\n",
		"v/bashonly.sh": "#!/bin/bash\n[[ -n x ]]\n",
		"v/pipe.sh":     "#!/bin/bash\nset -o pipefail\ngo test | grep -q ok\n",
		"v/clean.sh":    "#!/bin/bash\necho ok\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	withFakeShellCheck(t, func(interp, text string) (string, error) {
		if strings.Contains(text, "if true; then") {
			return "x: line 4: syntax error: unexpected end of file\n", errors.New("exit status 2")
		}
		return "", nil
	})
	stories := []Story{{ID: "s1", Title: "S", Tasks: []Task{
		{ID: "t1", Title: "A", VerificationScript: "v/syntax.sh"},
		{ID: "t2", Title: "B", VerificationScript: "v/caller.sh"},
		{ID: "t3", Title: "C", VerificationScript: "v/pipe.sh"},
		{ID: "t4", Title: "D", VerificationScript: "v/clean.sh"},
		{ID: "t5", Title: "E", VerificationScript: "go test ./..."},
		{ID: "t6", Title: "F", VerificationScript: "v/missing.sh"},
	}}}
	return dir, stories
}

func TestVerifyScriptPreflightRun(t *testing.T) {
	dir, stories := preflightRunFixture(t)
	got := RunVerifyScriptPreflight(dir, stories)
	var keys []string
	for _, f := range got {
		if f.Severity != config.DecomposeLintSeverityBlock || f.Story != "s1" {
			t.Fatalf("finding %+v", f)
		}
		if f.Task == "t4" {
			t.Fatalf("clean script produced %+v", f)
		}
		if f.RuleID == RuleVerifyScriptAllowedFiles {
			continue // caller.sh names a literal path, so its allowed list is checked too
		}
		keys = append(keys, f.Task+" "+f.RuleID+" "+strings.SplitN(f.Message, " ", 2)[0])
	}
	sort.Strings(keys)
	want := []string{
		"t1 " + RuleVerifyScriptSyntax + " v/syntax.sh:4",
		"t2 " + RuleVerifyScriptShellMismatch + " v/caller.sh:2",
		"t3 " + RuleVerifyScriptPipefailGrepQuiet + " v/pipe.sh:3",
	}
	if strings.Join(keys, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v\nwant %v\nall %+v", keys, want, got)
	}
	for _, f := range got {
		if f.RuleID == RuleVerifyScriptAllowedFiles && (f.Task != "t2" || !strings.HasPrefix(f.Message, "v/caller.sh:")) {
			t.Fatalf("unexpected allowed-files finding %+v", f)
		}
	}
}

func TestVerifyScriptPreflightRunHookModeOff(t *testing.T) {
	dir, stories := preflightRunFixture(t)
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{Mode: config.DecomposeLintModeOff}
	m, err := NewManager(t.TempDir(), cfg, func(DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1", ProjectDir: dir}
	retried := false
	_, _, out := m.decomposeLintHook(prd, "T", "raw", stories, func(string) (string, string, []Story, error) {
		retried = true
		return "", "", nil, errors.New("no")
	})
	if retried || len(out) != 1 {
		t.Fatalf("retried=%v stories=%d", retried, len(out))
	}
	if prd.DecomposeLint == nil || prd.DecomposeLint.Mode != config.DecomposeLintModeOff {
		t.Fatalf("record = %+v", prd.DecomposeLint)
	}
	if _, ok := lintHookFindingRule(prd.DecomposeLint.Findings, RuleVerifyScriptPipefailGrepQuiet); !ok {
		t.Fatalf("findings = %+v", prd.DecomposeLint.Findings)
	}
}
