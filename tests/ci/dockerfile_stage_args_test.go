package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A build argument declared before the first FROM is empty inside every stage
// unless the stage declares it again. A Dockerfile that uses ${DW_USER} must
// therefore declare ARG DW_USER after its last FROM.
func TestDockerfilesRedeclareDWUserInTheFinalStage(t *testing.T) {
	files, err := filepath.Glob("../../docker/dockerfiles/Dockerfile.*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no Dockerfiles found: %v", err)
	}
	from := regexp.MustCompile(`(?m)^FROM\s`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		locs := from.FindAllStringIndex(src, -1)
		if len(locs) == 0 || !strings.Contains(src, "${DW_USER}") {
			continue
		}
		last := src[locs[len(locs)-1][0]:]
		if !regexp.MustCompile(`(?m)^ARG DW_USER(=|\s*$)`).MatchString(last) {
			t.Errorf("%s uses ${DW_USER} but does not declare ARG DW_USER after its last FROM, so it is empty in the final stage", filepath.Base(f))
		}
	}
}
