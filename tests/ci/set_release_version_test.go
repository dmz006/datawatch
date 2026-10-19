package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readWorkingFile returns the current content of path inside the temp repo,
// reading the working tree rather than a committed blob, so uncommitted
// edits made by the script are visible.
func readWorkingFile(t *testing.T, r *mergeRepo, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestSetReleaseVersionWritesBothFiles runs the script with 1.0.1 and checks
// that both version files now read 1.0.1 and that app/core.go, which the
// script must not touch, is unchanged.
func TestSetReleaseVersionWritesBothFiles(t *testing.T) {
	r := newMergeRepo(t, "set-release-version.sh")

	coreBefore := readWorkingFile(t, r, "app/core.go")

	out, err := r.runScript("set-release-version.sh", "1.0.1", "test release")
	if err != nil {
		t.Fatalf("expected script to succeed, got error: %v\n%s", err, out)
	}

	for _, path := range []string{"cmd/datawatch/main.go", "internal/server/api.go"} {
		got := readWorkingFile(t, r, path)
		if !strings.Contains(got, "var Version = \"1.0.1\"\n") {
			t.Fatalf("%s should declare version 1.0.1, got:\n%s", path, got)
		}
	}

	if coreAfter := readWorkingFile(t, r, "app/core.go"); coreAfter != coreBefore {
		t.Fatalf("app/core.go changed; before:\n%s\nafter:\n%s", coreBefore, coreAfter)
	}
}
