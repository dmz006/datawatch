package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The PWA story scripts talk to a sandbox listener with a self-signed certificate.
// They must scope that exemption to a dedicated https.Agent rather than disabling
// certificate verification for the whole Node process.
func TestStoryScriptsDoNotDisableTLSVerificationGlobally(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "scripts", "test-stories", "pwa", "*.mjs"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no story scripts found: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, "NODE_TLS_REJECT_UNAUTHORIZED") {
				t.Errorf("%s:%d disables TLS verification process-wide", f, i+1)
			}
		}
	}
	for _, name := range []string{"TS-821.mjs", "TS-824.mjs"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test-stories", "pwa", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "new https.Agent({ rejectUnauthorized: false })") {
			t.Errorf("%s lost its scoped sandbox agent", name)
		}
	}
}
