package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// removalManifest is a feature pack with one verified task and one read-only
// task. Titles carry the pack name so two packs in one test never collide.
func removalManifest(name, version string) string {
	return fmt.Sprintf(`
name: %s
version: %s
description: pack for removal tests
kinds: [feature]
tasks:
  - title: Review the change for %s
    prompt: prompts/review.md
    verification_script: verify/review.sh
  - title: Summarise the tree for %s
    read_only: true
    report_path: .datawatch/reports/summary.md
`, name, version, name, name)
}

// installedRemovalPack installs a passing pack name@version under root, accepts
// nothing, and returns the pack loaded back from its installed folder.
func installedRemovalPack(t *testing.T, root, name, version string) *Pack {
	t.Helper()
	src := writePackDir(t, removalManifest(name, version), validInstallFiles())
	if err := os.Chmod(filepath.Join(src, "verify", "review.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	inst, err := Install(src, root)
	if err != nil {
		t.Fatalf("Install %s %s: %v", name, version, err)
	}
	p, err := LoadPackFolder(inst.Dir)
	if err != nil {
		t.Fatalf("LoadPackFolder %s: %v", inst.Dir, err)
	}
	return p
}

func TestRemoveDeletesFolderAndTrustRecord(t *testing.T) {
	root := t.TempDir()
	p := installedRemovalPack(t, root, "removable", "1.0.0")

	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(p, "operator", time.Now()); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if err := Remove("removable", "1.0.0", root); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "removable", "1.0.0")); !os.IsNotExist(err) {
		t.Errorf("installed folder still present after Remove (stat err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "removable")); !os.IsNotExist(err) {
		t.Errorf("empty name folder still present after Remove (stat err %v)", err)
	}
	reloaded, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Record("removable", "1.0.0"); ok {
		t.Error("trust record still present after Remove")
	}
}

func TestRemoveKeepsOtherVersionsAndRecords(t *testing.T) {
	root := t.TempDir()
	v1 := installedRemovalPack(t, root, "shared", "1.0.0")
	v2 := installedRemovalPack(t, root, "shared", "2.0.0")
	other := installedRemovalPack(t, root, "other", "1.0.0")

	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, p := range []*Pack{v1, v2, other} {
		if _, err := store.Accept(p, "operator", now); err != nil {
			t.Fatalf("Accept %s %s: %v", p.Name, p.Version, err)
		}
	}

	if err := Remove("shared", "1.0.0", root); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "shared", "2.0.0", "pack.yaml")); err != nil {
		t.Errorf("sibling version 2.0.0 was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "other", "1.0.0", "pack.yaml")); err != nil {
		t.Errorf("unrelated pack was touched: %v", err)
	}
	reloaded, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Record("shared", "1.0.0"); ok {
		t.Error("removed version's trust record still present")
	}
	for _, key := range [][2]string{{"shared", "2.0.0"}, {"other", "1.0.0"}} {
		if _, ok := reloaded.Record(key[0], key[1]); !ok {
			t.Errorf("trust record for %s %s was dropped", key[0], key[1])
		}
	}
	if err := reloaded.Runnable(v2); err != nil {
		t.Errorf("sibling version no longer runnable: %v", err)
	}
}

func TestRemoveWithoutTrustRecordWritesNoTrustFile(t *testing.T) {
	root := t.TempDir()
	installedRemovalPack(t, root, "unaccepted", "1.0.0")

	if err := Remove("unaccepted", "1.0.0", root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, trustFileName)); !os.IsNotExist(err) {
		t.Errorf("trust.json created by Remove of an unaccepted pack (stat err %v)", err)
	}
}

func TestRemoveRefusesNotInstalled(t *testing.T) {
	root := t.TempDir()
	if err := Remove("ghost", "1.0.0", root); err == nil {
		t.Fatal("Remove of a pack that is not installed: want error, got nil")
	}
}

func TestRemoveRefusesUnsafeNames(t *testing.T) {
	root := t.TempDir()
	// A sibling folder outside the install root must survive every refused call.
	outside := filepath.Join(filepath.Dir(root), "outside-pack-"+filepath.Base(root))
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })

	cases := []struct{ name, version string }{
		{"", "1.0.0"},
		{"pack", ""},
		{".", "1.0.0"},
		{"..", "1.0.0"},
		{"pack/x", "1.0.0"},
		{"pack", ".."},
		{"pack", "1.0/0"},
	}
	for _, c := range cases {
		if err := Remove(c.name, c.version, root); err == nil {
			t.Errorf("Remove(%q, %q): want error, got nil", c.name, c.version)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("folder outside the install root was touched: %v", err)
	}
}

// TestRemoveKeepsAutomatonTaskListUnchanged pins the rule that removing a pack
// never changes an Automaton's resolved task list. The Automaton resolves its
// tasks from the packs it was created with, held in memory, so the list it
// resolves before removal must match the list it resolves after.
func TestRemoveKeepsAutomatonTaskListUnchanged(t *testing.T) {
	root := t.TempDir()
	alpha := installedRemovalPack(t, root, "alpha", "1.0.0")
	beta := installedRemovalPack(t, root, "beta", "1.0.0")
	active := []*Pack{alpha, beta}

	before, err := ResolveTaskList(KindFeature, active)
	if err != nil {
		t.Fatalf("ResolveTaskList before removal: %v", err)
	}
	if len(before) != 4 {
		t.Fatalf("resolved %d tasks before removal, want 4", len(before))
	}

	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(alpha, "operator", time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := Remove("alpha", "1.0.0", root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(alpha.Dir); !os.IsNotExist(err) {
		t.Fatalf("alpha folder still on disk after Remove (stat err %v)", err)
	}

	after, err := ResolveTaskList(KindFeature, active)
	if err != nil {
		t.Fatalf("ResolveTaskList after removal: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("resolved task list changed by Remove\nbefore: %v\nafter:  %v",
			titlesOf(before), titlesOf(after))
	}
}

func TestRemoveRefusesEntryThatIsNotAFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray", "1.0.0"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove("stray", "1.0.0", root); err == nil {
		t.Fatal("Remove of a plain file in place of a version folder: want error, got nil")
	}
	if _, err := os.Stat(filepath.Join(root, "stray", "1.0.0")); err != nil {
		t.Errorf("refused entry was deleted: %v", err)
	}
}

func TestRemoveRefusesDamagedTrustStore(t *testing.T) {
	root := t.TempDir()
	installedRemovalPack(t, root, "damaged", "1.0.0")
	if err := os.WriteFile(filepath.Join(root, trustFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove("damaged", "1.0.0", root); err == nil {
		t.Fatal("Remove with a damaged trust.json: want error, got nil")
	}
	if _, err := os.Stat(filepath.Join(root, "damaged", "1.0.0", "pack.yaml")); err != nil {
		t.Errorf("pack folder deleted although trust store could not be read: %v", err)
	}
}
