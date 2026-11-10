package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillFolder creates dir (relative to base) holding a SKILL.md, and
// a pack.yaml too when asPack is true.
func writeSkillFolder(t *testing.T, base, rel string, asPack bool) {
	t.Helper()
	dir := filepath.Join(base, rel)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+filepath.Base(rel)+"\n---\nbody"), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if asPack {
		if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte("name: "+filepath.Base(rel)+"\n"), 0644); err != nil {
			t.Fatalf("write pack.yaml: %v", err)
		}
	}
}

func TestClassifySkillDir_PackYamlMakesPack(t *testing.T) {
	base := t.TempDir()
	writeSkillFolder(t, base, "packs/bundle", true)
	writeSkillFolder(t, base, "skills/plain", false)

	if got := ClassifySkillDir(filepath.Join(base, "packs/bundle")); got != SkillTypePack {
		t.Errorf("folder with pack.yaml: got %q, want %q", got, SkillTypePack)
	}
	if got := ClassifySkillDir(filepath.Join(base, "skills/plain")); got != SkillTypeSkill {
		t.Errorf("folder without pack.yaml: got %q, want %q", got, SkillTypeSkill)
	}
}

func TestClassifySkillDir_PackYamlDirectoryIsNotPack(t *testing.T) {
	base := t.TempDir()
	// A directory named pack.yaml is not a pack manifest.
	if err := os.MkdirAll(filepath.Join(base, "odd", "pack.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := ClassifySkillDir(filepath.Join(base, "odd")); got != SkillTypeSkill {
		t.Errorf("pack.yaml directory: got %q, want %q", got, SkillTypeSkill)
	}
}

func TestClassifySkillDir_MissingDirIsSkill(t *testing.T) {
	if got := ClassifySkillDir(filepath.Join(t.TempDir(), "absent")); got != SkillTypeSkill {
		t.Errorf("missing dir: got %q, want %q", got, SkillTypeSkill)
	}
}

func TestSkillType_JSONTagIsType(t *testing.T) {
	b, err := json.Marshal(Synced{Registry: "r", Name: "n", Type: SkillTypePack})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"type":"pack"`) {
		t.Errorf("Synced JSON missing type field: %s", b)
	}
	b, err = json.Marshal(AvailableSkill{Registry: "r", Name: "n", Type: SkillTypeSkill})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"type":"skill"`) {
		t.Errorf("AvailableSkill JSON missing type field: %s", b)
	}
}

// Store files written before the type field existed must load with every
// entry typed "skill".
func TestStoreLoad_OldFileWithoutTypeDefaultsToSkill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.json")
	old := `{
  "registries": [{"name":"r","kind":"git","url":"https://example.com/x.git","enabled":true}],
  "synced": [{"registry":"r","name":"alpha","path":"/p/alpha"}],
  "available_cache": {
    "r": [{"registry":"r","name":"beta","path":"skills/beta"}]
  }
}`
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sk, ok := s.GetSynced("r", "alpha")
	if !ok {
		t.Fatal("synced alpha not loaded")
	}
	if sk.Type != SkillTypeSkill {
		t.Errorf("synced Type = %q, want %q", sk.Type, SkillTypeSkill)
	}
	avail := s.Available("r")
	if len(avail) != 1 {
		t.Fatalf("available len = %d, want 1", len(avail))
	}
	if avail[0].Type != SkillTypeSkill {
		t.Errorf("available Type = %q, want %q", avail[0].Type, SkillTypeSkill)
	}
}

// Browse on a cached registry re-classifies each entry from its folder,
// so a pack is reported as a pack even when the cache predates the field.
func TestManagerBrowse_SetsTypeFromFolder(t *testing.T) {
	registryName := "packs-registry"
	m := newTestManagerWithRegistry(t, registryName, "skills/x")
	clone := filepath.Join(m.Git.CacheDir, registryName)
	writeSkillFolder(t, clone, "bundles/starter", true)
	writeSkillFolder(t, clone, "skills/single", false)

	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "starter", Path: "bundles/starter"},
		{Registry: registryName, Name: "single", Path: "skills/single"},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	got, err := m.Browse(registryName)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	types := map[string]string{}
	for _, a := range got {
		types[a.Name] = a.Type
	}
	if types["starter"] != SkillTypePack {
		t.Errorf("starter Type = %q, want %q", types["starter"], SkillTypePack)
	}
	if types["single"] != SkillTypeSkill {
		t.Errorf("single Type = %q, want %q", types["single"], SkillTypeSkill)
	}
}

// Sync records the type of each copied entry on its Synced record.
func TestManagerSync_SetsType(t *testing.T) {
	registryName := "sync-types"
	m := newTestManagerWithRegistry(t, registryName, "skills/x")
	clone := filepath.Join(m.Git.CacheDir, registryName)
	writeSkillFolder(t, clone, "bundles/starter", true)
	writeSkillFolder(t, clone, "skills/single", false)

	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "starter", Path: "bundles/starter"},
		{Registry: registryName, Name: "single", Path: "skills/single"},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	out, err := m.Sync(registryName, []string{"*"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("Sync returned %d entries, want 2", len(out))
	}
	for _, sk := range out {
		want := SkillTypeSkill
		if sk.Name == "starter" {
			want = SkillTypePack
		}
		if sk.Type != want {
			t.Errorf("synced %s Type = %q, want %q", sk.Name, sk.Type, want)
		}
		stored, ok := m.Store.GetSynced(registryName, sk.Name)
		if !ok {
			t.Fatalf("synced %s not persisted", sk.Name)
		}
		if stored.Type != want {
			t.Errorf("persisted %s Type = %q, want %q", sk.Name, stored.Type, want)
		}
	}
}
