package supervisorloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var bigHandover = strings.Repeat("x", MaxDeliveredPromptChars+10)

// A schedule name carries a request field. One that climbs out of the
// supervisor folder must not make the daemon write a file somewhere else.
func TestDeliverablePromptRefusesNameThatClimbsOut(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "a", "b", "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"supervisor-p-x/../../../../escaped",
		"../../../escaped",
		"a/../../../../escaped",
		"..",
		"",
		"a\x00b",
	} {
		if _, err := DeliverablePrompt(project, name, bigHandover); err == nil {
			t.Errorf("name %q: want an error", name)
		}
	}
	var found []string
	_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	if len(found) != 0 {
		t.Fatalf("files were written: %v", found)
	}
}

// A repository can carry .datawatch/supervisor as a link to somewhere else.
// The handover must not be written through it.
func TestDeliverablePromptRefusesLinkOutOfProject(t *testing.T) {
	for _, linked := range []string{".datawatch", filepath.Join(".datawatch", "supervisor")} {
		project, outside := t.TempDir(), t.TempDir()
		link := filepath.Join(project, linked)
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("cannot create symlinks: %v", err)
		}
		if _, err := DeliverablePrompt(project, "watch", bigHandover); err == nil {
			t.Errorf("%s linked out: want an error", linked)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("%s linked out: wrote outside the project: %v", linked, entries)
		}
	}
}

// The handover file keeps its place, text and private mode, and a project
// name with a slash (project names are not restricted) still works.
func TestDeliverablePromptKeepsPlaceAndMode(t *testing.T) {
	for _, name := range []string{"supervisor-alpha-watch", "supervisor-org/repo-watch", "supervisor-a..b-night pass"} {
		project := t.TempDir()
		prompt, err := DeliverablePrompt(project, name, bigHandover)
		if err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
		path := HandoverFilePath(project, name)
		if !strings.Contains(prompt, path) {
			t.Errorf("name %q: prompt does not name %s", name, path)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != bigHandover {
			t.Fatalf("name %q: read %s: %v", name, path, err)
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("name %q: mode %v %v", name, st.Mode().Perm(), err)
		}
	}
}
