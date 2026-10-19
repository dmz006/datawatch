package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// writeAlreadyLandedScripts writes a script that exits 0 and one that prints
// a recognizable line and exits 1 into dir, returning their names relative
// to dir.
func writeAlreadyLandedScripts(t *testing.T, dir string) (passing, failing string) {
	t.Helper()
	passing = "pass.sh"
	failing = "fail.sh"
	scripts := map[string]string{
		passing: "#!/usr/bin/env bash\nexit 0\n",
		failing: "#!/usr/bin/env bash\necho \"FATAL: marker token not found in deliverable\"\nexit 1\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write script %s: %v", name, err)
		}
	}
	return passing, failing
}

// failMarker is the line the failing test script prints; it must surface in
// the handled failure's issues so the executor's failure hint can carry it.
const failMarker = "FATAL: marker token not found in deliverable"

func TestVerdictForAlreadyLanded(t *testing.T) {
	dir := t.TempDir()
	passing, failing := writeAlreadyLandedScripts(t, dir)
	missing := "missing-verify.sh" // deliberately never written

	tests := []struct {
		name          string
		priorAttempts int
		script        string
		wantTrue      bool
		wantOK        bool
		// pass-branch assertions
		wantUnmodifiedRetry bool
		// handled-failure assertions
		wantCategory string
		wantSummary  []string // each substring must appear in Summary
		wantIssues   []string // each substring must appear in joined Issues
	}{
		// First attempt: every branch falls through unhandled, even a passing
		// script — an empty-diff first attempt must not pass as "already landed".
		{name: "first attempt with passing script does not verify", priorAttempts: 0, script: passing, wantTrue: false},
		{name: "first attempt with failing script does not verify", priorAttempts: 0, script: failing, wantTrue: false},
		{name: "first attempt with missing script does not produce setup verdict", priorAttempts: 0, script: missing, wantTrue: false},
		// No script to run means nothing can be verified on retry either.
		{name: "retry with empty script path returns false", priorAttempts: 1, script: "", wantTrue: false},
		{name: "retry with whitespace script path returns false", priorAttempts: 1, script: "   ", wantTrue: false},
		// Branch 1: a genuine retry whose verification script passes verifies
		// and flags UnmodifiedRetry.
		{name: "retry with passing script verifies", priorAttempts: 1, script: passing,
			wantTrue: true, wantOK: true, wantUnmodifiedRetry: true,
			wantSummary: []string{"already landed", "passed"}},
		{name: "later retry with passing script verifies", priorAttempts: 3, script: passing,
			wantTrue: true, wantOK: true, wantUnmodifiedRetry: true,
			wantSummary: []string{"already landed", "passed"}},
		// Branch 2: retry whose script exists but fails — handled failure that
		// carries the exit code and the script output.
		{name: "retry with failing script is handled failure with output", priorAttempts: 1, script: failing,
			wantTrue: true, wantOK: false,
			wantSummary: []string{"fail.sh", "exit code 1", "no new changes"},
			wantIssues:  []string{"1", failMarker}},
		{name: "later retry with failing script is handled failure", priorAttempts: 4, script: failing,
			wantTrue: true, wantOK: false,
			wantSummary: []string{"fail.sh", "exit code 1"},
			wantIssues:  []string{failMarker}},
		// Branch 3: retry with a missing script is a setup fault naming the path.
		{name: "retry with missing script is handled setup fault", priorAttempts: 1, script: missing,
			wantTrue: true, wantOK: false, wantCategory: "setup",
			wantSummary: []string{"missing-verify.sh", "setup"},
			wantIssues:  []string{"missing-verify.sh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, gotTrue := verdictForAlreadyLanded(tt.script, dir, tt.priorAttempts)
			if gotTrue != tt.wantTrue {
				t.Fatalf("want true=%v, got %v (res %+v)", tt.wantTrue, gotTrue, res)
			}
			if !gotTrue {
				// Unhandled: must be exactly the zero VerificationResult so the
				// caller falls through to the existing no-output failure.
				if !reflect.DeepEqual(res, autonomouspkg.VerificationResult{}) {
					t.Fatalf("unhandled result must be the zero VerificationResult, got %+v", res)
				}
				return
			}
			if res.OK != tt.wantOK {
				t.Fatalf("want OK=%v, got %v", tt.wantOK, res.OK)
			}
			if res.Category != tt.wantCategory {
				t.Fatalf("want Category=%q, got %q", tt.wantCategory, res.Category)
			}
			if res.VerifiedAt.IsZero() {
				t.Fatalf("handled verdict must set VerifiedAt, got %+v", res)
			}
			for _, want := range tt.wantSummary {
				if !strings.Contains(res.Summary, want) {
					t.Fatalf("Summary %q does not contain %q", res.Summary, want)
				}
			}
			joined := strings.Join(res.Issues, "\n---\n")
			for _, want := range tt.wantIssues {
				if !strings.Contains(joined, want) {
					t.Fatalf("Issues %q do not contain %q", joined, want)
				}
			}
			if tt.wantUnmodifiedRetry {
				if !res.UnmodifiedRetry {
					t.Fatalf("pass verdict must set UnmodifiedRetry, got %+v", res)
				}
				if res.Severity != "low" {
					t.Fatalf("pass verdict want Severity low, got %q", res.Severity)
				}
			} else {
				if res.UnmodifiedRetry {
					t.Fatalf("failure verdict must not set UnmodifiedRetry, got %+v", res)
				}
			}
		})
	}
}
