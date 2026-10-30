// identity.backups_to_keep config key: default, effective helper, validator,
// load-time validation and YAML/JSON round trip.

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIdentityBackupsToKeep_DefaultWhenAbsent(t *testing.T) {
	for name, c := range map[string]IdentityConfig{
		"zero value":     {},
		"default config": DefaultConfig().Identity,
	} {
		if got := c.EffectiveBackupsToKeep(); got != 10 {
			t.Errorf("%s: backups_to_keep = %d, want 10", name, got)
		}
		if c.BackupsToKeep != nil {
			t.Errorf("%s: stored value must be absent (nil), got %v", name, *c.BackupsToKeep)
		}
	}
	if DefaultIdentityBackupsToKeep != 10 {
		t.Errorf("DefaultIdentityBackupsToKeep = %d, want 10", DefaultIdentityBackupsToKeep)
	}
}

func TestIdentityBackupsToKeep_EffectiveReadsExplicit(t *testing.T) {
	for _, n := range []int{1, 3, 10, 50} {
		n := n
		if got := (IdentityConfig{BackupsToKeep: &n}).EffectiveBackupsToKeep(); got != n {
			t.Errorf("explicit %d read back as %d", n, got)
		}
	}
}

func TestIdentityBackupsToKeep_Validator(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		err := ValidateIdentityBackupsToKeep(n)
		if err == nil {
			t.Errorf("%d must be refused", n)
			continue
		}
		for _, frag := range []string{"identity.backups_to_keep", ">= 1", "default 10"} {
			if !strings.Contains(err.Error(), frag) {
				t.Errorf("%d: message %q lacks %q", n, err, frag)
			}
		}
	}
	for _, n := range []int{1, 10, 500} {
		if err := ValidateIdentityBackupsToKeep(n); err != nil {
			t.Errorf("%d must be accepted, got %v", n, err)
		}
	}
}

func TestIdentityBackupsToKeep_RoundTrip(t *testing.T) {
	n := 4
	in := IdentityConfig{BackupsToKeep: &n}
	y, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML IdentityConfig
	if err := yaml.Unmarshal(y, &fromYAML); err != nil || fromYAML.EffectiveBackupsToKeep() != 4 {
		t.Errorf("yaml round trip: %v %+v", err, fromYAML)
	}
	j, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON IdentityConfig
	if err := json.Unmarshal(j, &fromJSON); err != nil || fromJSON.EffectiveBackupsToKeep() != 4 {
		t.Errorf("json round trip: %v %+v", err, fromJSON)
	}
	// An absent key stays absent.
	var empty IdentityConfig
	if err := yaml.Unmarshal([]byte("{}"), &empty); err != nil || empty.BackupsToKeep != nil {
		t.Errorf("absent key must stay nil: %v %+v", err, empty)
	}
}

func TestIdentityBackupsToKeep_LoadFromFile(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := Load(write("identity:\n  backups_to_keep: 3\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Identity.EffectiveBackupsToKeep(); got != 3 {
		t.Errorf("loaded backups_to_keep = %d, want 3", got)
	}
	for _, bad := range []string{"0", "-2"} {
		_, err := Load(write("identity:\n  backups_to_keep: " + bad + "\n"))
		if err == nil || !strings.Contains(err.Error(), "identity.backups_to_keep") {
			t.Errorf("Load with %s: want a refusal naming the key, got %v", bad, err)
		}
	}
}

// The annotated template's "Identity" heading is a comment banner about
// machine identification; it must not turn into an `identity:` key.
func TestIdentityBackupsToKeep_TemplateBannerIsNotAKey(t *testing.T) {
	out := GenerateAnnotatedConfig(DefaultConfig())
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "identity:") {
			t.Errorf("template emits an identity key line: %q", line)
		}
	}
}
