// SEC-022 — Manifest.Verify ("verification command, run after sync") was
// declared in the manifest format but never actually executed anywhere.
// These tests pin runSkillVerify directly and its wiring into Sync.

package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunSkillVerify_EmptyCommandPasses(t *testing.T) {
	ok, reason := runSkillVerify("", t.TempDir())
	if !ok || reason != "" {
		t.Fatalf("empty verify command must trivially pass, got ok=%v reason=%q", ok, reason)
	}
}

func TestRunSkillVerify_PassingCommand(t *testing.T) {
	ok, reason := runSkillVerify("exit 0", t.TempDir())
	if !ok || reason != "" {
		t.Fatalf("exit 0 must pass, got ok=%v reason=%q", ok, reason)
	}
}

func TestRunSkillVerify_FailingCommandReportsReason(t *testing.T) {
	ok, reason := runSkillVerify("echo 'missing dependency' >&2; exit 1", t.TempDir())
	if ok {
		t.Fatal("a command exiting non-zero must fail verification")
	}
	if reason == "" {
		t.Error("expected a non-empty failure reason")
	}
}

func TestRunSkillVerify_RunsInSkillDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	ok, reason := runSkillVerify("test -f marker.txt", dir)
	if !ok {
		t.Fatalf("verify command should run with cwd set to the skill directory, got reason=%q", reason)
	}
}

func syncTestManagerWithManifest(t *testing.T, registryName, skillPath, verify string) (*Manager, string) {
	t.Helper()
	dataDir := t.TempDir()
	m, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	reg := &Registry{Name: registryName, Kind: "git", URL: "https://example.com/fake.git", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := m.Store.CreateRegistry(reg); err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}
	cloneDir := filepath.Join(dataDir, ".skills-cache", registryName, skillPath)
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		t.Fatalf("mkdir fake clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "SKILL.md"), []byte("---\nname: x\n---\nbody"), 0644); err != nil {
		t.Fatalf("write fake SKILL.md: %v", err)
	}
	return m, dataDir
}

func TestSync_PassingVerifyMarksRecordVerified(t *testing.T) {
	registryName := "verify-pass-registry"
	m, _ := syncTestManagerWithManifest(t, registryName, "skills/good", "")
	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "good-skill", Path: "skills/good", Manifest: &Manifest{Name: "good-skill", Verify: "exit 0"}},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	synced, err := m.Sync(registryName, []string{"*"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(synced) != 1 {
		t.Fatalf("expected 1 synced skill, got %d", len(synced))
	}
	if !synced[0].Verified {
		t.Errorf("expected Verified=true for a passing verify command, got VerifyError=%q", synced[0].VerifyError)
	}
}

func TestSync_FailingVerifyMarksRecordUnverifiedButStillSyncs(t *testing.T) {
	registryName := "verify-fail-registry"
	m, _ := syncTestManagerWithManifest(t, registryName, "skills/bad", "")
	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "bad-skill", Path: "skills/bad", Manifest: &Manifest{Name: "bad-skill", Verify: "exit 1"}},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	synced, err := m.Sync(registryName, []string{"*"})
	if err != nil {
		t.Fatalf("Sync must not itself error on a failed verify (no interactive operator to gate here): %v", err)
	}
	if len(synced) != 1 {
		t.Fatalf("expected 1 synced skill, got %d", len(synced))
	}
	if synced[0].Verified {
		t.Error("expected Verified=false for a failing verify command")
	}
	if synced[0].VerifyError == "" {
		t.Error("expected a non-empty VerifyError")
	}
	// The skill is still physically on disk -- Sync's job is to report, not
	// gate; the trust_unverified rollback decision lives at the REST layer.
	if _, err := os.Stat(filepath.Join(synced[0].Path, "SKILL.md")); err != nil {
		t.Fatalf("expected the skill to still be copied to disk: %v", err)
	}
}

func TestSync_NoVerifyCommandIsTriviallyVerified(t *testing.T) {
	registryName := "no-verify-registry"
	m, _ := syncTestManagerWithManifest(t, registryName, "skills/plain", "")
	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "plain-skill", Path: "skills/plain", Manifest: &Manifest{Name: "plain-skill"}},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	synced, err := m.Sync(registryName, []string{"*"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !synced[0].Verified {
		t.Error("a skill that declares no verify command must not be newly blocked by its absence")
	}
}
