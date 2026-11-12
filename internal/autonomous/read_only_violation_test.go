package autonomous

import (
	"strings"
	"testing"
)

func TestReadOnlyViolationPassesWhenEntryDisappears(t *testing.T) {
	baseline := " M internal/a.go\n?? notes.md"
	now := " M internal/a.go"

	if err := readOnlyStatusViolation(now, baseline); err != nil {
		t.Fatalf("expected nil error when a baseline entry disappeared, got %v", err)
	}
}

func TestReadOnlyViolationNamesNewEntry(t *testing.T) {
	baseline := " M internal/a.go"
	now := " M internal/a.go\n?? scratch/new.txt"

	err := readOnlyStatusViolation(now, baseline)
	if err == nil {
		t.Fatal("expected an error when a new status entry appears, got nil")
	}
	if err.Error() == "" {
		t.Errorf("expected a non-empty error message, got empty string")
	}
	if !strings.Contains(err.Error(), "?? scratch/new.txt") {
		t.Errorf("expected error to name the new entry, got %q", err.Error())
	}
	if !strings.HasSuffix(err.Error(), "(?? scratch/new.txt)") {
		t.Errorf("expected message to end with \"(?? scratch/new.txt)\", got %q", err.Error())
	}
}

func TestReadOnlyViolationPassesOnEqualStatus(t *testing.T) {
	identical := " M internal/a.go\n?? notes.md"

	if err := readOnlyStatusViolation(identical, identical); err != nil {
		t.Fatalf("expected nil error for identical status, got %v", err)
	}
}
