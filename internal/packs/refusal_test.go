package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const refusalTrustManifest = `
name: refusal-trust-pack
version: 1.0.0
description: Third-party pack used to check trust refusal
tasks:
  - title: Check the tree
    verification_script: verify/check.sh
`

const refusalCommandManifest = `
name: refusal-command-pack
version: 1.0.0
description: Pack whose verification_script is a command string
tasks:
  - title: Run a command
    verification_script: "go test ./... && echo ok"
`

const refusalRuleManifest = `
name: refusal-rule-pack
version: 1.0.0
description: Pack with a task that breaks a rule
tasks:
  - title: Good task
    verification_script: verify/check.sh
  - title: Missing script
    prompt: prompts/step.md
`

// An untrusted third-party pack is refused until a trust record exists.
func TestRefusalUntrustedThirdPartyPackNeedsTrustRecord(t *testing.T) {
	root := t.TempDir()
	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatalf("LoadTrustStore: %v", err)
	}

	dir := writePackDir(t, refusalTrustManifest, map[string]string{"verify/check.sh": shellScript})
	pack, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: %v", err)
	}
	pack.Source = "third-party-registry"

	// No record yet: refused with the operator-facing message.
	err = store.Runnable(pack)
	if err == nil {
		t.Fatal("Runnable before accept: want refusal, got nil")
	}
	if !strings.Contains(err.Error(), "pack not accepted") {
		t.Errorf("Runnable error = %q, want it to say the pack is not accepted", err)
	}

	// Accept records the pack at its digest; the same store now allows it.
	if _, err := store.Accept(pack, "operator", time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := store.Runnable(pack); err != nil {
		t.Errorf("Runnable after accept: %v", err)
	}

	// A store reloaded from disk sees the record too.
	reloaded, err := LoadTrustStore(root)
	if err != nil {
		t.Fatalf("reload trust store: %v", err)
	}
	if err := reloaded.Runnable(pack); err != nil {
		t.Errorf("Runnable after reload: %v", err)
	}

	// Changing a pack file changes the digest, so the old record no longer
	// trusts the pack.
	if err := os.WriteFile(filepath.Join(dir, "verify", "check.sh"), []byte(shellScript+"# changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder changed: %v", err)
	}
	changed.Source = pack.Source
	if err := reloaded.Runnable(changed); err == nil {
		t.Error("Runnable of changed pack: want refusal, got nil")
	}
}

// A pack task whose verification_script is a command string is refused by
// CheckTaskRules.
func TestRefusalCommandStringVerificationScript(t *testing.T) {
	dir := writePackDir(t, refusalCommandManifest, map[string]string{"verify/check.sh": shellScript})
	pack, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: %v", err)
	}
	if len(pack.Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(pack.Tasks))
	}

	err = CheckTaskRules(pack, pack.Tasks[0])
	if err == nil {
		t.Fatal("CheckTaskRules with a command-string verification_script: want refusal, got nil")
	}
	for _, want := range []string{"verification_script must be a script file", "Run a command"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}

	// A plain script path on the same pack passes, so the refusal is about
	// the command string and not the pack.
	ok := pack.Tasks[0]
	ok.VerificationScript = "verify/check.sh"
	if err := CheckTaskRules(pack, ok); err != nil {
		t.Errorf("CheckTaskRules with a script path: %v", err)
	}
}

// A pack that breaks a rule is refused at Install with the rule named, and
// nothing is installed.
func TestRefusalRuleBreakingPackRefusedAtInstall(t *testing.T) {
	src := writePackDir(t, refusalRuleManifest, map[string]string{
		"prompts/step.md": "Do the step.\n",
		"verify/check.sh": shellScript,
	})
	destRoot := t.TempDir()

	installed, err := Install(src, destRoot)
	if err == nil {
		t.Fatalf("Install of a rule-breaking pack: want refusal, got %+v", installed)
	}
	if installed != nil {
		t.Errorf("Install returned a pack %+v alongside the refusal", installed)
	}
	for _, want := range []string{"pack rule", "must name a verification_script", "Missing script"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}

	entries, rerr := os.ReadDir(destRoot)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		t.Errorf("refused install left %d entries in the install root", len(entries))
	}
}
