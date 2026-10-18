package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeAlreadyLandedScripts writes a script that exits 0 and one that exits 1
// into dir, returning their names relative to dir.
func writeAlreadyLandedScripts(t *testing.T, dir string) (passing, failing string) {
	t.Helper()
	passing = "pass.sh"
	failing = "fail.sh"
	scripts := map[string]string{
		passing: "#!/usr/bin/env bash\nexit 0\n",
		failing: "#!/usr/bin/env bash\nexit 1\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write script %s: %v", name, err)
		}
	}
	return passing, failing
}

func TestVerdictForAlreadyLanded(t *testing.T) {
	dir := t.TempDir()
	passing, failing := writeAlreadyLandedScripts(t, dir)

	tests := []struct {
		name          string
		priorAttempts int
		script        string
		wantOK        bool
		wantTrue      bool
	}{
		// An empty-diff first attempt must not pass as "already landed".
		{name: "first attempt with passing script does not verify", priorAttempts: 0, script: passing, wantOK: false, wantTrue: false},
		{name: "first attempt with failing script does not verify", priorAttempts: 0, script: failing, wantOK: false, wantTrue: false},
		// A genuine retry whose verification script passes verifies.
		{name: "retry with passing script verifies", priorAttempts: 1, script: passing, wantOK: true, wantTrue: true},
		{name: "later retry with passing script verifies", priorAttempts: 3, script: passing, wantOK: true, wantTrue: true},
		// No script to run means nothing can be verified.
		{name: "retry with empty script path returns false", priorAttempts: 1, script: "", wantOK: false, wantTrue: false},
		{name: "retry with whitespace script path returns false", priorAttempts: 1, script: "   ", wantOK: false, wantTrue: false},
		// A failing script never verifies, whatever the attempt count.
		{name: "retry with failing script returns false", priorAttempts: 1, script: failing, wantOK: false, wantTrue: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, gotTrue := verdictForAlreadyLanded(tt.script, dir, tt.priorAttempts)
			if gotTrue != tt.wantTrue {
				t.Fatalf("want true=%v, got %v (res %+v)", tt.wantTrue, gotTrue, res)
			}
			if res.OK != tt.wantOK {
				t.Fatalf("want OK=%v, got %v", tt.wantOK, res.OK)
			}
			if tt.wantTrue {
				if res.Severity != "low" || res.Summary == "" || res.VerifiedAt.IsZero() {
					t.Fatalf("unexpected result fields: %+v", res)
				}
			}
		})
	}
}
