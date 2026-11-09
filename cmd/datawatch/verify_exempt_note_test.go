package main

import (
	"strings"
	"testing"
)

// TestVerifierAcceptsExemptNote: a note task with an exemption reason and no
// verification script passes with no changes; the same task without a reason
// does not, and neither does a task that has a script.
func TestVerifierAcceptsExemptNote(t *testing.T) {
	res, handled := verdictForExemptNoChange("prose only, reviewed by the verifier", "")
	if !handled {
		t.Fatal("note task with a reason: want handled verdict")
	}
	if !res.OK {
		t.Fatalf("note task with a reason: want OK, got %+v", res)
	}
	if want := "prose only, reviewed by the verifier"; !strings.Contains(res.Summary, want) {
		t.Errorf("summary %q should quote the reason %q", res.Summary, want)
	}

	if _, handled := verdictForExemptNoChange("", ""); handled {
		t.Error("note task without a reason must not be handled (must still fail)")
	}
	if _, handled := verdictForExemptNoChange("   ", ""); handled {
		t.Error("blank reason must not be handled (must still fail)")
	}
	if _, handled := verdictForExemptNoChange("prose only", "go test ./..."); handled {
		t.Error("task with a verification script must not take the exemption")
	}
}
