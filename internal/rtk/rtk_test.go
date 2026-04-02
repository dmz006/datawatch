package rtk

import (
	"testing"
)

func TestCheckInstalled(t *testing.T) {
	// This test depends on whether rtk is installed on the system
	status := CheckInstalled()
	// Just verify it doesn't panic and returns a valid struct
	if status.Binary == "" {
		t.Error("binary should be set to default")
	}
	// If installed, version should be non-empty
	if status.Installed && status.Version == "" {
		t.Error("installed but version empty")
	}
}

func TestSetBinary(t *testing.T) {
	SetBinary("/usr/local/bin/rtk-custom")
	if binary != "/usr/local/bin/rtk-custom" {
		t.Errorf("expected custom binary, got %s", binary)
	}
	// Reset
	SetBinary("rtk")
}

func TestCollectStats(t *testing.T) {
	data := CollectStats()
	if data == nil {
		t.Fatal("CollectStats returned nil")
	}
	// Should have at least the installed key
	if _, ok := data["installed"]; !ok {
		t.Error("missing 'installed' key in stats")
	}
}
