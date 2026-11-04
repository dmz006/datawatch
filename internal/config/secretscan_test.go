package config

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEffectiveSecretScanMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", GitSecretScanShadow},  // unset reads shadow (shadow-first rollout)
		{"off", GitSecretScanOff},
		{"shadow", GitSecretScanShadow},
		{"enforce", GitSecretScanEnforce},
		{"block", GitSecretScanShadow}, // unknown falls back to shadow
		{"ENFORCE", GitSecretScanShadow},
	}
	for _, tc := range cases {
		g := GitConfig{SecretScan: GitSecretScanConfig{Mode: tc.in}}
		if got := g.EffectiveSecretScanMode(); got != tc.want {
			t.Errorf("EffectiveSecretScanMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateGitSecretScanMode(t *testing.T) {
	for _, ok := range []string{"", "off", "shadow", "enforce"} {
		if err := ValidateGitSecretScanMode(ok); err != nil {
			t.Errorf("ValidateGitSecretScanMode(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"on", "block", "yes"} {
		if err := ValidateGitSecretScanMode(bad); err == nil {
			t.Errorf("ValidateGitSecretScanMode(%q) = nil, want error", bad)
		}
	}
}

func TestSecretScanRefusesPush(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", false},        // shadow default: never refuses
		{"off", false},
		{"shadow", false},
		{"enforce", true},
		{"bogus", false},   // falls back to shadow
	}
	for _, tc := range cases {
		g := GitConfig{SecretScan: GitSecretScanConfig{Mode: tc.mode}}
		if got := g.SecretScanRefusesPush(); got != tc.want {
			t.Errorf("SecretScanRefusesPush(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestEffectiveSecretScanDetectors(t *testing.T) {
	g := GitConfig{}
	if got, want := g.EffectiveSecretScanDetectors(), GitSecretScanDefaultDetectors; !reflect.DeepEqual(got, want) {
		t.Errorf("unset detectors = %v, want %v", got, want)
	}
	// The default must be a copy: mutating the result must not change
	// the package variable.
	got := g.EffectiveSecretScanDetectors()
	got[0] = "mutated"
	if GitSecretScanDefaultDetectors[0] == "mutated" {
		t.Error("EffectiveSecretScanDetectors returned the backing slice, not a copy")
	}

	custom := GitConfig{SecretScan: GitSecretScanConfig{Detectors: []string{"github_token", "slack_token"}}}
	if got, want := custom.EffectiveSecretScanDetectors(), []string{"github_token", "slack_token"}; !reflect.DeepEqual(got, want) {
		t.Errorf("configured detectors = %v, want %v", got, want)
	}
}

func TestEffectiveSecretScanAllowList(t *testing.T) {
	g := GitConfig{}
	al := g.EffectiveSecretScanAllowList()
	if al.Paths == nil || al.Fingerprints == nil {
		t.Fatalf("unset allow-list should read as empty slices, got %#v", al)
	}
	if len(al.Paths) != 0 || len(al.Fingerprints) != 0 {
		t.Fatalf("unset allow-list should be empty, got %#v", al)
	}

	custom := GitConfig{SecretScan: GitSecretScanConfig{AllowList: GitSecretAllowList{
		Paths:        []string{"vendor/*"},
		Fingerprints: []string{"0123456789abcdef"},
	}}}
	al = custom.EffectiveSecretScanAllowList()
	if len(al.Paths) != 1 || al.Paths[0] != "vendor/*" {
		t.Errorf("paths = %v", al.Paths)
	}
	if len(al.Fingerprints) != 1 || al.Fingerprints[0] != "0123456789abcdef" {
		t.Errorf("fingerprints = %v", al.Fingerprints)
	}
}

func TestSecretScanBlockYAMLRoundTrip(t *testing.T) {
	const yamlText = `
git:
  push_on_complete: branch
  push_remote: origin
  secret_scan:
    mode: enforce
    detectors:
      - private_key
      - github_token
    allow_list:
      paths:
        - vendor/*
      fingerprints:
        - 0123456789abcdef
`
	var c Config
	if err := yaml.Unmarshal([]byte(yamlText), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := c.Git.EffectiveSecretScanMode(); got != GitSecretScanEnforce {
		t.Errorf("mode = %q, want enforce", got)
	}
	if got := c.Git.EffectiveSecretScanDetectors(); len(got) != 2 || got[0] != "private_key" || got[1] != "github_token" {
		t.Errorf("detectors = %v", got)
	}
	al := c.Git.EffectiveSecretScanAllowList()
	if len(al.Paths) != 1 || al.Paths[0] != "vendor/*" {
		t.Errorf("paths = %v", al.Paths)
	}
	if len(al.Fingerprints) != 1 || al.Fingerprints[0] != "0123456789abcdef" {
		t.Errorf("fingerprints = %v", al.Fingerprints)
	}
}

func TestValidateGitSecretScanDetectors(t *testing.T) {
	if err := ValidateGitSecretScanDetectors(nil); err != nil {
		t.Fatalf("empty list: %v", err)
	}
	if err := ValidateGitSecretScanDetectors([]string{"private_key", " aws_access_key ", ""}); err != nil {
		t.Fatalf("known names: %v", err)
	}
	if err := ValidateGitSecretScanDetectors([]string{"private_key", "privat_key"}); err == nil {
		t.Fatal("a misspelt detector name must be rejected")
	}
}
