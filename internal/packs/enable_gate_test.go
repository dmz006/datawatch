package packs

import (
	"testing"

	"github.com/dmz006/datawatch/internal/skills"
)

const enableGateMessage = "packs need at least one connected skills registry source; connect one in the skills registry settings"

func TestCheckEnableZeroSourcesFails(t *testing.T) {
	err := CheckEnable(nil)
	if err == nil {
		t.Fatal("no sources: expected error")
	}
	if err.Error() != enableGateMessage {
		t.Errorf("error = %q, want %q", err.Error(), enableGateMessage)
	}

	if err := CheckEnable([]skills.Registry{}); err == nil {
		t.Fatal("empty slice: expected error")
	}
}

func TestCheckEnableDisabledSourceFails(t *testing.T) {
	connected := []skills.Registry{{Name: "off", Kind: "git", Enabled: false}}
	err := CheckEnable(connected)
	if err == nil {
		t.Fatal("only disabled source: expected error")
	}
	if err.Error() != enableGateMessage {
		t.Errorf("error = %q, want %q", err.Error(), enableGateMessage)
	}
}

func TestCheckEnableOneEnabledSourcePasses(t *testing.T) {
	connected := []skills.Registry{{Name: "main", Kind: "git", Enabled: true}}
	if err := CheckEnable(connected); err != nil {
		t.Fatalf("one enabled source: unexpected error: %v", err)
	}
}

func TestCheckEnableMixedSourcesPasses(t *testing.T) {
	connected := []skills.Registry{
		{Name: "off", Kind: "git", Enabled: false},
		{Name: "main", Kind: "git", Enabled: true},
	}
	if err := CheckEnable(connected); err != nil {
		t.Fatalf("mixed sources with one enabled: unexpected error: %v", err)
	}
}
