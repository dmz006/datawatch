package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package's tests run commit-guard reaping, which walks the temp
// directory. TestMain must therefore give the test process its own.
func TestSessionTestsRunInPrivateTempDir(t *testing.T) {
	if base := filepath.Base(os.TempDir()); !strings.HasPrefix(base, "dw-test-tmp-") {
		t.Fatalf("session tests share the temp directory %q", os.TempDir())
	}
}
