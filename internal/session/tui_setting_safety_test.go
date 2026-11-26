package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTUISettingLeavesAFileItCannotRead(t *testing.T) {
	for _, body := range []string{"{\n // mine\n \"model\":\"x\"\n}", `{"model":`, "null"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ensureClaudeTUISetting(dir); err == nil {
			t.Errorf("%q: no error", body)
		}
		if got, _ := os.ReadFile(path); string(got) != body {
			t.Errorf("%q rewritten to %q", body, got)
		}
	}
}
