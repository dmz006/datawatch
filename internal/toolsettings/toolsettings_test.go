package toolsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The JSON reader replaces bytes that are not UTF-8, so writing such a file
// back would change it.
func TestReadRefusesBytesItWouldChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{\"name\":\"caf\xe9\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(path); err == nil || !strings.Contains(err.Error(), "left as it is") {
		t.Errorf("err = %v, want one saying the file is left as it is", err)
	}
}
