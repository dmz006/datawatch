package genartifacts

import (
	"strings"
	"testing"
)

func TestOverridePathRejectsUnknownKind(t *testing.T) {
	if got := OverridePath(Kind("../../etc/passwd")); got != "" {
		t.Fatalf("OverridePath for an unknown kind = %q, want empty", got)
	}
	for _, k := range Kinds() {
		if got := OverridePath(k); got == "" || strings.Contains(got, "..") {
			t.Fatalf("OverridePath(%s) = %q", k, got)
		}
	}
}
