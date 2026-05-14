package hookinstaller

import (
	"encoding/json"
	"os"
	"testing"
)

func TestInstallWritesNestedHookFormat(t *testing.T) {
	dir := t.TempDir()
	if err := Install(dir, "test:abc123", "https://localhost:8443", "tok"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dir + "/.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatal("no hooks key")
	}
	for _, event := range []string{"Stop", "PostToolUse", "UserPromptSubmit", "SubagentStop"} {
		arr, ok := hooks[event].([]any)
		if !ok || len(arr) == 0 {
			t.Fatalf("event %s missing", event)
		}
		group, ok := arr[0].(map[string]any)
		if !ok {
			t.Fatalf("event %s: entry not a map", event)
		}
		inner, ok := group["hooks"].([]any)
		if !ok || len(inner) == 0 {
			t.Fatalf("event %s: no inner hooks array (got old flat format)", event)
		}
		cmd, ok := inner[0].(map[string]any)
		if !ok {
			t.Fatalf("event %s: inner[0] not a map", event)
		}
		if cmd["type"] != "command" {
			t.Fatalf("event %s: type != command", event)
		}
		t.Logf("%s: %v", event, cmd["command"])
	}
}

func TestInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	Install(dir, "test:first", "https://localhost:8443", "")
	Install(dir, "test:second", "https://localhost:8443", "") // second call must not duplicate
	data, _ := os.ReadFile(dir + "/.claude/settings.json")
	var doc map[string]any
	json.Unmarshal(data, &doc)
	hooks := doc["hooks"].(map[string]any)
	arr := hooks["Stop"].([]any)
	if len(arr) != 1 {
		t.Fatalf("expected 1 Stop entry after 2 installs, got %d", len(arr))
	}
}

func TestInstallUpgradesOldFlatFormat(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed settings.json with old flat-format hook entry
	_ = os.MkdirAll(dir+"/.claude", 0o755)
	sprintDir := dir + "/.claude/sprint"
	_ = os.MkdirAll(sprintDir, 0o755)
	scriptPath := sprintDir + "/post-event.sh"
	oldFlat := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"type": "command", "command": scriptPath + " Stop"},
			},
		},
	}
	b, _ := json.Marshal(oldFlat)
	os.WriteFile(dir+"/.claude/settings.json", b, 0o644)

	// Install should add nested entry alongside old flat one
	Install(dir, "test:upgrade", "https://localhost:8443", "tok")
	data, _ := os.ReadFile(dir + "/.claude/settings.json")
	var doc map[string]any
	json.Unmarshal(data, &doc)
	hooks := doc["hooks"].(map[string]any)
	arr := hooks["Stop"].([]any)
	// Expect 2: old flat + new nested
	if len(arr) != 2 {
		t.Fatalf("expected 2 Stop entries (old flat + new nested), got %d", len(arr))
	}
	// Second entry must be the nested group
	group, ok := arr[1].(map[string]any)
	if !ok {
		t.Fatal("second entry not a map")
	}
	if _, ok := group["hooks"]; !ok {
		t.Fatal("second entry missing inner hooks array (not nested format)")
	}
}
