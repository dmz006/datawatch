package platformcontext

import (
	"os"
	"testing"
)

func TestDeliveryModesCoverEveryBackendDirectory(t *testing.T) {
	entries, err := os.ReadDir("../llm/backends")
	if err != nil {
		t.Fatal(err)
	}
	modes := DeliveryModes()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := modes[e.Name()]; !ok {
			t.Errorf("backend %q has no delivery mode", e.Name())
		}
	}
	for _, fam := range []string{"claude-code", "pi-prompt", "opencode-acp", "opencode-prompt", "goose-prompt", "subprocess"} {
		if _, ok := modes[fam]; !ok {
			t.Errorf("family %q has no delivery mode", fam)
		}
	}
	if modes["claude-code"] != DeliveryRulesFile {
		t.Errorf("claude-code = %q, want rules_file", modes["claude-code"])
	}
	for name, m := range modes {
		switch m {
		case DeliveryRulesFile, DeliveryFirstPrompt, DeliveryRulesFileAndFirstPrompt:
		default:
			t.Errorf("%s has invalid mode %q", name, m)
		}
	}
}
